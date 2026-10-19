package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
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

const (
	defaultListLimit = 50
	maxListLimit     = 200
)

// ThumbnailListItem is a thumbnail entry of the paged listing
type ThumbnailListItem struct {
	ID            int64    `json:"id"`
	MovieFilename string   `json:"movie_filename"`
	ThumbnailURL  string   `json:"thumbnail_url"`
	Viewed        bool     `json:"viewed"`
	Tags          []string `json:"tags"`
}

// ThumbnailListResponse is a page of thumbnails; NextAfterID is 0 when there are no more pages
type ThumbnailListResponse struct {
	Items       []ThumbnailListItem `json:"items"`
	NextAfterID int64               `json:"next_after_id"`
}

// parseThumbnailFilter reads listing filters from the query string
func parseThumbnailFilter(r *http.Request) (models.ThumbnailFilter, error) {
	q := r.URL.Query()
	f := models.ThumbnailFilter{Limit: defaultListLimit}

	switch q.Get("viewed") {
	case "":
	case "0", "false":
		viewed := false
		f.Viewed = &viewed
	case "1", "true":
		viewed := true
		f.Viewed = &viewed
	default:
		return f, fmt.Errorf("viewed must be 0 or 1")
	}

	switch q.Get("untagged") {
	case "", "0", "false":
	case "1", "true":
		f.Untagged = true
	default:
		return f, fmt.Errorf("untagged must be true or false")
	}

	for param, dst := range map[string]*string{"tag": &f.Tag, "without_tag": &f.WithoutTag} {
		if raw := q.Get(param); raw != "" {
			tag, err := models.NormalizeTag(raw)
			if err != nil {
				return f, fmt.Errorf("%s: %w", param, err)
			}
			*dst = tag
		}
	}

	if raw := q.Get("after_id"); raw != "" {
		afterID, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || afterID < 0 {
			return f, fmt.Errorf("after_id must be a non-negative integer")
		}
		f.AfterID = afterID
	}

	if raw := q.Get("limit"); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit < 1 || limit > maxListLimit {
			return f, fmt.Errorf("limit must be between 1 and %d", maxListLimit)
		}
		f.Limit = limit
	}

	return f, nil
}

// handleAPIListThumbnails returns a cursor-paged list of successful thumbnails with their tags
func (s *Server) handleAPIListThumbnails(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	filter, err := parseThumbnailFilter(r)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	thumbnails, err := s.db.ListThumbnails(filter)
	if err != nil {
		s.log.WithError(err).Error("Failed to list thumbnails")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Internal server error"})
		return
	}

	ids := make([]int64, len(thumbnails))
	for i, thumbnail := range thumbnails {
		ids[i] = thumbnail.ID
	}
	tagsByID, err := s.db.GetTagsForIDs(ids)
	if err != nil {
		s.log.WithError(err).Error("Failed to get tags for thumbnail list")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Internal server error"})
		return
	}

	resp := ThumbnailListResponse{Items: make([]ThumbnailListItem, 0, len(thumbnails))}
	for _, thumbnail := range thumbnails {
		tags := tagsByID[thumbnail.ID]
		if tags == nil {
			tags = []string{}
		}
		resp.Items = append(resp.Items, ThumbnailListItem{
			ID:            thumbnail.ID,
			MovieFilename: thumbnail.MovieFilename,
			ThumbnailURL:  (&url.URL{Path: "/thumbnails/" + thumbnail.ThumbnailPath}).EscapedPath(),
			Viewed:        thumbnail.IsViewed(),
			Tags:          tags,
		})
	}
	if len(thumbnails) == filter.Limit {
		resp.NextAfterID = thumbnails[len(thumbnails)-1].ID
	}

	json.NewEncoder(w).Encode(resp)
}
