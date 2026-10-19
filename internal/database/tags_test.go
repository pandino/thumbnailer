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

func assertTags(t *testing.T, db *DB, id int64, want ...string) {
	t.Helper()
	got, err := db.GetTags(id)
	if err != nil {
		t.Fatalf("GetTags failed: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("GetTags(%d) = %v; want %v", id, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("GetTags(%d) = %v; want %v", id, got, want)
		}
	}
}

func TestTagCRUD(t *testing.T) {
	db := newTestDB(t)
	th := addTestThumbnail(t, db, "a.mp4", models.StatusSuccess, 0)

	assertTags(t, db, th.ID)

	if err := db.SetTags(th.ID, []string{"zeta", "alpha"}); err != nil {
		t.Fatalf("SetTags failed: %v", err)
	}
	assertTags(t, db, th.ID, "alpha", "zeta")

	if err := db.AddTags(th.ID, []string{"alpha", "beta"}); err != nil {
		t.Fatalf("AddTags failed: %v", err)
	}
	assertTags(t, db, th.ID, "alpha", "beta", "zeta")

	if err := db.RemoveTags(th.ID, []string{"zeta", "missing"}); err != nil {
		t.Fatalf("RemoveTags failed: %v", err)
	}
	assertTags(t, db, th.ID, "alpha", "beta")

	if err := db.SetTags(th.ID, nil); err != nil {
		t.Fatalf("SetTags(nil) failed: %v", err)
	}
	assertTags(t, db, th.ID)
}

func TestTagsSurviveUpsertAndAreDeletedWithThumbnail(t *testing.T) {
	db := newTestDB(t)
	th := addTestThumbnail(t, db, "a.mp4", models.StatusPending, 0)
	if err := db.SetTags(th.ID, []string{"keep"}); err != nil {
		t.Fatalf("SetTags failed: %v", err)
	}

	// Rescan updates the same movie path through INSERT OR REPLACE
	updated := addTestThumbnail(t, db, "a.mp4", models.StatusSuccess, 0)
	if updated.ID != th.ID {
		// UpsertThumbnail only fills ID for new structs; re-read to compare
		row, _ := db.GetByMoviePath(th.MoviePath)
		if row == nil || row.ID != th.ID {
			t.Fatalf("upsert changed thumbnail id")
		}
	}
	assertTags(t, db, th.ID, "keep")

	if err := db.DeleteThumbnail(th.MoviePath); err != nil {
		t.Fatalf("DeleteThumbnail failed: %v", err)
	}
	assertTags(t, db, th.ID)
}

func TestCleanupOrphansRemovesTags(t *testing.T) {
	db := newTestDB(t)
	th := addTestThumbnail(t, db, "a.mp4", models.StatusDeleted, 0)
	if err := db.SetTags(th.ID, []string{"gone"}); err != nil {
		t.Fatalf("SetTags failed: %v", err)
	}
	if _, err := db.CleanupOrphans(); err != nil {
		t.Fatalf("CleanupOrphans failed: %v", err)
	}
	assertTags(t, db, th.ID)
}

func TestGetTagCounts(t *testing.T) {
	db := newTestDB(t)
	a := addTestThumbnail(t, db, "a.mp4", models.StatusSuccess, 0)
	b := addTestThumbnail(t, db, "b.mp4", models.StatusSuccess, 1)
	c := addTestThumbnail(t, db, "c.mp4", models.StatusSuccess, 0)
	d := addTestThumbnail(t, db, "d.mp4", models.StatusDeleted, 0)

	db.SetTags(a.ID, []string{"common", "beta"})
	db.SetTags(b.ID, []string{"common", "alpha"})
	db.SetTags(c.ID, []string{"common", "alpha"})
	db.SetTags(d.ID, []string{"beta", "beta2"}) // not counted: not success

	counts, err := db.GetTagCounts()
	if err != nil {
		t.Fatalf("GetTagCounts failed: %v", err)
	}

	want := []models.TagCount{
		{Tag: "common", Count: 3, Unviewed: 2},
		{Tag: "alpha", Count: 2, Unviewed: 1},
		{Tag: "beta", Count: 1, Unviewed: 1},
	}
	if len(counts) != len(want) {
		t.Fatalf("GetTagCounts = %+v; want %+v", counts, want)
	}
	for i := range want {
		if counts[i] != want[i] {
			t.Fatalf("GetTagCounts = %+v; want %+v", counts, want)
		}
	}
}

func TestRandomUnviewedWithTag(t *testing.T) {
	db := newTestDB(t)
	tagged := addTestThumbnail(t, db, "tagged.mp4", models.StatusSuccess, 0)
	taggedViewed := addTestThumbnail(t, db, "tagged-viewed.mp4", models.StatusSuccess, 1)
	addTestThumbnail(t, db, "untagged.mp4", models.StatusSuccess, 0)
	db.SetTags(tagged.ID, []string{"x"})
	db.SetTags(taggedViewed.ID, []string{"x"})

	for i := 0; i < 20; i++ {
		th, err := db.GetRandomUnviewedThumbnailWithTag("x")
		if err != nil {
			t.Fatalf("GetRandomUnviewedThumbnailWithTag failed: %v", err)
		}
		if th == nil || th.ID != tagged.ID {
			t.Fatalf("expected only the unviewed tagged thumbnail, got %+v", th)
		}
	}

	th, err := db.GetRandomUnviewedThumbnailWithTag("x", tagged.ID)
	if err != nil || th != nil {
		t.Fatalf("expected no thumbnail after excluding, got %+v, %v", th, err)
	}

	count, err := db.GetUnviewedCountByTag("x")
	if err != nil || count != 1 {
		t.Fatalf("GetUnviewedCountByTag = %d, %v; want 1", count, err)
	}

	// Empty tag disables the filter
	th, err = db.GetRandomUnviewedThumbnailWithTag("", tagged.ID)
	if err != nil || th == nil || th.MovieFilename != "untagged.mp4" {
		t.Fatalf("expected untagged thumbnail with empty tag filter, got %+v, %v", th, err)
	}
}
