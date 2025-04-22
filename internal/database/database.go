package database

import (
	"database/sql"
	"fmt"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/pandino/movie-thumbnailer-go/internal/models"
)

// DB represents the database connection and operations
type DB struct {
	db *sql.DB
}

// New creates a new database connection and initializes the schema
func New(dbPath string) (*DB, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Configure SQLite for better concurrency
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(time.Hour)

	// Initialize database schema
	if err := initSchema(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return &DB{db: db}, nil
}

// Close closes the database connection
func (d *DB) Close() error {
	return d.db.Close()
}

// Initialize the database schema
func initSchema(db *sql.DB) error {
	// Create thumbnails table with all required fields
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS thumbnails (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			movie_path TEXT NOT NULL UNIQUE,
			movie_filename TEXT NOT NULL,
			thumbnail_path TEXT,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			status TEXT DEFAULT 'pending',
			viewed INTEGER DEFAULT 0,
			width INTEGER DEFAULT 0,
			height INTEGER DEFAULT 0,
			duration REAL DEFAULT 0,
			error_message TEXT
		);
		
		-- Index for faster queries by status
		CREATE INDEX IF NOT EXISTS idx_thumbnails_status ON thumbnails(status);
		
		-- Index for faster queries by viewed status
		CREATE INDEX IF NOT EXISTS idx_thumbnails_viewed ON thumbnails(viewed);
		
		-- Trigger to update 'updated_at' on update
		CREATE TRIGGER IF NOT EXISTS thumbnails_updated_at 
		AFTER UPDATE ON thumbnails
		BEGIN
			UPDATE thumbnails SET updated_at = CURRENT_TIMESTAMP
			WHERE id = NEW.id;
		END;
	`)

	return err
}

// Add creates a new thumbnail record in the database
func (d *DB) Add(thumbnail *models.Thumbnail) error {
	_, err := d.db.Exec(`
		INSERT OR REPLACE INTO thumbnails 
		(movie_path, movie_filename, thumbnail_path, status, viewed, width, height, duration) 
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		thumbnail.MoviePath,
		thumbnail.MovieFilename,
		thumbnail.ThumbnailPath,
		thumbnail.Status,
		thumbnail.Viewed,
		thumbnail.Width,
		thumbnail.Height,
		thumbnail.Duration,
	)
	return err
}

// UpdateStatus updates the status of a thumbnail
func (d *DB) UpdateStatus(moviePath string, status string, errorMsg string) error {
	_, err := d.db.Exec(`
		UPDATE thumbnails 
		SET status = ?, error_message = ?
		WHERE movie_path = ?`,
		status, errorMsg, moviePath,
	)
	return err
}

// MarkAsViewed marks a thumbnail as viewed
func (d *DB) MarkAsViewed(thumbnailPath string) error {
	_, err := d.db.Exec(`
		UPDATE thumbnails 
		SET viewed = 1
		WHERE thumbnail_path = ?`,
		thumbnailPath,
	)
	return err
}

// GetByMoviePath retrieves a thumbnail by its movie path
func (d *DB) GetByMoviePath(moviePath string) (*models.Thumbnail, error) {
	thumbnail := &models.Thumbnail{}
	err := d.db.QueryRow(`
		SELECT 
			id, movie_path, movie_filename, thumbnail_path, 
			created_at, updated_at, status, viewed, 
			width, height, duration, error_message
		FROM thumbnails 
		WHERE movie_path = ?`,
		moviePath,
	).Scan(
		&thumbnail.ID, &thumbnail.MoviePath, &thumbnail.MovieFilename, &thumbnail.ThumbnailPath,
		&thumbnail.CreatedAt, &thumbnail.UpdatedAt, &thumbnail.Status, &thumbnail.Viewed,
		&thumbnail.Width, &thumbnail.Height, &thumbnail.Duration, &thumbnail.ErrorMessage,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return thumbnail, err
}

// GetByThumbnailPath retrieves a thumbnail by its thumbnail path
func (d *DB) GetByThumbnailPath(thumbnailPath string) (*models.Thumbnail, error) {
	thumbnail := &models.Thumbnail{}
	err := d.db.QueryRow(`
		SELECT 
			id, movie_path, movie_filename, thumbnail_path, 
			created_at, updated_at, status, viewed, 
			width, height, duration, error_message
		FROM thumbnails 
		WHERE thumbnail_path = ?`,
		thumbnailPath,
	).Scan(
		&thumbnail.ID, &thumbnail.MoviePath, &thumbnail.MovieFilename, &thumbnail.ThumbnailPath,
		&thumbnail.CreatedAt, &thumbnail.UpdatedAt, &thumbnail.Status, &thumbnail.Viewed,
		&thumbnail.Width, &thumbnail.Height, &thumbnail.Duration, &thumbnail.ErrorMessage,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return thumbnail, err
}

// GetUnviewedThumbnails retrieves all unviewed thumbnails
func (d *DB) GetUnviewedThumbnails() ([]*models.Thumbnail, error) {
	rows, err := d.db.Query(`
		SELECT 
			id, movie_path, movie_filename, thumbnail_path, 
			created_at, updated_at, status, viewed,
			width, height, duration, error_message
		FROM thumbnails 
		WHERE status = 'success' AND viewed = 0
		ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanThumbnails(rows)
}

// GetViewedThumbnails retrieves all viewed thumbnails
func (d *DB) GetViewedThumbnails() ([]*models.Thumbnail, error) {
	rows, err := d.db.Query(`
		SELECT 
			id, movie_path, movie_filename, thumbnail_path, 
			created_at, updated_at, status, viewed,
			width, height, duration, error_message
		FROM thumbnails 
		WHERE status = 'success' AND viewed = 1
		ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanThumbnails(rows)
}

// GetPendingThumbnails retrieves all pending thumbnails
func (d *DB) GetPendingThumbnails() ([]*models.Thumbnail, error) {
	rows, err := d.db.Query(`
		SELECT 
			id, movie_path, movie_filename, thumbnail_path, 
			created_at, updated_at, status, viewed,
			width, height, duration, error_message
		FROM thumbnails 
		WHERE status = 'pending'
		ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanThumbnails(rows)
}

// GetErrorThumbnails retrieves all thumbnails with errors
func (d *DB) GetErrorThumbnails() ([]*models.Thumbnail, error) {
	rows, err := d.db.Query(`
		SELECT 
			id, movie_path, movie_filename, thumbnail_path, 
			created_at, updated_at, status, viewed,
			width, height, duration, error_message
		FROM thumbnails 
		WHERE status = 'error'
		ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanThumbnails(rows)
}

// GetAllThumbnails retrieves all thumbnails
func (d *DB) GetAllThumbnails() ([]*models.Thumbnail, error) {
	rows, err := d.db.Query(`
		SELECT 
			id, movie_path, movie_filename, thumbnail_path, 
			created_at, updated_at, status, viewed,
			width, height, duration, error_message
		FROM thumbnails
		ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanThumbnails(rows)
}

// ResetViewedStatus resets the viewed status of all thumbnails
func (d *DB) ResetViewedStatus() (int64, error) {
	result, err := d.db.Exec(`
		UPDATE thumbnails 
		SET viewed = 0 
		WHERE viewed = 1`,
	)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// DeleteThumbnail deletes a thumbnail record
func (d *DB) DeleteThumbnail(moviePath string) error {
	_, err := d.db.Exec(`
		DELETE FROM thumbnails 
		WHERE movie_path = ?`,
		moviePath,
	)
	return err
}

// GetStats retrieves statistics about the thumbnails
func (d *DB) GetStats() (*models.Stats, error) {
	stats := &models.Stats{}

	err := d.db.QueryRow(`
		SELECT
			COUNT(*) as total,
			SUM(CASE WHEN status = 'success' THEN 1 ELSE 0 END) as success,
			SUM(CASE WHEN status = 'error' THEN 1 ELSE 0 END) as error,
			SUM(CASE WHEN status = 'pending' THEN 1 ELSE 0 END) as pending,
			SUM(CASE WHEN status = 'success' AND viewed = 1 THEN 1 ELSE 0 END) as viewed,
			SUM(CASE WHEN status = 'success' AND viewed = 0 THEN 1 ELSE 0 END) as unviewed
		FROM thumbnails
	`).Scan(
		&stats.Total,
		&stats.Success,
		&stats.Error,
		&stats.Pending,
		&stats.Viewed,
		&stats.Unviewed,
	)

	return stats, err
}

// Helper function to scan rows into thumbnail structs
func scanThumbnails(rows *sql.Rows) ([]*models.Thumbnail, error) {
	var thumbnails []*models.Thumbnail
	for rows.Next() {
		thumbnail := &models.Thumbnail{}
		err := rows.Scan(
			&thumbnail.ID, &thumbnail.MoviePath, &thumbnail.MovieFilename, &thumbnail.ThumbnailPath,
			&thumbnail.CreatedAt, &thumbnail.UpdatedAt, &thumbnail.Status, &thumbnail.Viewed,
			&thumbnail.Width, &thumbnail.Height, &thumbnail.Duration, &thumbnail.ErrorMessage,
		)
		if err != nil {
			return nil, err
		}
		thumbnails = append(thumbnails, thumbnail)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return thumbnails, nil
}

// CleanupOrphans removes database entries for missing movies
func (d *DB) CleanupOrphans() (int64, error) {
	result, err := d.db.Exec(`
		DELETE FROM thumbnails
		WHERE status = 'deleted'
	`)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// Vacuum optimizes the database
func (d *DB) Vacuum() error {
	_, err := d.db.Exec("VACUUM")
	return err
}
