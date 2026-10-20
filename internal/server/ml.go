package server

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"

	"github.com/pandino/movie-thumbnailer-go/internal/models"
	"github.com/sirupsen/logrus"
)

// Endpoints for the preference-model worker (scripts/taste), which runs on a machine that is
// only sometimes on: it pulls grids and decisions, and pushes embeddings and scores back.

const (
	defaultMLPageLimit = 200
	maxMLPageLimit     = 1000
	maxMLBodyBytes     = 64 << 20
)

// MLItemsResponse is a page of grids; NextAfterID is 0 when there are no more pages
type MLItemsResponse struct {
	Items       []models.MLItem `json:"items"`
	NextAfterID int64           `json:"next_after_id"`
}

// EmbeddingsRequest uploads embeddings computed by one model
type EmbeddingsRequest struct {
	Model      string             `json:"model"`
	Embeddings []models.Embedding `json:"embeddings"`
}

// EmbeddingsResponse is a page of embeddings; NextAfterID is 0 when there are no more pages
type EmbeddingsResponse struct {
	Model       string             `json:"model"`
	Embeddings  []models.Embedding `json:"embeddings"`
	NextAfterID int64              `json:"next_after_id"`
}

// DecisionsResponse is a page of the decision log; NextAfterID is 0 when there are no more pages
type DecisionsResponse struct {
	Decisions   []models.Decision `json:"decisions"`
	NextAfterID int64             `json:"next_after_id"`
}

// ScoresRequest replaces all scores and the model status shown on the control page (Meta)
type ScoresRequest struct {
	Scores []models.Score    `json:"scores"`
	Meta   map[string]string `json:"meta"`
}

func writeMLJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeMLError(w http.ResponseWriter, status int, msg string) {
	writeMLJSON(w, status, map[string]string{"error": msg})
}

// parseCursor reads after_id and limit from the query string
func parseCursor(r *http.Request) (int64, int, error) {
	q := r.URL.Query()
	var afterID int64
	limit := defaultMLPageLimit

	if raw := q.Get("after_id"); raw != "" {
		v, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || v < 0 {
			return 0, 0, fmt.Errorf("after_id must be a non-negative integer")
		}
		afterID = v
	}
	if raw := q.Get("limit"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v < 1 || v > maxMLPageLimit {
			return 0, 0, fmt.Errorf("limit must be between 1 and %d", maxMLPageLimit)
		}
		limit = v
	}
	return afterID, limit, nil
}

// handleMLItems lists live and decided grids, optionally only those missing an embedding
func (s *Server) handleMLItems(w http.ResponseWriter, r *http.Request) {
	afterID, limit, err := parseCursor(r)
	if err != nil {
		writeMLError(w, http.StatusBadRequest, err.Error())
		return
	}

	items, err := s.db.ListMLItems(r.URL.Query().Get("missing_model"), afterID, limit)
	if err != nil {
		s.log.WithError(err).Error("Failed to list ML items")
		writeMLError(w, http.StatusInternalServerError, "Internal server error")
		return
	}

	resp := MLItemsResponse{Items: items}
	for i := range resp.Items {
		// The DB returns the grid's path relative to the thumbnails directory
		resp.Items[i].GridURL = (&url.URL{Path: "/thumbnails/" + resp.Items[i].GridURL}).EscapedPath()
	}
	if len(items) == limit {
		resp.NextAfterID = items[len(items)-1].ID
	}
	writeMLJSON(w, http.StatusOK, resp)
}

// handlePutEmbeddings stores a batch of embeddings
func (s *Server) handlePutEmbeddings(w http.ResponseWriter, r *http.Request) {
	var req EmbeddingsRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxMLBodyBytes)).Decode(&req); err != nil {
		writeMLError(w, http.StatusBadRequest, "Invalid JSON request body")
		return
	}
	if req.Model == "" {
		writeMLError(w, http.StatusBadRequest, "model is required")
		return
	}
	for _, e := range req.Embeddings {
		if e.ThumbnailID <= 0 || len(e.Vector) == 0 {
			writeMLError(w, http.StatusBadRequest, "every embedding needs an id and a non-empty vector")
			return
		}
	}

	if err := s.db.PutEmbeddings(req.Model, req.Embeddings); err != nil {
		s.log.WithError(err).Error("Failed to store embeddings")
		writeMLError(w, http.StatusInternalServerError, "Internal server error")
		return
	}
	s.log.WithFields(logrus.Fields{"model": req.Model, "count": len(req.Embeddings)}).Debug("Stored embeddings")
	writeMLJSON(w, http.StatusOK, map[string]int{"stored": len(req.Embeddings)})
}

// handleListEmbeddings returns a page of embeddings of one model
func (s *Server) handleListEmbeddings(w http.ResponseWriter, r *http.Request) {
	afterID, limit, err := parseCursor(r)
	if err != nil {
		writeMLError(w, http.StatusBadRequest, err.Error())
		return
	}
	model := r.URL.Query().Get("model")
	if model == "" {
		writeMLError(w, http.StatusBadRequest, "model is required")
		return
	}

	embeddings, err := s.db.ListEmbeddings(model, afterID, limit)
	if err != nil {
		s.log.WithError(err).Error("Failed to list embeddings")
		writeMLError(w, http.StatusInternalServerError, "Internal server error")
		return
	}

	resp := EmbeddingsResponse{Model: model, Embeddings: embeddings}
	if len(embeddings) == limit {
		resp.NextAfterID = embeddings[len(embeddings)-1].ThumbnailID
	}
	writeMLJSON(w, http.StatusOK, resp)
}

// handleListDecisions returns a page of the decision log
func (s *Server) handleListDecisions(w http.ResponseWriter, r *http.Request) {
	afterID, limit, err := parseCursor(r)
	if err != nil {
		writeMLError(w, http.StatusBadRequest, err.Error())
		return
	}

	decisions, err := s.db.ListDecisions(afterID, limit)
	if err != nil {
		s.log.WithError(err).Error("Failed to list decisions")
		writeMLError(w, http.StatusInternalServerError, "Internal server error")
		return
	}

	resp := DecisionsResponse{Decisions: decisions}
	if len(decisions) == limit {
		resp.NextAfterID = decisions[len(decisions)-1].ID
	}
	writeMLJSON(w, http.StatusOK, resp)
}

// handlePutScores replaces all scores and updates the model status
func (s *Server) handlePutScores(w http.ResponseWriter, r *http.Request) {
	var req ScoresRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxMLBodyBytes)).Decode(&req); err != nil {
		writeMLError(w, http.StatusBadRequest, "Invalid JSON request body")
		return
	}
	for _, sc := range req.Scores {
		if sc.ThumbnailID <= 0 || !validProbability(sc.PDelete) || (sc.PArchive != nil && !validProbability(*sc.PArchive)) {
			writeMLError(w, http.StatusBadRequest, "every score needs an id and probabilities between 0 and 1")
			return
		}
	}

	if err := s.db.ReplaceScores(req.Scores, req.Meta); err != nil {
		s.log.WithError(err).Error("Failed to store scores")
		writeMLError(w, http.StatusInternalServerError, "Internal server error")
		return
	}
	s.log.WithField("count", len(req.Scores)).Info("Stored preference scores")
	writeMLJSON(w, http.StatusOK, map[string]int{"stored": len(req.Scores)})
}

// handleMLStatus reports decision counts, embedding/score coverage and model status
func (s *Server) handleMLStatus(w http.ResponseWriter, r *http.Request) {
	status, err := s.db.GetMLStatus()
	if err != nil {
		s.log.WithError(err).Error("Failed to get ML status")
		writeMLError(w, http.StatusInternalServerError, "Internal server error")
		return
	}
	writeMLJSON(w, http.StatusOK, status)
}

func validProbability(p float64) bool {
	return !math.IsNaN(p) && p >= 0 && p <= 1
}
