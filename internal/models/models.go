package models

import (
	"fmt"
	"time"
)

// Thumbnail represents a thumbnail generated from a movie file
type Thumbnail struct {
	ID            int64     `json:"id"`
	MoviePath     string    `json:"movie_path"`
	MovieFilename string    `json:"movie_filename"`
	ThumbnailPath string    `json:"thumbnail_path"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	Status        string    `json:"status"`
	Viewed        int       `json:"viewed"`
	Width         int       `json:"width"`
	Height        int       `json:"height"`
	Duration      float64   `json:"duration"`
	FileSize      int64     `json:"file_size"`
	ErrorMessage  string    `json:"error_message,omitempty"`
	Source        string    `json:"source"`
}

// Stats represents statistics about the thumbnails
type Stats struct {
	Total     int `json:"total"`
	Success   int `json:"success"`
	Error     int `json:"error"`
	Pending   int `json:"pending"`
	Viewed    int `json:"viewed"`
	Unviewed  int `json:"unviewed"`
	Deleted   int `json:"deleted"`
	Generated int `json:"generated"`
	Imported  int `json:"imported"`
}

// Constants for thumbnail status values
const (
	StatusPending = "pending"
	StatusSuccess = "success"
	StatusError   = "error"
	StatusDeleted = "deleted"
)

// Constants for thumbnail source values
const (
	SourceGenerated = "generated"
	SourceImported  = "imported"
)

// ValidStatus checks if a status value is valid
func ValidStatus(status string) bool {
	switch status {
	case StatusPending, StatusSuccess, StatusError, StatusDeleted:
		return true
	default:
		return false
	}
}

// ValidSource checks if a source value is valid
func ValidSource(source string) bool {
	switch source {
	case SourceGenerated, SourceImported:
		return true
	default:
		return false
	}
}

// IsViewed returns true if the thumbnail has been viewed
func (t *Thumbnail) IsViewed() bool {
	return t.Viewed == 1
}

// MarkAsViewed marks the thumbnail as viewed
func (t *Thumbnail) MarkAsViewed() {
	t.Viewed = 1
}

// ResetViewed marks the thumbnail as unviewed
func (t *Thumbnail) ResetViewed() {
	t.Viewed = 0
}

// IsSuccess returns true if the thumbnail was successfully generated
func (t *Thumbnail) IsSuccess() bool {
	return t.Status == StatusSuccess
}

// IsPending returns true if the thumbnail is pending generation
func (t *Thumbnail) IsPending() bool {
	return t.Status == StatusPending
}

// IsError returns true if the thumbnail generation resulted in an error
func (t *Thumbnail) IsError() bool {
	return t.Status == StatusError
}

// IsDeleted returns true if the thumbnail is marked for deletion
func (t *Thumbnail) IsDeleted() bool {
	return t.Status == StatusDeleted
}

// IsImported returns true if the thumbnail was imported rather than generated
func (t *Thumbnail) IsImported() bool {
	return t.Source == SourceImported
}

// GetDurationFormatted returns the duration in a human-readable format
func (t *Thumbnail) GetDurationFormatted() string {
	hours := int(t.Duration) / 3600
	minutes := (int(t.Duration) % 3600) / 60
	seconds := int(t.Duration) % 60

	if hours > 0 {
		return fmt.Sprintf("%d:%02d:%02d", hours, minutes, seconds)
	}
	return fmt.Sprintf("%d:%02d", minutes, seconds)
}

// GetResolution returns the video resolution as a string
func (t *Thumbnail) GetResolution() string {
	return fmt.Sprintf("%dx%d", t.Width, t.Height)
}

// GetFileSizeFormatted returns the file size in a human-readable format
func (t *Thumbnail) GetFileSizeFormatted() string {
	const (
		B  = 1
		KB = 1024 * B
		MB = 1024 * KB
		GB = 1024 * MB
		TB = 1024 * GB
	)

	size := float64(t.FileSize)
	switch {
	case t.FileSize >= TB:
		return fmt.Sprintf("%.2f TB", size/TB)
	case t.FileSize >= GB:
		return fmt.Sprintf("%.2f GB", size/GB)
	case t.FileSize >= MB:
		return fmt.Sprintf("%.2f MB", size/MB)
	case t.FileSize >= KB:
		return fmt.Sprintf("%.2f KB", size/KB)
	default:
		return fmt.Sprintf("%d B", t.FileSize)
	}
}
