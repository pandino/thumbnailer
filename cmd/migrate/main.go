package main

import (
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	_ "github.com/mattn/go-sqlite3"
	"github.com/pandino/movie-thumbnailer-go/internal/config"
)

func main() {
	var dbPath = flag.String("db", "", "Path to the SQLite database file (overrides config)")
	var help = flag.Bool("h", false, "Show help")
	flag.Parse()

	if *help {
		fmt.Printf("Usage: %s [-db <database_path>]\n", os.Args[0])
		fmt.Println("\nMigration utility for movie-thumbnailer database")
		fmt.Println("This utility:")
		fmt.Println("  1. Adds the file_size column if it doesn't exist")
		fmt.Println("  2. Scans existing records and populates file_size for movies that exist")
		fmt.Println("  3. Uses the same configuration as the web app")
		fmt.Println("\nIf -db is not specified, uses the same database path as the web app")
		flag.PrintDefaults()
		os.Exit(0)
	}

	// Load configuration (same as web app)
	cfg := config.New()

	// Allow override of database path
	databasePath := cfg.DBPath
	if *dbPath != "" {
		databasePath = *dbPath
	}

	log.Printf("Starting database migration for: %s", databasePath)
	log.Printf("Movie directory: %s", cfg.MoviesDir)

	// First, check if we need to add the file_size column
	if err := ensureFileSizeColumn(databasePath); err != nil {
		log.Fatalf("Failed to ensure file_size column exists: %v", err)
	}

	// Run migration using direct SQL
	if err := runDirectSQLMigration(databasePath, cfg.MoviesDir); err != nil {
		log.Fatalf("Migration failed: %v", err)
	}

	log.Println("Migration completed successfully")
}

func ensureFileSizeColumn(dbPath string) error {
	// Open a direct connection to check and modify schema
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer db.Close()

	// Check if file_size column exists
	rows, err := db.Query("PRAGMA table_info(thumbnails)")
	if err != nil {
		return fmt.Errorf("failed to get table info: %w", err)
	}
	defer rows.Close()

	hasFileSizeColumn := false
	for rows.Next() {
		var cid int
		var name, dataType string
		var notNull int
		var dfltValue sql.NullString
		var pk sql.NullString
		err := rows.Scan(&cid, &name, &dataType, &notNull, &dfltValue, &pk)
		if err != nil {
			return fmt.Errorf("failed to scan column info: %w", err)
		}
		if name == "file_size" {
			hasFileSizeColumn = true
			break
		}
	}

	if !hasFileSizeColumn {
		log.Println("Adding file_size column to thumbnails table...")
		_, err = db.Exec("ALTER TABLE thumbnails ADD COLUMN file_size INTEGER DEFAULT 0")
		if err != nil {
			return fmt.Errorf("failed to add file_size column: %w", err)
		}
		log.Println("file_size column added successfully")
	} else {
		log.Println("file_size column already exists")
	}

	return nil
}

func runDirectSQLMigration(dbPath string, movieDir string) error {
	// Open a direct connection
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer db.Close()

	// Configure SQLite for better concurrency
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	log.Println("Starting file size population...")

	// Begin a transaction to avoid locking issues
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Get all thumbnails that might need file size updates
	rows, err := tx.Query(`
		SELECT id, movie_path, movie_filename, thumbnail_path, status, file_size
		FROM thumbnails
		ORDER BY id
	`)
	if err != nil {
		return fmt.Errorf("failed to query thumbnails: %w", err)
	}
	defer rows.Close()

	// Collect all the updates we need to make
	type updateInfo struct {
		id       int64
		fileSize int64
		delete   bool
	}
	var updates []updateInfo

	updated := 0
	missing := 0
	errors := 0
	total := 0

	for rows.Next() {
		total++
		var id int64
		var moviePath, movieFilename, status string
		var fileSize int64
		var thumbnailPath sql.NullString

		err := rows.Scan(&id, &moviePath, &movieFilename, &thumbnailPath, &status, &fileSize)
		if err != nil {
			log.Printf("Warning: Failed to scan row: %v", err)
			errors++
			continue
		}

		if total%100 == 0 {
			log.Printf("Processing %d thumbnails...", total)
		}

		// Skip if file_size is already set (not 0)
		if fileSize > 0 {
			continue
		}

		// Get file info for the movie
		fileInfo, err := os.Stat(mapMoviePath(moviePath, movieDir))
		if err != nil {
			if os.IsNotExist(err) {
				missing++
				updates = append(updates, updateInfo{id: id, delete: true})
				continue
			}
			log.Printf("Warning: Failed to get file info for %s: %v", moviePath, err)
			errors++
			continue
		}

		updates = append(updates, updateInfo{id: id, fileSize: fileInfo.Size()})
		updated++
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("error iterating rows: %w", err)
	}
	rows.Close()

	// Now apply all updates in batch
	log.Printf("Applying %d updates...", len(updates))
	for _, update := range updates {
		if update.delete {
			_, err := tx.Exec("UPDATE thumbnails SET status = 'deleted' WHERE id = ?", update.id)
			if err != nil {
				log.Printf("Warning: Failed to mark record %d as deleted: %v", update.id, err)
			}
		} else {
			_, err := tx.Exec("UPDATE thumbnails SET file_size = ? WHERE id = ?", update.fileSize, update.id)
			if err != nil {
				log.Printf("Warning: Failed to update file size for record %d: %v", update.id, err)
			}
		}
	}

	// Commit the transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	log.Printf("Migration summary:")
	log.Printf("  - Total records processed: %d", total)
	log.Printf("  - Updated file sizes: %d", updated)
	log.Printf("  - Missing files (marked as deleted): %d", missing)
	log.Printf("  - Errors: %d", errors)

	return nil
}

// mapMoviePath attempts to map a database path to the current movie directory
func mapMoviePath(dbPath, movieDir string) string {
	// If the path already exists as-is, use it
	if _, err := os.Stat(dbPath); err == nil {
		return dbPath
	}

	// Extract just the filename from the database path
	filename := filepath.Base(dbPath)

	// Try the filename directly in the movie directory
	mappedPath := filepath.Join(movieDir, filename)
	if _, err := os.Stat(mappedPath); err == nil {
		return mappedPath
	}

	// If it's a nested path, try to preserve some directory structure
	// e.g., /host/movies/genre/movie.mp4 -> /movies/genre/movie.mp4
	pathParts := strings.Split(filepath.Clean(dbPath), string(filepath.Separator))

	// Try with the last 2 parts (directory + filename)
	if len(pathParts) >= 2 {
		relativePath := filepath.Join(pathParts[len(pathParts)-2], pathParts[len(pathParts)-1])
		mappedPath = filepath.Join(movieDir, relativePath)
		if _, err := os.Stat(mappedPath); err == nil {
			return mappedPath
		}
	}

	// Try with the last 3 parts (for deeper nesting)
	if len(pathParts) >= 3 {
		relativePath := filepath.Join(pathParts[len(pathParts)-3], pathParts[len(pathParts)-2], pathParts[len(pathParts)-1])
		mappedPath = filepath.Join(movieDir, relativePath)
		if _, err := os.Stat(mappedPath); err == nil {
			return mappedPath
		}
	}

	// If nothing works, return the original path
	return dbPath
}
