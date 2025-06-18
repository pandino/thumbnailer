package server

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/pandino/movie-thumbnailer-go/internal/config"
	"github.com/pandino/movie-thumbnailer-go/internal/database"
	"github.com/pandino/movie-thumbnailer-go/internal/metrics"
	"github.com/pandino/movie-thumbnailer-go/internal/scanner"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"
)

// VersionInfo holds application version information
type VersionInfo struct {
	Version   string
	Commit    string
	BuildDate string
}

// Server handles HTTP requests for the application
type Server struct {
	cfg     *config.Config
	db      *database.DB
	scanner *scanner.Scanner
	log     *logrus.Logger
	server  *http.Server
	router  *mux.Router
	appCtx  context.Context
	version *VersionInfo
	metrics *metrics.Metrics
}

// New creates a new Server
func New(cfg *config.Config, db *database.DB, scanner *scanner.Scanner, log *logrus.Logger, appCtx context.Context, version *VersionInfo) *Server {
	s := &Server{
		cfg:     cfg,
		db:      db,
		scanner: scanner,
		log:     log,
		router:  mux.NewRouter(),
		appCtx:  appCtx,
		version: version,
		metrics: metrics.New(),
	}

	// Initialize routes
	s.routes()

	// Configure HTTP server
	s.server = &http.Server{
		Addr:         fmt.Sprintf("%s:%s", cfg.ServerHost, cfg.ServerPort),
		Handler:      s.router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	return s
}

// Start begins the HTTP server
func (s *Server) Start() error {
	s.log.Infof("Starting server on %s:%s", s.cfg.ServerHost, s.cfg.ServerPort)
	return s.server.ListenAndServe()
}

// Shutdown gracefully stops the HTTP server
func (s *Server) Shutdown(ctx context.Context) error {
	s.log.Info("Shutting down server")
	return s.server.Shutdown(ctx)
}

// routes initializes the HTTP routes
func (s *Server) routes() {
	// Middleware
	s.router.Use(s.loggingMiddleware)
	s.router.Use(s.recoveryMiddleware)

	// Static files
	fs := http.FileServer(http.Dir(s.cfg.StaticDir))
	s.router.PathPrefix("/static/").Handler(http.StripPrefix("/static/", fs))

	// Thumbnails
	tfs := http.FileServer(http.Dir(s.cfg.ThumbnailsDir))
	s.router.PathPrefix("/thumbnails/").Handler(http.StripPrefix("/thumbnails/", tfs))

	// Control page routes
	s.router.HandleFunc("/", s.handleControlPage).Methods("GET")
	s.router.HandleFunc("/scan", s.handleScan).Methods("POST")
	s.router.HandleFunc("/cleanup", s.handleCleanup).Methods("POST")
	s.router.HandleFunc("/reset-views", s.handleResetViews).Methods("POST")
	s.router.HandleFunc("/process-deletions", s.handleProcessDeletions).Methods("POST")
	s.router.HandleFunc("/undo-delete", s.handleUndoDelete).Methods("POST")

	// Slideshow routes
	s.router.HandleFunc("/slideshow", s.handleSlideshow).Methods("GET")
	s.router.HandleFunc("/slideshow/next", s.handleSlideshowNext).Methods("GET")
	s.router.HandleFunc("/slideshow/previous", s.handleSlideshowPrevious).Methods("GET")
	s.router.HandleFunc("/slideshow/mark-viewed", s.handleMarkViewed).Methods("POST")
	s.router.HandleFunc("/slideshow/delete", s.handleDelete).Methods("POST")
	s.router.HandleFunc("/slideshow/finish", s.handleSlideshowFinish).Methods("GET")
	s.router.HandleFunc("/slideshow/delete-and-finish", s.handleDeleteAndFinish).Methods("POST")

	// API routes
	s.router.HandleFunc("/api/stats", s.handleStats).Methods("GET")
	s.router.HandleFunc("/api/thumbnails", s.handleThumbnails).Methods("GET")
	s.router.HandleFunc("/api/thumbnails/{id}", s.handleThumbnail).Methods("GET")
	s.router.HandleFunc("/api/slideshow/next-image", s.handleSlideshowNextImage).Methods("GET")

	// Metrics endpoint
	s.router.Handle("/metrics", promhttp.Handler()).Methods("GET")

	// 404 handler
	s.router.NotFoundHandler = http.HandlerFunc(s.handleNotFound)
}

// loggingMiddleware logs HTTP requests and records metrics
func (s *Server) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Wrap the response writer to capture status code
		ww := NewWrappedResponseWriter(w)

		// Call the next handler
		next.ServeHTTP(ww, r)

		// Calculate duration
		duration := time.Since(start)

		// Get the matched route for better endpoint grouping
		var route *mux.Route
		var endpoint string
		if route = mux.CurrentRoute(r); route != nil {
			if template, err := route.GetPathTemplate(); err == nil {
				endpoint = template
			} else {
				endpoint = r.URL.Path
			}
		} else {
			endpoint = r.URL.Path
		}

		// Record metrics
		s.metrics.RecordHTTPRequest(r.Method, endpoint, fmt.Sprintf("%d", ww.Status()), duration)

		// Log the request
		s.log.WithFields(logrus.Fields{
			"method":     r.Method,
			"path":       r.URL.Path,
			"status":     ww.Status(),
			"duration":   duration,
			"user-agent": r.UserAgent(),
			"remote":     r.RemoteAddr,
		}).Info("HTTP request")
	})
}

// recoveryMiddleware recovers from panics and returns a 500 error
func (s *Server) recoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				s.log.WithField("error", err).Error("Panic in HTTP handler")
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// WrappedResponseWriter is a wrapper for http.ResponseWriter that captures the status code
type WrappedResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

// NewWrappedResponseWriter creates a new WrappedResponseWriter
func NewWrappedResponseWriter(w http.ResponseWriter) *WrappedResponseWriter {
	return &WrappedResponseWriter{
		ResponseWriter: w,
		statusCode:     http.StatusOK,
	}
}

// WriteHeader captures the status code and calls the underlying WriteHeader
func (w *WrappedResponseWriter) WriteHeader(code int) {
	w.statusCode = code
	w.ResponseWriter.WriteHeader(code)
}

// Status returns the HTTP status code
func (w *WrappedResponseWriter) Status() int {
	return w.statusCode
}

// GetMetrics returns the metrics instance for use by other components
func (s *Server) GetMetrics() *metrics.Metrics {
	return s.metrics
}

// UpdateScanner updates the scanner reference in the server
func (s *Server) UpdateScanner(scanner *scanner.Scanner) {
	s.scanner = scanner
}

// UpdateMetricsFromStats updates Prometheus metrics with current database stats
func (s *Server) UpdateMetricsFromStats() {
	stats, err := s.db.GetStats()
	if err != nil {
		s.log.WithError(err).Error("Failed to get database stats for metrics")
		return
	}

	// Update thumbnail counts
	s.metrics.UpdateThumbnailCounts(stats.Success, stats.Error, stats.Pending, stats.Deleted)

	// Update file sizes
	s.metrics.UpdateFileSizes(stats.ViewedSize, stats.UnviewedSize)
}
