package database

import (
	"path/filepath"
	"testing"

	"github.com/pandino/movie-thumbnailer-go/internal/models"
)

func newTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func addTestThumbnail(t *testing.T, db *DB, name string, status string, viewed int) *models.Thumbnail {
	t.Helper()
	thumbnail := &models.Thumbnail{
		MoviePath:     "/movies/" + name,
		MovieFilename: name,
		ThumbnailPath: name + ".jpg",
		Status:        status,
		Viewed:        viewed,
	}
	if err := db.UpsertThumbnail(thumbnail); err != nil {
		t.Fatalf("failed to upsert thumbnail: %v", err)
	}
	return thumbnail
}
