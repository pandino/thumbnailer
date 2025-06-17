package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"path/filepath"
	"strconv"
	"time"

	"github.com/gorilla/mux"
	"github.com/pandino/movie-thumbnailer-go/internal/models" // Add missing import
	"github.com/sirupsen/logrus"
)

// formatBytes converts bytes to human readable format
func formatBytes(bytes int64) string {
	const (
		B  = 1
		KB = 1024 * B
		MB = 1024 * KB
		GB = 1024 * MB
		TB = 1024 * GB
	)

	size := float64(bytes)
	switch {
	case bytes >= TB:
		return fmt.Sprintf("%.2f TB", size/TB)
	case bytes >= GB:
		return fmt.Sprintf("%.2f GB", size/GB)
	case bytes >= MB:
		return fmt.Sprintf("%.2f MB", size/MB)
	case bytes >= KB:
		return fmt.Sprintf("%.2f KB", size/KB)
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

type SessionData struct {
	TotalImages     int   `json:"total_images"`
	ViewedCount     int   `json:"viewed_count"`
	NavigationCount int   `json:"navigation_count"` // Track actual navigation through slideshow
	CurrentID       int64 `json:"current_id"`
	StartedAt       int64 `json:"started_at"`
	PreviousID      int64 `json:"previous_id"`    // Store previous thumbnail ID for single undo/navigation
	NextID          int64 `json:"next_id"`        // Store next thumbnail ID for coordination with prefetcher
	PendingDelete   bool  `json:"pending_delete"` // Flag indicating if PreviousID thumbnail is marked for deletion
	DeletedSize     int64 `json:"deleted_size"`   // Total size in bytes of movies deleted in this session
}

// getSessionFromCookie retrieves and validates session data from cookie
func (s *Server) getSessionFromCookie(r *http.Request) (*SessionData, error) {
	sessionCookie, err := r.Cookie("slideshow_session")
	if err != nil {
		return nil, fmt.Errorf("no session cookie found: %w", err)
	}

	if sessionCookie.Value == "" {
		return nil, fmt.Errorf("empty session cookie")
	}

	// Decode the cookie value
	jsonData, err := base64.StdEncoding.DecodeString(sessionCookie.Value)
	if err != nil {
		return nil, fmt.Errorf("failed to decode session cookie: %w", err)
	}

	var session SessionData
	if err := json.Unmarshal(jsonData, &session); err != nil {
		return nil, fmt.Errorf("failed to unmarshal session data: %w", err)
	}

	return &session, nil
}

// saveSessionToCookie saves session data to cookie
func (s *Server) saveSessionToCookie(w http.ResponseWriter, session *SessionData) error {
	sessionJSON, err := json.Marshal(session)
	if err != nil {
		return fmt.Errorf("failed to marshal session data: %w", err)
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "slideshow_session",
		Value:    base64.StdEncoding.EncodeToString(sessionJSON),
		Path:     "/",
		MaxAge:   86400 * 30, // 30 days
		HttpOnly: true,
	})

	return nil
}

// createNewSession creates a new session with initial data
func (s *Server) createNewSession() (*SessionData, error) {
	stats, err := s.scanner.GetStats()
	if err != nil {
		s.log.WithError(err).Error("Failed to get stats for new session")
		// Continue with zero count as fallback
		stats = &models.Stats{}
	}

	session := &SessionData{
		TotalImages:     stats.Unviewed,
		ViewedCount:     0,
		NavigationCount: 0,
		CurrentID:       0,
		StartedAt:       time.Now().Unix(),
		PreviousID:      0,
		NextID:          0,
		PendingDelete:   false,
		DeletedSize:     0,
	}

	return session, nil
}

// redirectToSlideshow redirects to /slideshow without ID parameter (uses session state)
func (s *Server) redirectToSlideshow(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/slideshow", http.StatusSeeOther)
}

// requireValidSession checks for valid session and redirects to /slideshow if not found
func (s *Server) requireValidSession(w http.ResponseWriter, r *http.Request) (*SessionData, bool) {
	session, err := s.getSessionFromCookie(r)
	if err != nil {
		s.log.WithError(err).Debug("No valid session found, redirecting to slideshow")
		s.redirectToSlideshow(w, r)
		return nil, false
	}

	// Additional validation: check if session has meaningful data
	if session.StartedAt == 0 {
		s.log.Debug("Session has no start time, redirecting to slideshow")
		s.redirectToSlideshow(w, r)
		return nil, false
	}

	return session, true
}

// handleControlPage renders the control page
func (s *Server) handleControlPage(w http.ResponseWriter, r *http.Request) {
	stats, err := s.scanner.GetStats()
	if err != nil {
		s.log.WithError(err).Error("Failed to get stats")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Check for existing session from cookie
	var hasSession bool
	var sessionViewedCount int
	var sessionTotalCount int
	var sessionDeletedSize int64

	sessionCookie, err := r.Cookie("slideshow_session")
	if err == nil && sessionCookie.Value != "" {
		// Decode the cookie value
		jsonData, err := base64.StdEncoding.DecodeString(sessionCookie.Value)
		if err == nil {
			var session SessionData
			err = json.Unmarshal(jsonData, &session)
			if err == nil && session.TotalImages > 0 {
				hasSession = true
				sessionViewedCount = session.ViewedCount
				sessionTotalCount = session.TotalImages
				sessionDeletedSize = session.DeletedSize
			}
		}
	}

	// Parse template
	tmpl, err := template.ParseFiles(filepath.Join(s.cfg.TemplatesDir, "control.html"))
	if err != nil {
		s.log.WithError(err).Error("Failed to parse template")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Render template with data
	data := struct {
		Stats                       *models.Stats
		IsScanning                  bool
		HasSession                  bool
		SessionViewedCount          int
		SessionTotalCount           int
		SessionDeletedSize          int64
		Version                     *VersionInfo
		ViewedSizeFormatted         string
		UnviewedSizeFormatted       string
		SessionDeletedSizeFormatted string
	}{
		Stats:                       stats,
		IsScanning:                  s.scanner.IsScanning(),
		HasSession:                  hasSession,
		SessionViewedCount:          sessionViewedCount,
		SessionTotalCount:           sessionTotalCount,
		SessionDeletedSize:          sessionDeletedSize,
		Version:                     s.version,
		ViewedSizeFormatted:         formatBytes(stats.ViewedSize),
		UnviewedSizeFormatted:       formatBytes(stats.UnviewedSize),
		SessionDeletedSizeFormatted: formatBytes(sessionDeletedSize),
	}

	if err := tmpl.Execute(w, data); err != nil {
		s.log.WithError(err).Error("Failed to render template")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
}

// handleScan triggers a scan for new movies
func (s *Server) handleScan(w http.ResponseWriter, r *http.Request) {
	if s.scanner.IsScanning() {
		http.Error(w, "Scan already in progress", http.StatusConflict)
		return
	}

	// Create a timeout context derived from the application context
	// 30 minutes should be enough for a manual triggered scan
	ctx, cancel := context.WithTimeout(s.appCtx, 30*time.Minute)

	go func() {
		defer cancel() // Ensure context is cancelled when operation completes
		if err := s.scanner.ScanMovies(ctx); err != nil {
			s.log.WithError(err).Error("Scan failed")
		}
	}()

	// Redirect back to control page
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// handleCleanup triggers a cleanup of orphaned entries and thumbnails
func (s *Server) handleCleanup(w http.ResponseWriter, r *http.Request) {
	if s.cfg.DisableDeletion {
		http.Error(w, "Cleanup is disabled via DISABLE_DELETION flag", http.StatusForbidden)
		return
	}

	if s.scanner.IsScanning() {
		http.Error(w, "Cannot perform cleanup while scanning", http.StatusConflict)
		return
	}

	// Create a timeout context derived from the application context
	ctx, cancel := context.WithTimeout(s.appCtx, 10*time.Minute)

	go func() {
		defer cancel() // Ensure context is cancelled when operation completes
		if err := s.scanner.CleanupOrphans(ctx); err != nil {
			s.log.WithError(err).Error("Cleanup failed")
		}
	}()

	// Redirect back to control page
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// handleResetViews resets the viewed status of all thumbnails
func (s *Server) handleResetViews(w http.ResponseWriter, r *http.Request) {
	count, err := s.scanner.ResetViewedStatus()
	if err != nil {
		s.log.WithError(err).Error("Failed to reset views")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Set success message in flash
	// (Simplified for this example - you might want to use sessions for proper flash messages)
	http.SetCookie(w, &http.Cookie{
		Name:  "flash",
		Value: "Reset viewed status for " + strconv.FormatInt(count, 10) + " thumbnails",
		Path:  "/",
	})

	// Redirect back to control page
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// handleProcessDeletions triggers immediate processing of the deletion queue
func (s *Server) handleProcessDeletions(w http.ResponseWriter, r *http.Request) {
	if s.cfg.DisableDeletion {
		http.Error(w, "Deletion processing is disabled via DISABLE_DELETION flag", http.StatusForbidden)
		return
	}

	if s.scanner.IsScanning() {
		http.Error(w, "Cannot process deletions while scanning", http.StatusConflict)
		return
	}

	// Get the count of deleted items before processing
	stats, err := s.scanner.GetStats()
	if err != nil {
		s.log.WithError(err).Error("Failed to get stats")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	deletedCount := stats.Deleted

	// Create a timeout context derived from the application context
	ctx, cancel := context.WithTimeout(s.appCtx, 15*time.Minute)

	// Process the deletion queue
	go func() {
		defer cancel() // Ensure context is cancelled when operation completes
		if err := s.scanner.CleanupOrphans(ctx); err != nil {
			s.log.WithError(err).Error("Process deletions failed")
		}
	}()

	// Set success message in flash
	http.SetCookie(w, &http.Cookie{
		Name:  "flash",
		Value: fmt.Sprintf("Processing %d items for deletion in the background", deletedCount),
		Path:  "/",
	})

	// Redirect back to control page
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// handleSlideshow renders the slideshow page
func (s *Server) handleSlideshow(w http.ResponseWriter, r *http.Request) {
	// Check if a new session was requested
	newSession := r.URL.Query().Get("new") == "true"
	s.log.WithField("newSession", newSession).WithField("url", r.URL.String()).Info("Slideshow request received")

	var session *SessionData

	if newSession {
		// Create a new session
		var err error
		session, err = s.createNewSession()
		if err != nil {
			s.log.WithError(err).Error("Failed to create new session")
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		s.log.Info("Created new session with CurrentID=0, ViewedCount=0, NavigationCount=0, PreviousID=0, NextID=0")

		// Save to cookie
		if err := s.saveSessionToCookie(w, session); err != nil {
			s.log.WithError(err).Error("Failed to save new session to cookie")
			// Continue without session cookie
		}
	} else {
		// Try to get existing session from cookie
		var err error
		session, err = s.getSessionFromCookie(r)
		if err != nil {
			// No valid session found, create a new one
			s.log.WithError(err).Debug("No valid session found, creating new session")
			session, err = s.createNewSession()
			if err != nil {
				s.log.WithError(err).Error("Failed to create fallback session")
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}

			// Save to cookie
			if err := s.saveSessionToCookie(w, session); err != nil {
				s.log.WithError(err).Error("Failed to save fallback session to cookie")
				// Continue without session cookie
			}
		}
	}

	// Use session's current ID as target (no more ID parameter support)
	targetID := session.CurrentID

	// Get the thumbnail to display
	var thumbnail *models.Thumbnail
	var err error

	if targetID > 0 {
		// Get the specified thumbnail (either from session or query parameter)
		s.log.WithField("targetID", targetID).Info("Attempting to get thumbnail by ID")
		thumbnail, err = s.db.GetByID(targetID)
		if err != nil || thumbnail == nil {
			// If the stored thumbnail doesn't exist anymore, get a new random one
			s.log.WithError(err).WithField("targetID", targetID).Warn("Stored thumbnail not found, getting new random thumbnail")
			thumbnail, err = s.db.GetRandomUnviewedThumbnail()
		} else {
			s.log.WithField("foundThumbnailID", thumbnail.ID).Info("Successfully found thumbnail by ID")
		}
	} else {
		// No current thumbnail in session, get a random unviewed thumbnail
		s.log.Info("No targetID, getting random unviewed thumbnail")
		thumbnail, err = s.db.GetRandomUnviewedThumbnail()
	}

	if err != nil {
		s.log.WithError(err).Error("Failed to get thumbnail")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// If no thumbnail found, redirect to control page
	if thumbnail == nil {
		http.SetCookie(w, &http.Cookie{
			Name:  "flash",
			Value: "No unviewed thumbnails found",
			Path:  "/",
		})
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	// Update session with current thumbnail
	s.log.WithFields(map[string]interface{}{
		"thumbnailID":            thumbnail.ID,
		"sessionCurrentID":       session.CurrentID,
		"sessionViewedCount":     session.ViewedCount,
		"sessionNavigationCount": session.NavigationCount,
		"sessionPreviousID":      session.PreviousID,
		"newSession":             newSession,
	}).Info("Before session update check")

	shouldUpdateSession := false
	if newSession {
		// For new sessions, always set the first thumbnail without incrementing counters
		if session.CurrentID == 0 {
			s.log.Info("New session: setting first thumbnail without incrementing counters")
			session.CurrentID = thumbnail.ID
			shouldUpdateSession = true
		}
	} else if thumbnail.ID != session.CurrentID {
		// For existing sessions, only update if we're viewing a different thumbnail
		s.log.Info("Existing session: viewing different thumbnail, updating with navigation logic")
		if session.CurrentID > 0 {
			// This is actual navigation between thumbnails
			session.ViewedCount++
			session.NavigationCount++ // Track navigation
			session.PreviousID = session.CurrentID
		}
		session.CurrentID = thumbnail.ID
		shouldUpdateSession = true
	}

	if shouldUpdateSession {
		s.log.WithFields(map[string]interface{}{
			"newCurrentID":       session.CurrentID,
			"newViewedCount":     session.ViewedCount,
			"newNavigationCount": session.NavigationCount,
			"newPreviousID":      session.PreviousID,
		}).Info("Updating session")

		// Pre-determine the next thumbnail for prefetch coordination
		// Only do this if we don't already have a NextID or if this is a new session
		if session.NextID == 0 || newSession {
			nextThumbnail, err := s.db.GetRandomUnviewedThumbnail()
			if err == nil && nextThumbnail != nil {
				session.NextID = nextThumbnail.ID
				s.log.WithFields(logrus.Fields{
					"nextID":  session.NextID,
					"context": "slideshow_display",
				}).Info("Pre-determined next thumbnail for prefetch coordination")
			}
		}

		// Save the updated session
		if err := s.saveSessionToCookie(w, session); err != nil {
			s.log.WithError(err).Error("Failed to save updated session")
		}
	} else {
		s.log.Info("No session update needed")
	}

	// Calculate current position in this session
	position := session.NavigationCount + 1

	// Parse template
	tmpl, err := template.ParseFiles(filepath.Join(s.cfg.TemplatesDir, "slideshow.html"))
	if err != nil {
		s.log.WithError(err).Error("Failed to parse template")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Check if this is the last thumbnail by seeing if there are any more unviewed thumbnails
	// excluding the current one and any pending viewed thumbnails
	var excludeForCount []int64
	excludeForCount = append(excludeForCount, thumbnail.ID)
	// Also exclude the previous thumbnail that will be marked as viewed on next navigation
	if session.PreviousID > 0 && session.PreviousID != thumbnail.ID {
		excludeForCount = append(excludeForCount, session.PreviousID)
	}

	remainingThumbnail, err := s.db.GetRandomUnviewedThumbnailExcluding(excludeForCount...)
	isLastThumbnail := (err != nil || remainingThumbnail == nil)

	s.log.WithFields(logrus.Fields{
		"currentThumbnailID":  thumbnail.ID,
		"previousThumbnailID": session.PreviousID,
		"excludeForCount":     excludeForCount,
		"remainingThumbnail": func() interface{} {
			if remainingThumbnail != nil {
				return remainingThumbnail.ID
			}
			return "nil"
		}(),
		"isLastThumbnail": isLastThumbnail,
		"err":             err,
	}).Info("Last thumbnail check")

	// Render template with data
	data := struct {
		Thumbnail                   *models.Thumbnail
		Total                       int
		Current                     int
		HasPrevious                 bool
		PendingDelete               bool
		IsLastThumbnail             bool
		SessionDeletedSize          int64
		SessionDeletedSizeFormatted string
	}{
		Thumbnail:                   thumbnail,
		Total:                       session.TotalImages,
		Current:                     position,
		HasPrevious:                 session.PreviousID > 0 && session.PreviousID != session.CurrentID,
		PendingDelete:               session.PendingDelete,
		IsLastThumbnail:             isLastThumbnail,
		SessionDeletedSize:          session.DeletedSize,
		SessionDeletedSizeFormatted: formatBytes(session.DeletedSize),
	}

	if err := tmpl.Execute(w, data); err != nil {
		s.log.WithError(err).Error("Failed to render template")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
}

// handleSlideshowNext shows the next thumbnail in the slideshow
func (s *Server) handleSlideshowNext(w http.ResponseWriter, r *http.Request) {
	// Require valid session - redirect to /slideshow if none found
	session, ok := s.requireValidSession(w, r)
	if !ok {
		return // already redirected
	}

	// Get current ID from session
	currentID := session.CurrentID

	// Check if this is a skip operation (don't mark as viewed)
	skipViewing := r.URL.Query().Get("skip") == "true"

	// First, commit any pending viewing from previous navigation
	if session.PreviousID != 0 && session.PreviousID != currentID && !session.PendingDelete {
		// Mark the previous thumbnail as viewed (delayed from last navigation)
		if err := s.db.MarkAsViewedByID(session.PreviousID); err != nil {
			s.log.WithError(err).WithField("thumbnail_id", session.PreviousID).Error("Failed to mark previous thumbnail as viewed")
		} else {
			s.log.WithField("thumbnail_id", session.PreviousID).Info("Marked previous thumbnail as viewed (delayed)")
			session.ViewedCount++
		}
	}

	// Commit any pending deletion when moving to next (regardless of skip or normal navigation)
	if session.PendingDelete && session.PreviousID != 0 && session.PreviousID != currentID {
		// Get the thumbnail to obtain its file size before marking for deletion
		deletedThumbnail, err := s.db.GetByID(session.PreviousID)
		if err != nil {
			s.log.WithError(err).WithField("thumbnail_id", session.PreviousID).Error("Failed to get thumbnail for deletion size tracking")
		}

		if err := s.db.MarkForDeletionByID(session.PreviousID); err != nil {
			s.log.WithError(err).WithField("thumbnail_id", session.PreviousID).Error("Failed to commit pending deletion")
		} else {
			s.log.WithField("thumbnail_id", session.PreviousID).Info("Committed pending deletion to database")

			// Add the file size to the session's deleted size counter
			if deletedThumbnail != nil {
				session.DeletedSize += deletedThumbnail.FileSize
				s.log.WithFields(logrus.Fields{
					"thumbnail_id":       session.PreviousID,
					"file_size":          deletedThumbnail.FileSize,
					"total_deleted_size": session.DeletedSize,
				}).Info("Added deleted movie size to session counter")
			}
		}
		// Clear the pending deletion
		session.PendingDelete = false
	}

	// Get a random unviewed thumbnail instead of the next in sequence
	// But first check if we already have a NextID stored in session
	var nextThumbnail *models.Thumbnail
	var err error

	if session.NextID > 0 {
		// Use the pre-determined next thumbnail
		nextThumbnail, err = s.db.GetByID(session.NextID)
		if err != nil {
			s.log.WithError(err).WithField("nextID", session.NextID).Error("Failed to get predetermined next thumbnail")
			// Fall back to random
			session.NextID = 0
		} else if nextThumbnail != nil && nextThumbnail.IsViewed() {
			// The predetermined thumbnail was already viewed, get a new one
			s.log.WithField("nextID", session.NextID).Info("Predetermined next thumbnail was already viewed, getting new random")
			session.NextID = 0
			nextThumbnail = nil
		} else if nextThumbnail != nil {
			s.log.WithFields(logrus.Fields{
				"nextID":        session.NextID,
				"thumbnailPath": nextThumbnail.ThumbnailPath,
				"movieFilename": nextThumbnail.MovieFilename,
			}).Info("Using predetermined next thumbnail (coordinated with prefetcher)")
		}
	}

	// If we don't have a valid predetermined thumbnail, get a random one
	if nextThumbnail == nil {
		s.log.Info("Getting random thumbnail (no predetermined NextID or it was invalid)")

		// Exclude current ID to avoid getting the same thumbnail
		var excludeIDs []int64
		if currentID > 0 {
			excludeIDs = append(excludeIDs, currentID)
		}

		nextThumbnail, err = s.db.GetRandomUnviewedThumbnailExcluding(excludeIDs...)
		if err != nil {
			s.log.WithError(err).Error("Failed to get next thumbnail")
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
	}

	// If no next thumbnail, redirect to control page
	if nextThumbnail == nil {
		http.SetCookie(w, &http.Cookie{
			Name:  "flash",
			Value: "No more thumbnails to view",
			Path:  "/",
		})
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	// Now determine if we should set up undo for the current slide
	// Store current ID as previous for single undo, but don't mark as viewed yet
	if currentID > 0 && !skipViewing {
		thumbnail, err := s.db.GetByID(currentID)
		if err == nil && thumbnail != nil && thumbnail.Status != models.StatusDeleted {
			// Store current ID as previous for single undo (viewing will be deferred)
			session.PreviousID = currentID
		}
	} else if currentID > 0 && skipViewing {
		// For skip operation, check if the next thumbnail is different from current
		// If we're skipping the last thumbnail, nextThumbnail will be nil or same as current
		if nextThumbnail != nil && nextThumbnail.ID != currentID {
			// Store current ID as previous for navigation only if next is different
			session.PreviousID = currentID
		} else {
			// If we're skipping the last thumbnail or getting the same thumbnail,
			// don't update PreviousID to avoid same ID issue
			s.log.WithFields(logrus.Fields{
				"currentID": currentID,
				"nextID": func() int64 {
					if nextThumbnail != nil {
						return nextThumbnail.ID
					} else {
						return 0
					}
				}(),
			}).Info("Skip operation on last thumbnail or same thumbnail - not updating PreviousID")
		}
	}

	// Update session with next thumbnail
	session.CurrentID = nextThumbnail.ID
	session.NavigationCount++ // Increment navigation counter

	// Pre-determine the next thumbnail for coordination with prefetcher
	session.NextID = 0 // Reset first

	// Exclude current ID and previous ID to avoid duplicates
	var excludeIDs []int64
	excludeIDs = append(excludeIDs, session.CurrentID)
	if session.PreviousID > 0 {
		excludeIDs = append(excludeIDs, session.PreviousID)
	}

	nextNextThumbnail, err := s.db.GetRandomUnviewedThumbnailExcluding(excludeIDs...)
	if err == nil && nextNextThumbnail != nil {
		session.NextID = nextNextThumbnail.ID
		s.log.WithFields(logrus.Fields{
			"currentID": session.CurrentID,
			"nextID":    session.NextID,
		}).Debug("Pre-determined next thumbnail for prefetch coordination")
	}

	// Save the updated session
	if err := s.saveSessionToCookie(w, session); err != nil {
		s.log.WithError(err).Error("Failed to save updated session")
	}

	// Redirect to slideshow without ID parameter (uses session state)
	s.redirectToSlideshow(w, r)
}

// handleSlideshowPrevious implements undo functionality for deletions and navigation
func (s *Server) handleSlideshowPrevious(w http.ResponseWriter, r *http.Request) {
	// Require valid session - redirect to /slideshow if none found
	session, ok := s.requireValidSession(w, r)
	if !ok {
		return // already redirected
	}

	// Get current ID from session
	currentID := session.CurrentID

	// Check if there's a pending deletion to undo - if so, always clear it regardless of current position
	if session.PendingDelete {
		// This is an undo operation - clear the pending deletion
		s.log.WithFields(logrus.Fields{
			"thumbnail": currentID,
		}).Info("Undoing pending deletion")

		// Clear the pending deletion from session
		session.PendingDelete = false
		session.PreviousID = 0 // Reset previous ID so undo button gets disabled

		// Save the updated session
		if err := s.saveSessionToCookie(w, session); err != nil {
			s.log.WithError(err).Error("Failed to save session after undo")
		}

		// Stay on the current thumbnail (reload without delete flag)
		s.redirectToSlideshow(w, r)
		return
	}

	// Regular previous navigation - check if we have a previous thumbnail
	if session.PreviousID == 0 {
		// No previous thumbnail, redirect back to current
		s.redirectToSlideshow(w, r)
		return
	}

	// Get the previous ID and check if it's valid
	var prevID int64 = 0
	var validPrevFound bool = false

	// With single undo, we only check the previous ID
	if session.PreviousID > 0 {
		// Check if this thumbnail still exists and is not deleted
		prevThumbnail, err := s.db.GetByID(session.PreviousID)
		if err == nil && prevThumbnail != nil && prevThumbnail.Status != models.StatusDeleted {
			prevID = session.PreviousID
			validPrevFound = true
		}
	}

	// If no valid previous thumbnail found, stay on current
	if !validPrevFound {
		s.redirectToSlideshow(w, r)
		return
	}

	// Update session with previous thumbnail ID
	session.CurrentID = prevID
	session.NextID = currentID // Save current slide as next ID for return navigation
	session.PreviousID = 0     // Clear previous ID after going back (single undo consumed)

	// When undoing navigation, we don't want to mark the previous slide as viewed
	// since the user is going back to it

	// Save the updated session
	if err := s.saveSessionToCookie(w, session); err != nil {
		s.log.WithError(err).Error("Failed to save session after navigation")
	}

	// Redirect to slideshow without ID parameter (uses session state)
	s.redirectToSlideshow(w, r)
}

// handleMarkViewed marks the current thumbnail as viewed using session data
func (s *Server) handleMarkViewed(w http.ResponseWriter, r *http.Request) {
	// Require valid session - redirect to /slideshow if none found
	session, ok := s.requireValidSession(w, r)
	if !ok {
		return // already redirected
	}

	// Use current ID from session
	thumbnailID := session.CurrentID
	if thumbnailID == 0 {
		http.Error(w, "No current thumbnail in session", http.StatusBadRequest)
		return
	}

	// Mark as viewed using session's current ID
	if err := s.db.MarkAsViewedByID(thumbnailID); err != nil {
		s.log.WithError(err).WithField("thumbnail_id", thumbnailID).Error("Failed to mark as viewed")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Update session viewed count
	session.ViewedCount++

	// Save the updated session
	if err := s.saveSessionToCookie(w, session); err != nil {
		s.log.WithError(err).Error("Failed to save session after marking viewed")
	}

	// If ajax request, return JSON response
	if r.Header.Get("X-Requested-With") == "XMLHttpRequest" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"success": true})
		return
	}

	// Otherwise redirect to next (no longer passing current ID)
	http.Redirect(w, r, "/slideshow/next", http.StatusSeeOther)
}

// handleDelete marks a movie for deletion in the session (soft delete with undo capability)
func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	// Require valid session - redirect to /slideshow if none found
	session, ok := s.requireValidSession(w, r)
	if !ok {
		return // already redirected
	}

	// Use current ID from session
	thumbnailID := session.CurrentID
	if thumbnailID == 0 {
		http.Error(w, "No current thumbnail in session", http.StatusBadRequest)
		return
	}

	// Get the thumbnail record
	thumbnail, err := s.db.GetByID(thumbnailID)
	if err != nil {
		s.log.WithError(err).WithField("thumbnail_id", thumbnailID).Error("Failed to get thumbnail")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	if thumbnail == nil {
		http.Error(w, "Thumbnail not found", http.StatusNotFound)
		return
	}

	// If there's already a pending deletion, commit it to the database first
	if session.PendingDelete && session.PreviousID != 0 {
		// Get the thumbnail to obtain its file size before marking for deletion
		deletedThumbnail, err := s.db.GetByID(session.PreviousID)
		if err != nil {
			s.log.WithError(err).WithField("thumbnail_id", session.PreviousID).Error("Failed to get thumbnail for deletion size tracking")
		}

		if err := s.db.MarkForDeletionByID(session.PreviousID); err != nil {
			s.log.WithError(err).WithField("thumbnail_id", session.PreviousID).Error("Failed to commit pending deletion")
			// Continue anyway - don't fail the current operation
		} else {
			s.log.WithField("thumbnail_id", session.PreviousID).Info("Committed pending deletion to database")

			// Add the file size to the session's deleted size counter
			if deletedThumbnail != nil {
				session.DeletedSize += deletedThumbnail.FileSize
				s.log.WithFields(logrus.Fields{
					"thumbnail_id":       session.PreviousID,
					"file_size":          deletedThumbnail.FileSize,
					"total_deleted_size": session.DeletedSize,
				}).Info("Added deleted movie size to session counter")
			}
		}
	}

	// Mark the current thumbnail for deletion in the session only (not in database yet)
	session.PreviousID = thumbnail.ID // Set as previous for undo functionality
	session.PendingDelete = true      // Flag that PreviousID is pending deletion

	// Save the updated session
	if err := s.saveSessionToCookie(w, session); err != nil {
		s.log.WithError(err).Error("Failed to save session after marking for deletion")
	}

	s.log.WithFields(logrus.Fields{
		"movie":        thumbnail.MoviePath,
		"thumbnail_id": thumbnail.ID,
	}).Info("Marked movie for deletion in session (pending)")

	// If ajax request, return JSON response
	if r.Header.Get("X-Requested-With") == "XMLHttpRequest" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"success": true})
		return
	}

	// Otherwise redirect to next (no longer passing current ID)
	http.Redirect(w, r, "/slideshow/next", http.StatusSeeOther)
}

// handleUndoDelete restores a movie that was marked for deletion
func (s *Server) handleUndoDelete(w http.ResponseWriter, r *http.Request) {
	// Get thumbnail ID from form
	thumbnailIDStr := r.FormValue("id")
	if thumbnailIDStr == "" {
		http.Error(w, "Thumbnail ID is required", http.StatusBadRequest)
		return
	}

	thumbnailID, err := strconv.ParseInt(thumbnailIDStr, 10, 64)
	if err != nil {
		s.log.WithError(err).Error("Invalid thumbnail ID")
		http.Error(w, "Invalid thumbnail ID", http.StatusBadRequest)
		return
	}

	// Get the thumbnail record
	thumbnail, err := s.db.GetByID(thumbnailID)
	if err != nil {
		s.log.WithError(err).WithField("thumbnail_id", thumbnailID).Error("Failed to get thumbnail")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	if thumbnail == nil {
		http.Error(w, "Thumbnail not found", http.StatusNotFound)
		return
	}

	// Make sure it's marked as deleted
	if thumbnail.Status != models.StatusDeleted {
		http.Error(w, "Thumbnail is not marked for deletion", http.StatusBadRequest)
		return
	}

	// Restore the thumbnail by setting status back to success
	if err := s.db.RestoreFromDeletionByID(thumbnailID); err != nil {
		s.log.WithError(err).WithField("thumbnail_id", thumbnailID).Error("Failed to restore from deletion")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	s.log.WithField("thumbnail_id", thumbnailID).WithField("movie", thumbnail.MoviePath).Info("Restored movie from deletion")

	// If ajax request, return JSON response
	if r.Header.Get("X-Requested-With") == "XMLHttpRequest" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"success": true})
		return
	}

	// Otherwise redirect to control page
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// handleStats returns statistics as JSON
func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	stats, err := s.scanner.GetStats()
	if err != nil {
		s.log.WithError(err).Error("Failed to get stats")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

// handleThumbnails returns a list of thumbnails as JSON
func (s *Server) handleThumbnails(w http.ResponseWriter, r *http.Request) {
	// Get query parameters
	status := r.URL.Query().Get("status")
	viewed := r.URL.Query().Get("viewed")
	limitStr := r.URL.Query().Get("limit")

	// Default limit of 10 if not specified
	limit := 10
	if limitStr != "" {
		var err error
		limit, err = strconv.Atoi(limitStr)
		if err != nil {
			limit = 10 // Default to 10 on parse error
		}
	}

	var thumbnails []*models.Thumbnail
	var err error

	// Get thumbnails based on filters
	if status == "success" && viewed == "0" {
		thumbnails, err = s.db.GetUnviewedThumbnails()
	} else if status == "success" && viewed == "1" {
		thumbnails, err = s.db.GetViewedThumbnails()
	} else if status == "pending" {
		thumbnails, err = s.db.GetPendingThumbnails()
	} else if status == "error" {
		thumbnails, err = s.db.GetErrorThumbnails()
	} else if status == "deleted" {
		thumbnails, err = s.db.GetDeletedThumbnails(limit)
	} else {
		thumbnails, err = s.db.GetAllThumbnails()
	}

	if err != nil {
		s.log.WithError(err).Error("Failed to get thumbnails")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(thumbnails)
}

// handleThumbnail returns a single thumbnail as JSON
func (s *Server) handleThumbnail(w http.ResponseWriter, r *http.Request) {
	// Get thumbnail ID from URL
	vars := mux.Vars(r)
	idStr := vars["id"]

	// Convert ID from string to int64
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		s.log.WithError(err).WithField("id", idStr).Error("Invalid thumbnail ID")
		http.Error(w, "Invalid thumbnail ID", http.StatusBadRequest)
		return
	}

	// Get thumbnail by ID - we need to add this method to the database package
	thumbnail, err := s.db.GetByID(id)
	if err != nil {
		s.log.WithError(err).WithField("id", id).Error("Failed to get thumbnail")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Check if thumbnail was found
	if thumbnail == nil {
		http.Error(w, "Thumbnail not found", http.StatusNotFound)
		return
	}

	// Return thumbnail as JSON
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(thumbnail); err != nil {
		s.log.WithError(err).Error("Failed to encode thumbnail")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
}

// handleSlideshowNextImage returns the next thumbnail image path without navigation
func (s *Server) handleSlideshowNextImage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Require valid session
	session, err := s.getSessionFromCookie(r)
	if err != nil {
		s.log.WithError(err).Debug("No valid session found for next image request")
		http.Error(w, "No slideshow session found", http.StatusBadRequest)
		return
	}

	// Get next thumbnail using the pre-determined NextID from session
	var nextThumbnail *models.Thumbnail
	if session.NextID > 0 {
		nextThumbnail, err = s.db.GetByID(session.NextID)
		if err != nil {
			s.log.WithError(err).WithField("nextID", session.NextID).Error("Failed to get predetermined next thumbnail for prefetch")
			// Return empty response instead of error to not break the UI
			json.NewEncoder(w).Encode(map[string]interface{}{
				"hasNext": false,
			})
			return
		}

		// Double-check the thumbnail is still unviewed
		if nextThumbnail != nil && nextThumbnail.IsViewed() {
			s.log.WithField("nextID", session.NextID).Info("Predetermined next thumbnail was already viewed")
			nextThumbnail = nil
		} else if nextThumbnail != nil {
			s.log.WithFields(logrus.Fields{
				"nextID":        session.NextID,
				"thumbnailPath": nextThumbnail.ThumbnailPath,
				"movieFilename": nextThumbnail.MovieFilename,
			}).Info("Using predetermined next thumbnail for prefetch")
		}
	} else {
		s.log.WithField("sessionNextID", session.NextID).Info("No NextID in session, cannot prefetch")
	}

	if nextThumbnail == nil {
		// No more thumbnails
		s.log.Info("No next thumbnail available for prefetch")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"hasNext": false,
		})
		return
	}

	// Return the thumbnail path for prefetching
	response := map[string]interface{}{
		"hasNext":       true,
		"thumbnailPath": nextThumbnail.ThumbnailPath,
		"movieFilename": nextThumbnail.MovieFilename,
	}

	s.log.WithFields(logrus.Fields{
		"thumbnailPath": nextThumbnail.ThumbnailPath,
		"movieFilename": nextThumbnail.MovieFilename,
	}).Debug("Providing next image for prefetch")

	json.NewEncoder(w).Encode(response)
}

// handleNotFound handles 404 errors
func (s *Server) handleNotFound(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNotFound)
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte("<html><body><h1>404 Not Found</h1><p>The requested page could not be found.</p></body></html>"))
}

// handleSlideshowFinish marks the current thumbnail as viewed and ends the slideshow session
func (s *Server) handleSlideshowFinish(w http.ResponseWriter, r *http.Request) {
	// Require valid session - redirect to /slideshow if none found
	session, ok := s.requireValidSession(w, r)
	if !ok {
		return // already redirected
	}

	// Get current ID from session
	currentID := session.CurrentID
	if currentID == 0 {
		http.Error(w, "No current thumbnail in session", http.StatusBadRequest)
		return
	}

	// First, commit any pending viewing from previous navigation
	if session.PreviousID != 0 && session.PreviousID != currentID && !session.PendingDelete {
		// Mark the previous thumbnail as viewed (delayed from last navigation)
		if err := s.db.MarkAsViewedByID(session.PreviousID); err != nil {
			s.log.WithError(err).WithField("thumbnail_id", session.PreviousID).Error("Failed to mark previous thumbnail as viewed during finish")
		} else {
			s.log.WithField("thumbnail_id", session.PreviousID).Info("Marked previous thumbnail as viewed (delayed) during finish")
		}
	}

	// Mark the current thumbnail as viewed
	if err := s.db.MarkAsViewedByID(currentID); err != nil {
		s.log.WithError(err).WithField("thumbnail_id", currentID).Error("Failed to mark thumbnail as viewed")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	s.log.WithField("thumbnail_id", currentID).Info("Marked last thumbnail as viewed and finishing slideshow")

	// Clear the session cookie to end the slideshow
	http.SetCookie(w, &http.Cookie{
		Name:    "slideshow_session",
		Value:   "",
		Path:    "/",
		Expires: time.Unix(0, 0), // Expire immediately
	})

	// Set success message
	http.SetCookie(w, &http.Cookie{
		Name:  "flash",
		Value: "Slideshow completed! All thumbnails have been viewed.",
		Path:  "/",
	})

	// Redirect to control page
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// handleDeleteAndFinish deletes the current thumbnail and ends the slideshow session
func (s *Server) handleDeleteAndFinish(w http.ResponseWriter, r *http.Request) {
	// Require valid session - redirect to /slideshow if none found
	session, ok := s.requireValidSession(w, r)
	if !ok {
		return // already redirected
	}

	// Get current ID from session
	currentID := session.CurrentID
	if currentID == 0 {
		http.Error(w, "No current thumbnail in session", http.StatusBadRequest)
		return
	}

	// Get the thumbnail record
	thumbnail, err := s.db.GetByID(currentID)
	if err != nil {
		s.log.WithError(err).WithField("thumbnail_id", currentID).Error("Failed to get thumbnail")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	if thumbnail == nil {
		http.Error(w, "Thumbnail not found", http.StatusNotFound)
		return
	}

	// Immediately mark for deletion in database (no undo for last thumbnail)
	if err := s.db.MarkForDeletionByID(currentID); err != nil {
		s.log.WithError(err).WithField("thumbnail_id", currentID).Error("Failed to mark thumbnail for deletion")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Add the file size to the session's deleted size counter
	session.DeletedSize += thumbnail.FileSize

	s.log.WithFields(logrus.Fields{
		"thumbnail_id":       currentID,
		"movie_path":         thumbnail.MoviePath,
		"file_size":          thumbnail.FileSize,
		"total_deleted_size": session.DeletedSize,
	}).Info("Marked last thumbnail for deletion and finishing slideshow")

	// Clear the session cookie to end the slideshow
	http.SetCookie(w, &http.Cookie{
		Name:    "slideshow_session",
		Value:   "",
		Path:    "/",
		Expires: time.Unix(0, 0), // Expire immediately
	})

	// Set success message
	http.SetCookie(w, &http.Cookie{
		Name:  "flash",
		Value: "Thumbnail deleted and slideshow completed!",
		Path:  "/",
	})

	// If ajax request, return JSON response
	if r.Header.Get("X-Requested-With") == "XMLHttpRequest" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success":  true,
			"redirect": "/",
		})
		return
	}

	// Redirect to control page
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
