package worker

import (
	"context"
	"fmt"
	"time"

	"github.com/pandino/movie-thumbnailer-go/internal/config"
	"github.com/pandino/movie-thumbnailer-go/internal/scanner"
	"github.com/sirupsen/logrus"
)

// Worker manages background tasks for the application
type Worker struct {
	cfg     *config.Config
	scanner *scanner.Scanner
	log     *logrus.Logger
}

// New creates a new Worker
func New(cfg *config.Config, scanner *scanner.Scanner, log *logrus.Logger) *Worker {
	return &Worker{
		cfg:     cfg,
		scanner: scanner,
		log:     log,
	}
}

// Start begins the background task processing
func (w *Worker) Start(ctx context.Context) {
	w.log.Info("Starting background worker")

	// Perform an initial scan at startup
	go func() {
		w.log.Info("Running initial scan")
		if err := w.scanner.ScanMovies(ctx); err != nil {
			w.log.WithError(err).Error("Initial scan failed")
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
			if err := w.scanner.ScanMovies(ctx); err != nil {
				w.log.WithError(err).Error("Scheduled scan failed")
			}
		case <-cleanupTicker.C:
			// Skip if a scan is already in progress
			if w.scanner.IsScanning() {
				w.log.Info("Skipping scheduled cleanup because a scan is in progress")
				continue
			}

			w.log.Info("Running scheduled cleanup")
			if err := w.scanner.CleanupOrphans(ctx); err != nil {
				w.log.WithError(err).Error("Scheduled cleanup failed")
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
		if err := w.scanner.ScanMovies(ctx); err != nil {
			w.log.WithError(err).Error("Manual scan failed")
		}
	}()

	return nil
}

// PerformCleanup performs a cleanup of orphaned entries, thumbnails, and processes items marked for deletion
func (w *Worker) PerformCleanup(ctx context.Context) error {
	if w.scanner.IsScanning() {
		return fmt.Errorf("cannot perform cleanup while scan is in progress")
	}

	w.log.Info("Triggering manual cleanup")
	go func() {
		if err := w.scanner.CleanupOrphans(ctx); err != nil {
			w.log.WithError(err).Error("Manual cleanup failed")
		}
	}()

	return nil
}
