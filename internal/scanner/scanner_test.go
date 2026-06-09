package scanner

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/pandino/movie-thumbnailer-go/internal/config"
	"github.com/sirupsen/logrus"
)

func newTestScanner(dirs []string) *Scanner {
	cfg := &config.Config{
		MoviesDirs:     dirs,
		FileExtensions: []string{"mp4", "mkv"},
	}
	log := logrus.New()
	log.SetOutput(os.Stderr)
	return &Scanner{cfg: cfg, log: log}
}

func TestResolveMoviePaths(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()

	touch(t, filepath.Join(dir1, "movie.mp4"))
	touch(t, filepath.Join(dir2, "movie.mp4"))
	touch(t, filepath.Join(dir1, "only1.mp4"))

	s := newTestScanner([]string{dir1, dir2})

	t.Run("present in both", func(t *testing.T) {
		paths := s.resolveMoviePaths("movie.mp4")
		if len(paths) != 2 {
			t.Fatalf("expected 2 paths, got %v", paths)
		}
	})

	t.Run("present in one", func(t *testing.T) {
		paths := s.resolveMoviePaths("only1.mp4")
		if len(paths) != 1 || paths[0] != filepath.Join(dir1, "only1.mp4") {
			t.Fatalf("unexpected paths: %v", paths)
		}
	})

	t.Run("absent from all", func(t *testing.T) {
		paths := s.resolveMoviePaths("ghost.mp4")
		if len(paths) != 0 {
			t.Fatalf("expected no paths, got %v", paths)
		}
	})
}

func TestFindMovieFiles_Deduplication(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()

	touch(t, filepath.Join(dir1, "shared.mp4"))
	touch(t, filepath.Join(dir2, "shared.mp4"))
	touch(t, filepath.Join(dir1, "unique1.mkv"))
	touch(t, filepath.Join(dir2, "unique2.mp4"))

	s := newTestScanner([]string{dir1, dir2})

	files, err := s.findMovieFiles(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	basenames := make([]string, len(files))
	for i, f := range files {
		basenames[i] = filepath.Base(f)
	}
	sort.Strings(basenames)

	want := []string{"shared.mp4", "unique1.mkv", "unique2.mp4"}
	if len(basenames) != len(want) {
		t.Fatalf("got %v, want %v", basenames, want)
	}
	for i := range basenames {
		if basenames[i] != want[i] {
			t.Errorf("index %d: got %q, want %q", i, basenames[i], want[i])
		}
	}

	// shared.mp4 should come from dir1 (first volume wins)
	for _, f := range files {
		if filepath.Base(f) == "shared.mp4" && filepath.Dir(f) != dir1 {
			t.Errorf("shared.mp4 should resolve to dir1, got %s", f)
		}
	}
}

func TestFindMovieFiles_MissingDirSkipped(t *testing.T) {
	dir1 := t.TempDir()
	touch(t, filepath.Join(dir1, "a.mp4"))

	s := newTestScanner([]string{dir1, "/nonexistent/path"})

	files, err := s.findMovieFiles(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || filepath.Base(files[0]) != "a.mp4" {
		t.Errorf("unexpected files: %v", files)
	}
}

func touch(t *testing.T, path string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
}
