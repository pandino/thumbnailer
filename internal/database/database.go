package database

import (
	"crypto/rand"
	"database/sql"
	"fmt"
	"math/big"
	mathrand "math/rand"
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
			file_size INTEGER DEFAULT 0,
			error_message TEXT NOT NULL DEFAULT '',
			source TEXT DEFAULT 'generated'
		);
		
		-- Index for faster queries by status
		CREATE INDEX IF NOT EXISTS idx_thumbnails_status ON thumbnails(status);
		
		-- Index for faster queries by viewed status
		CREATE INDEX IF NOT EXISTS idx_thumbnails_viewed ON thumbnails(viewed);
		
		-- Index for faster queries by source
		CREATE INDEX IF NOT EXISTS idx_thumbnails_source ON thumbnails(source);
		
		-- Trigger to update 'updated_at' on update
		CREATE TRIGGER IF NOT EXISTS thumbnails_updated_at 
		AFTER UPDATE ON thumbnails
		BEGIN
			UPDATE thumbnails SET updated_at = CURRENT_TIMESTAMP
			WHERE id = NEW.id;
		END;
	`)

	if err != nil {
		return err
	}

	return nil
}

// Add creates a new thumbnail record in the database
func (d *DB) Add(thumbnail *models.Thumbnail) error {
	// Set default source if not specified
	if thumbnail.Source == "" {
		thumbnail.Source = models.SourceGenerated
	}

	_, err := d.db.Exec(`
		INSERT OR REPLACE INTO thumbnails 
		(movie_path, movie_filename, thumbnail_path, status, viewed, width, height, duration, file_size, error_message, source) 
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		thumbnail.MoviePath,
		thumbnail.MovieFilename,
		thumbnail.ThumbnailPath,
		thumbnail.Status,
		thumbnail.Viewed,
		thumbnail.Width,
		thumbnail.Height,
		thumbnail.Duration,
		thumbnail.FileSize,
		thumbnail.ErrorMessage,
		thumbnail.Source,
	)
	return err
}

// UpsertThumbnail performs a true upsert operation (insert or update) in a single query
func (d *DB) UpsertThumbnail(thumbnail *models.Thumbnail) error {
	// Set default source if not specified
	if thumbnail.Source == "" {
		thumbnail.Source = models.SourceGenerated
	}

	// SQLite supports "INSERT OR REPLACE" syntax for upsert operations
	// For this to work correctly, we need to make sure movie_path is set as UNIQUE in the schema
	_, err := d.db.Exec(`
        INSERT OR REPLACE INTO thumbnails 
        (id, movie_path, movie_filename, thumbnail_path, status, viewed, 
         width, height, duration, file_size, error_message, source,
         created_at, updated_at) 
        VALUES 
        (
            (SELECT id FROM thumbnails WHERE movie_path = ?), 
            ?, ?, ?, ?, ?, 
            ?, ?, ?, ?, ?, ?,
            COALESCE((SELECT created_at FROM thumbnails WHERE movie_path = ?), CURRENT_TIMESTAMP),
            CURRENT_TIMESTAMP
        )`,
		thumbnail.MoviePath, // For the subquery to find existing ID
		thumbnail.MoviePath,
		thumbnail.MovieFilename,
		thumbnail.ThumbnailPath,
		thumbnail.Status,
		thumbnail.Viewed,
		thumbnail.Width,
		thumbnail.Height,
		thumbnail.Duration,
		thumbnail.FileSize,
		thumbnail.ErrorMessage,
		thumbnail.Source,
		thumbnail.MoviePath, // For the created_at preservation
	)

	if err != nil {
		return fmt.Errorf("failed to upsert thumbnail: %w", err)
	}

	// If this was a new record, we should fetch the ID
	if thumbnail.ID == 0 {
		var id int64
		err := d.db.QueryRow("SELECT id FROM thumbnails WHERE movie_path = ?", thumbnail.MoviePath).Scan(&id)
		if err == nil {
			thumbnail.ID = id
		}
	}

	return nil
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

// MarkAsViewedByID marks a thumbnail as viewed by ID
func (d *DB) MarkAsViewedByID(id int64) error {
	_, err := d.db.Exec(`
		UPDATE thumbnails 
		SET viewed = 1
		WHERE id = ?`,
		id,
	)
	return err
}

// MarkForDeletionByID marks a thumbnail for deletion by ID without actually deleting it
func (d *DB) MarkForDeletionByID(id int64) error {
	_, err := d.db.Exec(`
		UPDATE thumbnails 
		SET status = 'deleted'
		WHERE id = ?`,
		id,
	)
	return err
}

// GetByID retrieves a thumbnail by its ID
func (d *DB) GetByID(id int64) (*models.Thumbnail, error) {
	thumbnail := &models.Thumbnail{}
	err := d.db.QueryRow(`
		SELECT 
			id, movie_path, movie_filename, thumbnail_path, 
			created_at, updated_at, status, viewed, 
			width, height, duration, file_size, error_message, source
		FROM thumbnails 
		WHERE id = ?`,
		id,
	).Scan(
		&thumbnail.ID, &thumbnail.MoviePath, &thumbnail.MovieFilename, &thumbnail.ThumbnailPath,
		&thumbnail.CreatedAt, &thumbnail.UpdatedAt, &thumbnail.Status, &thumbnail.Viewed,
		&thumbnail.Width, &thumbnail.Height, &thumbnail.Duration, &thumbnail.FileSize, &thumbnail.ErrorMessage, &thumbnail.Source,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("error fetching thumbnail with ID %d: %w", id, err)
	}
	return thumbnail, nil
}

// GetByMoviePath retrieves a thumbnail by its movie path
func (d *DB) GetByMoviePath(moviePath string) (*models.Thumbnail, error) {
	thumbnail := &models.Thumbnail{}
	err := d.db.QueryRow(`
		SELECT 
			id, movie_path, movie_filename, thumbnail_path, 
			created_at, updated_at, status, viewed, 
			width, height, duration, file_size, error_message, source
		FROM thumbnails 
		WHERE movie_path = ?`,
		moviePath,
	).Scan(
		&thumbnail.ID, &thumbnail.MoviePath, &thumbnail.MovieFilename, &thumbnail.ThumbnailPath,
		&thumbnail.CreatedAt, &thumbnail.UpdatedAt, &thumbnail.Status, &thumbnail.Viewed,
		&thumbnail.Width, &thumbnail.Height, &thumbnail.Duration, &thumbnail.FileSize, &thumbnail.ErrorMessage, &thumbnail.Source,
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
			width, height, duration, file_size, error_message, source
		FROM thumbnails 
		WHERE thumbnail_path = ?`,
		thumbnailPath,
	).Scan(
		&thumbnail.ID, &thumbnail.MoviePath, &thumbnail.MovieFilename, &thumbnail.ThumbnailPath,
		&thumbnail.CreatedAt, &thumbnail.UpdatedAt, &thumbnail.Status, &thumbnail.Viewed,
		&thumbnail.Width, &thumbnail.Height, &thumbnail.Duration, &thumbnail.FileSize, &thumbnail.ErrorMessage, &thumbnail.Source,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return thumbnail, err
}

// GetRandomUnviewedThumbnail gets a random unviewed thumbnail
func (d *DB) GetRandomUnviewedThumbnail() (*models.Thumbnail, error) {
	// First, count the total number of unviewed thumbnails
	var count int
	err := d.db.QueryRow(`
		SELECT COUNT(*) 
		FROM thumbnails 
		WHERE status = 'success' AND viewed = 0 AND status != 'deleted'
	`).Scan(&count)

	if err != nil {
		return nil, fmt.Errorf("failed to count unviewed thumbnails: %w", err)
	}

	// If no unviewed thumbnails, return nil
	if count == 0 {
		return nil, nil
	}

	// Generate a random offset
	// We're using crypto/rand for better randomness
	randomNum, err := rand.Int(rand.Reader, big.NewInt(int64(count)))
	if err != nil {
		// Fall back to math/rand if crypto/rand fails
		offset := mathrand.Intn(count)
		randomNum = big.NewInt(int64(offset))
	}

	// Get a random thumbnail using LIMIT and OFFSET
	thumbnail := &models.Thumbnail{}
	err = d.db.QueryRow(`
		SELECT 
			id, movie_path, movie_filename, thumbnail_path, 
			created_at, updated_at, status, viewed,
			width, height, duration, file_size, error_message, source
		FROM thumbnails 
		WHERE status = 'success' AND viewed = 0 AND status != 'deleted'
		LIMIT 1 OFFSET ?
	`, randomNum.Int64()).Scan(
		&thumbnail.ID, &thumbnail.MoviePath, &thumbnail.MovieFilename, &thumbnail.ThumbnailPath,
		&thumbnail.CreatedAt, &thumbnail.UpdatedAt, &thumbnail.Status, &thumbnail.Viewed,
		&thumbnail.Width, &thumbnail.Height, &thumbnail.Duration, &thumbnail.FileSize, &thumbnail.ErrorMessage, &thumbnail.Source,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}

	return thumbnail, err
}

// GetRandomUnviewedThumbnailExcluding gets a random unviewed thumbnail excluding specific IDs
func (d *DB) GetRandomUnviewedThumbnailExcluding(excludeIDs ...int64) (*models.Thumbnail, error) {
	// Build the exclude condition
	excludeCondition := ""
	excludeArgs := []interface{}{}

	if len(excludeIDs) > 0 {
		excludeCondition = " AND id NOT IN ("
		for i, id := range excludeIDs {
			if i > 0 {
				excludeCondition += ", "
			}
			excludeCondition += "?"
			excludeArgs = append(excludeArgs, id)
		}
		excludeCondition += ")"
	}

	// First, count the total number of unviewed thumbnails excluding the specified IDs
	var count int
	countQuery := `
		SELECT COUNT(*) 
		FROM thumbnails 
		WHERE status = 'success' AND viewed = 0 AND status != 'deleted'` + excludeCondition

	err := d.db.QueryRow(countQuery, excludeArgs...).Scan(&count)
	if err != nil {
		return nil, fmt.Errorf("failed to count unviewed thumbnails: %w", err)
	}

	// If no unviewed thumbnails available, return nil
	if count == 0 {
		return nil, nil
	}

	// Generate a random offset
	randomNum, err := rand.Int(rand.Reader, big.NewInt(int64(count)))
	if err != nil {
		// Fall back to math/rand if crypto/rand fails
		offset := mathrand.Intn(count)
		randomNum = big.NewInt(int64(offset))
	}

	// Get a random thumbnail using LIMIT and OFFSET
	thumbnail := &models.Thumbnail{}
	selectQuery := `
		SELECT 
			id, movie_path, movie_filename, thumbnail_path, 
			created_at, updated_at, status, viewed,
			width, height, duration, file_size, error_message, source
		FROM thumbnails 
		WHERE status = 'success' AND viewed = 0 AND status != 'deleted'` + excludeCondition + `
		LIMIT 1 OFFSET ?`

	selectArgs := append(excludeArgs, randomNum.Int64())
	err = d.db.QueryRow(selectQuery, selectArgs...).Scan(
		&thumbnail.ID, &thumbnail.MoviePath, &thumbnail.MovieFilename, &thumbnail.ThumbnailPath,
		&thumbnail.CreatedAt, &thumbnail.UpdatedAt, &thumbnail.Status, &thumbnail.Viewed,
		&thumbnail.Width, &thumbnail.Height, &thumbnail.Duration, &thumbnail.FileSize, &thumbnail.ErrorMessage, &thumbnail.Source,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}

	return thumbnail, err
}

// GetDeletedThumbnails retrieves thumbnails marked for deletion
// If limit > 0, only that many items will be returned
// If limit = 0, all matching thumbnails will be returned
func (d *DB) GetDeletedThumbnails(limit int) ([]*models.Thumbnail, error) {

	var rows *sql.Rows
	var err error

	query := `
        SELECT 
            id, movie_path, movie_filename, thumbnail_path, 
            created_at, updated_at, status, viewed,
            width, height, duration, file_size, error_message, source
        FROM thumbnails 
        WHERE status = 'deleted'
        ORDER BY updated_at DESC`

	// Add limit clause if a limit is specified
	if limit > 0 {
		rows, err = d.db.Query(query+" LIMIT ?", limit)
	} else {
		rows, err = d.db.Query(query)
	}

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanThumbnails(rows)
}

// GetFirstUnviewedThumbnail gets the first unviewed thumbnail
func (d *DB) GetFirstUnviewedThumbnail() (*models.Thumbnail, error) {
	thumbnail := &models.Thumbnail{}
	err := d.db.QueryRow(`
        SELECT 
            id, movie_path, movie_filename, thumbnail_path, 
            created_at, updated_at, status, viewed,
            width, height, duration, file_size, error_message, source
        FROM thumbnails 
        WHERE status = 'success' AND viewed = 0 AND status != 'deleted'
        ORDER BY id ASC
        LIMIT 1
    `).Scan(
		&thumbnail.ID, &thumbnail.MoviePath, &thumbnail.MovieFilename, &thumbnail.ThumbnailPath,
		&thumbnail.CreatedAt, &thumbnail.UpdatedAt, &thumbnail.Status, &thumbnail.Viewed,
		&thumbnail.Width, &thumbnail.Height, &thumbnail.Duration, &thumbnail.FileSize, &thumbnail.ErrorMessage, &thumbnail.Source,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}

	return thumbnail, err
}

// GetNextUnviewedThumbnail gets the next unviewed thumbnail after the given ID
func (d *DB) GetNextUnviewedThumbnail(currentID int64) (*models.Thumbnail, error) {
	thumbnail := &models.Thumbnail{}
	err := d.db.QueryRow(`
        SELECT 
            id, movie_path, movie_filename, thumbnail_path, 
            created_at, updated_at, status, viewed,
            width, height, duration, file_size, error_message, source
        FROM thumbnails 
        WHERE status = 'success' AND viewed = 0 AND status != 'deleted' AND id > ?
        ORDER BY id ASC
        LIMIT 1
    `, currentID).Scan(
		&thumbnail.ID, &thumbnail.MoviePath, &thumbnail.MovieFilename, &thumbnail.ThumbnailPath,
		&thumbnail.CreatedAt, &thumbnail.UpdatedAt, &thumbnail.Status, &thumbnail.Viewed,
		&thumbnail.Width, &thumbnail.Height, &thumbnail.Duration, &thumbnail.FileSize, &thumbnail.ErrorMessage, &thumbnail.Source,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}

	return thumbnail, err
}

// GetPreviousThumbnail gets the previous thumbnail before the given ID
func (d *DB) GetPreviousThumbnail(currentID int64) (*models.Thumbnail, error) {
	// If current ID is 0, return nil (no previous)
	if currentID == 0 {
		return nil, nil
	}

	thumbnail := &models.Thumbnail{}
	err := d.db.QueryRow(`
        SELECT 
            id, movie_path, movie_filename, thumbnail_path, 
            created_at, updated_at, status, viewed,
            width, height, duration, file_size, error_message, source
        FROM thumbnails 
        WHERE status = 'success' AND status != 'deleted' AND id < ?
        ORDER BY id DESC
        LIMIT 1
    `, currentID).Scan(
		&thumbnail.ID, &thumbnail.MoviePath, &thumbnail.MovieFilename, &thumbnail.ThumbnailPath,
		&thumbnail.CreatedAt, &thumbnail.UpdatedAt, &thumbnail.Status, &thumbnail.Viewed,
		&thumbnail.Width, &thumbnail.Height, &thumbnail.Duration, &thumbnail.FileSize, &thumbnail.ErrorMessage, &thumbnail.Source,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}

	return thumbnail, err
}

// GetUnviewedThumbnailCount returns the total count of unviewed thumbnails
func (d *DB) GetUnviewedThumbnailCount() (int, error) {
	var count int
	err := d.db.QueryRow(`
        SELECT COUNT(*)
        FROM thumbnails 
        WHERE status = 'success' AND viewed = 0 AND status != 'deleted'
    `).Scan(&count)

	return count, err
}

// GetThumbnailPosition gets the position of a thumbnail in the unviewed sequence
func (d *DB) GetThumbnailPosition(id int64) (int, error) {
	var position int
	err := d.db.QueryRow(`
        SELECT COUNT(*) + 1
        FROM thumbnails
        WHERE status = 'success' AND viewed = 0 AND status != 'deleted' AND id < ?
    `, id).Scan(&position)

	return position, err
}

// GetUnviewedThumbnails retrieves all unviewed thumbnails
func (d *DB) GetUnviewedThumbnails() ([]*models.Thumbnail, error) {
	rows, err := d.db.Query(`
        SELECT 
            id, movie_path, movie_filename, thumbnail_path, 
            created_at, updated_at, status, viewed,
            width, height, duration, file_size, error_message, source
        FROM thumbnails 
        WHERE status = 'success' AND viewed = 0
        ORDER BY updated_at DESC
        LIMIT 10`,
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
			width, height, duration, file_size, error_message, source
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
			width, height, duration, file_size, error_message, source
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
			width, height, duration, file_size, error_message, source
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
			width, height, duration, file_size, error_message, source
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

// RestoreFromDeletion restores a thumbnail from deletion status back to success
func (d *DB) RestoreFromDeletion(moviePath string) error {
	_, err := d.db.Exec(`
        UPDATE thumbnails 
        SET status = 'success', viewed = 0
        WHERE movie_path = ? AND status = 'deleted'`,
		moviePath,
	)
	return err
}

// RestoreFromDeletionByID restores a thumbnail from deletion status back to success by ID
func (d *DB) RestoreFromDeletionByID(id int64) error {
	_, err := d.db.Exec(`
        UPDATE thumbnails 
        SET status = 'success', viewed = 0
        WHERE id = ? AND status = 'deleted'`,
		id,
	)
	return err
}

func (d *DB) GetStats() (*models.Stats, error) {
	stats := &models.Stats{}

	err := d.db.QueryRow(`
		SELECT
			COUNT(*) as total,
			SUM(CASE WHEN status = 'success' THEN 1 ELSE 0 END) as success,
			SUM(CASE WHEN status = 'error' THEN 1 ELSE 0 END) as error,
			SUM(CASE WHEN status = 'pending' THEN 1 ELSE 0 END) as pending,
			SUM(CASE WHEN status = 'success' AND viewed = 1 THEN 1 ELSE 0 END) as viewed,
			SUM(CASE WHEN status = 'success' AND viewed = 0 THEN 1 ELSE 0 END) as unviewed,
			SUM(CASE WHEN status = 'deleted' THEN 1 ELSE 0 END) as deleted,
			SUM(CASE WHEN source = 'generated' THEN 1 ELSE 0 END) as generated,
			SUM(CASE WHEN source = 'imported' THEN 1 ELSE 0 END) as imported,
			COALESCE(SUM(CASE WHEN status = 'success' AND viewed = 1 THEN file_size ELSE 0 END), 0) as viewed_size,
			COALESCE(SUM(CASE WHEN status = 'success' AND viewed = 0 THEN file_size ELSE 0 END), 0) as unviewed_size
		FROM thumbnails
	`).Scan(
		&stats.Total,
		&stats.Success,
		&stats.Error,
		&stats.Pending,
		&stats.Viewed,
		&stats.Unviewed,
		&stats.Deleted,
		&stats.Generated,
		&stats.Imported,
		&stats.ViewedSize,
		&stats.UnviewedSize,
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
			&thumbnail.Width, &thumbnail.Height, &thumbnail.Duration, &thumbnail.FileSize, &thumbnail.ErrorMessage, &thumbnail.Source,
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
