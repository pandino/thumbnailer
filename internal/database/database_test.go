package database

import (
	"testing"

	"github.com/pandino/movie-thumbnailer-go/internal/models"
)

func TestGetStatsEmptyDatabase(t *testing.T) {
	db := newTestDB(t)
	stats, err := db.GetStats()
	if err != nil {
		t.Fatalf("GetStats on empty database failed: %v", err)
	}
	if *stats != (models.Stats{}) {
		t.Errorf("GetStats on empty database = %+v; want all zeros", stats)
	}
}
