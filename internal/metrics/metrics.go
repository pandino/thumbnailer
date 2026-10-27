package metrics

import (
	"strconv"
	"time"

	"github.com/pandino/movie-thumbnailer-go/internal/models"
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

	// Cleanup metrics
	CleanupDeletedMoviesTotal *prometheus.CounterVec
	CleanupDeletedMoviesSize  *prometheus.CounterVec

	// Preference model metrics, from what the worker last reported (ml_meta) and the database
	MLTrainedTimestamp prometheus.Gauge
	MLCVAUC            *prometheus.GaugeVec
	MLBrier            *prometheus.GaugeVec
	MLLiveAUC          prometheus.Gauge
	MLLiveAUCArchive   prometheus.Gauge
	MLEmbedded         prometheus.Gauge
	MLScored           prometheus.Gauge
}

// Outcome models trained by the worker; their ml_meta keys are auc_<model> and brier_<model>
var mlModels = []string{"delete", "archive"}

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

		// Cleanup metrics
		CleanupDeletedMoviesTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "movie_thumbnailer_cleanup_deleted_movies_total",
				Help: "Total number of movies deleted during cleanup operations",
			},
			[]string{"operation_type"},
		),
		CleanupDeletedMoviesSize: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "movie_thumbnailer_cleanup_deleted_movies_size_bytes_total",
				Help: "Total size in bytes of movies deleted during cleanup operations",
			},
			[]string{"operation_type"},
		),

		// Preference model metrics
		MLTrainedTimestamp: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "movie_thumbnailer_ml_trained_timestamp_seconds",
				Help: "Unix time the preference model was last trained (0 = never)",
			},
		),
		MLCVAUC: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "movie_thumbnailer_ml_cv_auc",
				Help: "Cross-validated AUC of each outcome model (1 = perfect ranking, 0.5 = chance)",
			},
			[]string{"model"},
		),
		MLBrier: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "movie_thumbnailer_ml_brier",
				Help: "Brier score of each outcome model's calibrated probabilities (lower is better)",
			},
			[]string{"model"},
		),
		MLLiveAUC: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "movie_thumbnailer_ml_live_auc",
				Help: "AUC of the delete scores shown at decision time on recent slideshow decisions (0 = not enough decisions)",
			},
		),
		MLLiveAUCArchive: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "movie_thumbnailer_ml_live_auc_archive",
				Help: "AUC of the archive (favorite) scores shown at decision time on recent slideshow and player decisions (0 = not enough decisions)",
			},
		),
		MLEmbedded: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "movie_thumbnailer_ml_embedded_movies",
				Help: "Movies with an embedding from the model the worker last trained with",
			},
		),
		MLScored: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "movie_thumbnailer_ml_scored_movies",
				Help: "Movies with a current score",
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

// RecordCleanupDeletedMovies records metrics for deleted movies during cleanup
func (m *Metrics) RecordCleanupDeletedMovies(result string, size int64) {
	m.CleanupDeletedMoviesTotal.WithLabelValues(result).Inc()
	m.CleanupDeletedMoviesSize.WithLabelValues(result).Add(float64(size))
}

// UpdateThumbnailCounts updates the thumbnail count metrics
func (m *Metrics) UpdateThumbnailCounts(success, error, pending, deleted, archived int) {
	m.ThumbnailsTotal.WithLabelValues("success").Set(float64(success))
	m.ThumbnailsTotal.WithLabelValues("error").Set(float64(error))
	m.ThumbnailsTotal.WithLabelValues("pending").Set(float64(pending))
	m.ThumbnailsTotal.WithLabelValues("deleted").Set(float64(deleted))
	m.ThumbnailsTotal.WithLabelValues("archived").Set(float64(archived))
}

// UpdateFileSizes updates the file size metrics
func (m *Metrics) UpdateFileSizes(viewedSize, unviewedSize int64) {
	m.TotalFileSize.WithLabelValues("viewed").Set(float64(viewedSize))
	m.TotalFileSize.WithLabelValues("unviewed").Set(float64(unviewedSize))
}

// RecordCleanupDeletedMovie records metrics for movies deleted during cleanup
func (m *Metrics) RecordCleanupDeletedMovie(operationType string, fileSize int64) {
	m.CleanupDeletedMoviesTotal.WithLabelValues(operationType).Inc()
	m.CleanupDeletedMoviesSize.WithLabelValues(operationType).Add(float64(fileSize))
}

// UpdateModelStatus updates the preference model metrics. Per-model values the worker didn't
// report (a model not trained yet) are removed rather than left at their last value.
func (m *Metrics) UpdateModelStatus(status *models.MLStatus) {
	m.MLTrainedTimestamp.Set(float64(TrainedUnix(status.Meta["trained_at"])))
	for _, model := range mlModels {
		setOrDelete(m.MLCVAUC, model, status.Meta["auc_"+model])
		setOrDelete(m.MLBrier, model, status.Meta["brier_"+model])
	}
	m.MLLiveAUC.Set(status.LiveAUC)
	m.MLLiveAUCArchive.Set(status.LiveAUCArchive)
	m.MLEmbedded.Set(float64(status.Embedded))
	m.MLScored.Set(float64(status.Scored))
}

func setOrDelete(gauge *prometheus.GaugeVec, label, value string) {
	if v, err := strconv.ParseFloat(value, 64); err == nil {
		gauge.WithLabelValues(label).Set(v)
	} else {
		gauge.DeleteLabelValues(label)
	}
}

// TrainedUnix converts the worker's trained_at ("2006-01-02 15:04", the worker's local time) to
// Unix seconds, or 0 if it is missing or malformed. The worker and the server both run with
// TZ=Europe/Zurich, so the server's local time zone is the worker's.
func TrainedUnix(trainedAt string) int64 {
	t, err := time.ParseInLocation("2006-01-02 15:04", trainedAt, time.Local)
	if err != nil {
		return 0
	}
	return t.Unix()
}
