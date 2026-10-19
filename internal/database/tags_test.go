package database

import (
	"path/filepath"
	"strings"
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

func listIDs(t *testing.T, db *DB, f models.ThumbnailFilter) []int64 {
	t.Helper()
	if f.Limit == 0 {
		f.Limit = 100
	}
	thumbnails, err := db.ListThumbnails(f)
	if err != nil {
		t.Fatalf("ListThumbnails(%+v) failed: %v", f, err)
	}
	ids := []int64{}
	for _, th := range thumbnails {
		ids = append(ids, th.ID)
	}
	return ids
}

func assertIDs(t *testing.T, name string, got []int64, want ...int64) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: got %v; want %v", name, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s: got %v; want %v", name, got, want)
		}
	}
}

func TestListThumbnailsFilters(t *testing.T) {
	db := newTestDB(t)
	a := addTestThumbnail(t, db, "a.mp4", models.StatusSuccess, 0) // tags: cats, ai-reviewed
	b := addTestThumbnail(t, db, "b.mp4", models.StatusSuccess, 1) // tags: cats
	c := addTestThumbnail(t, db, "c.mp4", models.StatusSuccess, 0) // untagged
	d := addTestThumbnail(t, db, "d.mp4", models.StatusSuccess, 1) // untagged
	e := addTestThumbnail(t, db, "e.mp4", models.StatusError, 0)   // never listed
	db.SetTags(a.ID, []string{"cats", "ai-reviewed"})
	db.SetTags(b.ID, []string{"cats"})
	db.SetTags(e.ID, []string{"cats"})

	unviewed, viewed := false, true
	assertIDs(t, "all", listIDs(t, db, models.ThumbnailFilter{}), a.ID, b.ID, c.ID, d.ID)
	assertIDs(t, "unviewed", listIDs(t, db, models.ThumbnailFilter{Viewed: &unviewed}), a.ID, c.ID)
	assertIDs(t, "viewed", listIDs(t, db, models.ThumbnailFilter{Viewed: &viewed}), b.ID, d.ID)
	assertIDs(t, "untagged", listIDs(t, db, models.ThumbnailFilter{Untagged: true}), c.ID, d.ID)
	assertIDs(t, "tag", listIDs(t, db, models.ThumbnailFilter{Tag: "cats"}), a.ID, b.ID)
	assertIDs(t, "without_tag", listIDs(t, db, models.ThumbnailFilter{WithoutTag: "ai-reviewed"}), b.ID, c.ID, d.ID)
	assertIDs(t, "unviewed+without_tag", listIDs(t, db, models.ThumbnailFilter{Viewed: &unviewed, WithoutTag: "ai-reviewed"}), c.ID)
	assertIDs(t, "unviewed+untagged", listIDs(t, db, models.ThumbnailFilter{Viewed: &unviewed, Untagged: true}), c.ID)

	// Cursor paging
	assertIDs(t, "page 1", listIDs(t, db, models.ThumbnailFilter{Limit: 2}), a.ID, b.ID)
	assertIDs(t, "page 2", listIDs(t, db, models.ThumbnailFilter{Limit: 2, AfterID: b.ID}), c.ID, d.ID)
	assertIDs(t, "page 3", listIDs(t, db, models.ThumbnailFilter{Limit: 2, AfterID: d.ID}))
}

func TestGetTagsForIDs(t *testing.T) {
	db := newTestDB(t)
	a := addTestThumbnail(t, db, "a.mp4", models.StatusSuccess, 0)
	b := addTestThumbnail(t, db, "b.mp4", models.StatusSuccess, 0)
	c := addTestThumbnail(t, db, "c.mp4", models.StatusSuccess, 0)
	db.SetTags(a.ID, []string{"z", "y"})
	db.SetTags(b.ID, []string{"x"})

	got, err := db.GetTagsForIDs([]int64{a.ID, b.ID, c.ID})
	if err != nil {
		t.Fatalf("GetTagsForIDs failed: %v", err)
	}
	if strings.Join(got[a.ID], ",") != "y,z" || strings.Join(got[b.ID], ",") != "x" || len(got[c.ID]) != 0 {
		t.Errorf("GetTagsForIDs = %v", got)
	}

	empty, err := db.GetTagsForIDs(nil)
	if err != nil || len(empty) != 0 {
		t.Errorf("GetTagsForIDs(nil) = %v, %v", empty, err)
	}
}
