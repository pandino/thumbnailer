package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/pandino/movie-thumbnailer-go/internal/config"
	"github.com/pandino/movie-thumbnailer-go/internal/database"
	"github.com/pandino/movie-thumbnailer-go/internal/scanner"
	"github.com/pandino/movie-thumbnailer-go/internal/server"
	"github.com/pandino/movie-thumbnailer-go/internal/worker"
	"github.com/sirupsen/logrus"
)

func main() {
	// Initialize logger
	log := logrus.New()
	log.SetOutput(os.Stdout)
	log.SetFormatter(&logrus.TextFormatter{
		FullTimestamp: true,
	})

	// Load configuration
	cfg := config.New()
	if cfg.Debug {
		log.SetLevel(logrus.DebugLevel)
	} else {
		log.SetLevel(logrus.InfoLevel)
	}

	log.Info("Starting Movie Thumbnailer")
	log.Debugf("Configuration: Movies=%s, Thumbnails=%s, Data=%s",
		cfg.MoviesDir, cfg.ThumbnailsDir, cfg.DataDir)

	// Create directories
	createDirIfNotExists(cfg.ThumbnailsDir, log)
	createDirIfNotExists(cfg.DataDir, log)

	// Initialize database
	db, err := database.New(cfg.DBPath)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()

	// Setup context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize scanner
	s := scanner.New(cfg, db, log)

	// Initialize background worker
	w := worker.New(cfg, s, log)

	// Initialize HTTP server
	srv := server.New(cfg, db, s, log, ctx)

	// Start background worker
	go w.Start(ctx)

	// Start HTTP server
	go func() {
		if err := srv.Start(); err != nil {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	// Setup graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)

	// Wait for termination signal
	<-quit
	log.Info("Shutting down...")

	// Stop the background worker
	cancel()

	// Shutdown HTTP server
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Errorf("Server shutdown failed: %v", err)
	}

	log.Info("Shutdown complete")
}

func createDirIfNotExists(path string, log *logrus.Logger) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		log.Infof("Creating directory: %s", path)
		if err := os.MkdirAll(path, 0755); err != nil {
			log.Fatalf("Failed to create directory %s: %v", path, err)
		}
	}
}
