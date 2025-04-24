package scanner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/pandino/movie-thumbnailer-go/internal/config"
	"github.com/pandino/movie-thumbnailer-go/internal/database"
	"github.com/pandino/movie-thumbnailer-go/internal/ffmpeg"
	"github.com/pandino/movie-thumbnailer-go/internal/models"
	"github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"
)

// Scanner handles scanning for movie files and managing thumbnails
type Scanner struct {
	cfg         *config.Config
	db          *database.DB
	thumbnailer *ffmpeg.Thumbnailer
	log         *logrus.Logger
	lock        sync.Mutex
	isScanning  bool
}

// New creates a new Scanner
func New(cfg *config.Config, db *database.DB, log *logrus.Logger) *Scanner {
	return &Scanner{
		cfg:         cfg,
		db:          db,
		thumbnailer: ffmpeg.New(cfg, log),
		log:         log,
		isScanning:  false,
	}
}

// IsScanning returns whether a scan is currently in progress
func (s *Scanner) IsScanning() bool {
	s.lock.Lock()
	defer s.lock.Unlock()
	return s.isScanning
}

// ScanMovies scans for movie files and generates thumbnails for new files
func (s *Scanner) ScanMovies(ctx context.Context) error {
	s.lock.Lock()
	if s.isScanning {
		s.lock.Unlock()
		return fmt.Errorf("scan already in progress")
	}
	s.isScanning = true
	s.lock.Unlock()

	defer func() {
		s.lock.Lock()
		s.isScanning = false
		s.lock.Unlock()
	}()

	s.log.Info("Starting movie scan")

	// Check if context is already done before starting
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		// Continue with scan
	}

	// Find all movie files
	movieFiles, err := s.findMovieFiles(ctx)
	if err != nil {
		return fmt.Errorf("failed to find movie files: %w", err)
	}

	s.log.Infof("Found %d movie files", len(movieFiles))

	// Process movies in parallel
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(s.cfg.MaxWorkers)

	for _, moviePath := range movieFiles {
		moviePath := moviePath // Capture variable for goroutine

		// Check if context is cancelled
		select {
		case <-gctx.Done():
			return gctx.Err()
		default:
			// Continue processing
		}

		// Check if thumbnail already exists and is successful
		movieFilename := filepath.Base(moviePath)
		thumbnail, err := s.db.GetByMoviePath(movieFilename)
		if err != nil {
			s.log.WithError(err).WithField("movie", moviePath).Error("Failed to check database")
			continue
		}

		// Skip if thumbnail already exists and is successful
		if thumbnail != nil && thumbnail.Status == "success" {
			continue
		}

		// Process the movie in parallel
		g.Go(func() error {
			return s.processMovie(gctx, moviePath)
		})
	}

	// Wait for all thumbnails to be processed
	if err := g.Wait(); err != nil {
		s.log.WithError(err).Error("Error during movie processing")
		return err
	}

	// Check context before continuing with cleanup
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		// Continue with cleanup
	}

	// Clean up orphaned entries and thumbnails
	if err := s.CleanupOrphans(ctx); err != nil {
		s.log.WithError(err).Error("Error during orphan cleanup")
		return err
	}

	s.log.Info("Movie scan completed successfully")
	return nil
}

// findMovieFiles returns a list of all movie files in the input directory
func (s *Scanner) findMovieFiles(ctx context.Context) ([]string, error) {
	var movieFiles []string

	// Check if input directory exists
	if _, err := os.Stat(s.cfg.MoviesDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("movies directory does not exist: %s", s.cfg.MoviesDir)
	}

	// Check for context cancellation
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
		// Continue processing
	}

	// Walk the directory and find movie files
	err := filepath.Walk(s.cfg.MoviesDir, func(path string, info os.FileInfo, err error) error {
		// Periodically check for context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			// Continue processing
		}

		if err != nil {
			return err
		}

		// Skip directories
		if info.IsDir() {
			return nil
		}

		// Check file extension
		ext := strings.ToLower(filepath.Ext(path))
		if ext == "" {
			return nil
		}

		// Remove the dot from extension
		ext = ext[1:]

		// Check if extension is in the allowed list
		for _, allowedExt := range s.cfg.FileExtensions {
			if ext == strings.ToLower(allowedExt) {
				movieFiles = append(movieFiles, path)
				break
			}
		}

		return nil
	})

	return movieFiles, err
}

// processMovie generates a thumbnail for a movie file
func (s *Scanner) processMovie(ctx context.Context, moviePath string) error {
	s.log.WithField("movie", moviePath).Info("Processing movie")

	// Check for context cancellation
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		// Continue processing
	}

	// Generate expected thumbnail filename
	movieFilename := filepath.Base(moviePath)
	thumbnailFilename := strings.TrimSuffix(movieFilename, filepath.Ext(movieFilename)) + ".jpg"
	thumbnailPath := filepath.Join(s.cfg.ThumbnailsDir, thumbnailFilename)

	// Initialize a thumbnail record - will be either inserted or updated
	thumbnail := &models.Thumbnail{
		MoviePath:     movieFilename,
		MovieFilename: movieFilename,
		ThumbnailPath: thumbnailFilename,
		Status:        models.StatusPending,
	}

	// Check if thumbnail file already exists on disk
	fileExists := false
	if _, err := os.Stat(thumbnailPath); err == nil {
		fileExists = true
	}

	// Get existing record if any - this doesn't cause an extra query because
	// our UpsertThumbnail implementation will do this check internally
	existingThumbnail, err := s.db.GetByMoviePath(movieFilename)
	if err != nil {
		s.log.WithError(err).WithField("movie", moviePath).Error("Failed to check database")
		return fmt.Errorf("failed to check database for movie %s: %w", moviePath, err)
	}

	// If thumbnail exists in DB and is successful, and the file exists, nothing to do
	if existingThumbnail != nil && existingThumbnail.Status == models.StatusSuccess && fileExists {
		s.log.WithField("movie", moviePath).Debug("Thumbnail already exists and is successful, skipping")
		return nil
	}

	// If we have an existing record, preserve some values
	if existingThumbnail != nil {
		thumbnail.ID = existingThumbnail.ID
		thumbnail.CreatedAt = existingThumbnail.CreatedAt
		thumbnail.Viewed = existingThumbnail.Viewed
	}

	// Save the pending status - this ensures other processes know this movie is being processed
	// and establishes the record in the database
	if err := s.db.UpsertThumbnail(thumbnail); err != nil {
		s.log.WithError(err).WithField("movie", moviePath).Error("Failed to save pending status")
		return fmt.Errorf("failed to save pending status for movie %s: %w", moviePath, err)
	}

	// Check for context cancellation before creating thumbnail
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		// Continue processing
	}

	// Generate the thumbnail
	generatedThumbnail, err := s.thumbnailer.CreateThumbnail(ctx, moviePath, s.db)
	if err != nil {
		s.log.WithError(err).WithField("movie", moviePath).Error("Failed to create thumbnail")

		// Update status to error
		thumbnail.Status = models.StatusError
		thumbnail.ErrorMessage = fmt.Sprintf("Failed to create thumbnail: %v", err)

		// Save the error status
		if upsertErr := s.db.UpsertThumbnail(thumbnail); upsertErr != nil {
			s.log.WithError(upsertErr).WithField("movie", moviePath).Error("Failed to save error status")
		}

		return fmt.Errorf("failed to create thumbnail for movie %s: %w", moviePath, err)
	}

	// Update our record with the generated thumbnail data
	thumbnail.Status = generatedThumbnail.Status
	thumbnail.Width = generatedThumbnail.Width
	thumbnail.Height = generatedThumbnail.Height
	thumbnail.Duration = generatedThumbnail.Duration
	thumbnail.ErrorMessage = generatedThumbnail.ErrorMessage

	// Save the final status
	if err := s.db.UpsertThumbnail(thumbnail); err != nil {
		s.log.WithError(err).WithField("movie", moviePath).Error("Failed to save final status")
		return fmt.Errorf("failed to save final status for movie %s: %w", moviePath, err)
	}

	s.log.WithFields(logrus.Fields{
		"movie":      moviePath,
		"status":     thumbnail.Status,
		"duration":   thumbnail.Duration,
		"resolution": fmt.Sprintf("%dx%d", thumbnail.Width, thumbnail.Height),
	}).Info("Processed movie")

	return nil
}

// CleanupOrphans removes database entries for missing movies, orphaned thumbnails,
// and processes items marked for deletion
func (s *Scanner) CleanupOrphans(ctx context.Context) error {
	s.log.Info("Cleaning up orphaned entries, thumbnails, and processing deletion queue")

	// First, process items marked for deletion
	if err := s.processDeletedItems(ctx); err != nil {
		s.log.WithError(err).Error("Error processing deleted items")
		// Check if the context is done before continuing
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			// Continue with other cleanup steps
		}
	}

	// Get all thumbnails from database (except deleted ones that were just processed)
	thumbnails, err := s.db.GetAllThumbnails()
	if err != nil {
		return fmt.Errorf("failed to get thumbnails: %w", err)
	}

	var orphanedCount, missingCount int

	// Check each thumbnail
	for i, thumbnail := range thumbnails {
		// Periodically check for context cancellation
		if i%100 == 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
				// Continue processing
			}
		}

		// Skip already deleted thumbnails
		if thumbnail.Status == models.StatusDeleted {
			continue
		}

		// Check if movie file exists
		moviePath := filepath.Join(s.cfg.MoviesDir, thumbnail.MoviePath)
		if _, err := os.Stat(moviePath); os.IsNotExist(err) {
			s.log.WithField("movie", moviePath).Info("Movie file not found, removing from database")

			// Delete the thumbnail if it exists
			if thumbnail.ThumbnailPath != "" {
				thumbnailPath := filepath.Join(s.cfg.ThumbnailsDir, thumbnail.ThumbnailPath)
				if _, err := os.Stat(thumbnailPath); err == nil {
					if err := os.Remove(thumbnailPath); err != nil {
						s.log.WithError(err).WithField("thumbnail", thumbnailPath).Error("Failed to delete orphaned thumbnail")
					} else {
						s.log.WithField("thumbnail", thumbnailPath).Info("Deleted orphaned thumbnail")
						orphanedCount++
					}
				}
			}

			// Remove from database
			if err := s.db.DeleteThumbnail(thumbnail.MoviePath); err != nil {
				s.log.WithError(err).WithField("movie", thumbnail.MoviePath).Error("Failed to delete from database")
			} else {
				missingCount++
			}
		}
	}

	s.log.Infof("Cleanup completed: removed %d database entries for missing movies and deleted %d orphaned thumbnails", missingCount, orphanedCount)

	// Check context before continuing
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		// Continue processing
	}

	// Find orphaned thumbnails (thumbnails without database entries)
	return s.cleanupOrphanedThumbnails(ctx)
}

// cleanupOrphanedThumbnails removes thumbnail files that don't have database entries
func (s *Scanner) cleanupOrphanedThumbnails(ctx context.Context) error {
	// Get all thumbnails from the database
	thumbnails, err := s.db.GetAllThumbnails()
	if err != nil {
		return fmt.Errorf("failed to get thumbnails: %w", err)
	}

	// Build a map of thumbnail filenames for quick lookup
	thumbnailMap := make(map[string]bool)
	for _, thumbnail := range thumbnails {
		if thumbnail.ThumbnailPath != "" {
			thumbnailMap[thumbnail.ThumbnailPath] = true
		}
	}

	// Check all files in the thumbnails directory
	files, err := os.ReadDir(s.cfg.ThumbnailsDir)
	if err != nil {
		return fmt.Errorf("failed to read thumbnails directory: %w", err)
	}

	var orphanedCount int

	for i, file := range files {
		// Check for context cancellation periodically
		if i%100 == 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
				// Continue processing
			}
		}

		if file.IsDir() {
			continue
		}

		// Skip non-jpg files
		if !strings.HasSuffix(strings.ToLower(file.Name()), ".jpg") {
			continue
		}

		// Check if file is in the database
		if !thumbnailMap[file.Name()] {
			s.log.WithField("thumbnail", file.Name()).Info("Orphaned thumbnail found, deleting")

			// Delete the file
			thumbnailPath := filepath.Join(s.cfg.ThumbnailsDir, file.Name())
			if err := os.Remove(thumbnailPath); err != nil {
				s.log.WithError(err).WithField("thumbnail", thumbnailPath).Error("Failed to delete orphaned thumbnail")
			} else {
				orphanedCount++
			}
		}
	}

	s.log.Infof("Thumbnail cleanup completed: deleted %d orphaned thumbnail files", orphanedCount)
	return nil
}

// processDeletedItems processes all items marked for deletion
func (s *Scanner) processDeletedItems(ctx context.Context) error {
	// Get all thumbnails marked for deletion
	thumbnails, err := s.db.GetDeletedThumbnails()
	if err != nil {
		return fmt.Errorf("failed to get deleted thumbnails: %w", err)
	}

	s.log.Infof("Processing %d items marked for deletion", len(thumbnails))

	for i, thumbnail := range thumbnails {
		// Check for context cancellation periodically
		if i%10 == 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
				// Continue processing
			}
		}

		// Delete the thumbnail file if it exists
		if thumbnail.ThumbnailPath != "" {
			thumbnailPath := filepath.Join(s.cfg.ThumbnailsDir, thumbnail.ThumbnailPath)
			if _, err := os.Stat(thumbnailPath); err == nil {
				if err := os.Remove(thumbnailPath); err != nil {
					s.log.WithError(err).WithField("thumbnail", thumbnailPath).Error("Failed to delete thumbnail file")
				} else {
					s.log.WithField("thumbnail", thumbnailPath).Info("Deleted thumbnail file")
				}
			}
		}

		// Delete the movie file if it exists
		fullMoviePath := filepath.Join(s.cfg.MoviesDir, thumbnail.MoviePath)
		if _, err := os.Stat(fullMoviePath); err == nil {
			if err := os.Remove(fullMoviePath); err != nil {
				s.log.WithError(err).WithField("movie", fullMoviePath).Error("Failed to delete movie file")
				// Don't remove from database on error so we can retry later
				continue
			}
			s.log.WithField("movie", fullMoviePath).Info("Deleted movie file")
		}

		// Remove from database
		if err := s.db.DeleteThumbnail(thumbnail.MoviePath); err != nil {
			s.log.WithError(err).WithField("movie", thumbnail.MoviePath).Error("Failed to delete from database")
		}
	}

	return nil
}

// ResetViewedStatus resets the viewed status of all thumbnails
func (s *Scanner) ResetViewedStatus() (int64, error) {
	s.log.Info("Resetting viewed status for all thumbnails")
	count, err := s.db.ResetViewedStatus()
	if err != nil {
		return 0, fmt.Errorf("failed to reset viewed status: %w", err)
	}
	s.log.Infof("Reset viewed status for %d thumbnails", count)
	return count, nil
}

// GetStats returns statistics about the thumbnails
func (s *Scanner) GetStats() (*models.Stats, error) {
	return s.db.GetStats()
}

// DeleteMovie deletes a movie and its thumbnail
func (s *Scanner) DeleteMovie(ctx context.Context, moviePath string) error {
	s.log.WithField("movie", moviePath).Info("Deleting movie and thumbnail")

	// Get the thumbnail record
	thumbnail, err := s.db.GetByMoviePath(filepath.Base(moviePath))
	if err != nil {
		return fmt.Errorf("failed to get thumbnail: %w", err)
	}

	if thumbnail == nil {
		return fmt.Errorf("movie not found in database: %s", moviePath)
	}

	// Check for context cancellation
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		// Continue processing
	}

	// Delete the thumbnail file if it exists
	if thumbnail.ThumbnailPath != "" {
		thumbnailPath := filepath.Join(s.cfg.ThumbnailsDir, thumbnail.ThumbnailPath)
		if _, err := os.Stat(thumbnailPath); err == nil {
			if err := os.Remove(thumbnailPath); err != nil {
				s.log.WithError(err).WithField("thumbnail", thumbnailPath).Error("Failed to delete thumbnail file")
			} else {
				s.log.WithField("thumbnail", thumbnailPath).Info("Deleted thumbnail file")
			}
		}
	}

	// Check for context cancellation
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		// Continue processing
	}

	// Delete the movie file if it exists
	fullMoviePath := filepath.Join(s.cfg.MoviesDir, thumbnail.MoviePath)
	if _, err := os.Stat(fullMoviePath); err == nil {
		if err := os.Remove(fullMoviePath); err != nil {
			s.log.WithError(err).WithField("movie", fullMoviePath).Error("Failed to delete movie file")
			return fmt.Errorf("failed to delete movie file: %w", err)
		}
		s.log.WithField("movie", fullMoviePath).Info("Deleted movie file")
	}

	// Remove from database
	if err := s.db.DeleteThumbnail(thumbnail.MoviePath); err != nil {
		return fmt.Errorf("failed to delete from database: %w", err)
	}

	return nil
}
