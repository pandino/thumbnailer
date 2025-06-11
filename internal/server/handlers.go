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
)

type SessionData struct {
	TotalImages int     `json:"total_images"`
	ViewedCount int     `json:"viewed_count"`
	CurrentID   int64   `json:"current_id"`
	StartedAt   int64   `json:"started_at"`
	History     []int64 `json:"history"` // Store previous thumbnail IDs (limited to 2)
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

// handleResetHistory resets the slideshow history
func (s *Server) handleResetHistory(w http.ResponseWriter, r *http.Request) {
	// Get session data from cookie
	var session SessionData
	sessionCookie, err := r.Cookie("slideshow_session")
	if err == nil && sessionCookie.Value != "" {
		// Decode the cookie value
		jsonData, err := base64.StdEncoding.DecodeString(sessionCookie.Value)
		if err == nil {
			err = json.Unmarshal(jsonData, &session)
			if err != nil {
				// Initialize default session on unmarshal error
				session = SessionData{
					TotalImages: 0,
					ViewedCount: 0,
					CurrentID:   0,
					StartedAt:   time.Now().Unix(),
					History:     []int64{},
				}
			}
		}
	}

	// Reset the history array but keep other session data
	session.History = []int64{}

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

	// If AJAX request, return JSON
	if r.Header.Get("X-Requested-With") == "XMLHttpRequest" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"success": true})
		return
	}

	// Get a random unviewed thumbnail
	randomThumbnail, err := s.db.GetRandomUnviewedThumbnail()
	if err != nil {
		s.log.WithError(err).Error("Failed to get random thumbnail")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// If no thumbnails found, redirect to control page
	if randomThumbnail == nil {
		http.SetCookie(w, &http.Cookie{
			Name:  "flash",
			Value: "No unviewed thumbnails found",
			Path:  "/",
		})
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	// Redirect to slideshow with the random thumbnail
	http.Redirect(w, r, fmt.Sprintf("/slideshow?id=%d", randomThumbnail.ID), http.StatusSeeOther)
}

// handleSlideshow renders the slideshow page
func (s *Server) handleSlideshow(w http.ResponseWriter, r *http.Request) {
	// Check if a new session was requested
	newSession := r.URL.Query().Get("new") == "true"

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
			History:     []int64{}, // Initialize empty history
		}

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
					s.log.WithError(err).Error("Failed to unmarshal session data")
					// Initialize new session as fallback
					stats, err := s.scanner.GetStats()
					if err != nil {
						stats = &models.Stats{}
					}
					session = SessionData{
						TotalImages: stats.Unviewed,
						ViewedCount: 0,
						CurrentID:   0,
						StartedAt:   time.Now().Unix(),
						History:     []int64{},
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
				History:     []int64{},
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
	var currentID int64 = session.CurrentID
	if idParam != "" {
		var err error
		currentID, err = strconv.ParseInt(idParam, 10, 64)
		if err != nil {
			s.log.WithError(err).Error("Invalid ID parameter")
			http.Error(w, "Invalid ID parameter", http.StatusBadRequest)
			return
		}
	}

	// Get the thumbnail to display
	var thumbnail *models.Thumbnail
	var err error

	if currentID > 0 {
		// Get the specified thumbnail
		thumbnail, err = s.db.GetByID(currentID)
	} else {
		// Get a random unviewed thumbnail instead of the first one
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

	// Update session with current thumbnail if it's changed
	if thumbnail.ID != session.CurrentID {
		// If it's a new thumbnail, push the previous to history stack and increment the viewed count
		if session.CurrentID > 0 && thumbnail.ID != session.CurrentID {
			session.ViewedCount++

			// Only store the previous ID in history if it's not already in history
			// and it's a valid ID (not 0)
			if session.CurrentID > 0 && !contains(session.History, session.CurrentID) {
				// Add current to history, limit to 2 items
				session.History = append([]int64{session.CurrentID}, session.History...)
				if len(session.History) > 2 {
					session.History = session.History[:2]
				}
			}
		}
		session.CurrentID = thumbnail.ID

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

	// Calculate current position in this session
	position := session.ViewedCount + 1

	// Count how many valid back steps we have
	backCount := 0
	for _, id := range session.History {
		// Check if the thumbnail exists and is not deleted
		historyThumb, err := s.db.GetByID(id)
		if err == nil && historyThumb != nil && historyThumb.Status != models.StatusDeleted {
			backCount++
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
		Thumbnail *models.Thumbnail
		Total     int
		Current   int
		BackCount int
		History   []int64
	}{
		Thumbnail: thumbnail,
		Total:     session.TotalImages,
		Current:   position,
		BackCount: backCount,
		History:   session.History,
	}

	if err := tmpl.Execute(w, data); err != nil {
		s.log.WithError(err).Error("Failed to render template")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
}

// Helper function to check if a slice contains a value
func contains(slice []int64, value int64) bool {
	for _, item := range slice {
		if item == value {
			return true
		}
	}
	return false
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
					History:     []int64{},
				}
			}
		}
	}

	// Mark current thumbnail as viewed
	if currentID > 0 {
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

				// Add current to history, limit to 2 items
				if !contains(session.History, currentID) {
					session.History = append([]int64{currentID}, session.History...)
					if len(session.History) > 2 {
						session.History = session.History[:2]
					}
				}
			}
		}
	}

	// Get a random unviewed thumbnail instead of the next in sequence
	nextThumbnail, err := s.db.GetRandomUnviewedThumbnail()
	if err != nil {
		s.log.WithError(err).Error("Failed to get next thumbnail")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
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
					History:     []int64{},
				}
			}
		}
	}

	// Check if we have history
	if len(session.History) == 0 {
		// No history, redirect back to current
		http.Redirect(w, r, fmt.Sprintf("/slideshow?id=%d", currentID), http.StatusSeeOther)
		return
	}

	// Get the previous ID from history
	var prevID int64 = 0
	var validPrevFound bool = false
	var newHistory []int64

	// Iterate through history to find first valid thumbnail
	for i, id := range session.History {
		// Check if this thumbnail still exists and is not deleted
		prevThumbnail, err := s.db.GetByID(id)
		if err == nil && prevThumbnail != nil && prevThumbnail.Status != models.StatusDeleted {
			prevID = id
			validPrevFound = true

			// The new history should exclude the current one we're navigating to
			newHistory = session.History[i+1:]
			break
		}
	}

	// If no valid previous thumbnail found, stay on current
	if !validPrevFound {
		http.Redirect(w, r, fmt.Sprintf("/slideshow?id=%d", currentID), http.StatusSeeOther)
		return
	}

	// Update session with previous thumbnail ID
	session.CurrentID = prevID
	session.History = newHistory // Update history to exclude the one we just went back to

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

// handleNotFound handles 404 errors
func (s *Server) handleNotFound(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNotFound)
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte("<html><body><h1>404 Not Found</h1><p>The requested page could not be found.</p></body></html>"))
}
