package server

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	"github.com/pandino/movie-thumbnailer-go/internal/config"
	"github.com/pandino/movie-thumbnailer-go/internal/database"
	"github.com/pandino/movie-thumbnailer-go/internal/models"
	"github.com/sirupsen/logrus"
)

// newTagTestServer builds a real Server backed by a temporary SQLite database
func newTagTestServer(t *testing.T) (*Server, *database.DB) {
	t.Helper()
	db, err := database.New(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)

	wd, _ := os.Getwd()
	s := &Server{
		cfg:    &config.Config{TemplatesDir: filepath.Join(wd, "..", "..", "web", "templates")},
		db:     db,
		log:    logger,
		router: mux.NewRouter(),
	}
	return s, db
}

func addTagTestThumbnail(t *testing.T, db *database.DB, name string, viewed int) *models.Thumbnail {
	t.Helper()
	th := &models.Thumbnail{
		MoviePath:     "/movies/" + name,
		MovieFilename: name,
		ThumbnailPath: name + ".jpg",
		Status:        models.StatusSuccess,
		Viewed:        viewed,
	}
	if err := db.UpsertThumbnail(th); err != nil {
		t.Fatalf("failed to upsert thumbnail: %v", err)
	}
	return th
}

func decodeTags(t *testing.T, rr *httptest.ResponseRecorder) TagsResponse {
	t.Helper()
	var resp TagsResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	return resp
}

func TestThumbnailTagEndpointsByID(t *testing.T) {
	s, db := newTagTestServer(t)
	th := addTagTestThumbnail(t, db, "a.mp4", 0)
	id := "1"
	if th.ID != 1 {
		t.Fatalf("unexpected thumbnail id %d", th.ID)
	}

	call := func(method, path string, vars map[string]string, body string, h http.HandlerFunc) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req = mux.SetURLVars(req, vars)
		rr := httptest.NewRecorder()
		h(rr, req)
		return rr
	}

	// Replace
	rr := call("PUT", "/api/thumbnails/1/tags", map[string]string{"id": id}, `{"tags":["Foo"," bar ","foo"]}`, s.handleSetThumbnailTags)
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT status = %d; body %s", rr.Code, rr.Body.String())
	}
	if resp := decodeTags(t, rr); strings.Join(resp.Tags, ",") != "bar,foo" {
		t.Errorf("PUT tags = %v; want [bar foo]", resp.Tags)
	}

	// Add
	rr = call("POST", "/api/thumbnails/1/tags", map[string]string{"id": id}, `{"tags":["baz"]}`, s.handleAddThumbnailTags)
	if resp := decodeTags(t, rr); strings.Join(resp.Tags, ",") != "bar,baz,foo" {
		t.Errorf("POST tags = %v; want [bar baz foo]", resp.Tags)
	}

	// Remove one (case-insensitive through normalization)
	rr = call("DELETE", "/api/thumbnails/1/tags/BAR", map[string]string{"id": id, "tag": "BAR"}, "", s.handleRemoveThumbnailTag)
	if resp := decodeTags(t, rr); strings.Join(resp.Tags, ",") != "baz,foo" {
		t.Errorf("DELETE tags = %v; want [baz foo]", resp.Tags)
	}

	// Get
	rr = call("GET", "/api/thumbnails/1/tags", map[string]string{"id": id}, "", s.handleGetThumbnailTags)
	if resp := decodeTags(t, rr); !resp.Success || strings.Join(resp.Tags, ",") != "baz,foo" {
		t.Errorf("GET = %+v", resp)
	}

	// Errors
	if rr := call("PUT", "/api/thumbnails/1/tags", map[string]string{"id": id}, `{"tags":[""]}`, s.handleSetThumbnailTags); rr.Code != http.StatusBadRequest {
		t.Errorf("invalid tag status = %d; want 400", rr.Code)
	}
	if rr := call("PUT", "/api/thumbnails/1/tags", map[string]string{"id": id}, `not json`, s.handleSetThumbnailTags); rr.Code != http.StatusBadRequest {
		t.Errorf("bad json status = %d; want 400", rr.Code)
	}
	if rr := call("GET", "/api/thumbnails/99/tags", map[string]string{"id": "99"}, "", s.handleGetThumbnailTags); rr.Code != http.StatusNotFound {
		t.Errorf("unknown id status = %d; want 404", rr.Code)
	}
	if rr := call("GET", "/api/thumbnails/x/tags", map[string]string{"id": "x"}, "", s.handleGetThumbnailTags); rr.Code != http.StatusBadRequest {
		t.Errorf("bad id status = %d; want 400", rr.Code)
	}
}

func TestVideoTagEndpointsByFilename(t *testing.T) {
	s, db := newTagTestServer(t)
	addTagTestThumbnail(t, db, "movie.mp4", 0)

	rr := httptest.NewRecorder()
	s.handleAPISetVideoTags(rr, httptest.NewRequest("PUT", "/api/v1/video/tags", strings.NewReader(`{"filename":"movie.mp4","tags":["a","b"]}`)))
	if resp := decodeTags(t, rr); rr.Code != http.StatusOK || strings.Join(resp.Tags, ",") != "a,b" || resp.Filename != "movie.mp4" {
		t.Fatalf("PUT = %d %+v", rr.Code, resp)
	}

	rr = httptest.NewRecorder()
	s.handleAPIUpdateVideoTags(rr, httptest.NewRequest("POST", "/api/v1/video/tags", strings.NewReader(`{"filename":"movie.mp4","add":["c"],"remove":["a"]}`)))
	if resp := decodeTags(t, rr); strings.Join(resp.Tags, ",") != "b,c" {
		t.Errorf("POST tags = %v; want [b c]", resp.Tags)
	}

	rr = httptest.NewRecorder()
	req := mux.SetURLVars(httptest.NewRequest("GET", "/api/v1/video/tags/movie.mp4", nil), map[string]string{"filename": "movie.mp4"})
	s.handleAPIGetVideoTags(rr, req)
	if resp := decodeTags(t, rr); strings.Join(resp.Tags, ",") != "b,c" {
		t.Errorf("GET tags = %v; want [b c]", resp.Tags)
	}

	rr = httptest.NewRecorder()
	s.handleAPIUpdateVideoTags(rr, httptest.NewRequest("POST", "/api/v1/video/tags", strings.NewReader(`{"filename":"missing.mp4","add":["c"]}`)))
	if rr.Code != http.StatusNotFound {
		t.Errorf("missing video status = %d; want 404", rr.Code)
	}

	rr = httptest.NewRecorder()
	s.handleAPIUpdateVideoTags(rr, httptest.NewRequest("POST", "/api/v1/video/tags", strings.NewReader(`{"add":["c"]}`)))
	if rr.Code != http.StatusBadRequest {
		t.Errorf("missing filename status = %d; want 400", rr.Code)
	}
}

func TestHandleAPITags(t *testing.T) {
	s, db := newTagTestServer(t)
	a := addTagTestThumbnail(t, db, "a.mp4", 0)
	b := addTagTestThumbnail(t, db, "b.mp4", 1)
	db.SetTags(a.ID, []string{"common", "solo"})
	db.SetTags(b.ID, []string{"common"})

	rr := httptest.NewRecorder()
	s.handleAPITags(rr, httptest.NewRequest("GET", "/api/tags", nil))

	var counts []models.TagCount
	if err := json.NewDecoder(rr.Body).Decode(&counts); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if len(counts) != 2 || counts[0] != (models.TagCount{Tag: "common", Count: 2, Unviewed: 1}) || counts[1].Tag != "solo" {
		t.Errorf("tag counts = %+v", counts)
	}
}

func TestTagSlideshowSession(t *testing.T) {
	s, db := newTagTestServer(t)
	tagged := addTagTestThumbnail(t, db, "tagged.mp4", 0)
	addTagTestThumbnail(t, db, "untagged-1.mp4", 0)
	addTagTestThumbnail(t, db, "untagged-2.mp4", 0)
	db.SetTags(tagged.ID, []string{"pick me"})

	rr := httptest.NewRecorder()
	s.handleSlideshow(rr, httptest.NewRequest("GET", "/slideshow?new=true&tag=Pick%20Me", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("slideshow status = %d; body %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "tagged.mp4") || !strings.Contains(rr.Body.String(), "Tag: pick me") {
		t.Errorf("slideshow page does not show the tagged thumbnail and tag")
	}

	var session SessionData
	for _, c := range rr.Result().Cookies() {
		if c.Name == "slideshow_session" {
			raw, _ := base64.StdEncoding.DecodeString(c.Value)
			json.Unmarshal(raw, &session)
		}
	}
	if session.Tag != "pick me" || session.CurrentID != tagged.ID || session.TotalImages != 1 {
		t.Errorf("session = %+v; want tag 'pick me', current %d, total 1", session, tagged.ID)
	}
	// The prefetch candidate must also come from the tag (it may equal the current one)
	if session.NextID != tagged.ID {
		t.Errorf("NextID = %d; want tagged thumbnail %d", session.NextID, tagged.ID)
	}

	// Invalid tag is rejected
	rr = httptest.NewRecorder()
	s.handleSlideshow(rr, httptest.NewRequest("GET", "/slideshow?new=true&tag=a%2Fb", nil))
	if rr.Code != http.StatusBadRequest {
		t.Errorf("invalid tag status = %d; want 400", rr.Code)
	}
}

func TestHandleAPIListThumbnails(t *testing.T) {
	s, db := newTagTestServer(t)
	a := addTagTestThumbnail(t, db, "a.mp4", 0)
	b := addTagTestThumbnail(t, db, "with space.mp4", 0)
	addTagTestThumbnail(t, db, "c.mp4", 1)
	db.SetTags(a.ID, []string{"ai-reviewed", "cats"})

	get := func(query string) (*httptest.ResponseRecorder, ThumbnailListResponse) {
		rr := httptest.NewRecorder()
		s.handleAPIListThumbnails(rr, httptest.NewRequest("GET", "/api/v1/thumbnails?"+query, nil))
		var resp ThumbnailListResponse
		if rr.Code == http.StatusOK {
			if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
				t.Fatalf("failed to decode: %v", err)
			}
		}
		return rr, resp
	}

	// First page is full, so a cursor is returned
	rr, resp := get("limit=1")
	if rr.Code != http.StatusOK || len(resp.Items) != 1 || resp.NextAfterID != a.ID {
		t.Fatalf("page 1 = %d %+v", rr.Code, resp)
	}
	if item := resp.Items[0]; item.ThumbnailURL != "/thumbnails/a.mp4.jpg" || item.Viewed || strings.Join(item.Tags, ",") != "ai-reviewed,cats" {
		t.Errorf("item = %+v", item)
	}

	// Marker filter skips the reviewed one; partial page ends paging
	rr, resp = get("viewed=0&without_tag=AI-Reviewed")
	if rr.Code != http.StatusOK || len(resp.Items) != 1 || resp.Items[0].ID != b.ID || resp.NextAfterID != 0 {
		t.Fatalf("without_tag = %d %+v", rr.Code, resp)
	}
	if resp.Items[0].ThumbnailURL != "/thumbnails/with%20space.mp4.jpg" || resp.Items[0].Tags == nil {
		t.Errorf("item = %+v", resp.Items[0])
	}

	for _, bad := range []string{"viewed=2", "untagged=maybe", "after_id=-1", "after_id=x", "limit=0", "limit=201", "tag=a%2Fb", "without_tag=%20"} {
		if rr, _ := get(bad); rr.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d; want 400", bad, rr.Code)
		}
	}
}
