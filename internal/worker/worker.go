package worker

import (
	"context"
	"fmt"
	"time"

	"github.com/pandino/movie-thumbnailer-go/internal/config"
	"github.com/pandino/movie-thumbnailer-go/internal/metrics"
	"github.com/pandino/movie-thumbnailer-go/internal/scanner"
	"github.com/sirupsen/logrus"
)

// Worker manages background tasks for the application
type Worker struct {
	cfg     *config.Config
	scanner *scanner.Scanner
	log     *logrus.Logger
	metrics *metrics.Metrics
}

// New creates a new Worker
func New(cfg *config.Config, scanner *scanner.Scanner, log *logrus.Logger, metrics *metrics.Metrics) *Worker {
	return &Worker{
		cfg:     cfg,
		scanner: scanner,
		log:     log,
		metrics: metrics,
	}
}

// Start begins the background task processing
func (w *Worker) Start(ctx context.Context) {
	w.log.Info("Starting background worker")

	// Perform an initial scan at startup
	go func() {
		w.log.Info("Running initial scan")
		start := time.Now()

		// Create a child context that can be cancelled either by the worker context or app shutdown
		scanCtx, cancel := context.WithCancel(ctx)
		defer cancel()

		if err := w.scanner.ScanMovies(scanCtx); err != nil {
			w.log.WithError(err).Error("Initial scan failed")
			if w.metrics != nil {
				w.metrics.RecordScanOperation("error", time.Since(start))
				w.metrics.RecordBackgroundTask("initial_scan", "error")
			}
		} else {
			if w.metrics != nil {
				w.metrics.RecordScanOperation("success", time.Since(start))
				w.metrics.RecordBackgroundTask("initial_scan", "success")
			}
		}
	}()

	// Set up ticker for periodic scans
	scanTicker := time.NewTicker(w.cfg.ScanInterval)
	defer scanTicker.Stop()

	// Set up ticker for periodic cleanups (every 6 hours)
	cleanupInterval := 6 * time.Hour
	cleanupTicker := time.NewTicker(cleanupInterval)
	defer cleanupTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			w.log.Info("Worker shutting down")
			return
		case <-scanTicker.C:
			// Skip if a scan is already in progress
			if w.scanner.IsScanning() {
				w.log.Info("Skipping scheduled scan because a scan is already in progress")
				continue
			}

			w.log.Info("Running scheduled scan")
			start := time.Now()

			// Create a child context for each scan operation
			scanCtx, cancel := context.WithCancel(ctx)
			defer cancel()

			if err := w.scanner.ScanMovies(scanCtx); err != nil {
				w.log.WithError(err).Error("Scheduled scan failed")
				if w.metrics != nil {
					w.metrics.RecordScanOperation("error", time.Since(start))
					w.metrics.RecordBackgroundTask("scheduled_scan", "error")
				}
			} else {
				if w.metrics != nil {
					w.metrics.RecordScanOperation("success", time.Since(start))
					w.metrics.RecordBackgroundTask("scheduled_scan", "success")
				}
			}
		case <-cleanupTicker.C:
			// Skip if deletion is disabled
			if w.cfg.DisableDeletion {
				w.log.Debug("Skipping scheduled cleanup because deletion is disabled")
				continue
			}

			// Skip if a scan is already in progress
			if w.scanner.IsScanning() {
				w.log.Info("Skipping scheduled cleanup because a scan is in progress")
				continue
			}

			w.log.Info("Running scheduled cleanup")
			start := time.Now()

			// Create a child context for each cleanup operation
			cleanupCtx, cancel := context.WithCancel(ctx)
			defer cancel()

			if err := w.scanner.CleanupOrphans(cleanupCtx); err != nil {
				w.log.WithError(err).Error("Scheduled cleanup failed")
				if w.metrics != nil {
					w.metrics.RecordBackgroundTask("cleanup", "error")
				}
			} else {
				duration := time.Since(start)
				if w.metrics != nil {
					w.metrics.RecordBackgroundTask("cleanup", "success")
				}
				w.log.WithField("duration", duration).Info("Scheduled cleanup completed")
			}
		}
	}
}

// PerformScan triggers a scan on demand
func (w *Worker) PerformScan(ctx context.Context) error {
	if w.scanner.IsScanning() {
		w.log.Info("Scan already in progress")
		return nil
	}

	w.log.Info("Triggering manual scan")
	go func() {
		start := time.Now()

		// Create a child context that will be cancelled either by the provided context or app shutdown
		scanCtx, cancel := context.WithCancel(ctx)
		defer cancel()

		if err := w.scanner.ScanMovies(scanCtx); err != nil {
			w.log.WithError(err).Error("Manual scan failed")
			if w.metrics != nil {
				w.metrics.RecordScanOperation("error", time.Since(start))
				w.metrics.RecordBackgroundTask("manual_scan", "error")
			}
		} else {
			if w.metrics != nil {
				w.metrics.RecordScanOperation("success", time.Since(start))
				w.metrics.RecordBackgroundTask("manual_scan", "success")
			}
		}
	}()

	return nil
}

// PerformCleanup performs a cleanup of orphaned entries, thumbnails, and processes items marked for deletion
func (w *Worker) PerformCleanup(ctx context.Context) error {
	if w.cfg.DisableDeletion {
		w.log.Info("Cleanup requested but deletion is disabled")
		return fmt.Errorf("cleanup is disabled via DISABLE_DELETION flag")
	}

	if w.scanner.IsScanning() {
		return fmt.Errorf("cannot perform cleanup while scan is in progress")
	}

	w.log.Info("Triggering manual cleanup")
	go func() {
		// Create a child context that will be cancelled either by the provided context or app shutdown
		cleanupCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		if err := w.scanner.CleanupOrphans(cleanupCtx); err != nil {
			w.log.WithError(err).Error("Manual cleanup failed")
		}
	}()

	return nil
}
