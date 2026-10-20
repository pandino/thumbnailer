package scanner

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/pandino/movie-thumbnailer-go/internal/models"
)

// makeTestVideo writes a 40 s test video (the grid skips the first 30 s)
func makeTestVideo(t *testing.T, path string) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	cmd := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "testsrc=size=320x180:rate=10",
		"-t", "40", "-g", "10", "-pix_fmt", "yuv420p", path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("cannot create test video: %v\n%s", err, out)
	}
}

func (env *decisionTestEnv) process(t *testing.T, name string) *models.Thumbnail {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := env.s.processMovie(ctx, filepath.Join(env.moviesDir, name), 0, 1); err != nil {
		t.Fatalf("processMovie(%s): %v", name, err)
	}
	th, err := env.db.GetByMoviePath(name)
	if err != nil || th == nil {
		t.Fatalf("no row for %s: %v", name, err)
	}
	return th
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// Movies that differ only by extension used to share X.jpg: the scan adopted the other
// movie's grid, and deciding on one of them took the grid away from the other.
func TestSameNameDifferentExtensionKeepSeparateGrids(t *testing.T) {
	env := newDecisionTestEnv(t)
	env.s.cfg.ImportExisting = true
	makeTestVideo(t, filepath.Join(env.moviesDir, "movie.mkv"))
	makeTestVideo(t, filepath.Join(env.moviesDir, "movie.mp4"))
	// A grid in the old naming, left by some other movie
	writeJPEG(t, filepath.Join(env.thumbsDir, "movie.jpg"), 10, 10)

	mkv := env.process(t, "movie.mkv")
	mp4 := env.process(t, "movie.mp4")
	if mkv.ThumbnailPath != "movie.mkv.jpg" || mp4.ThumbnailPath != "movie.mp4.jpg" {
		t.Fatalf("grids = %q, %q; want movie.mkv.jpg, movie.mp4.jpg", mkv.ThumbnailPath, mp4.ThumbnailPath)
	}
	for _, th := range []*models.Thumbnail{mkv, mp4} {
		if th.Status != models.StatusSuccess || th.Source != models.SourceGenerated {
			t.Errorf("%s: status %s, source %s; want a generated grid, not the imported movie.jpg",
				th.MovieFilename, th.Status, th.Source)
		}
	}

	env.db.MarkForArchival(mkv.ID, models.StagePlayer)
	if err := env.s.CleanupOrphans(context.Background()); err != nil {
		t.Fatalf("CleanupOrphans: %v", err)
	}
	if !fileExists(filepath.Join(env.thumbsDir, DecidedDirName, filepathID(mkv.ID))) {
		t.Error("archived movie's grid not kept in decided/")
	}
	if !fileExists(filepath.Join(env.thumbsDir, mp4.ThumbnailPath)) {
		t.Error("archiving movie.mkv removed the grid of movie.mp4")
	}
}

// Rows from before the naming change keep their grid: nothing is regenerated or renamed
func TestExistingGridKeepsItsName(t *testing.T) {
	env := newDecisionTestEnv(t)
	makeTestVideo(t, filepath.Join(env.moviesDir, "old.mp4"))
	th := &models.Thumbnail{
		MoviePath:     "old.mp4",
		MovieFilename: "old.mp4",
		ThumbnailPath: "old.jpg",
		Status:        models.StatusSuccess,
		Viewed:        1,
	}
	writeJPEG(t, filepath.Join(env.thumbsDir, "old.jpg"), 2596, 740)
	if err := env.db.UpsertThumbnail(th); err != nil {
		t.Fatal(err)
	}

	got := env.process(t, "old.mp4")
	if got.ThumbnailPath != "old.jpg" || got.Status != models.StatusSuccess || got.Viewed != 1 {
		t.Errorf("row = %+v; want it unchanged", got)
	}
	if fileExists(filepath.Join(env.thumbsDir, "old.mp4.jpg")) {
		t.Error("grid regenerated under the new name")
	}
}

// A row whose grid is gone gets a new grid under the new name
func TestMissingGridRegeneratedUnderNewName(t *testing.T) {
	env := newDecisionTestEnv(t)
	makeTestVideo(t, filepath.Join(env.moviesDir, "lost.mp4"))
	th := &models.Thumbnail{
		MoviePath:     "lost.mp4",
		MovieFilename: "lost.mp4",
		ThumbnailPath: "lost.jpg",
		Status:        models.StatusPending,
	}
	if err := env.db.UpsertThumbnail(th); err != nil {
		t.Fatal(err)
	}

	got := env.process(t, "lost.mp4")
	if got.ID != th.ID || got.ThumbnailPath != "lost.mp4.jpg" || got.Status != models.StatusSuccess {
		t.Errorf("row = %+v; want the same row with a new lost.mp4.jpg grid", got)
	}
	if !fileExists(filepath.Join(env.thumbsDir, "lost.mp4.jpg")) {
		t.Error("grid file not written")
	}
}
