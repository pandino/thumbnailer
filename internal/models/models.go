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
	Total        int   `json:"total"`
	Success      int   `json:"success"`
	Error        int   `json:"error"`
	Pending      int   `json:"pending"`
	Viewed       int   `json:"viewed"`
	Unviewed     int   `json:"unviewed"`
	Deleted      int   `json:"deleted"`
	Archived     int   `json:"archived"`
	Generated    int   `json:"generated"`
	Imported     int   `json:"imported"`
	ViewedSize   int64 `json:"viewed_size"`   // Total file size of viewed movies in bytes
	UnviewedSize int64 `json:"unviewed_size"` // Total file size of unviewed movies in bytes
}

// Constants for thumbnail status values
const (
	StatusPending  = "pending"
	StatusSuccess  = "success"
	StatusError    = "error"
	StatusDeleted  = "deleted"
	StatusArchived = "archived"
)

// Constants for thumbnail source values
const (
	SourceGenerated = "generated"
	SourceImported  = "imported"
)

// Decision stages: where the user made the choice
const (
	StageSlideshow = "slideshow" // judged from the thumbnail grid
	StagePlayer    = "player"    // judged after watching (MPV scripts)
)

// Decision actions. ActionUndo cancels the previous decision of the same stage.
const (
	ActionDelete  = "delete"
	ActionArchive = "archive"
	ActionPass    = "pass"
	ActionUndo    = "undo"
)

// Decision sources: recorded as it happened, or reconstructed afterwards
const (
	DecisionLive     = "live"
	DecisionBackfill = "backfill"
)

// Decision is one entry of the append-only decision log used to train the preference model
type Decision struct {
	ID                int64     `json:"id"`
	ThumbnailID       int64     `json:"thumbnail_id"`
	MovieFilename     string    `json:"movie_filename"`
	Stage             string    `json:"stage"`
	Action            string    `json:"action"`
	Source            string    `json:"source"`
	PDeleteAtDecision *float64  `json:"p_delete_at_decision,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
}

// GridGeometry describes the tile layout of a thumbnail grid, so single frames can be cropped
// out of it. Cols == 0 means the layout is unknown (e.g. an imported grid of another size).
type GridGeometry struct {
	Cols       int `json:"cols"`
	Rows       int `json:"rows"`
	TileWidth  int `json:"tile_width"`
	TileHeight int `json:"tile_height"`
	Padding    int `json:"padding"` // used both between tiles and as the outer margin
}

// Known reports whether the layout is known
func (g GridGeometry) Known() bool {
	return g.Cols > 0 && g.Rows > 0
}

// ImageSize returns the pixel size of a grid with this layout
func (g GridGeometry) ImageSize() (int, int) {
	return g.Cols*g.TileWidth + (g.Cols+1)*g.Padding, g.Rows*g.TileHeight + (g.Rows+1)*g.Padding
}

// MLItem is a grid the preference model can learn from or score: a live thumbnail or a
// decided movie whose grid was kept after its file was deleted or archived
type MLItem struct {
	ID            int64        `json:"id"`
	MovieFilename string       `json:"movie_filename"`
	GridURL       string       `json:"grid_url"`
	Live          bool         `json:"live"`
	Geometry      GridGeometry `json:"geometry"`
}

// Embedding is an image embedding of one grid computed by a given model
type Embedding struct {
	ThumbnailID int64     `json:"id"`
	Vector      []float32 `json:"vector"`
}

// Score holds the preference model's predictions for one thumbnail
type Score struct {
	ThumbnailID int64    `json:"id"`
	PDelete     float64  `json:"p_delete"`
	PArchive    *float64 `json:"p_archive,omitempty"`
}

// Slideshow orders
const (
	OrderRandom       = "random"
	OrderLikelyDelete = "likely-delete"
	OrderUncertain    = "uncertain"
	OrderLikelyKeep   = "likely-keep"
)

// ValidOrder checks if a slideshow order value is valid
func ValidOrder(order string) bool {
	switch order {
	case OrderRandom, OrderLikelyDelete, OrderUncertain, OrderLikelyKeep:
		return true
	default:
		return false
	}
}

// MLStatus summarizes the preference model for the control page
type MLStatus struct {
	Decisions      map[string]int    `json:"decisions"` // "stage/action" -> count, undo excluded
	Embedded       int               `json:"embedded"`
	Scored         int               `json:"scored"`
	Meta           map[string]string `json:"meta"`            // reported by the worker
	AgreementN     int               `json:"agreement_n"`     // recent slideshow decisions that had a score
	AgreementRatio float64           `json:"agreement_ratio"` // share where the score agreed with the decision
}

// ValidStatus checks if a status value is valid
func ValidStatus(status string) bool {
	switch status {
	case StatusPending, StatusSuccess, StatusError, StatusDeleted, StatusArchived:
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

// IsArchived returns true if the thumbnail is marked for archival
func (t *Thumbnail) IsArchived() bool {
	return t.Status == StatusArchived
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
