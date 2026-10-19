package server

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	"github.com/pandino/movie-thumbnailer-go/internal/models"
	"github.com/sirupsen/logrus"
)

// TagsRequest is the body for replacing or adding tags by thumbnail ID
type TagsRequest struct {
	Tags []string `json:"tags"`
}

// VideoTagsRequest is the body for tag operations by movie filename
type VideoTagsRequest struct {
	Filename string   `json:"filename"`
	Tags     []string `json:"tags,omitempty"`   // PUT: replace all tags
	Add      []string `json:"add,omitempty"`    // POST: tags to add
	Remove   []string `json:"remove,omitempty"` // POST: tags to remove
}

// TagsResponse is returned by all tag endpoints for a single thumbnail
type TagsResponse struct {
	Success     bool     `json:"success"`
	Error       string   `json:"error,omitempty"`
	Filename    string   `json:"filename,omitempty"`
	ThumbnailID int64    `json:"thumbnail_id,omitempty"`
	Tags        []string `json:"tags"`
}

// tagOp describes a change to a thumbnail's tags
type tagOp struct {
	set     []string // replaces all tags when replace is true
	add     []string
	remove  []string
	replace bool
}

// writeTagsJSON writes a TagsResponse with the given status code
func writeTagsJSON(w http.ResponseWriter, status int, resp TagsResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if resp.Tags == nil {
		resp.Tags = []string{}
	}
	json.NewEncoder(w).Encode(resp)
}

// handleAPITags returns all tags with their frequency, most frequent first
func (s *Server) handleAPITags(w http.ResponseWriter, r *http.Request) {
	counts, err := s.db.GetTagCounts()
	if err != nil {
		s.log.WithError(err).Error("Failed to get tag counts")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	if counts == nil {
		counts = []models.TagCount{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(counts)
}

// thumbnailFromIDVar resolves the {id} route variable to a thumbnail, writing an error response on failure
func (s *Server) thumbnailFromIDVar(w http.ResponseWriter, r *http.Request) (*models.Thumbnail, bool) {
	idStr := mux.Vars(r)["id"]
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeTagsJSON(w, http.StatusBadRequest, TagsResponse{Error: "Invalid thumbnail ID"})
		return nil, false
	}

	thumbnail, err := s.db.GetByID(id)
	if err != nil {
		s.log.WithError(err).WithField("thumbnail_id", id).Error("Failed to get thumbnail")
		writeTagsJSON(w, http.StatusInternalServerError, TagsResponse{Error: "Internal server error", ThumbnailID: id})
		return nil, false
	}
	if thumbnail == nil {
		writeTagsJSON(w, http.StatusNotFound, TagsResponse{Error: "Thumbnail not found", ThumbnailID: id})
		return nil, false
	}
	return thumbnail, true
}

// thumbnailFromFilename resolves a movie filename to a thumbnail, writing an error response on failure
func (s *Server) thumbnailFromFilename(w http.ResponseWriter, filename string) (*models.Thumbnail, bool) {
	if filename == "" {
		writeTagsJSON(w, http.StatusBadRequest, TagsResponse{Error: "Filename is required"})
		return nil, false
	}

	thumbnail, err := s.db.GetByMovieFilename(filename)
	if err != nil {
		s.log.WithError(err).WithField("filename", filename).Error("Database error when searching for video")
		writeTagsJSON(w, http.StatusInternalServerError, TagsResponse{Error: "Internal server error", Filename: filename})
		return nil, false
	}
	if thumbnail == nil {
		writeTagsJSON(w, http.StatusNotFound, TagsResponse{Error: "Video not found", Filename: filename})
		return nil, false
	}
	return thumbnail, true
}

// applyTagOp normalizes and applies a tag change, then writes the thumbnail's resulting tags
func (s *Server) applyTagOp(w http.ResponseWriter, thumbnail *models.Thumbnail, op tagOp, filename string) {
	set, err := models.NormalizeTags(op.set)
	if err == nil {
		op.set = set
		op.add, err = models.NormalizeTags(op.add)
	}
	if err == nil {
		op.remove, err = models.NormalizeTags(op.remove)
	}
	if err != nil {
		writeTagsJSON(w, http.StatusBadRequest, TagsResponse{Error: err.Error(), Filename: filename, ThumbnailID: thumbnail.ID})
		return
	}

	if op.replace {
		err = s.db.SetTags(thumbnail.ID, op.set)
	}
	if err == nil && len(op.add) > 0 {
		err = s.db.AddTags(thumbnail.ID, op.add)
	}
	if err == nil && len(op.remove) > 0 {
		err = s.db.RemoveTags(thumbnail.ID, op.remove)
	}
	if err != nil {
		s.log.WithError(err).WithField("thumbnail_id", thumbnail.ID).Error("Failed to update tags")
		writeTagsJSON(w, http.StatusInternalServerError, TagsResponse{Error: "Failed to update tags", Filename: filename, ThumbnailID: thumbnail.ID})
		return
	}

	s.log.WithFields(logrus.Fields{
		"thumbnail_id": thumbnail.ID,
		"set":          op.set,
		"add":          op.add,
		"remove":       op.remove,
	}).Info("Updated thumbnail tags via API")

	s.writeThumbnailTags(w, thumbnail, filename)
}

// writeThumbnailTags writes the current tags of a thumbnail
func (s *Server) writeThumbnailTags(w http.ResponseWriter, thumbnail *models.Thumbnail, filename string) {
	tags, err := s.db.GetTags(thumbnail.ID)
	if err != nil {
		s.log.WithError(err).WithField("thumbnail_id", thumbnail.ID).Error("Failed to get tags")
		writeTagsJSON(w, http.StatusInternalServerError, TagsResponse{Error: "Internal server error", Filename: filename, ThumbnailID: thumbnail.ID})
		return
	}
	writeTagsJSON(w, http.StatusOK, TagsResponse{Success: true, Filename: filename, ThumbnailID: thumbnail.ID, Tags: tags})
}

// handleGetThumbnailTags returns the tags of a thumbnail by ID
func (s *Server) handleGetThumbnailTags(w http.ResponseWriter, r *http.Request) {
	thumbnail, ok := s.thumbnailFromIDVar(w, r)
	if !ok {
		return
	}
	s.writeThumbnailTags(w, thumbnail, "")
}

// handleSetThumbnailTags replaces all tags of a thumbnail by ID
func (s *Server) handleSetThumbnailTags(w http.ResponseWriter, r *http.Request) {
	thumbnail, ok := s.thumbnailFromIDVar(w, r)
	if !ok {
		return
	}
	var req TagsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeTagsJSON(w, http.StatusBadRequest, TagsResponse{Error: "Invalid JSON request body", ThumbnailID: thumbnail.ID})
		return
	}
	s.applyTagOp(w, thumbnail, tagOp{set: req.Tags, replace: true}, "")
}

// handleAddThumbnailTags adds tags to a thumbnail by ID
func (s *Server) handleAddThumbnailTags(w http.ResponseWriter, r *http.Request) {
	thumbnail, ok := s.thumbnailFromIDVar(w, r)
	if !ok {
		return
	}
	var req TagsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeTagsJSON(w, http.StatusBadRequest, TagsResponse{Error: "Invalid JSON request body", ThumbnailID: thumbnail.ID})
		return
	}
	s.applyTagOp(w, thumbnail, tagOp{add: req.Tags}, "")
}

// handleRemoveThumbnailTag removes a single tag from a thumbnail by ID
func (s *Server) handleRemoveThumbnailTag(w http.ResponseWriter, r *http.Request) {
	thumbnail, ok := s.thumbnailFromIDVar(w, r)
	if !ok {
		return
	}
	s.applyTagOp(w, thumbnail, tagOp{remove: []string{mux.Vars(r)["tag"]}}, "")
}

// handleAPIGetVideoTags returns the tags of a video by filename
func (s *Server) handleAPIGetVideoTags(w http.ResponseWriter, r *http.Request) {
	filename := mux.Vars(r)["filename"]
	thumbnail, ok := s.thumbnailFromFilename(w, filename)
	if !ok {
		return
	}
	s.writeThumbnailTags(w, thumbnail, filename)
}

// handleAPISetVideoTags replaces all tags of a video by filename
func (s *Server) handleAPISetVideoTags(w http.ResponseWriter, r *http.Request) {
	var req VideoTagsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeTagsJSON(w, http.StatusBadRequest, TagsResponse{Error: "Invalid JSON request body"})
		return
	}
	thumbnail, ok := s.thumbnailFromFilename(w, req.Filename)
	if !ok {
		return
	}
	s.applyTagOp(w, thumbnail, tagOp{set: req.Tags, replace: true}, req.Filename)
}

// handleAPIUpdateVideoTags adds and/or removes tags of a video by filename
func (s *Server) handleAPIUpdateVideoTags(w http.ResponseWriter, r *http.Request) {
	var req VideoTagsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeTagsJSON(w, http.StatusBadRequest, TagsResponse{Error: "Invalid JSON request body"})
		return
	}
	thumbnail, ok := s.thumbnailFromFilename(w, req.Filename)
	if !ok {
		return
	}
	s.applyTagOp(w, thumbnail, tagOp{add: req.Add, remove: req.Remove}, req.Filename)
}
