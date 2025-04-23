package server

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/pandino/movie-thumbnailer-go/internal/config"
	"github.com/pandino/movie-thumbnailer-go/internal/database"
	"github.com/pandino/movie-thumbnailer-go/internal/scanner"
	"github.com/sirupsen/logrus"
)

// Server handles HTTP requests for the application
type Server struct {
	cfg     *config.Config
	db      *database.DB
	scanner *scanner.Scanner
	log     *logrus.Logger
	server  *http.Server
	router  *mux.Router
}

// New creates a new Server
func New(cfg *config.Config, db *database.DB, scanner *scanner.Scanner, log *logrus.Logger) *Server {
	s := &Server{
		cfg:     cfg,
		db:      db,
		scanner: scanner,
		log:     log,
		router:  mux.NewRouter(),
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

	// API routes
	s.router.HandleFunc("/api/stats", s.handleStats).Methods("GET")
	s.router.HandleFunc("/api/thumbnails", s.handleThumbnails).Methods("GET")
	s.router.HandleFunc("/api/thumbnails/{id}", s.handleThumbnail).Methods("GET")

	// 404 handler
	s.router.NotFoundHandler = http.HandlerFunc(s.handleNotFound)
}

// loggingMiddleware logs HTTP requests
func (s *Server) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Wrap the response writer to capture status code
		ww := NewWrappedResponseWriter(w)

		// Call the next handler
		next.ServeHTTP(ww, r)

		// Log the request
		s.log.WithFields(logrus.Fields{
			"method":     r.Method,
			"path":       r.URL.Path,
			"status":     ww.Status(),
			"duration":   time.Since(start),
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
