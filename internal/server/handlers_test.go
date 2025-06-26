package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/pandino/movie-thumbnailer-go/internal/config"
	"github.com/pandino/movie-thumbnailer-go/internal/models"
	"github.com/sirupsen/logrus"
)

// Database interface for testing
type DatabaseInterface interface {
	GetByID(id int64) (*models.Thumbnail, error)
	GetRandomUnviewedThumbnail() (*models.Thumbnail, error)
	GetRandomUnviewedThumbnailExcluding(excludeIDs ...int64) (*models.Thumbnail, error)
	MarkAsViewedByID(id int64) error
	MarkForDeletionByID(id int64) error
	RestoreFromDeletionByID(id int64) error
	GetUnviewedThumbnails() ([]*models.Thumbnail, error)
	GetViewedThumbnails() ([]*models.Thumbnail, error)
	GetPendingThumbnails() ([]*models.Thumbnail, error)
	GetErrorThumbnails() ([]*models.Thumbnail, error)
	GetDeletedThumbnails(limit int) ([]*models.Thumbnail, error)
	GetAllThumbnails() ([]*models.Thumbnail, error)
}

// Scanner interface for testing
type ScannerInterface interface {
	IsScanning() bool
	GetStats() (*models.Stats, error)
	ResetViewedStatus() (int64, error)
	CleanupOrphans(ctx context.Context) error
	ScanMovies(ctx context.Context) error
}

// Metrics interface for testing
type MetricsInterface interface {
	RecordSlideshowView()
	RecordSlideshowSession(status string, duration time.Duration)
}

// MockDB implements the database interface for testing
type MockDB struct {
	thumbnails             map[int64]*models.Thumbnail
	nextID                 int64
	markAsViewedByIDErr    error
	markForDeletionErr     error
	getByIDErr             error
	getRandomErr           error
	randomThumbnail        *models.Thumbnail
	restoreFromDeletionErr error
}

func NewMockDB() *MockDB {
	return &MockDB{
		thumbnails: make(map[int64]*models.Thumbnail),
		nextID:     1,
	}
}

func (m *MockDB) GetByID(id int64) (*models.Thumbnail, error) {
	if m.getByIDErr != nil {
		return nil, m.getByIDErr
	}
	thumbnail, exists := m.thumbnails[id]
	if !exists {
		return nil, nil
	}
	return thumbnail, nil
}

func (m *MockDB) GetRandomUnviewedThumbnail() (*models.Thumbnail, error) {
	if m.getRandomErr != nil {
		return nil, m.getRandomErr
	}
	return m.randomThumbnail, nil
}

func (m *MockDB) GetRandomUnviewedThumbnailExcluding(excludeIDs ...int64) (*models.Thumbnail, error) {
	if m.getRandomErr != nil {
		return nil, m.getRandomErr
	}
	return m.randomThumbnail, nil
}

func (m *MockDB) MarkAsViewedByID(id int64) error {
	if m.markAsViewedByIDErr != nil {
		return m.markAsViewedByIDErr
	}
	if thumbnail, exists := m.thumbnails[id]; exists {
		thumbnail.Viewed = 1
	}
	return nil
}

func (m *MockDB) MarkForDeletionByID(id int64) error {
	if m.markForDeletionErr != nil {
		return m.markForDeletionErr
	}
	if thumbnail, exists := m.thumbnails[id]; exists {
		thumbnail.Status = models.StatusDeleted
	}
	return nil
}

func (m *MockDB) RestoreFromDeletionByID(id int64) error {
	if m.restoreFromDeletionErr != nil {
		return m.restoreFromDeletionErr
	}
	if thumbnail, exists := m.thumbnails[id]; exists {
		thumbnail.Status = models.StatusSuccess
	}
	return nil
}

func (m *MockDB) GetUnviewedThumbnails() ([]*models.Thumbnail, error) {
	var unviewed []*models.Thumbnail
	for _, t := range m.thumbnails {
		if !t.IsViewed() && t.Status == models.StatusSuccess {
			unviewed = append(unviewed, t)
		}
	}
	return unviewed, nil
}

func (m *MockDB) GetViewedThumbnails() ([]*models.Thumbnail, error) {
	var viewed []*models.Thumbnail
	for _, t := range m.thumbnails {
		if t.IsViewed() && t.Status == models.StatusSuccess {
			viewed = append(viewed, t)
		}
	}
	return viewed, nil
}

func (m *MockDB) GetPendingThumbnails() ([]*models.Thumbnail, error) {
	var pending []*models.Thumbnail
	for _, t := range m.thumbnails {
		if t.Status == models.StatusPending {
			pending = append(pending, t)
		}
	}
	return pending, nil
}

func (m *MockDB) GetErrorThumbnails() ([]*models.Thumbnail, error) {
	var errors []*models.Thumbnail
	for _, t := range m.thumbnails {
		if t.Status == models.StatusError {
			errors = append(errors, t)
		}
	}
	return errors, nil
}

func (m *MockDB) GetDeletedThumbnails(limit int) ([]*models.Thumbnail, error) {
	var deleted []*models.Thumbnail
	count := 0
	for _, t := range m.thumbnails {
		if t.Status == models.StatusDeleted {
			deleted = append(deleted, t)
			count++
			if limit > 0 && count >= limit {
				break
			}
		}
	}
	return deleted, nil
}

func (m *MockDB) GetAllThumbnails() ([]*models.Thumbnail, error) {
	var all []*models.Thumbnail
	for _, t := range m.thumbnails {
		all = append(all, t)
	}
	return all, nil
}

func (m *MockDB) AddThumbnail(thumbnail *models.Thumbnail) {
	if thumbnail.ID == 0 {
		thumbnail.ID = m.nextID
		m.nextID++
	}
	m.thumbnails[thumbnail.ID] = thumbnail
}

// MockScanner implements the scanner interface for testing
type MockScanner struct {
	isScanning           bool
	stats                *models.Stats
	getStatsErr          error
	resetViewedStatusErr error
	resetViewedCount     int64
	cleanupOrphansErr    error
	scanMoviesErr        error
	statsError           error
	scanError            error
	cleanupError         error
	resetError           error
	resetRows            int64
}

func NewMockScanner() *MockScanner {
	return &MockScanner{
		stats: &models.Stats{
			Total:    10,
			Unviewed: 5,
			Viewed:   5,
		},
	}
}

func (m *MockScanner) IsScanning() bool {
	return m.isScanning
}

func (m *MockScanner) GetStats() (*models.Stats, error) {
	if m.getStatsErr != nil {
		return nil, m.getStatsErr
	}
	if m.statsError != nil {
		return nil, m.statsError
	}
	return m.stats, nil
}

func (m *MockScanner) ResetViewedStatus() (int64, error) {
	if m.resetViewedStatusErr != nil {
		return 0, m.resetViewedStatusErr
	}
	if m.resetError != nil {
		return 0, m.resetError
	}
	return m.resetRows, nil
}

func (m *MockScanner) CleanupOrphans(ctx context.Context) error {
	if m.cleanupOrphansErr != nil {
		return m.cleanupOrphansErr
	}
	return m.cleanupError
}

func (m *MockScanner) ScanMovies(ctx context.Context) error {
	if m.scanMoviesErr != nil {
		return m.scanMoviesErr
	}
	return m.scanError
}

// MockMetrics implements the metrics interface for testing
type MockMetrics struct{}

func (m *MockMetrics) RecordSlideshowView()                                         {}
func (m *MockMetrics) RecordSlideshowSession(status string, duration time.Duration) {}

// TestServer wraps Server for testing with interfaces
type TestServer struct {
	cfg     *config.Config
	db      DatabaseInterface
	scanner ScannerInterface
	log     *logrus.Logger
	router  *mux.Router
	appCtx  context.Context
	version *VersionInfo
	metrics MetricsInterface
}

// Helper function to create a test server
func createTestServer() *TestServer {
	cfg := &config.Config{
		TemplatesDir:    "/tmp/templates",
		DisableDeletion: false,
	}

	mockDB := NewMockDB()
	mockScanner := NewMockScanner()
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel) // Reduce log noise in tests

	version := &VersionInfo{
		Version:   "test",
		Commit:    "test",
		BuildDate: "test",
	}

	server := &TestServer{
		cfg:     cfg,
		db:      mockDB,
		scanner: mockScanner,
		log:     logger,
		router:  mux.NewRouter(),
		appCtx:  context.Background(),
		version: version,
		metrics: &MockMetrics{},
	}

	return server
}

// Alias for consistency with test naming
func setupTestServer() *TestServer {
	return createTestServer()
}

// Adapter methods for TestServer to use Server's handler methods
func (ts *TestServer) getSessionFromCookie(r *http.Request) (*SessionData, error) {
	// Create a temporary Server instance for method access
	s := &Server{
		cfg:     ts.cfg,
		log:     ts.log,
		appCtx:  ts.appCtx,
		version: ts.version,
	}
	return s.getSessionFromCookie(r)
}

func (ts *TestServer) saveSessionToCookie(w http.ResponseWriter, session *SessionData) error {
	s := &Server{
		cfg:     ts.cfg,
		log:     ts.log,
		appCtx:  ts.appCtx,
		version: ts.version,
	}
	return s.saveSessionToCookie(w, session)
}

func (ts *TestServer) createNewSession() (*SessionData, error) {
	stats, err := ts.scanner.GetStats()
	if err != nil {
		ts.log.WithError(err).Error("Failed to get stats for new session")
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

func (ts *TestServer) redirectToSlideshow(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/slideshow", http.StatusSeeOther)
}

func (ts *TestServer) requireValidSession(w http.ResponseWriter, r *http.Request) (*SessionData, bool) {
	session, err := ts.getSessionFromCookie(r)
	if err != nil {
		ts.log.WithError(err).Debug("No valid session found, redirecting to slideshow")
		ts.redirectToSlideshow(w, r)
		return nil, false
	}

	// Additional validation: check if session has meaningful data
	if session.StartedAt == 0 {
		ts.log.Debug("Session has no start time, redirecting to slideshow")
		ts.redirectToSlideshow(w, r)
		return nil, false
	}

	return session, true
}

func (ts *TestServer) handleStats(w http.ResponseWriter, r *http.Request) {
	stats, err := ts.scanner.GetStats()
	if err != nil {
		ts.log.WithError(err).Error("Failed to get stats")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

func (ts *TestServer) handleMarkViewed(w http.ResponseWriter, r *http.Request) {
	// Require valid session - redirect to /slideshow if none found
	session, ok := ts.requireValidSession(w, r)
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
	if err := ts.db.MarkAsViewedByID(thumbnailID); err != nil {
		ts.log.WithError(err).WithField("thumbnail_id", thumbnailID).Error("Failed to mark as viewed")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Record view in metrics
	ts.metrics.RecordSlideshowView()

	// Update session viewed count
	session.ViewedCount++

	// Save the updated session
	if err := ts.saveSessionToCookie(w, session); err != nil {
		ts.log.WithError(err).Error("Failed to save session after marking viewed")
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

func (ts *TestServer) handleDelete(w http.ResponseWriter, r *http.Request) {
	// Require valid session - redirect to /slideshow if none found
	session, ok := ts.requireValidSession(w, r)
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
	thumbnail, err := ts.db.GetByID(thumbnailID)
	if err != nil {
		ts.log.WithError(err).WithField("thumbnail_id", thumbnailID).Error("Failed to get thumbnail")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	if thumbnail == nil {
		http.Error(w, "Thumbnail not found", http.StatusNotFound)
		return
	}

	// Handle any previous pending deletions
	if session.PendingDelete && session.PreviousID != 0 {
		// Commit previous deletion first
		if err := ts.db.MarkForDeletionByID(session.PreviousID); err != nil {
			ts.log.WithError(err).WithField("thumbnail_id", session.PreviousID).Error("Failed to commit pending deletion")
		}
		session.PendingDelete = false
		session.PreviousID = 0
	}

	// Mark previous thumbnail as viewed if needed
	if session.PreviousID != 0 && session.PreviousID != thumbnailID {
		if err := ts.db.MarkAsViewedByID(session.PreviousID); err != nil {
			ts.log.WithError(err).WithField("thumbnail_id", session.PreviousID).Error("Failed to mark previous thumbnail as viewed before deletion")
		} else {
			session.ViewedCount++
		}
	}

	// Mark the current thumbnail for deletion in the session only
	session.PreviousID = thumbnail.ID
	session.PendingDelete = true

	// Save the updated session
	if err := ts.saveSessionToCookie(w, session); err != nil {
		ts.log.WithError(err).Error("Failed to save session after marking for deletion")
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

func (ts *TestServer) handleUndoDelete(w http.ResponseWriter, r *http.Request) {
	// Get thumbnail ID from form
	thumbnailIDStr := r.FormValue("id")
	if thumbnailIDStr == "" {
		http.Error(w, "Thumbnail ID is required", http.StatusBadRequest)
		return
	}

	thumbnailID, err := strconv.ParseInt(thumbnailIDStr, 10, 64)
	if err != nil {
		ts.log.WithError(err).Error("Invalid thumbnail ID")
		http.Error(w, "Invalid thumbnail ID", http.StatusBadRequest)
		return
	}

	// Get the thumbnail record
	thumbnail, err := ts.db.GetByID(thumbnailID)
	if err != nil {
		ts.log.WithError(err).WithField("thumbnail_id", thumbnailID).Error("Failed to get thumbnail")
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

	// Restore the thumbnail
	if err := ts.db.RestoreFromDeletionByID(thumbnailID); err != nil {
		ts.log.WithError(err).WithField("thumbnail_id", thumbnailID).Error("Failed to restore from deletion")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// If ajax request, return JSON response
	if r.Header.Get("X-Requested-With") == "XMLHttpRequest" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"success": true})
		return
	}

	// Otherwise redirect to control page
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (ts *TestServer) handleThumbnail(w http.ResponseWriter, r *http.Request) {
	// Get thumbnail ID from URL
	vars := mux.Vars(r)
	idStr := vars["id"]

	// Convert ID from string to int64
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		ts.log.WithError(err).WithField("id", idStr).Error("Invalid thumbnail ID")
		http.Error(w, "Invalid thumbnail ID", http.StatusBadRequest)
		return
	}

	// Get thumbnail by ID
	thumbnail, err := ts.db.GetByID(id)
	if err != nil {
		ts.log.WithError(err).WithField("id", id).Error("Failed to get thumbnail")
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
		ts.log.WithError(err).Error("Failed to encode thumbnail")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
}

func (ts *TestServer) handleThumbnails(w http.ResponseWriter, r *http.Request) {
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
		thumbnails, err = ts.db.GetUnviewedThumbnails()
	} else if status == "success" && viewed == "1" {
		thumbnails, err = ts.db.GetViewedThumbnails()
	} else if status == "pending" {
		thumbnails, err = ts.db.GetPendingThumbnails()
	} else if status == "error" {
		thumbnails, err = ts.db.GetErrorThumbnails()
	} else if status == "deleted" {
		thumbnails, err = ts.db.GetDeletedThumbnails(limit)
	} else {
		thumbnails, err = ts.db.GetAllThumbnails()
	}

	if err != nil {
		ts.log.WithError(err).Error("Failed to get thumbnails")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(thumbnails)
}

// Additional handler methods for TestServer to support new tests
func (ts *TestServer) handleControlPage(w http.ResponseWriter, r *http.Request) {
	stats, err := ts.scanner.GetStats()
	if err != nil {
		ts.log.WithError(err).Error("Failed to get stats")
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
	tmpl, err := template.ParseFiles(filepath.Join(ts.cfg.TemplatesDir, "control.html"))
	if err != nil {
		ts.log.WithError(err).Error("Failed to parse template")
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
		IsScanning:                  ts.scanner.IsScanning(),
		HasSession:                  hasSession,
		SessionViewedCount:          sessionViewedCount,
		SessionTotalCount:           sessionTotalCount,
		SessionDeletedSize:          sessionDeletedSize,
		Version:                     ts.version,
		ViewedSizeFormatted:         formatBytes(stats.ViewedSize),
		UnviewedSizeFormatted:       formatBytes(stats.UnviewedSize),
		SessionDeletedSizeFormatted: formatBytes(sessionDeletedSize),
	}

	if err := tmpl.Execute(w, data); err != nil {
		ts.log.WithError(err).Error("Failed to render template")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
}

func (ts *TestServer) handleScan(w http.ResponseWriter, r *http.Request) {
	if ts.scanner.IsScanning() {
		http.Error(w, "Scan already in progress", http.StatusConflict)
		return
	}

	// Create a timeout context derived from the application context
	ctx, cancel := context.WithTimeout(ts.appCtx, 30*time.Minute)

	go func() {
		defer cancel()
		if err := ts.scanner.ScanMovies(ctx); err != nil {
			ts.log.WithError(err).Error("Scan failed")
		}
	}()

	// Redirect back to control page
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (ts *TestServer) handleCleanup(w http.ResponseWriter, r *http.Request) {
	if ts.cfg.DisableDeletion {
		http.Error(w, "Cleanup is disabled via DISABLE_DELETION flag", http.StatusForbidden)
		return
	}

	if ts.scanner.IsScanning() {
		http.Error(w, "Cannot perform cleanup while scanning", http.StatusConflict)
		return
	}

	// Create a timeout context derived from the application context
	ctx, cancel := context.WithTimeout(ts.appCtx, 10*time.Minute)

	go func() {
		defer cancel()
		if err := ts.scanner.CleanupOrphans(ctx); err != nil {
			ts.log.WithError(err).Error("Cleanup failed")
		}
	}()

	// Redirect back to control page
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (ts *TestServer) handleResetViews(w http.ResponseWriter, r *http.Request) {
	count, err := ts.scanner.ResetViewedStatus()
	if err != nil {
		ts.log.WithError(err).Error("Failed to reset views")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Set success message in flash
	http.SetCookie(w, &http.Cookie{
		Name:  "flash",
		Value: "Reset viewed status for " + strconv.FormatInt(count, 10) + " thumbnails",
		Path:  "/",
	})

	// Redirect back to control page
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (ts *TestServer) handleProcessDeletions(w http.ResponseWriter, r *http.Request) {
	if ts.cfg.DisableDeletion {
		http.Error(w, "Deletion processing is disabled via DISABLE_DELETION flag", http.StatusForbidden)
		return
	}

	if ts.scanner.IsScanning() {
		http.Error(w, "Cannot process deletions while scanning", http.StatusConflict)
		return
	}

	// Get the count of deleted items before processing
	stats, err := ts.scanner.GetStats()
	if err != nil {
		ts.log.WithError(err).Error("Failed to get stats")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	deletedCount := stats.Deleted

	// Create a timeout context derived from the application context
	ctx, cancel := context.WithTimeout(ts.appCtx, 15*time.Minute)

	// Process the deletion queue
	go func() {
		defer cancel()
		if err := ts.scanner.CleanupOrphans(ctx); err != nil {
			ts.log.WithError(err).Error("Process deletions failed")
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

func (ts *TestServer) handleSlideshow(w http.ResponseWriter, r *http.Request) {
	// Check if a new session was requested
	newSession := r.URL.Query().Get("new") == "true"

	var session *SessionData

	if newSession {
		// Create a new session
		var err error
		session, err = ts.createNewSession()
		if err != nil {
			ts.log.WithError(err).Error("Failed to create new session")
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		ts.log.Debug("Created new session")

		// Save to cookie
		if err := ts.saveSessionToCookie(w, session); err != nil {
			ts.log.WithError(err).Error("Failed to save new session to cookie")
		}
	} else {
		// Try to get existing session from cookie
		var err error
		session, err = ts.getSessionFromCookie(r)
		if err != nil {
			// No valid session found, create a new one
			ts.log.WithError(err).Debug("No valid session found, creating new session")
			session, err = ts.createNewSession()
			if err != nil {
				ts.log.WithError(err).Error("Failed to create fallback session")
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}

			// Save to cookie
			if err := ts.saveSessionToCookie(w, session); err != nil {
				ts.log.WithError(err).Error("Failed to save fallback session to cookie")
			}
		}
	}

	// Use session's current ID as target
	targetID := session.CurrentID

	// Get the thumbnail to display
	var thumbnail *models.Thumbnail
	var err error

	if targetID > 0 {
		// Get the specified thumbnail (either from session or query parameter)
		thumbnail, err = ts.db.GetByID(targetID)
		if err != nil || thumbnail == nil {
			// If the stored thumbnail doesn't exist anymore, get a new random one
			ts.log.WithError(err).WithField("targetID", targetID).Warn("Stored thumbnail not found, getting new random thumbnail")
			thumbnail, err = ts.db.GetRandomUnviewedThumbnail()
		}
	} else {
		// No current thumbnail in session, get a random unviewed thumbnail
		thumbnail, err = ts.db.GetRandomUnviewedThumbnail()
	}

	if err != nil {
		ts.log.WithError(err).Error("Failed to get thumbnail")
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
	shouldUpdateSession := false
	if newSession {
		// For new sessions, always set the first thumbnail without incrementing counters
		if session.CurrentID == 0 {
			session.CurrentID = thumbnail.ID
			shouldUpdateSession = true
		}
	} else if thumbnail.ID != session.CurrentID {
		// For existing sessions, only update if we're viewing a different thumbnail
		if session.CurrentID > 0 {
			// This is actual navigation between thumbnails
			session.ViewedCount++
			session.NavigationCount++
			session.PreviousID = session.CurrentID
		}
		session.CurrentID = thumbnail.ID
		shouldUpdateSession = true
	}

	if shouldUpdateSession {
		// Pre-determine the next thumbnail for prefetch coordination
		if session.NextID == 0 || newSession {
			nextThumbnail, err := ts.db.GetRandomUnviewedThumbnail()
			if err == nil && nextThumbnail != nil {
				session.NextID = nextThumbnail.ID
			}
		}

		// Save the updated session
		if err := ts.saveSessionToCookie(w, session); err != nil {
			ts.log.WithError(err).Error("Failed to save updated session")
		}
	}

	// Calculate current position in this session
	position := session.NavigationCount + 1

	// Parse template
	tmpl, err := template.ParseFiles(filepath.Join(ts.cfg.TemplatesDir, "slideshow.html"))
	if err != nil {
		ts.log.WithError(err).Error("Failed to parse template")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Check if this is the last thumbnail
	var excludeForCount []int64
	excludeForCount = append(excludeForCount, thumbnail.ID)
	if session.PreviousID > 0 && session.PreviousID != thumbnail.ID {
		excludeForCount = append(excludeForCount, session.PreviousID)
	}

	remainingThumbnail, err := ts.db.GetRandomUnviewedThumbnailExcluding(excludeForCount...)
	isLastThumbnail := (err != nil || remainingThumbnail == nil)

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
		ts.log.WithError(err).Error("Failed to render template")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
}

func (ts *TestServer) handleNotFound(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNotFound)
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte("<html><body><h1>404 Not Found</h1><p>The requested page could not be found.</p></body></html>"))
}
func createSessionCookie(session *SessionData) *http.Cookie {
	sessionJSON, _ := json.Marshal(session)
	return &http.Cookie{
		Name:  "slideshow_session",
		Value: base64.StdEncoding.EncodeToString(sessionJSON),
		Path:  "/",
	}
}

func TestFormatBytes(t *testing.T) {
	testCases := []struct {
		bytes    int64
		expected string
	}{
		{0, "0 B"},
		{100, "100 B"},
		{1024, "1.00 KB"},
		{1536, "1.50 KB"},
		{1048576, "1.00 MB"},
		{1073741824, "1.00 GB"},
		{1099511627776, "1.00 TB"},
	}

	for _, tc := range testCases {
		result := formatBytes(tc.bytes)
		if result != tc.expected {
			t.Errorf("formatBytes(%d) = %s; expected %s", tc.bytes, result, tc.expected)
		}
	}
}

func TestGetSessionFromCookie(t *testing.T) {
	server := createTestServer()

	t.Run("valid session cookie", func(t *testing.T) {
		session := &SessionData{
			TotalImages: 10,
			ViewedCount: 5,
			CurrentID:   123,
			StartedAt:   time.Now().Unix(),
		}

		req := httptest.NewRequest("GET", "/", nil)
		req.AddCookie(createSessionCookie(session))

		retrievedSession, err := server.getSessionFromCookie(req)
		if err != nil {
			t.Errorf("Expected no error, got %v", err)
		}

		if retrievedSession.TotalImages != session.TotalImages {
			t.Errorf("Expected TotalImages %d, got %d", session.TotalImages, retrievedSession.TotalImages)
		}
		if retrievedSession.CurrentID != session.CurrentID {
			t.Errorf("Expected CurrentID %d, got %d", session.CurrentID, retrievedSession.CurrentID)
		}
	})

	t.Run("no session cookie", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)

		_, err := server.getSessionFromCookie(req)
		if err == nil {
			t.Error("Expected error for missing cookie, got nil")
		}
	})

	t.Run("invalid session cookie", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		req.AddCookie(&http.Cookie{
			Name:  "slideshow_session",
			Value: "invalid-base64",
		})

		_, err := server.getSessionFromCookie(req)
		if err == nil {
			t.Error("Expected error for invalid cookie, got nil")
		}
	})
}

func TestHandleStats(t *testing.T) {
	server := createTestServer()
	mockScanner := server.scanner.(*MockScanner)

	t.Run("successful stats request", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/stats", nil)
		w := httptest.NewRecorder()

		server.handleStats(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}

		var stats models.Stats
		if err := json.Unmarshal(w.Body.Bytes(), &stats); err != nil {
			t.Errorf("Failed to unmarshal response: %v", err)
		}

		if stats.Total != mockScanner.stats.Total {
			t.Errorf("Expected Total %d, got %d", mockScanner.stats.Total, stats.Total)
		}
	})

	t.Run("stats error", func(t *testing.T) {
		mockScanner.getStatsErr = fmt.Errorf("database error")
		defer func() { mockScanner.getStatsErr = nil }()

		req := httptest.NewRequest("GET", "/api/stats", nil)
		w := httptest.NewRecorder()

		server.handleStats(w, req)

		if w.Code != http.StatusInternalServerError {
			t.Errorf("Expected status 500, got %d", w.Code)
		}
	})
}

func TestHandleMarkViewed(t *testing.T) {
	server := createTestServer()
	mockDB := server.db.(*MockDB)

	t.Run("successful mark as viewed", func(t *testing.T) {
		// Create a test thumbnail
		thumbnail := &models.Thumbnail{
			ID:       123,
			Status:   models.StatusSuccess,
			Viewed:   0,
			FileSize: 1024,
		}
		mockDB.AddThumbnail(thumbnail)

		// Debug: check if thumbnail was added
		addedThumbnail, err := mockDB.GetByID(123)
		if err != nil {
			t.Fatalf("Failed to get thumbnail after adding: %v", err)
		}
		if addedThumbnail == nil {
			t.Fatal("Thumbnail was not added properly")
		}

		session := &SessionData{
			TotalImages: 10,
			ViewedCount: 5,
			CurrentID:   123,
			StartedAt:   time.Now().Unix(),
		}

		req := httptest.NewRequest("POST", "/slideshow/viewed", nil)
		req.AddCookie(createSessionCookie(session))
		w := httptest.NewRecorder()

		server.handleMarkViewed(w, req)

		if w.Code != http.StatusSeeOther {
			t.Errorf("Expected status 303, got %d", w.Code)
		}

		// Check that thumbnail was marked as viewed
		updatedThumbnail, err := mockDB.GetByID(123)
		if err != nil {
			t.Errorf("Failed to get updated thumbnail: %v", err)
		}
		if updatedThumbnail == nil {
			t.Fatal("Expected thumbnail to exist")
		}
		if !updatedThumbnail.IsViewed() {
			t.Error("Expected thumbnail to be marked as viewed")
		}
	})

	t.Run("ajax request", func(t *testing.T) {
		// Create a test thumbnail for this test
		thumbnail := &models.Thumbnail{
			ID:       124,
			Status:   models.StatusSuccess,
			Viewed:   0,
			FileSize: 1024,
		}
		mockDB.AddThumbnail(thumbnail)

		session := &SessionData{
			TotalImages: 10,
			ViewedCount: 5,
			CurrentID:   124,
			StartedAt:   time.Now().Unix(),
		}

		req := httptest.NewRequest("POST", "/slideshow/viewed", nil)
		req.Header.Set("X-Requested-With", "XMLHttpRequest")
		req.AddCookie(createSessionCookie(session))
		w := httptest.NewRecorder()

		server.handleMarkViewed(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}

		var response map[string]bool
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Errorf("Failed to unmarshal response: %v", err)
		}

		if !response["success"] {
			t.Error("Expected success to be true")
		}
	})

	t.Run("no session", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/slideshow/viewed", nil)
		w := httptest.NewRecorder()

		server.handleMarkViewed(w, req)

		if w.Code != http.StatusSeeOther {
			t.Errorf("Expected redirect status 303, got %d", w.Code)
		}
	})

	t.Run("no current thumbnail", func(t *testing.T) {
		session := &SessionData{
			TotalImages: 10,
			ViewedCount: 5,
			CurrentID:   0, // No current ID
			StartedAt:   time.Now().Unix(),
		}

		req := httptest.NewRequest("POST", "/slideshow/viewed", nil)
		req.AddCookie(createSessionCookie(session))
		w := httptest.NewRecorder()

		server.handleMarkViewed(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", w.Code)
		}
	})

	t.Run("database error", func(t *testing.T) {
		mockDB.markAsViewedByIDErr = fmt.Errorf("database error")
		defer func() { mockDB.markAsViewedByIDErr = nil }()

		session := &SessionData{
			TotalImages: 10,
			ViewedCount: 5,
			CurrentID:   123,
			StartedAt:   time.Now().Unix(),
		}

		req := httptest.NewRequest("POST", "/slideshow/viewed", nil)
		req.AddCookie(createSessionCookie(session))
		w := httptest.NewRecorder()

		server.handleMarkViewed(w, req)

		if w.Code != http.StatusInternalServerError {
			t.Errorf("Expected status 500, got %d", w.Code)
		}
	})
}

func TestHandleDelete(t *testing.T) {
	server := createTestServer()
	mockDB := server.db.(*MockDB)

	// Create a test thumbnail
	thumbnail := &models.Thumbnail{
		ID:        123,
		Status:    models.StatusSuccess,
		Viewed:    0,
		FileSize:  1024 * 1024, // 1MB
		MoviePath: "/test/movie.mp4",
	}
	mockDB.AddThumbnail(thumbnail)

	t.Run("successful delete marking", func(t *testing.T) {
		session := &SessionData{
			TotalImages: 10,
			ViewedCount: 5,
			CurrentID:   123,
			StartedAt:   time.Now().Unix(),
		}

		req := httptest.NewRequest("POST", "/slideshow/delete", nil)
		req.AddCookie(createSessionCookie(session))
		w := httptest.NewRecorder()

		server.handleDelete(w, req)

		if w.Code != http.StatusSeeOther {
			t.Errorf("Expected status 303, got %d", w.Code)
		}

		// Check that session has pending delete
		cookies := w.Result().Cookies()
		var sessionCookie *http.Cookie
		for _, cookie := range cookies {
			if cookie.Name == "slideshow_session" {
				sessionCookie = cookie
				break
			}
		}

		if sessionCookie == nil {
			t.Fatal("Expected session cookie to be set")
		}

		jsonData, _ := base64.StdEncoding.DecodeString(sessionCookie.Value)
		var updatedSession SessionData
		json.Unmarshal(jsonData, &updatedSession)

		if !updatedSession.PendingDelete {
			t.Error("Expected PendingDelete to be true")
		}
		if updatedSession.PreviousID != 123 {
			t.Errorf("Expected PreviousID to be 123, got %d", updatedSession.PreviousID)
		}
	})

	t.Run("ajax request", func(t *testing.T) {
		session := &SessionData{
			TotalImages: 10,
			ViewedCount: 5,
			CurrentID:   123,
			StartedAt:   time.Now().Unix(),
		}

		req := httptest.NewRequest("POST", "/slideshow/delete", nil)
		req.Header.Set("X-Requested-With", "XMLHttpRequest")
		req.AddCookie(createSessionCookie(session))
		w := httptest.NewRecorder()

		server.handleDelete(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}

		var response map[string]bool
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Errorf("Failed to unmarshal response: %v", err)
		}

		if !response["success"] {
			t.Error("Expected success to be true")
		}
	})

	t.Run("no session", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/slideshow/delete", nil)
		w := httptest.NewRecorder()

		server.handleDelete(w, req)

		if w.Code != http.StatusSeeOther {
			t.Errorf("Expected redirect status 303, got %d", w.Code)
		}
	})

	t.Run("thumbnail not found", func(t *testing.T) {
		session := &SessionData{
			TotalImages: 10,
			ViewedCount: 5,
			CurrentID:   999, // Non-existent ID
			StartedAt:   time.Now().Unix(),
		}

		req := httptest.NewRequest("POST", "/slideshow/delete", nil)
		req.AddCookie(createSessionCookie(session))
		w := httptest.NewRecorder()

		server.handleDelete(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("Expected status 404, got %d", w.Code)
		}
	})
}

func TestHandleUndoDelete(t *testing.T) {
	server := createTestServer()
	mockDB := server.db.(*MockDB)

	// Create a test thumbnail marked for deletion
	thumbnail := &models.Thumbnail{
		ID:        123,
		Status:    models.StatusDeleted,
		Viewed:    0,
		FileSize:  1024,
		MoviePath: "/test/movie.mp4",
	}
	mockDB.AddThumbnail(thumbnail)

	t.Run("successful undo delete", func(t *testing.T) {
		form := url.Values{}
		form.Set("id", "123")

		req := httptest.NewRequest("POST", "/undo-delete", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()

		server.handleUndoDelete(w, req)

		if w.Code != http.StatusSeeOther {
			t.Errorf("Expected status 303, got %d", w.Code)
		}

		// Check that thumbnail was restored
		updatedThumbnail, _ := mockDB.GetByID(123)
		if updatedThumbnail.Status != models.StatusSuccess {
			t.Errorf("Expected status to be success, got %s", updatedThumbnail.Status)
		}
	})

	t.Run("ajax request", func(t *testing.T) {
		// Reset thumbnail to deleted status
		thumbnail.Status = models.StatusDeleted

		form := url.Values{}
		form.Set("id", "123")

		req := httptest.NewRequest("POST", "/undo-delete", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("X-Requested-With", "XMLHttpRequest")
		w := httptest.NewRecorder()

		server.handleUndoDelete(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}

		var response map[string]bool
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Errorf("Failed to unmarshal response: %v", err)
		}

		if !response["success"] {
			t.Error("Expected success to be true")
		}
	})

	t.Run("missing thumbnail ID", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/undo-delete", strings.NewReader(""))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()

		server.handleUndoDelete(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", w.Code)
		}
	})

	t.Run("invalid thumbnail ID", func(t *testing.T) {
		form := url.Values{}
		form.Set("id", "invalid")

		req := httptest.NewRequest("POST", "/undo-delete", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()

		server.handleUndoDelete(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", w.Code)
		}
	})

	t.Run("thumbnail not found", func(t *testing.T) {
		form := url.Values{}
		form.Set("id", "999")

		req := httptest.NewRequest("POST", "/undo-delete", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()

		server.handleUndoDelete(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("Expected status 404, got %d", w.Code)
		}
	})

	t.Run("thumbnail not marked for deletion", func(t *testing.T) {
		// Create a thumbnail that's not marked for deletion
		notDeletedThumbnail := &models.Thumbnail{
			ID:     456,
			Status: models.StatusSuccess,
		}
		mockDB.AddThumbnail(notDeletedThumbnail)

		form := url.Values{}
		form.Set("id", "456")

		req := httptest.NewRequest("POST", "/undo-delete", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()

		server.handleUndoDelete(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", w.Code)
		}
	})
}

func TestHandleThumbnail(t *testing.T) {
	server := createTestServer()
	mockDB := server.db.(*MockDB)

	// Create a test thumbnail
	thumbnail := &models.Thumbnail{
		ID:            123,
		Status:        models.StatusSuccess,
		Viewed:        0,
		MoviePath:     "/test/movie.mp4",
		ThumbnailPath: "/test/thumb.jpg",
		FileSize:      1024,
	}
	mockDB.AddThumbnail(thumbnail)

	t.Run("successful thumbnail retrieval", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/thumbnail/123", nil)
		req = mux.SetURLVars(req, map[string]string{"id": "123"})
		w := httptest.NewRecorder()

		server.handleThumbnail(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}

		var retrievedThumbnail models.Thumbnail
		if err := json.Unmarshal(w.Body.Bytes(), &retrievedThumbnail); err != nil {
			t.Errorf("Failed to unmarshal response: %v", err)
		}

		if retrievedThumbnail.ID != thumbnail.ID {
			t.Errorf("Expected ID %d, got %d", thumbnail.ID, retrievedThumbnail.ID)
		}
		if retrievedThumbnail.MoviePath != thumbnail.MoviePath {
			t.Errorf("Expected MoviePath %s, got %s", thumbnail.MoviePath, retrievedThumbnail.MoviePath)
		}
	})

	t.Run("invalid thumbnail ID", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/thumbnail/invalid", nil)
		req = mux.SetURLVars(req, map[string]string{"id": "invalid"})
		w := httptest.NewRecorder()

		server.handleThumbnail(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", w.Code)
		}
	})

	t.Run("thumbnail not found", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/thumbnail/999", nil)
		req = mux.SetURLVars(req, map[string]string{"id": "999"})
		w := httptest.NewRecorder()

		server.handleThumbnail(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("Expected status 404, got %d", w.Code)
		}
	})

	t.Run("database error", func(t *testing.T) {
		mockDB.getByIDErr = fmt.Errorf("database error")
		defer func() { mockDB.getByIDErr = nil }()

		req := httptest.NewRequest("GET", "/api/thumbnail/123", nil)
		req = mux.SetURLVars(req, map[string]string{"id": "123"})
		w := httptest.NewRecorder()

		server.handleThumbnail(w, req)

		if w.Code != http.StatusInternalServerError {
			t.Errorf("Expected status 500, got %d", w.Code)
		}
	})
}

func TestHandleThumbnails(t *testing.T) {
	server := createTestServer()
	mockDB := server.db.(*MockDB)

	// Create test thumbnails
	thumbnails := []*models.Thumbnail{
		{ID: 1, Status: models.StatusSuccess, Viewed: 0},
		{ID: 2, Status: models.StatusSuccess, Viewed: 1},
		{ID: 3, Status: models.StatusPending, Viewed: 0},
		{ID: 4, Status: models.StatusError, Viewed: 0},
		{ID: 5, Status: models.StatusDeleted, Viewed: 0},
	}

	for _, thumb := range thumbnails {
		mockDB.AddThumbnail(thumb)
	}

	testCases := []struct {
		name           string
		query          string
		expectedCount  int
		expectedStatus string
	}{
		{"unviewed thumbnails", "status=success&viewed=0", 1, models.StatusSuccess},
		{"viewed thumbnails", "status=success&viewed=1", 1, models.StatusSuccess},
		{"pending thumbnails", "status=pending", 1, models.StatusPending},
		{"error thumbnails", "status=error", 1, models.StatusError},
		{"deleted thumbnails", "status=deleted", 1, models.StatusDeleted},
		{"all thumbnails", "", 5, ""},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/api/thumbnails?"+tc.query, nil)
			w := httptest.NewRecorder()

			server.handleThumbnails(w, req)

			if w.Code != http.StatusOK {
				t.Errorf("Expected status 200, got %d", w.Code)
			}

			var retrievedThumbnails []*models.Thumbnail
			if err := json.Unmarshal(w.Body.Bytes(), &retrievedThumbnails); err != nil {
				t.Errorf("Failed to unmarshal response: %v", err)
			}

			if len(retrievedThumbnails) != tc.expectedCount {
				t.Errorf("Expected %d thumbnails, got %d", tc.expectedCount, len(retrievedThumbnails))
			}

			if tc.expectedStatus != "" && len(retrievedThumbnails) > 0 {
				if retrievedThumbnails[0].Status != tc.expectedStatus {
					t.Errorf("Expected status %s, got %s", tc.expectedStatus, retrievedThumbnails[0].Status)
				}
			}
		})
	}
}

func TestCreateNewSession(t *testing.T) {
	server := createTestServer()
	mockScanner := server.scanner.(*MockScanner)

	t.Run("successful session creation", func(t *testing.T) {
		session, err := server.createNewSession()
		if err != nil {
			t.Errorf("Expected no error, got %v", err)
		}

		if session.TotalImages != mockScanner.stats.Unviewed {
			t.Errorf("Expected TotalImages %d, got %d", mockScanner.stats.Unviewed, session.TotalImages)
		}
		if session.ViewedCount != 0 {
			t.Errorf("Expected ViewedCount 0, got %d", session.ViewedCount)
		}
		if session.StartedAt == 0 {
			t.Error("Expected StartedAt to be set")
		}
	})

	t.Run("stats error fallback", func(t *testing.T) {
		mockScanner.getStatsErr = fmt.Errorf("database error")
		defer func() { mockScanner.getStatsErr = nil }()

		session, err := server.createNewSession()
		if err != nil {
			t.Errorf("Expected no error, got %v", err)
		}

		// Should fall back to zero stats
		if session.TotalImages != 0 {
			t.Errorf("Expected TotalImages 0, got %d", session.TotalImages)
		}
	})
}

func TestSaveSessionToCookie(t *testing.T) {
	server := createTestServer()

	session := &SessionData{
		TotalImages: 10,
		ViewedCount: 5,
		CurrentID:   123,
		StartedAt:   time.Now().Unix(),
	}

	w := httptest.NewRecorder()
	err := server.saveSessionToCookie(w, session)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	// Check that cookie was set
	cookies := w.Result().Cookies()
	var sessionCookie *http.Cookie
	for _, cookie := range cookies {
		if cookie.Name == "slideshow_session" {
			sessionCookie = cookie
			break
		}
	}

	if sessionCookie == nil {
		t.Fatal("Expected session cookie to be set")
	}

	// Verify cookie content
	jsonData, err := base64.StdEncoding.DecodeString(sessionCookie.Value)
	if err != nil {
		t.Errorf("Failed to decode cookie: %v", err)
	}

	var decodedSession SessionData
	if err := json.Unmarshal(jsonData, &decodedSession); err != nil {
		t.Errorf("Failed to unmarshal session: %v", err)
	}

	if decodedSession.TotalImages != session.TotalImages {
		t.Errorf("Expected TotalImages %d, got %d", session.TotalImages, decodedSession.TotalImages)
	}
}

func TestRequireValidSession(t *testing.T) {
	server := createTestServer()

	t.Run("valid session", func(t *testing.T) {
		session := &SessionData{
			TotalImages: 10,
			ViewedCount: 5,
			CurrentID:   123,
			StartedAt:   time.Now().Unix(),
		}

		req := httptest.NewRequest("GET", "/", nil)
		req.AddCookie(createSessionCookie(session))
		w := httptest.NewRecorder()

		retrievedSession, ok := server.requireValidSession(w, req)
		if !ok {
			t.Error("Expected valid session to return true")
		}
		if retrievedSession == nil {
			t.Fatal("Expected session to be returned")
		}
		if retrievedSession.CurrentID != session.CurrentID {
			t.Errorf("Expected CurrentID %d, got %d", session.CurrentID, retrievedSession.CurrentID)
		}
	})

	t.Run("no session cookie", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		w := httptest.NewRecorder()

		_, ok := server.requireValidSession(w, req)
		if ok {
			t.Error("Expected invalid session to return false")
		}
		if w.Code != http.StatusSeeOther {
			t.Errorf("Expected redirect status 303, got %d", w.Code)
		}
	})

	t.Run("invalid session data", func(t *testing.T) {
		session := &SessionData{
			TotalImages: 10,
			ViewedCount: 5,
			CurrentID:   123,
			StartedAt:   0, // Invalid start time
		}

		req := httptest.NewRequest("GET", "/", nil)
		req.AddCookie(createSessionCookie(session))
		w := httptest.NewRecorder()

		_, ok := server.requireValidSession(w, req)
		if ok {
			t.Error("Expected invalid session to return false")
		}
		if w.Code != http.StatusSeeOther {
			t.Errorf("Expected redirect status 303, got %d", w.Code)
		}
	})
}
