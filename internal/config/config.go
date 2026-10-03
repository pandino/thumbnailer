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
	MoviesDirs    []string
	ThumbnailsDir string
	DataDir       string
	ArchiveDir    string
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

	// Work window: scheduled scans, thumbnail generation and archive/delete
	// processing only run from WorkWindowStart (inclusive) to WorkWindowEnd
	// (exclusive), in hours of local time (set TZ). The window may wrap past
	// midnight; equal start and end means no restriction.
	WorkWindowStart int
	WorkWindowEnd   int

	// Deletion worker settings
	DisableDeletion bool

	// Import settings
	ImportExisting bool
}

// New creates a new Config with values from environment variables or defaults
func New() *Config {
	config := &Config{
		// Default directory paths
		MoviesDirs:    getEnvAsMovieDirs("MOVIE_INPUT_DIR", "/movies"),
		ThumbnailsDir: getEnv("THUMBNAIL_OUTPUT_DIR", "/thumbnails"),
		DataDir:       getEnv("DATA_DIR", "/data"),
		ArchiveDir:    getEnv("ARCHIVE_DIR", "/archive"),
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

		WorkWindowStart: getEnvAsHour("WORK_WINDOW_START", 11),
		WorkWindowEnd:   getEnvAsHour("WORK_WINDOW_END", 20),

		// Default deletion worker settings
		DisableDeletion: getEnvAsBool("DISABLE_DELETION", false),

		// Import settings
		ImportExisting: getEnvAsBool("IMPORT_EXISTING", false),
	}

	// Derive DB path - check DATABASE_PATH first, then default
	if dbPath := getEnv("DATABASE_PATH", ""); dbPath != "" {
		config.DBPath = dbPath
	} else {
		config.DBPath = filepath.Join(config.DataDir, "thumbnailer.db")
	}

	return config
}

// InWorkWindow reports whether t falls inside the work window.
func (c *Config) InWorkWindow(t time.Time) bool {
	start, end := c.WorkWindowStart%24, c.WorkWindowEnd%24
	if start == end {
		return true
	}
	h := t.Hour()
	if start < end {
		return h >= start && h < end
	}
	return h >= start || h < end
}

// WorkWindowEndAfter returns the first time after t at which the work window
// closes, or the zero time if the window is always open.
func (c *Config) WorkWindowEndAfter(t time.Time) time.Time {
	start, end := c.WorkWindowStart%24, c.WorkWindowEnd%24
	if start == end {
		return time.Time{}
	}
	closes := time.Date(t.Year(), t.Month(), t.Day(), end, 0, 0, 0, t.Location())
	if !closes.After(t) {
		closes = closes.AddDate(0, 0, 1)
	}
	return closes
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

// getEnvAsHour reads an hour of the day (0-24); invalid values fall back to the default.
func getEnvAsHour(key string, defaultValue int) int {
	if h := getEnvAsInt(key, defaultValue); h >= 0 && h <= 24 {
		return h
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

// getEnvAsMovieDirs parses a comma-separated list of directories, trimming whitespace
// and dropping empty entries. Falls back to a single-element slice of defaultValue.
func getEnvAsMovieDirs(key, defaultValue string) []string {
	raw := defaultValue
	if value, exists := os.LookupEnv(key); exists {
		raw = value
	}
	parts := strings.Split(raw, ",")
	var dirs []string
	for _, p := range parts {
		if d := strings.TrimSpace(p); d != "" {
			dirs = append(dirs, d)
		}
	}
	if len(dirs) == 0 {
		return []string{defaultValue}
	}
	return dirs
}
