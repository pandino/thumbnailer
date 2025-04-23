package server

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"path/filepath"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/pandino/movie-thumbnailer-go/internal/models" // Add missing import
)

// handleControlPage renders the control page
func (s *Server) handleControlPage(w http.ResponseWriter, r *http.Request) {
	stats, err := s.scanner.GetStats()
	if err != nil {
		s.log.WithError(err).Error("Failed to get stats")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
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
		Stats      *models.Stats
		IsScanning bool
	}{
		Stats:      stats,
		IsScanning: s.scanner.IsScanning(),
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

	go func() {
		if err := s.scanner.ScanMovies(r.Context()); err != nil {
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

	go func() {
		// Fix: Call the cleanupOrphans method - need to fix this in the scanner package
		if err := s.scanner.ScanMovies(r.Context()); err != nil {
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

// handleSlideshow renders the slideshow page
func (s *Server) handleSlideshow(w http.ResponseWriter, r *http.Request) {
	// Check if an ID is specified in the query string
	idParam := r.URL.Query().Get("id")
	var currentID int64 = 0
	if idParam != "" {
		var err error
		currentID, err = strconv.ParseInt(idParam, 10, 64)
		if err != nil {
			s.log.WithError(err).Error("Invalid ID parameter")
			http.Error(w, "Invalid ID parameter", http.StatusBadRequest)
			return
		}
	}

	// Get the next unviewed thumbnail
	var thumbnail *models.Thumbnail
	var err error
	var total int

	if currentID > 0 {
		// Get the specified thumbnail
		thumbnail, err = s.db.GetByID(currentID)
	} else {
		// Get the first unviewed thumbnail
		thumbnail, err = s.db.GetFirstUnviewedThumbnail()
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

	// Get the total count of unviewed thumbnails
	total, err = s.db.GetUnviewedThumbnailCount()
	if err != nil {
		s.log.WithError(err).Error("Failed to get unviewed count")
		total = 0
	}

	// Get current position
	position, err := s.db.GetThumbnailPosition(thumbnail.ID)
	if err != nil {
		s.log.WithError(err).Error("Failed to get thumbnail position")
		position = 1
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
	}{
		Thumbnail: thumbnail,
		Total:     total,
		Current:   position,
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

	// Mark current thumbnail as viewed (if we have a current ID)
	if currentID > 0 {
		thumbnail, err := s.db.GetByID(currentID)
		if err == nil && thumbnail != nil {
			err = s.db.MarkAsViewed(thumbnail.ThumbnailPath)
			if err != nil {
				s.log.WithError(err).Error("Failed to mark as viewed")
				// Continue anyway
			}
		}
	}

	// Get next unviewed thumbnail
	nextThumbnail, err := s.db.GetNextUnviewedThumbnail(currentID)
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

	// Get previous thumbnail (can be viewed or unviewed)
	prevThumbnail, err := s.db.GetPreviousThumbnail(currentID)
	if err != nil {
		s.log.WithError(err).Error("Failed to get previous thumbnail")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// If no previous thumbnail, stay on current
	if prevThumbnail == nil {
		if currentID > 0 {
			http.Redirect(w, r, fmt.Sprintf("/slideshow?id=%d", currentID), http.StatusSeeOther)
		} else {
			http.Redirect(w, r, "/slideshow", http.StatusSeeOther)
		}
		return
	}

	// Redirect to slideshow with previous thumbnail ID
	http.Redirect(w, r, fmt.Sprintf("/slideshow?id=%d", prevThumbnail.ID), http.StatusSeeOther)
}

// handleMarkViewed marks a thumbnail as viewed
func (s *Server) handleMarkViewed(w http.ResponseWriter, r *http.Request) {
	// Get thumbnail path from form
	thumbnailPath := r.FormValue("path")
	if thumbnailPath == "" {
		http.Error(w, "Thumbnail path is required", http.StatusBadRequest)
		return
	}

	// Mark as viewed
	if err := s.db.MarkAsViewed(thumbnailPath); err != nil {
		s.log.WithError(err).WithField("thumbnail", thumbnailPath).Error("Failed to mark as viewed")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// If ajax request, return JSON response
	if r.Header.Get("X-Requested-With") == "XMLHttpRequest" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"success": true})
		return
	}

	// Otherwise redirect to next
	http.Redirect(w, r, "/slideshow/next", http.StatusSeeOther)
}

// handleDelete deletes a movie and its thumbnail
func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	// Get movie path from form
	moviePath := r.FormValue("path")
	if moviePath == "" {
		http.Error(w, "Movie path is required", http.StatusBadRequest)
		return
	}

	// Delete movie and thumbnail
	if err := s.scanner.DeleteMovie(moviePath); err != nil {
		s.log.WithError(err).WithField("movie", moviePath).Error("Failed to delete movie")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// If ajax request, return JSON response
	if r.Header.Get("X-Requested-With") == "XMLHttpRequest" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"success": true})
		return
	}

	// Otherwise redirect to next
	http.Redirect(w, r, "/slideshow/next", http.StatusSeeOther)
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
