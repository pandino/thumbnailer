package scanner

import (
	"context"
	"database/sql"
	"fmt"
	"image"
	"image/jpeg"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/pandino/movie-thumbnailer-go/internal/config"
	"github.com/pandino/movie-thumbnailer-go/internal/database"
	"github.com/pandino/movie-thumbnailer-go/internal/ffmpeg"
	"github.com/pandino/movie-thumbnailer-go/internal/metrics"
	"github.com/pandino/movie-thumbnailer-go/internal/models"
	"github.com/sirupsen/logrus"
)

// metrics.New registers global collectors, so all tests share one instance
var (
	testMetricsOnce sync.Once
	testMetrics     *metrics.Metrics
)

type decisionTestEnv struct {
	s          *Scanner
	db         *database.DB
	moviesDir  string
	thumbsDir  string
	archiveDir string
}

func newDecisionTestEnv(t *testing.T) *decisionTestEnv {
	t.Helper()
	testMetricsOnce.Do(func() { testMetrics = metrics.New() })

	env := &decisionTestEnv{
		moviesDir:  t.TempDir(),
		thumbsDir:  t.TempDir(),
		archiveDir: t.TempDir(),
	}
	db, err := database.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	env.db = db

	cfg := &config.Config{
		MoviesDirs:     []string{env.moviesDir},
		ThumbnailsDir:  env.thumbsDir,
		ArchiveDir:     env.archiveDir,
		GridCols:       8,
		GridRows:       4,
		FileExtensions: []string{"mp4", "mkv"},
	}
	log := logrus.New()
	log.SetLevel(logrus.ErrorLevel)
	env.s = &Scanner{cfg: cfg, db: db, log: log, metrics: testMetrics, thumbnailer: ffmpeg.New(cfg, log, testMetrics)}
	return env
}

// writeJPEG writes a blank JPEG of the given size
func writeJPEG(t *testing.T, path string, w, h int) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := jpeg.Encode(f, image.NewRGBA(image.Rect(0, 0, w, h)), nil); err != nil {
		t.Fatal(err)
	}
}

// addMovie creates a movie file with a standard-size grid and its thumbnail row
func (env *decisionTestEnv) addMovie(t *testing.T, name string) *models.Thumbnail {
	t.Helper()
	touch(t, filepath.Join(env.moviesDir, name))
	th := &models.Thumbnail{
		MoviePath:     name,
		MovieFilename: name,
		ThumbnailPath: name + ".jpg",
		Status:        models.StatusSuccess,
		Duration:      42,
	}
	writeJPEG(t, filepath.Join(env.thumbsDir, th.ThumbnailPath), 2596, 740)
	if err := env.db.UpsertThumbnail(th); err != nil {
		t.Fatal(err)
	}
	return th
}

func TestCleanupKeepsDecidedGrids(t *testing.T) {
	env := newDecisionTestEnv(t)
	deleted := env.addMovie(t, "deleted.mp4")
	archived := env.addMovie(t, "archived.mp4")
	kept := env.addMovie(t, "kept.mp4")
	env.db.MarkForDeletionByID(deleted.ID)
	env.db.MarkForArchival(archived.ID, models.StagePlayer)

	if err := env.s.CleanupOrphans(context.Background()); err != nil {
		t.Fatalf("CleanupOrphans: %v", err)
	}

	// Movies are gone or archived, grids moved to decided/<id>.jpg
	if _, err := os.Stat(filepath.Join(env.moviesDir, "deleted.mp4")); !os.IsNotExist(err) {
		t.Error("deleted movie still on disk")
	}
	if _, err := os.Stat(filepath.Join(env.archiveDir, "archived.mp4")); err != nil {
		t.Error("archived movie not in archive")
	}
	for _, th := range []*models.Thumbnail{deleted, archived} {
		if _, err := os.Stat(filepath.Join(env.thumbsDir, th.ThumbnailPath)); !os.IsNotExist(err) {
			t.Errorf("%s: grid left in thumbnails dir", th.MovieFilename)
		}
		if _, err := os.Stat(filepath.Join(env.thumbsDir, DecidedDirName, filepathID(th.ID))); err != nil {
			t.Errorf("%s: decided grid missing: %v", th.MovieFilename, err)
		}
	}
	// The undecided grid is untouched, and the orphan cleanup didn't touch decided/
	if _, err := os.Stat(filepath.Join(env.thumbsDir, kept.ThumbnailPath)); err != nil {
		t.Error("kept grid was removed")
	}

	items, err := env.db.ListMLItems("", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[int64]models.MLItem{}
	for _, it := range items {
		byID[it.ID] = it
	}
	if it := byID[deleted.ID]; it.Live || it.GridURL != "decided/"+filepathID(deleted.ID) {
		t.Errorf("deleted item = %+v", it)
	}
	if it := byID[kept.ID]; !it.Live {
		t.Errorf("kept item = %+v", it)
	}
}

func filepathID(id int64) string {
	return fmt.Sprintf("%d.jpg", id)
}

func TestBackfillGeometry(t *testing.T) {
	env := newDecisionTestEnv(t)
	standard := env.addMovie(t, "standard.mp4")
	imported := env.addMovie(t, "imported.mp4")
	writeJPEG(t, filepath.Join(env.thumbsDir, imported.ThumbnailPath), 1200, 800)

	if err := env.s.BackfillGeometry(context.Background()); err != nil {
		t.Fatal(err)
	}
	items, _ := env.db.ListMLItems("", 0, 10)
	for _, it := range items {
		switch it.ID {
		case standard.ID:
			if it.Geometry != env.s.thumbnailer.Geometry() {
				t.Errorf("standard geometry = %+v", it.Geometry)
			}
		case imported.ID:
			if it.Geometry.Known() {
				t.Errorf("imported geometry = %+v; want unknown", it.Geometry)
			}
		}
	}
	// Both are recorded now, so nothing is left to check
	rest, _ := env.db.GetThumbnailsWithoutGeometry()
	if len(rest) != 0 {
		t.Errorf("%d thumbnails still without geometry", len(rest))
	}
}

func TestBackfillBootstrapDeletions(t *testing.T) {
	env := newDecisionTestEnv(t)

	// Live: two movies. Snapshot: those two plus one already processed, all queued for deletion.
	queued := env.addMovie(t, "queued.mp4")
	restored := env.addMovie(t, "restored.mp4")
	env.db.MarkForDeletionByID(queued.ID) // still in the queue (and already logged live)
	gone := &models.Thumbnail{ID: 900, MovieFilename: "gone.mp4", Duration: 99, Source: models.SourceGenerated}

	bootstrap := filepath.Join(env.thumbsDir, BootstrapDirName)
	if err := os.MkdirAll(filepath.Join(bootstrap, "deleted"), 0755); err != nil {
		t.Fatal(err)
	}
	writeJPEG(t, filepath.Join(bootstrap, "deleted", "900.jpg"), 2596, 740)
	writeSnapshot(t, filepath.Join(bootstrap, "thumbnailer-2026-10-20.db"), []snapshotRow{
		{gone.ID, "gone.mp4", models.StatusDeleted},
		{queued.ID, "queued.mp4", models.StatusDeleted},
		{restored.ID, "restored.mp4", models.StatusDeleted},
	})

	for i := 0; i < 2; i++ { // idempotent
		if err := env.s.backfillBootstrapDeletions(context.Background()); err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
	}

	decisions, _ := env.db.ListDecisions(0, 100)
	got := map[int64][]string{}
	for _, d := range decisions {
		got[d.ThumbnailID] = append(got[d.ThumbnailID], d.Source+":"+d.Action)
	}
	if len(got[gone.ID]) != 1 || got[gone.ID][0] != "backfill:delete" {
		t.Errorf("gone decisions = %v", got[gone.ID])
	}
	if len(got[queued.ID]) != 1 || got[queued.ID][0] != "live:delete" {
		t.Errorf("queued decisions = %v; the live decision must not be duplicated", got[queued.ID])
	}
	if len(got[restored.ID]) != 0 {
		t.Errorf("restored decisions = %v; restored since the snapshot, so none", got[restored.ID])
	}

	// The processed movie's grid is restored as a decided item with geometry
	items, _ := env.db.ListMLItems("", 0, 10)
	var found bool
	for _, it := range items {
		if it.ID == gone.ID {
			found = true
			if it.Live || it.GridURL != "decided/900.jpg" || !it.Geometry.Known() {
				t.Errorf("gone item = %+v", it)
			}
		}
	}
	if !found {
		t.Errorf("gone movie not listed: %+v", items)
	}
}

type snapshotRow struct {
	id       int64
	filename string
	status   string
}

// writeSnapshot creates a database snapshot with thumbnail rows at fixed IDs
func writeSnapshot(t *testing.T, path string, rows []snapshotRow) {
	t.Helper()
	schema, err := database.New(path)
	if err != nil {
		t.Fatal(err)
	}
	schema.Close()

	snap, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	defer snap.Close()
	for _, r := range rows {
		if _, err := snap.Exec(`
			INSERT INTO thumbnails (id, movie_path, movie_filename, thumbnail_path, status, duration)
			VALUES (?, ?, ?, ?, ?, 99)`,
			r.id, r.filename, r.filename, r.filename+".jpg", r.status,
		); err != nil {
			t.Fatal(err)
		}
	}
}

func TestBackfillArchive(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	env := newDecisionTestEnv(t)

	// A 40 s test video (the grid skips the first 30 s) and a file ffmpeg can't read
	movie := filepath.Join(env.archiveDir, "liked.mp4")
	cmd := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "testsrc=size=320x180:rate=10",
		"-t", "40", "-g", "10", "-pix_fmt", "yuv420p", movie)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("cannot create test video: %v\n%s", err, out)
	}
	if err := os.WriteFile(filepath.Join(env.archiveDir, "broken.mkv"), []byte("not a movie"), 0644); err != nil {
		t.Fatal(err)
	}
	touch(t, filepath.Join(env.archiveDir, "notes.txt"))

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	for i := 0; i < 2; i++ { // the second run must not regenerate or retry anything
		if err := env.s.backfillArchive(ctx); err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
	}

	items, _ := env.db.ListMLItems("", 0, 10)
	if len(items) != 1 || items[0].MovieFilename != "liked.mp4" || items[0].Live || !items[0].Geometry.Known() {
		t.Fatalf("items = %+v; want one decided grid for liked.mp4", items)
	}
	grid := filepath.Join(env.thumbsDir, items[0].GridURL)
	f, err := os.Open(grid)
	if err != nil {
		t.Fatal(err)
	}
	cfg, _, err := image.DecodeConfig(f)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	if w, h := items[0].Geometry.ImageSize(); cfg.Width != w || cfg.Height != h {
		t.Errorf("grid is %dx%d; geometry says %dx%d", cfg.Width, cfg.Height, w, h)
	}

	decisions, _ := env.db.ListDecisions(0, 10)
	if len(decisions) != 1 || decisions[0].Stage != models.StagePlayer || decisions[0].Action != models.ActionArchive {
		t.Errorf("decisions = %+v; want one player/archive", decisions)
	}
	// The unreadable file is remembered without a grid
	if exists, _ := env.db.DecidedMovieExists("broken.mkv"); !exists {
		t.Error("failed archive movie not recorded, it would be retried every scan")
	}
}
