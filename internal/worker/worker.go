package worker

import (
	"context"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/yourusername/movie-thumbnailer-go/internal/config"
	"github.com/yourusername/movie-thumbnailer-go/internal/scanner"
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
	ticker := time.NewTicker(w.cfg.ScanInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			w.log.Info("Worker shutting down")
			return
		case <-ticker.C:
			// Skip if a scan is already in progress
			if w.scanner.IsScanning() {
				w.log.Info("Skipping scheduled scan because a scan is already in progress")
				continue
			}

			w.log.Info("Running scheduled scan")
			if err := w.scanner.ScanMovies(ctx); err != nil {
				w.log.WithError(err).Error("Scheduled scan failed")
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

// PerformCleanup performs a cleanup of orphaned entries and thumbnails
func (w *Worker) PerformCleanup(ctx context.Context) error {
	if w.scanner.IsScanning() {
		return nil
	}

	w.log.Info("Triggering manual cleanup")
	go func() {
		if err := w.scanner.ScanMovies(ctx); err != nil {
			w.log.WithError(err).Error("Manual cleanup failed")
		}
	}()

	return nil
}
