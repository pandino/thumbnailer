package ffmpeg

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/pandino/movie-thumbnailer-go/internal/config"
	"github.com/pandino/movie-thumbnailer-go/internal/database"
	"github.com/pandino/movie-thumbnailer-go/internal/metrics"
	"github.com/pandino/movie-thumbnailer-go/internal/models"
	"github.com/sirupsen/logrus"
)

// Thumbnailer creates thumbnail grids from movie files using ffmpeg
type Thumbnailer struct {
	cfg     *config.Config
	log     *logrus.Logger
	metrics *metrics.Metrics
}

// New creates a new Thumbnailer
func New(cfg *config.Config, log *logrus.Logger, metrics *metrics.Metrics) *Thumbnailer {
	return &Thumbnailer{
		cfg:     cfg,
		log:     log,
		metrics: metrics,
	}
}

// CreateThumbnail generates a thumbnail grid for a movie file
func (t *Thumbnailer) CreateThumbnail(ctx context.Context, moviePath string, db *database.DB) (*models.Thumbnail, error) {
	// Generate thumbnail filename
	movieFilename := filepath.Base(moviePath)
	thumbnailFilename := strings.TrimSuffix(movieFilename, filepath.Ext(movieFilename)) + ".jpg"
	thumbnailPath := filepath.Join(t.cfg.ThumbnailsDir, thumbnailFilename)

	// Initialize thumbnail record
	thumbnail := &models.Thumbnail{
		MoviePath:     movieFilename,
		MovieFilename: movieFilename,
		ThumbnailPath: thumbnailFilename,
		Status:        "pending",
		Source:        models.SourceGenerated, // Set source as generated
	}

	// Save the pending status to the database right away
	// This allows other processes to see that this movie is being processed
	if db != nil {
		if err := db.UpsertThumbnail(thumbnail); err != nil {
			t.log.WithError(err).WithField("movie", moviePath).Error("Failed to save pending status")
			// Continue processing anyway
		}
	}

	// Get video metadata
	metadata, err := t.GetVideoMetadata(ctx, moviePath)
	if err != nil {
		t.log.WithError(err).WithField("movie", moviePath).Error("Failed to get video metadata")
		thumbnail.Status = "error"
		thumbnail.ErrorMessage = fmt.Sprintf("Failed to get video metadata: %v", err)

		// Save the error status
		if db != nil {
			if err := db.UpsertThumbnail(thumbnail); err != nil {
				t.log.WithError(err).WithField("movie", moviePath).Error("Failed to save error status")
			}
		}

		return thumbnail, err
	}

	// Update thumbnail with metadata
	thumbnail.Duration = metadata.Duration
	thumbnail.Width = metadata.Width
	thumbnail.Height = metadata.Height

	// Calculate keyframe interval for better thumbnail distribution
	interval, err := t.calculateKeyframeInterval(ctx, moviePath, metadata.Duration)
	if err != nil {
		t.log.WithError(err).WithField("movie", moviePath).Warn("Failed to calculate keyframe interval, using default")
		interval = 10 // Default interval if calculation fails
	}

	// Generate thumbnail grid
	err = t.generateThumbnailGrid(ctx, moviePath, thumbnailPath, interval)
	if err != nil {
		t.log.WithError(err).WithField("movie", moviePath).Error("Failed to generate thumbnail grid")
		thumbnail.Status = "error"
		thumbnail.ErrorMessage = fmt.Sprintf("Failed to generate thumbnail: %v", err)

		// Save the error status
		if db != nil {
			if err := db.UpsertThumbnail(thumbnail); err != nil {
				t.log.WithError(err).WithField("movie", moviePath).Error("Failed to save error status")
			}
		}

		return thumbnail, err
	}

	// Verify thumbnail was created
	if _, err := os.Stat(thumbnailPath); os.IsNotExist(err) {
		thumbnail.Status = "error"
		thumbnail.ErrorMessage = "Thumbnail file was not created"

		// Save the error status
		if db != nil {
			if err := db.UpsertThumbnail(thumbnail); err != nil {
				t.log.WithError(err).WithField("movie", moviePath).Error("Failed to save error status")
			}
		}

		return thumbnail, fmt.Errorf("thumbnail file was not created: %s", thumbnailPath)
	}

	// Update status to success
	thumbnail.Status = "success"

	// Save the final success status
	if db != nil {
		if err := db.UpsertThumbnail(thumbnail); err != nil {
			t.log.WithError(err).WithField("movie", moviePath).Error("Failed to save success status")
		}
	}

	return thumbnail, nil
}

// VideoMetadata stores information about a video file
type VideoMetadata struct {
	Duration float64
	Width    int
	Height   int
}

// FFProbeResponse represents the JSON structure returned by ffprobe
type FFProbeResponse struct {
	Streams []struct {
		Width  int `json:"width"`
		Height int `json:"height"`
	} `json:"streams"`
	Format struct {
		Duration string `json:"duration"`
	} `json:"format"`
}

// GetVideoMetadata extracts metadata from a video file using JSON output format
func (t *Thumbnailer) GetVideoMetadata(ctx context.Context, moviePath string) (*VideoMetadata, error) {
	start := time.Now()

	// Use ffprobe with JSON output format
	cmd := exec.CommandContext(
		ctx,
		"ffprobe",
		"-v", "error",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		"-select_streams", "v:0",
		moviePath,
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	execDuration := time.Since(start)

	if err != nil {
		if t.metrics != nil {
			t.metrics.RecordFFmpegExecution("error", execDuration)
		}
		return nil, fmt.Errorf("ffprobe error: %v - %s", err, stderr.String())
	}

	if t.metrics != nil {
		t.metrics.RecordFFmpegExecution("success", execDuration)
	}

	// Parse JSON output
	output := stdout.String()
	t.log.WithField("ffprobe_output", output).Debug("FFprobe raw output")

	var ffprobeData FFProbeResponse
	if err := json.Unmarshal([]byte(output), &ffprobeData); err != nil {
		return nil, fmt.Errorf("failed to parse ffprobe JSON output: %v", err)
	}

	// Validate streams data
	if len(ffprobeData.Streams) == 0 {
		return nil, fmt.Errorf("no video streams found in file")
	}

	// Extract width and height from the first video stream
	width := ffprobeData.Streams[0].Width
	height := ffprobeData.Streams[0].Height

	// Extract duration from format data
	duration, err := strconv.ParseFloat(ffprobeData.Format.Duration, 64)
	if err != nil {
		return nil, fmt.Errorf("failed to parse duration: %v", err)
	}

	// Validate the parsed values
	if width <= 0 || height <= 0 || duration <= 0 {
		return nil, fmt.Errorf("invalid metadata values: width=%d, height=%d, duration=%f", width, height, duration)
	}

	return &VideoMetadata{
		Duration: duration,
		Width:    width,
		Height:   height,
	}, nil
}

// calculateKeyframeInterval estimates an appropriate interval for thumbnail extraction
func (t *Thumbnailer) calculateKeyframeInterval(ctx context.Context, moviePath string, duration float64) (int, error) {
	// Skip first 30 seconds to avoid intros
	skipSeconds := 30.0
	if duration <= skipSeconds {
		skipSeconds = 0 // Don't skip for very short videos
	}

	adjustedDuration := duration - skipSeconds
	if adjustedDuration <= 0 {
		return 10, nil // Default for very short videos
	}

	// Sample a portion of the video to count keyframes
	sampleDuration := 180.0
	if adjustedDuration < sampleDuration {
		sampleDuration = adjustedDuration
	}

	// Build interval string for ffprobe (format: START%+DURATION)
	intervalStr := fmt.Sprintf("%.2f%%+%.2f", skipSeconds, sampleDuration)

	// Count keyframes in the sample
	cmd := exec.CommandContext(
		ctx,
		"ffprobe",
		"-v", "error",
		"-select_streams", "v:0",
		"-skip_frame", "nokey",
		"-show_entries", "frame=pict_type",
		"-of", "csv=p=0",
		"-read_intervals", intervalStr,
		moviePath,
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return 10, fmt.Errorf("ffprobe error: %v - %s", err, stderr.String())
	}

	// Count keyframes in output
	output := stdout.String()
	keyframeCount := strings.Count(output, "I") // I frames are keyframes

	if keyframeCount == 0 {
		return 10, nil // Default if no keyframes found
	}

	// Estimate total keyframes in the adjusted duration
	totalKeyframes := int((float64(keyframeCount) * adjustedDuration) / sampleDuration)

	// Calculate interval to distribute frames across the grid
	totalCells := t.cfg.GridCols * t.cfg.GridRows
	interval := (totalKeyframes * 8 / 10) / totalCells // Use 80% of keyframes
	if interval < 1 {
		interval = 1
	}

	return interval, nil
}

// generateThumbnailGrid creates a grid of thumbnails from a movie file
func (t *Thumbnailer) generateThumbnailGrid(ctx context.Context, moviePath, outputPath string, interval int) error {
	// Build ffmpeg command
	cmd := exec.CommandContext(
		ctx,
		"ffmpeg",
		"-v", "error",
		"-threads", "2",
		"-ss", "30", // Skip first 30 seconds
		"-skip_frame", "nokey",
		"-i", moviePath,
		"-vf", fmt.Sprintf("select='eq(pict_type,I)',select='not(mod(n,%d))',scale=320:180:force_original_aspect_ratio=decrease,pad=320:180:(ow-iw)/2:(oh-ih)/2,tile=%dx%d:padding=4:margin=4",
			interval, t.cfg.GridCols, t.cfg.GridRows),
		"-frames:v", "1",
		"-q:v", "3",
		"-update", "1",
		"-y",
		outputPath,
	)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// Extract error information from stderr
		errorMsg := parseFFmpegError(stderr.String())
		return fmt.Errorf("ffmpeg error: %v - %s", err, errorMsg)
	}

	return nil
}

// parseFFmpegError extracts relevant error information from ffmpeg output
func parseFFmpegError(stderr string) string {
	// Check for common error patterns
	patterns := []string{
		`(?m)Error .+`,
		`(?m)Invalid .+`,
		`(?m)failed .+`,
		`(?m)Conversion failed .+`,
	}

	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		matches := re.FindAllString(stderr, -1)
		if len(matches) > 0 {
			return strings.Join(matches, "; ")
		}
	}

	// If no specific error pattern is found, return a truncated stderr
	if len(stderr) > 200 {
		return stderr[:200] + "..."
	}
	return stderr
}
