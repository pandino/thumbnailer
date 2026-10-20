package scanner

import (
	"context"
	"database/sql"
	"fmt"
	"image"
	_ "image/jpeg"
	"os"
	"path/filepath"
	"time"

	"github.com/pandino/movie-thumbnailer-go/internal/models"
	"github.com/sirupsen/logrus"
)

// BootstrapDirName is the subdirectory of the thumbnails directory holding decisions saved by
// hand before decision logging existed: a snapshot of the database (*.db) and the grids of the
// thumbnails it had queued for deletion, as deleted/<id>.jpg.
const BootstrapDirName = "bootstrap"

// BackfillGeometry records the tile layout of grids that don't have one yet, both in the library
// and kept in the decided directory. A grid whose pixel size matches the current layout gets it;
// any other size (e.g. imported grids) is recorded as unknown so it isn't checked again.
func (s *Scanner) BackfillGeometry(ctx context.Context) error {
	thumbnails, err := s.db.GetThumbnailsWithoutGeometry()
	if err != nil {
		return fmt.Errorf("failed to get thumbnails without geometry: %w", err)
	}
	decided, err := s.db.GetDecidedGridsWithoutGeometry()
	if err != nil {
		return fmt.Errorf("failed to get decided grids without geometry: %w", err)
	}

	type grid struct {
		id   int64
		path string
	}
	grids := make([]grid, 0, len(thumbnails)+len(decided))
	for _, t := range thumbnails {
		grids = append(grids, grid{t.ID, filepath.Join(s.cfg.ThumbnailsDir, t.ThumbnailPath)})
	}
	for _, d := range decided {
		grids = append(grids, grid{d.ID, filepath.Join(s.cfg.ThumbnailsDir, DecidedDirName, d.GridFile)})
	}
	if len(grids) == 0 {
		return nil
	}

	var known, unknown int
	for i, g := range grids {
		if i%50 == 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
		}

		geometry, err := s.detectGeometry(g.path)
		if err != nil {
			s.log.WithError(err).WithField("thumbnail", g.path).Debug("Cannot read grid, geometry left for a later pass")
			continue
		}
		if err := s.db.SetGeometry(g.id, geometry); err != nil {
			return fmt.Errorf("failed to save geometry: %w", err)
		}
		if geometry.Known() {
			known++
		} else {
			unknown++
		}
	}

	s.log.WithFields(logrus.Fields{"known": known, "unknown": unknown}).Info("Recorded grid geometry")
	return nil
}

// detectGeometry returns the current grid layout if the image has its pixel size, or an unknown
// (zero) layout otherwise
func (s *Scanner) detectGeometry(path string) (models.GridGeometry, error) {
	f, err := os.Open(path)
	if err != nil {
		return models.GridGeometry{}, err
	}
	defer f.Close()

	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return models.GridGeometry{}, err
	}

	geometry := s.thumbnailer.Geometry()
	if w, h := geometry.ImageSize(); cfg.Width == w && cfg.Height == h {
		return geometry, nil
	}
	return models.GridGeometry{}, nil
}

// BackfillLabels reconstructs training data for the preference model from what existed before
// decisions were logged. Every step skips what is already recorded, so it can run after every
// scan until it has gone through everything.
func (s *Scanner) BackfillLabels(ctx context.Context) error {
	if err := s.backfillBootstrapDeletions(ctx); err != nil {
		return fmt.Errorf("bootstrap deletions: %w", err)
	}

	passed, err := s.db.BackfillViewedAsPassed()
	if err != nil {
		return fmt.Errorf("viewed thumbnails: %w", err)
	}
	if passed > 0 {
		s.log.WithField("count", passed).Info("Backfilled viewed thumbnails as passed")
	}

	if err := s.backfillArchive(ctx); err != nil {
		return fmt.Errorf("archive: %w", err)
	}
	return nil
}

// backfillBootstrapDeletions records the slideshow deletions found in the bootstrap snapshot.
// Thumbnails still waiting in the deletion queue only get their decision (cleanup keeps their
// grid later); ones already gone are restored as decided movies from the saved grids.
func (s *Scanner) backfillBootstrapDeletions(ctx context.Context) error {
	dir := filepath.Join(s.cfg.ThumbnailsDir, BootstrapDirName)
	snapshots, err := filepath.Glob(filepath.Join(dir, "*.db"))
	if err != nil || len(snapshots) == 0 {
		return err
	}

	for _, snapshot := range snapshots {
		if err := s.importBootstrapSnapshot(ctx, dir, snapshot); err != nil {
			return fmt.Errorf("%s: %w", filepath.Base(snapshot), err)
		}
	}
	return nil
}

func (s *Scanner) importBootstrapSnapshot(ctx context.Context, dir, snapshot string) error {
	snap, err := sql.Open("sqlite3", "file:"+snapshot+"?mode=ro")
	if err != nil {
		return err
	}
	defer snap.Close()

	rows, err := snap.Query(`
		SELECT id, movie_path, movie_filename, duration, width, height, file_size, source, updated_at
		FROM thumbnails
		WHERE status = 'deleted'`)
	if err != nil {
		return err
	}
	var deleted []*models.Thumbnail
	for rows.Next() {
		t := &models.Thumbnail{}
		if err := rows.Scan(&t.ID, &t.MoviePath, &t.MovieFilename, &t.Duration, &t.Width, &t.Height,
			&t.FileSize, &t.Source, &t.UpdatedAt); err != nil {
			rows.Close()
			return err
		}
		deleted = append(deleted, t)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	var recorded int
	for i, t := range deleted {
		if i%50 == 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
		}

		live, err := s.db.GetByID(t.ID)
		if err != nil {
			return err
		}
		switch {
		case live == nil:
			if err := s.restoreBootstrapGrid(dir, t); err != nil {
				s.log.WithError(err).WithField("movie", t.MovieFilename).Warn("Cannot restore bootstrap grid")
				continue
			}
		case live.Status != models.StatusDeleted:
			// Restored since the snapshot was taken: the deletion no longer stands
			continue
		}

		ok, err := s.db.RecordBackfillDecision(t.ID, t.MovieFilename, models.StageSlideshow, models.ActionDelete, t.UpdatedAt)
		if err != nil {
			return err
		}
		if ok {
			recorded++
		}
	}

	if recorded > 0 {
		s.log.WithFields(logrus.Fields{"snapshot": filepath.Base(snapshot), "count": recorded}).Info("Backfilled bootstrap deletions")
	}
	return nil
}

// restoreBootstrapGrid copies a saved grid into the decided directory and records its movie
func (s *Scanner) restoreBootstrapGrid(dir string, t *models.Thumbnail) error {
	exists, err := s.db.DecidedMovieExistsByID(t.ID)
	if err != nil || exists {
		return err
	}

	name := fmt.Sprintf("%d.jpg", t.ID)
	src := filepath.Join(dir, "deleted", name)
	dst := filepath.Join(s.cfg.ThumbnailsDir, DecidedDirName, name)
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	if err := s.copyFile(src, dst); err != nil {
		return err
	}

	geometry, err := s.detectGeometry(dst)
	if err != nil {
		return err
	}
	if err := s.db.SetGeometry(t.ID, geometry); err != nil {
		return err
	}
	return s.db.SaveDecidedMovie(t, name, t.UpdatedAt)
}

// backfillArchive generates grids for movies already in the archive directory and records them
// as archived after watching. A movie whose grid can't be generated is recorded without a grid,
// so it isn't retried on every scan.
func (s *Scanner) backfillArchive(ctx context.Context) error {
	entries, err := os.ReadDir(s.cfg.ArchiveDir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}

	var generated, failed int
	for _, entry := range entries {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		name := entry.Name()
		if entry.IsDir() || !s.isMovieFile(name) {
			continue
		}
		exists, err := s.db.DecidedMovieExists(name)
		if err != nil {
			return err
		}
		if exists {
			continue
		}
		live, err := s.db.GetByMovieFilename(name)
		if err != nil {
			return err
		}
		if live != nil {
			// Still (or again) in the library: its decision will be logged when it's made
			continue
		}

		if err := s.backfillArchivedMovie(ctx, filepath.Join(s.cfg.ArchiveDir, name)); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			s.log.WithError(err).WithField("movie", name).Warn("Cannot generate grid for archived movie")
			failed++
			continue
		}
		generated++
	}

	if generated > 0 || failed > 0 {
		s.log.WithFields(logrus.Fields{"generated": generated, "failed": failed}).Info("Backfilled archived movies")
	}
	return nil
}

func (s *Scanner) backfillArchivedMovie(ctx context.Context, moviePath string) error {
	info, err := os.Stat(moviePath)
	if err != nil {
		return err
	}
	id, err := s.db.AllocateID()
	if err != nil {
		return err
	}

	name := fmt.Sprintf("%d.jpg", id)
	dst := filepath.Join(s.cfg.ThumbnailsDir, DecidedDirName, name)
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}

	movie := &models.Thumbnail{
		ID:            id,
		MovieFilename: filepath.Base(moviePath),
		FileSize:      info.Size(),
		Source:        models.SourceGenerated,
	}
	start := time.Now()
	metadata, err := s.thumbnailer.GenerateGrid(ctx, moviePath, dst)
	if err != nil {
		os.Remove(dst)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// Remember the failure so the movie isn't retried on every scan
		if saveErr := s.db.SaveDecidedMovie(movie, "", info.ModTime()); saveErr != nil {
			s.log.WithError(saveErr).WithField("movie", moviePath).Error("Failed to record failed archived movie")
		}
		return err
	}

	movie.Duration = metadata.Duration
	movie.Width = metadata.Width
	movie.Height = metadata.Height
	if err := s.db.SetGeometry(id, s.thumbnailer.Geometry()); err != nil {
		return err
	}
	if err := s.db.SaveDecidedMovie(movie, name, info.ModTime()); err != nil {
		return err
	}
	if _, err := s.db.RecordBackfillDecision(id, movie.MovieFilename, models.StagePlayer, models.ActionArchive, info.ModTime()); err != nil {
		return err
	}

	s.log.WithFields(logrus.Fields{"movie": moviePath, "duration": time.Since(start)}).Info("Generated grid for archived movie")
	return nil
}
