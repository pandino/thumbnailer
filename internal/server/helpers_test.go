package server

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/gorilla/mux"
	"github.com/pandino/movie-thumbnailer-go/internal/config"
	"github.com/pandino/movie-thumbnailer-go/internal/database"
	"github.com/pandino/movie-thumbnailer-go/internal/metrics"
	"github.com/pandino/movie-thumbnailer-go/internal/models"
	"github.com/sirupsen/logrus"
)

// newDBTestServer builds a real Server backed by a temporary SQLite database
func newDBTestServer(t *testing.T) (*Server, *database.DB) {
	t.Helper()
	db, err := database.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	wd, _ := os.Getwd()
	s := &Server{
		cfg:    &config.Config{TemplatesDir: filepath.Join(wd, "..", "..", "web", "templates")},
		db:     db,
		log:    logger,
		router: mux.NewRouter(),
	}
	return s, db
}

func addDBTestThumbnail(t *testing.T, db *database.DB, name string, viewed int) *models.Thumbnail {
	t.Helper()
	th := &models.Thumbnail{
		MoviePath:     "/movies/" + name,
		MovieFilename: name,
		ThumbnailPath: name + ".jpg",
		Status:        models.StatusSuccess,
		Viewed:        viewed,
	}
	if err := db.UpsertThumbnail(th); err != nil {
		t.Fatalf("failed to upsert thumbnail: %v", err)
	}
	return th
}

// metrics.New registers global Prometheus collectors, so tests share one instance
var (
	testMetricsOnce sync.Once
	testMetrics     *metrics.Metrics
)

func sharedTestMetrics() *metrics.Metrics {
	testMetricsOnce.Do(func() { testMetrics = metrics.New() })
	return testMetrics
}
