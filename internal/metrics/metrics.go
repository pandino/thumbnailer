package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics holds all Prometheus metrics for the application
type Metrics struct {
	// HTTP metrics
	HTTPRequestsTotal     *prometheus.CounterVec
	HTTPRequestDuration   *prometheus.HistogramVec
	HTTPActiveConnections prometheus.Gauge

	// Application metrics
	ThumbnailsTotal             *prometheus.GaugeVec
	ThumbnailGenerationTotal    *prometheus.CounterVec
	ThumbnailGenerationDuration prometheus.Histogram

	// Scanning metrics
	ScanOperationsTotal *prometheus.CounterVec
	ScanDuration        prometheus.Histogram
	LastScanTimestamp   prometheus.Gauge

	// Slideshow metrics
	SlideshowSessionsTotal   *prometheus.CounterVec
	SlideshowSessionDuration prometheus.Histogram
	SlideshowViewsTotal      prometheus.Counter

	// Storage metrics
	TotalFileSize *prometheus.GaugeVec

	// Worker metrics
	BackgroundTasksTotal *prometheus.CounterVec
	WorkerErrors         *prometheus.CounterVec

	// FFmpeg metrics
	FFmpegExecutionsTotal *prometheus.CounterVec
	FFmpegDuration        prometheus.Histogram
}

// New creates and registers all Prometheus metrics
func New() *Metrics {
	return &Metrics{
		// HTTP metrics
		HTTPRequestsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "movie_thumbnailer_http_requests_total",
				Help: "Total number of HTTP requests",
			},
			[]string{"method", "endpoint", "status_code"},
		),
		HTTPRequestDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "movie_thumbnailer_http_request_duration_seconds",
				Help:    "Duration of HTTP requests in seconds",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"method", "endpoint"},
		),
		HTTPActiveConnections: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "movie_thumbnailer_http_active_connections",
				Help: "Number of active HTTP connections",
			},
		),

		// Application metrics
		ThumbnailsTotal: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "movie_thumbnailer_thumbnails_total",
				Help: "Total number of thumbnails by status",
			},
			[]string{"status"},
		),
		ThumbnailGenerationTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "movie_thumbnailer_thumbnail_generation_total",
				Help: "Total number of thumbnail generation attempts",
			},
			[]string{"result"},
		),
		ThumbnailGenerationDuration: promauto.NewHistogram(
			prometheus.HistogramOpts{
				Name:    "movie_thumbnailer_thumbnail_generation_duration_seconds",
				Help:    "Duration of thumbnail generation in seconds",
				Buckets: []float64{1, 5, 10, 30, 60, 120, 300}, // Custom buckets for video processing
			},
		),

		// Scanning metrics
		ScanOperationsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "movie_thumbnailer_scan_operations_total",
				Help: "Total number of scanning operations",
			},
			[]string{"result"},
		),
		ScanDuration: promauto.NewHistogram(
			prometheus.HistogramOpts{
				Name:    "movie_thumbnailer_scan_duration_seconds",
				Help:    "Duration of scanning operations in seconds",
				Buckets: []float64{1, 5, 10, 30, 60, 300, 600}, // Custom buckets for scanning
			},
		),
		LastScanTimestamp: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "movie_thumbnailer_last_scan_timestamp",
				Help: "Timestamp of the last successful scan",
			},
		),

		// Slideshow metrics
		SlideshowSessionsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "movie_thumbnailer_slideshow_sessions_total",
				Help: "Total number of slideshow sessions",
			},
			[]string{"result"},
		),
		SlideshowSessionDuration: promauto.NewHistogram(
			prometheus.HistogramOpts{
				Name:    "movie_thumbnailer_slideshow_session_duration_seconds",
				Help:    "Duration of slideshow sessions in seconds",
				Buckets: []float64{10, 30, 60, 300, 600, 1800, 3600}, // Custom buckets for sessions
			},
		),
		SlideshowViewsTotal: promauto.NewCounter(
			prometheus.CounterOpts{
				Name: "movie_thumbnailer_slideshow_views_total",
				Help: "Total number of images viewed in slideshow",
			},
		),

		// Storage metrics
		TotalFileSize: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "movie_thumbnailer_total_file_size_bytes",
				Help: "Total file size in bytes",
			},
			[]string{"category"},
		),

		// Worker metrics
		BackgroundTasksTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "movie_thumbnailer_background_tasks_total",
				Help: "Total number of background tasks executed",
			},
			[]string{"task_type", "result"},
		),
		WorkerErrors: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "movie_thumbnailer_worker_errors_total",
				Help: "Total number of worker errors",
			},
			[]string{"worker_type", "error_type"},
		),

		// FFmpeg metrics
		FFmpegExecutionsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "movie_thumbnailer_ffmpeg_executions_total",
				Help: "Total number of FFmpeg executions",
			},
			[]string{"result"},
		),
		FFmpegDuration: promauto.NewHistogram(
			prometheus.HistogramOpts{
				Name:    "movie_thumbnailer_ffmpeg_duration_seconds",
				Help:    "Duration of FFmpeg executions in seconds",
				Buckets: []float64{0.5, 1, 2, 5, 10, 30, 60}, // Custom buckets for FFmpeg
			},
		),
	}
}

// RecordHTTPRequest records metrics for an HTTP request
func (m *Metrics) RecordHTTPRequest(method, endpoint, statusCode string, duration time.Duration) {
	m.HTTPRequestsTotal.WithLabelValues(method, endpoint, statusCode).Inc()
	m.HTTPRequestDuration.WithLabelValues(method, endpoint).Observe(duration.Seconds())
}

// RecordThumbnailGeneration records metrics for thumbnail generation
func (m *Metrics) RecordThumbnailGeneration(result string, duration time.Duration) {
	m.ThumbnailGenerationTotal.WithLabelValues(result).Inc()
	m.ThumbnailGenerationDuration.Observe(duration.Seconds())
}

// RecordScanOperation records metrics for scan operations
func (m *Metrics) RecordScanOperation(result string, duration time.Duration) {
	m.ScanOperationsTotal.WithLabelValues(result).Inc()
	m.ScanDuration.Observe(duration.Seconds())
	if result == "success" {
		m.LastScanTimestamp.SetToCurrentTime()
	}
}

// RecordSlideshowSession records metrics for slideshow sessions
func (m *Metrics) RecordSlideshowSession(result string, duration time.Duration) {
	m.SlideshowSessionsTotal.WithLabelValues(result).Inc()
	m.SlideshowSessionDuration.Observe(duration.Seconds())
}

// RecordSlideshowView records a view in the slideshow
func (m *Metrics) RecordSlideshowView() {
	m.SlideshowViewsTotal.Inc()
}

// RecordBackgroundTask records metrics for background tasks
func (m *Metrics) RecordBackgroundTask(taskType, result string) {
	m.BackgroundTasksTotal.WithLabelValues(taskType, result).Inc()
}

// RecordWorkerError records worker errors
func (m *Metrics) RecordWorkerError(workerType, errorType string) {
	m.WorkerErrors.WithLabelValues(workerType, errorType).Inc()
}

// RecordFFmpegExecution records metrics for FFmpeg executions
func (m *Metrics) RecordFFmpegExecution(result string, duration time.Duration) {
	m.FFmpegExecutionsTotal.WithLabelValues(result).Inc()
	m.FFmpegDuration.Observe(duration.Seconds())
}

// UpdateThumbnailCounts updates the thumbnail count metrics
func (m *Metrics) UpdateThumbnailCounts(success, error, pending, deleted int) {
	m.ThumbnailsTotal.WithLabelValues("success").Set(float64(success))
	m.ThumbnailsTotal.WithLabelValues("error").Set(float64(error))
	m.ThumbnailsTotal.WithLabelValues("pending").Set(float64(pending))
	m.ThumbnailsTotal.WithLabelValues("deleted").Set(float64(deleted))
}

// UpdateFileSizes updates the file size metrics
func (m *Metrics) UpdateFileSizes(viewedSize, unviewedSize int64) {
	m.TotalFileSize.WithLabelValues("viewed").Set(float64(viewedSize))
	m.TotalFileSize.WithLabelValues("unviewed").Set(float64(unviewedSize))
}
