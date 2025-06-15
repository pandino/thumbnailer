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

type SessionData struct {
	TotalImages int   `json:"total_images"`
	ViewedCount int   `json:"viewed_count"`
	CurrentID   int64 `json:"current_id"`
	StartedAt   int64 `json:"started_at"`
	PreviousID  int64 `json:"previous_id"` // Store previous thumbnail ID for single undo
	NextID      int64 `json:"next_id"`     // Store next thumbnail ID for coordination with prefetcher
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
		Stats              *models.Stats
		IsScanning         bool
		HasSession         bool
		SessionViewedCount int
		SessionTotalCount  int
	}{
		Stats:              stats,
		IsScanning:         s.scanner.IsScanning(),
		HasSession:         hasSession,
		SessionViewedCount: sessionViewedCount,
		SessionTotalCount:  sessionTotalCount,
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

	// Initialize session data
	var session SessionData

	// Handle cookie-based session
	if newSession {
		// Create a new session - get the total count of unviewed thumbnails
		stats, err := s.scanner.GetStats()
		if err != nil {
			s.log.WithError(err).Error("Failed to get stats")
			// Continue with zero count as fallback
			stats = &models.Stats{}
		}

		// Initialize new session with the count of unviewed thumbnails
		session = SessionData{
			TotalImages: stats.Unviewed,
			ViewedCount: 0,
			CurrentID:   0,
			StartedAt:   time.Now().Unix(),
			PreviousID:  0, // Initialize empty previous ID
			NextID:      0, // Initialize empty next ID
		}
		s.log.Info("Created new session with CurrentID=0, ViewedCount=0, PreviousID=0, NextID=0")

		// Save to cookie
		sessionJSON, err := json.Marshal(session)
		if err != nil {
			s.log.WithError(err).Error("Failed to marshal session data")
			// Continue without session data
		} else {
			http.SetCookie(w, &http.Cookie{
				Name:     "slideshow_session",
				Value:    base64.StdEncoding.EncodeToString(sessionJSON),
				Path:     "/",
				MaxAge:   86400 * 30, // 30 days
				HttpOnly: true,
			})
		}
	} else {
		// Try to get existing session from cookie
		sessionCookie, err := r.Cookie("slideshow_session")
		if err == nil && sessionCookie.Value != "" {
			// Decode the cookie value
			jsonData, err := base64.StdEncoding.DecodeString(sessionCookie.Value)
			if err == nil {
				err = json.Unmarshal(jsonData, &session)
				if err != nil {
					s.log.WithError(err).Error("Failed to unmarshal session data") // Initialize new session as fallback
					stats, err := s.scanner.GetStats()
					if err != nil {
						stats = &models.Stats{}
					}
					session = SessionData{
						TotalImages: stats.Unviewed,
						ViewedCount: 0,
						CurrentID:   0,
						StartedAt:   time.Now().Unix(),
						PreviousID:  0,
						NextID:      0,
					}
				}
			}
		} else {
			// No session cookie found or empty value, initialize a default session
			stats, err := s.scanner.GetStats()
			if err != nil {
				stats = &models.Stats{}
			}
			session = SessionData{
				TotalImages: stats.Unviewed,
				ViewedCount: 0,
				CurrentID:   0,
				StartedAt:   time.Now().Unix(),
				PreviousID:  0,
			}

			// Save to cookie
			sessionJSON, err := json.Marshal(session)
			if err == nil {
				http.SetCookie(w, &http.Cookie{
					Name:     "slideshow_session",
					Value:    base64.StdEncoding.EncodeToString(sessionJSON),
					Path:     "/",
					MaxAge:   86400 * 30, // 30 days
					HttpOnly: true,
				})
			}
		}
	}

	// Check if an ID is specified in the query string for a specific thumbnail
	idParam := r.URL.Query().Get("id")
	var targetID int64 = session.CurrentID // Use session's current ID as default

	if idParam != "" {
		// Override with specific ID from query parameter
		var err error
		targetID, err = strconv.ParseInt(idParam, 10, 64)
		if err != nil {
			s.log.WithError(err).Error("Invalid ID parameter")
			http.Error(w, "Invalid ID parameter", http.StatusBadRequest)
			return
		}
	}

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
		"thumbnailID":        thumbnail.ID,
		"sessionCurrentID":   session.CurrentID,
		"sessionViewedCount": session.ViewedCount,
		"sessionPreviousID":  session.PreviousID,
		"newSession":         newSession,
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
			session.PreviousID = session.CurrentID
		}
		session.CurrentID = thumbnail.ID
		shouldUpdateSession = true
	}

	if shouldUpdateSession {
		s.log.WithFields(map[string]interface{}{
			"newCurrentID":   session.CurrentID,
			"newViewedCount": session.ViewedCount,
			"newPreviousID":  session.PreviousID,
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
		sessionJSON, err := json.Marshal(session)
		if err == nil {
			http.SetCookie(w, &http.Cookie{
				Name:     "slideshow_session",
				Value:    base64.StdEncoding.EncodeToString(sessionJSON),
				Path:     "/",
				MaxAge:   86400 * 30, // 30 days
				HttpOnly: true,
			})
		}
	} else {
		s.log.Info("No session update needed")
	}

	// Calculate current position in this session
	position := session.ViewedCount + 1

	// Check if we have a valid previous thumbnail for undo
	backCount := 0
	if session.PreviousID > 0 {
		// Check if the previous thumbnail exists and is not deleted
		prevThumb, err := s.db.GetByID(session.PreviousID)
		if err == nil && prevThumb != nil && prevThumb.Status != models.StatusDeleted {
			backCount = 1
		}
	}

	// Parse template
	tmpl, err := template.ParseFiles(filepath.Join(s.cfg.TemplatesDir, "slideshow.html"))
	if err != nil {
		s.log.WithError(err).Error("Failed to parse template")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Render template with data
	data := struct {
		Thumbnail   *models.Thumbnail
		Total       int
		Current     int
		BackCount   int
		HasPrevious bool
	}{
		Thumbnail:   thumbnail,
		Total:       session.TotalImages,
		Current:     position,
		BackCount:   backCount,
		HasPrevious: session.PreviousID > 0,
	}

	if err := tmpl.Execute(w, data); err != nil {
		s.log.WithError(err).Error("Failed to render template")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
}

// handleSlideshowNext shows the next thumbnail in the slideshow
func (s *Server) handleSlideshowNext(w http.ResponseWriter, r *http.Request) {
	// Get current thumbnail ID from query string
	currentIDStr := r.URL.Query().Get("current")
	var currentID int64 = 0

	if currentIDStr != "" {
		var err error
		currentID, err = strconv.ParseInt(currentIDStr, 10, 64)
		if err != nil {
			s.log.WithError(err).Error("Invalid current ID")
			http.Error(w, "Invalid ID parameter", http.StatusBadRequest)
			return
		}
	}

	// Get session from cookie
	var session SessionData
	sessionCookie, err := r.Cookie("slideshow_session")
	if err == nil && sessionCookie.Value != "" {
		// Decode the cookie value
		jsonData, err := base64.StdEncoding.DecodeString(sessionCookie.Value)
		if err == nil {
			err = json.Unmarshal(jsonData, &session)
			if err != nil {
				// Reset session on unmarshal error
				session = SessionData{
					TotalImages: 0,
					ViewedCount: 0,
					CurrentID:   0,
					StartedAt:   time.Now().Unix(),
					PreviousID:  0,
				}
			}
		}
	}

	// Check if this is a skip operation (don't mark as viewed)
	skipViewing := r.URL.Query().Get("skip") == "true"

	// Mark current thumbnail as viewed (unless skipping)
	if currentID > 0 && !skipViewing {
		thumbnail, err := s.db.GetByID(currentID)
		if err == nil && thumbnail != nil && thumbnail.Status != models.StatusDeleted {
			err = s.db.MarkAsViewed(thumbnail.ThumbnailPath)
			if err != nil {
				s.log.WithError(err).Error("Failed to mark as viewed")
				// Continue anyway
			}

			// Update session viewed count if this is the current session thumbnail
			if currentID == session.CurrentID {
				session.ViewedCount++

				// Store current ID as previous for single undo
				session.PreviousID = currentID
			}
		}
	} else if currentID > 0 && skipViewing {
		// For skip operation, just update the session to track the current ID as previous for navigation
		session.PreviousID = currentID
	}

	// Get a random unviewed thumbnail instead of the next in sequence
	// But first check if we already have a NextID stored in session
	var nextThumbnail *models.Thumbnail

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
		nextThumbnail, err = s.db.GetRandomUnviewedThumbnail()
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

	// Update session with next thumbnail
	session.CurrentID = nextThumbnail.ID

	// Pre-determine the next thumbnail for coordination with prefetcher
	session.NextID = 0 // Reset first
	nextNextThumbnail, err := s.db.GetRandomUnviewedThumbnail()
	if err == nil && nextNextThumbnail != nil {
		session.NextID = nextNextThumbnail.ID
		s.log.WithFields(logrus.Fields{
			"currentID": session.CurrentID,
			"nextID":    session.NextID,
		}).Debug("Pre-determined next thumbnail for prefetch coordination")
	}

	// Save the updated session
	sessionJSON, err := json.Marshal(session)
	if err == nil {
		http.SetCookie(w, &http.Cookie{
			Name:     "slideshow_session",
			Value:    base64.StdEncoding.EncodeToString(sessionJSON),
			Path:     "/",
			MaxAge:   86400 * 30, // 30 days
			HttpOnly: true,
		})
	}

	// Redirect to slideshow with next thumbnail ID
	http.Redirect(w, r, fmt.Sprintf("/slideshow?id=%d", nextThumbnail.ID), http.StatusSeeOther)
}

// handleSlideshowPrevious shows the previous thumbnail
func (s *Server) handleSlideshowPrevious(w http.ResponseWriter, r *http.Request) {
	// Get current thumbnail ID from query string
	currentIDStr := r.URL.Query().Get("current")
	var currentID int64 = 0

	if currentIDStr != "" {
		var err error
		currentID, err = strconv.ParseInt(currentIDStr, 10, 64)
		if err != nil {
			s.log.WithError(err).Error("Invalid current ID")
			http.Error(w, "Invalid ID parameter", http.StatusBadRequest)
			return
		}
	}

	// Get session data from cookie
	var session SessionData
	sessionCookie, err := r.Cookie("slideshow_session")
	if err == nil && sessionCookie.Value != "" {
		// Decode the cookie value
		jsonData, err := base64.StdEncoding.DecodeString(sessionCookie.Value)
		if err == nil {
			err = json.Unmarshal(jsonData, &session)
			if err != nil {
				// Reset session on unmarshal error
				session = SessionData{
					TotalImages: 0,
					ViewedCount: 0,
					CurrentID:   0,
					StartedAt:   time.Now().Unix(),
					PreviousID:  0,
				}
			}
		}
	}

	// Check if we have a previous thumbnail
	if session.PreviousID == 0 {
		// No previous thumbnail, redirect back to current
		http.Redirect(w, r, fmt.Sprintf("/slideshow?id=%d", currentID), http.StatusSeeOther)
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
		http.Redirect(w, r, fmt.Sprintf("/slideshow?id=%d", currentID), http.StatusSeeOther)
		return
	}

	// Update session with previous thumbnail ID
	session.CurrentID = prevID
	session.PreviousID = 0 // Clear previous ID after going back (single undo consumed)

	// Save the updated session
	sessionJSON, err := json.Marshal(session)
	if err == nil {
		http.SetCookie(w, &http.Cookie{
			Name:     "slideshow_session",
			Value:    base64.StdEncoding.EncodeToString(sessionJSON),
			Path:     "/",
			MaxAge:   86400 * 30, // 30 days
			HttpOnly: true,
		})
	}

	// Redirect to slideshow with previous thumbnail ID
	http.Redirect(w, r, fmt.Sprintf("/slideshow?id=%d", prevID), http.StatusSeeOther)
}

// handleMarkViewed marks a thumbnail as viewed
func (s *Server) handleMarkViewed(w http.ResponseWriter, r *http.Request) {
	// Get thumbnail path from form
	thumbnailPath := r.FormValue("path")
	if thumbnailPath == "" {
		http.Error(w, "Thumbnail path is required", http.StatusBadRequest)
		return
	}

	// Get thumbnail ID from form
	thumbnailIDStr := r.FormValue("id")
	var thumbnailID int64 = 0
	if thumbnailIDStr != "" {
		var err error
		thumbnailID, err = strconv.ParseInt(thumbnailIDStr, 10, 64)
		if err != nil {
			s.log.WithError(err).Error("Invalid thumbnail ID")
			// Continue anyway
		}
	}

	// Get session data from cookie
	type SessionData struct {
		TotalImages int   `json:"total_images"`
		ViewedCount int   `json:"viewed_count"`
		CurrentID   int64 `json:"current_id"`
		StartedAt   int64 `json:"started_at"`
	}

	var session SessionData
	sessionCookie, err := r.Cookie("slideshow_session")
	if err == nil && sessionCookie.Value != "" {
		// Decode the cookie value
		jsonData, err := base64.StdEncoding.DecodeString(sessionCookie.Value)
		if err == nil {
			err = json.Unmarshal(jsonData, &session)
			if err != nil {
				// Initialize a default session on unmarshal error
				session = SessionData{
					TotalImages: 0,
					ViewedCount: 0,
					CurrentID:   0,
					StartedAt:   time.Now().Unix(),
				}
			}
		}
	}

	// Mark as viewed
	if err := s.db.MarkAsViewed(thumbnailPath); err != nil {
		s.log.WithError(err).WithField("thumbnail", thumbnailPath).Error("Failed to mark as viewed")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Update session if the ID matches current session ID
	if thumbnailID > 0 && thumbnailID == session.CurrentID {
		session.ViewedCount++
		session.CurrentID = thumbnailID

		// Save the updated session
		sessionJSON, err := json.Marshal(session)
		if err == nil {
			http.SetCookie(w, &http.Cookie{
				Name:     "slideshow_session",
				Value:    base64.StdEncoding.EncodeToString(sessionJSON),
				Path:     "/",
				MaxAge:   86400 * 30, // 30 days
				HttpOnly: true,
			})
		}
	}

	// If ajax request, return JSON response
	if r.Header.Get("X-Requested-With") == "XMLHttpRequest" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"success": true})
		return
	}

	// Otherwise redirect to next
	http.Redirect(w, r, fmt.Sprintf("/slideshow/next?current=%s", thumbnailIDStr), http.StatusSeeOther)
}

// handleDelete marks a movie and its thumbnail for deletion
func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	// Get movie path from form
	moviePath := r.FormValue("path")
	if moviePath == "" {
		http.Error(w, "Movie path is required", http.StatusBadRequest)
		return
	}

	// Get the thumbnail record
	thumbnail, err := s.db.GetByMoviePath(moviePath)
	if err != nil {
		s.log.WithError(err).WithField("movie", moviePath).Error("Failed to get thumbnail")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	if thumbnail == nil {
		http.Error(w, "Movie not found", http.StatusNotFound)
		return
	}

	// Mark for deletion instead of deleting immediately
	if err := s.db.MarkForDeletion(moviePath); err != nil {
		s.log.WithError(err).WithField("movie", moviePath).Error("Failed to mark for deletion")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	s.log.WithField("movie", moviePath).Info("Marked movie for deletion")

	// If ajax request, return JSON response
	if r.Header.Get("X-Requested-With") == "XMLHttpRequest" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"success": true})
		return
	}

	// Otherwise redirect to next
	http.Redirect(w, r, "/slideshow/next", http.StatusSeeOther)
}

// handleUndoDelete restores a movie that was marked for deletion
func (s *Server) handleUndoDelete(w http.ResponseWriter, r *http.Request) {
	// Get movie path from form
	moviePath := r.FormValue("path")
	if moviePath == "" {
		http.Error(w, "Movie path is required", http.StatusBadRequest)
		return
	}

	// Get the thumbnail record
	thumbnail, err := s.db.GetByMoviePath(moviePath)
	if err != nil {
		s.log.WithError(err).WithField("movie", moviePath).Error("Failed to get thumbnail")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	if thumbnail == nil {
		http.Error(w, "Movie not found", http.StatusNotFound)
		return
	}

	// Make sure it's marked as deleted
	if thumbnail.Status != models.StatusDeleted {
		http.Error(w, "Movie is not marked for deletion", http.StatusBadRequest)
		return
	}

	// Restore the thumbnail by setting status back to success
	if err := s.db.RestoreFromDeletion(moviePath); err != nil {
		s.log.WithError(err).WithField("movie", moviePath).Error("Failed to restore from deletion")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	s.log.WithField("movie", moviePath).Info("Restored movie from deletion")

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

	// Get session cookie to verify we have an active slideshow session
	sessionCookie, err := r.Cookie("slideshow_session")
	if err != nil {
		// No session, return error
		http.Error(w, "No slideshow session found", http.StatusBadRequest)
		return
	}

	// Decode session to verify it's valid
	sessionData, err := base64.StdEncoding.DecodeString(sessionCookie.Value)
	if err != nil {
		s.log.WithError(err).Error("Failed to decode session")
		http.Error(w, "Invalid session", http.StatusBadRequest)
		return
	}

	var session SessionData
	if err := json.Unmarshal(sessionData, &session); err != nil {
		s.log.WithError(err).Error("Failed to unmarshal session")
		http.Error(w, "Invalid session", http.StatusBadRequest)
		return
	}

	// Get next thumbnail using the pre-determined NextID from session
	var nextThumbnail *models.Thumbnail
	if session.NextID > 0 {
		var err error
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
