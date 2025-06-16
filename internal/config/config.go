package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Config holds the application configuration
type Config struct {
	// Directory paths
	MoviesDir     string
	ThumbnailsDir string
	DataDir       string
	DBPath        string
	TemplatesDir  string
	StaticDir     string

	// Thumbnail generation
	GridCols       int
	GridRows       int
	MaxWorkers     int
	FileExtensions []string

	// Server settings
	ServerPort string
	ServerHost string

	// Background task settings
	ScanInterval time.Duration
	Debug        bool

	// Import settings
	ImportExisting bool

	// Deletion settings
	PreventDeletion bool
}

// New creates a new Config with values from environment variables or defaults
func New() *Config {
	config := &Config{
		// Default directory paths
		MoviesDir:     getEnv("MOVIE_INPUT_DIR", "/movies"),
		ThumbnailsDir: getEnv("THUMBNAIL_OUTPUT_DIR", "/thumbnails"),
		DataDir:       getEnv("DATA_DIR", "/data"),
		TemplatesDir:  getEnv("TEMPLATES_DIR", "./web/templates"),
		StaticDir:     getEnv("STATIC_DIR", "./web/static"),

		// Default thumbnail generation settings
		GridCols:       getEnvAsInt("GRID_COLS", 8),
		GridRows:       getEnvAsInt("GRID_ROWS", 4),
		MaxWorkers:     getEnvAsInt("MAX_WORKERS", 4),
		FileExtensions: getEnvAsSlice("FILE_EXTENSIONS", "mp4,mkv,avi,mov,mts,wmv"),

		// Default server settings
		ServerPort: getEnv("SERVER_PORT", "8080"),
		ServerHost: getEnv("SERVER_HOST", "0.0.0.0"),

		// Default background task settings
		ScanInterval: getEnvAsDuration("SCAN_INTERVAL", "1h"),
		Debug:        getEnvAsBool("DEBUG", false),

		// Import settings
		ImportExisting: getEnvAsBool("IMPORT_EXISTING", false),

		// Deletion settings
		PreventDeletion: getEnvAsBool("PREVENT_DELETION", false),
	}

	// Derive DB path - check DATABASE_PATH first, then default
	if dbPath := getEnv("DATABASE_PATH", ""); dbPath != "" {
		config.DBPath = dbPath
	} else {
		config.DBPath = filepath.Join(config.DataDir, "thumbnailer.db")
	}

	return config
}

// Helper functions to get environment variables with defaults

func getEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}

func getEnvAsInt(key string, defaultValue int) int {
	if value, exists := os.LookupEnv(key); exists {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

func getEnvAsSlice(key, defaultValue string) []string {
	if value, exists := os.LookupEnv(key); exists {
		return strings.Split(value, ",")
	}
	return strings.Split(defaultValue, ",")
}

func getEnvAsBool(key string, defaultValue bool) bool {
	if value, exists := os.LookupEnv(key); exists {
		if boolValue, err := strconv.ParseBool(value); err == nil {
			return boolValue
		}
	}
	return defaultValue
}

func getEnvAsDuration(key string, defaultValue string) time.Duration {
	if value, exists := os.LookupEnv(key); exists {
		if duration, err := time.ParseDuration(value); err == nil {
			return duration
		}
	}
	duration, _ := time.ParseDuration(defaultValue)
	return duration
}
