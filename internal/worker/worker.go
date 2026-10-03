package worker

import (
	"context"
	"errors"
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

// Start begins the background task processing. Scheduled scans and cleanups
// only run inside the configured work window; a scan still running when the
// window closes is cancelled and resumes at the next window.
func (w *Worker) Start(ctx context.Context) {
	w.log.WithFields(logrus.Fields{
		"start": w.cfg.WorkWindowStart,
		"end":   w.cfg.WorkWindowEnd,
	}).Info("Starting background worker")

	// Perform an initial scan at startup if inside the work window
	windowOpen := w.cfg.InWorkWindow(time.Now())
	if windowOpen {
		go func() {
			w.log.Info("Running initial scan")
			w.runScan(ctx, "initial_scan")
		}()
	} else {
		w.log.Info("Outside work window, deferring initial scan until the window opens")
	}

	// Set up ticker for periodic scans
	scanTicker := time.NewTicker(w.cfg.ScanInterval)
	defer scanTicker.Stop()

	// Set up ticker for periodic cleanups (every 6 hours)
	cleanupInterval := 6 * time.Hour
	cleanupTicker := time.NewTicker(cleanupInterval)
	defer cleanupTicker.Stop()

	// Check every minute whether the work window has opened
	windowTicker := time.NewTicker(time.Minute)
	defer windowTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			w.log.Info("Worker shutting down")
			return
		case <-windowTicker.C:
			open := w.cfg.InWorkWindow(time.Now())
			if open && !windowOpen && !w.scanner.IsScanning() {
				w.log.Info("Work window opened, running scan")
				w.runScan(ctx, "window_scan")
			}
			windowOpen = open
		case <-scanTicker.C:
			if !w.cfg.InWorkWindow(time.Now()) {
				w.log.Debug("Skipping scheduled scan outside work window")
				continue
			}

			// Skip if a scan is already in progress
			if w.scanner.IsScanning() {
				w.log.Info("Skipping scheduled scan because a scan is already in progress")
				continue
			}

			w.log.Info("Running scheduled scan")
			w.runScan(ctx, "scheduled_scan")
		case <-cleanupTicker.C:
			// Skip if deletion is disabled
			if w.cfg.DisableDeletion {
				w.log.Debug("Skipping scheduled cleanup because deletion is disabled")
				continue
			}

			if !w.cfg.InWorkWindow(time.Now()) {
				w.log.Debug("Skipping scheduled cleanup outside work window")
				continue
			}

			// Skip if a scan is already in progress
			if w.scanner.IsScanning() {
				w.log.Info("Skipping scheduled cleanup because a scan is in progress")
				continue
			}

			w.log.Info("Running scheduled cleanup")
			w.runCleanup(ctx)
		}
	}
}

// windowContext returns a child context that is cancelled when the work window closes.
func (w *Worker) windowContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if closes := w.cfg.WorkWindowEndAfter(time.Now()); !closes.IsZero() {
		return context.WithDeadline(ctx, closes)
	}
	return context.WithCancel(ctx)
}

// runScan runs a scan bounded by the work window and records its metrics.
func (w *Worker) runScan(ctx context.Context, task string) {
	start := time.Now()
	scanCtx, cancel := w.windowContext(ctx)
	defer cancel()

	err := w.scanner.ScanMovies(scanCtx)
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		w.log.Info("Work window closed, scan stopped; it will resume in the next window")
	case err != nil:
		w.log.WithError(err).WithField("task", task).Error("Scan failed")
		if w.metrics != nil {
			w.metrics.RecordScanOperation("error", time.Since(start))
			w.metrics.RecordBackgroundTask(task, "error")
		}
	default:
		if w.metrics != nil {
			w.metrics.RecordScanOperation("success", time.Since(start))
			w.metrics.RecordBackgroundTask(task, "success")
		}
	}
}

// runCleanup runs a cleanup bounded by the work window and records its metrics.
func (w *Worker) runCleanup(ctx context.Context) {
	start := time.Now()
	cleanupCtx, cancel := w.windowContext(ctx)
	defer cancel()

	if err := w.scanner.CleanupOrphans(cleanupCtx); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			w.log.Info("Work window closed, cleanup stopped; it will resume in the next window")
			return
		}
		w.log.WithError(err).Error("Scheduled cleanup failed")
		if w.metrics != nil {
			w.metrics.RecordBackgroundTask("cleanup", "error")
		}
		return
	}
	if w.metrics != nil {
		w.metrics.RecordBackgroundTask("cleanup", "success")
	}
	w.log.WithField("duration", time.Since(start)).Info("Scheduled cleanup completed")
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
