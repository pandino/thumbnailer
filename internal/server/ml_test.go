package server

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pandino/movie-thumbnailer-go/internal/models"
	"github.com/pandino/movie-thumbnailer-go/internal/scanner"
)

func mlRequest(t *testing.T, handler http.HandlerFunc, method, target string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	rr := httptest.NewRecorder()
	handler(rr, httptest.NewRequest(method, target, &buf))
	return rr
}

func TestMLEndpointsRoundTrip(t *testing.T) {
	s, db := newDBTestServer(t)
	a := addDBTestThumbnail(t, db, "with space.mp4", 0)
	b := addDBTestThumbnail(t, db, "b.mp4", 0)
	db.SetGeometry(a.ID, models.GridGeometry{Cols: 8, Rows: 4, TileWidth: 320, TileHeight: 180, Padding: 4})
	db.MarkForDeletionByID(b.ID)

	// Items, paged and with escaped grid URLs
	rr := mlRequest(t, s.handleMLItems, "GET", "/api/v1/ml/items?limit=1", nil)
	var page MLItemsResponse
	json.NewDecoder(rr.Body).Decode(&page)
	if rr.Code != http.StatusOK || len(page.Items) != 1 || page.NextAfterID != a.ID {
		t.Fatalf("items page = %d %+v", rr.Code, page)
	}
	if page.Items[0].GridURL != "/thumbnails/with%20space.mp4.jpg" || page.Items[0].Geometry.Cols != 8 {
		t.Errorf("item = %+v", page.Items[0])
	}

	// Embeddings in, then the item is no longer missing one
	rr = mlRequest(t, s.handlePutEmbeddings, "PUT", "/api/v1/ml/embeddings", EmbeddingsRequest{
		Model:      "siglip",
		Embeddings: []models.Embedding{{ThumbnailID: a.ID, Vector: []float32{0.5, -0.5}}},
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("put embeddings = %d %s", rr.Code, rr.Body.String())
	}
	rr = mlRequest(t, s.handleMLItems, "GET", "/api/v1/ml/items?missing_model=siglip", nil)
	json.NewDecoder(rr.Body).Decode(&page)
	if len(page.Items) != 1 || page.Items[0].ID != b.ID {
		t.Errorf("missing items = %+v", page.Items)
	}
	rr = mlRequest(t, s.handleListEmbeddings, "GET", "/api/v1/ml/embeddings?model=siglip", nil)
	var emb EmbeddingsResponse
	json.NewDecoder(rr.Body).Decode(&emb)
	if len(emb.Embeddings) != 1 || emb.Embeddings[0].Vector[1] != -0.5 {
		t.Errorf("embeddings = %+v", emb)
	}

	// Decisions
	rr = mlRequest(t, s.handleListDecisions, "GET", "/api/v1/ml/decisions", nil)
	var dec DecisionsResponse
	json.NewDecoder(rr.Body).Decode(&dec)
	if len(dec.Decisions) != 1 || dec.Decisions[0].ThumbnailID != b.ID || dec.Decisions[0].Action != models.ActionDelete {
		t.Errorf("decisions = %+v", dec)
	}

	// Scores: invalid probabilities are rejected, valid ones show up in the status
	bad := 1.5
	rr = mlRequest(t, s.handlePutScores, "PUT", "/api/v1/ml/scores", ScoresRequest{
		Scores: []models.Score{{ThumbnailID: a.ID, PDelete: 0.2, PArchive: &bad}},
	})
	if rr.Code != http.StatusBadRequest {
		t.Errorf("invalid score status = %d; want 400", rr.Code)
	}
	rr = mlRequest(t, s.handlePutScores, "PUT", "/api/v1/ml/scores", ScoresRequest{
		Scores: []models.Score{{ThumbnailID: a.ID, PDelete: 0.2}},
		Meta:   map[string]string{"genre_auc": "0.77"},
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("put scores = %d %s", rr.Code, rr.Body.String())
	}
	rr = mlRequest(t, s.handleMLStatus, "GET", "/api/v1/ml/status", nil)
	var status models.MLStatus
	json.NewDecoder(rr.Body).Decode(&status)
	if status.Scored != 1 || status.Embedded != 1 || status.Meta["genre_auc"] != "0.77" || status.Decisions["slideshow/delete"] != 1 {
		t.Errorf("status = %+v", status)
	}

	// Bad requests
	for _, tc := range []struct {
		handler http.HandlerFunc
		method  string
		target  string
		body    interface{}
	}{
		{s.handleMLItems, "GET", "/api/v1/ml/items?limit=0", nil},
		{s.handleListEmbeddings, "GET", "/api/v1/ml/embeddings", nil},
		{s.handlePutEmbeddings, "PUT", "/api/v1/ml/embeddings", EmbeddingsRequest{Embeddings: []models.Embedding{{ThumbnailID: 1, Vector: []float32{1}}}}},
		{s.handlePutEmbeddings, "PUT", "/api/v1/ml/embeddings", EmbeddingsRequest{Model: "m", Embeddings: []models.Embedding{{ThumbnailID: 1}}}},
	} {
		if rr := mlRequest(t, tc.handler, tc.method, tc.target, tc.body); rr.Code != http.StatusBadRequest {
			t.Errorf("%s %s = %d; want 400", tc.method, tc.target, rr.Code)
		}
	}
}

// sessionFrom decodes the last slideshow session cookie set by a response (handlers may save
// the session more than once while handling a request)
func sessionFrom(t *testing.T, rr *httptest.ResponseRecorder) (*SessionData, *http.Cookie) {
	t.Helper()
	var last *http.Cookie
	for _, c := range rr.Result().Cookies() {
		if c.Name == "slideshow_session" {
			last = c
		}
	}
	if last == nil {
		t.Fatal("no session cookie set")
	}
	raw, _ := base64.StdEncoding.DecodeString(last.Value)
	var session SessionData
	json.Unmarshal(raw, &session)
	return &session, last
}

func TestOrderedSlideshowSession(t *testing.T) {
	s, db := newDBTestServer(t)
	// Untagged sessions count unviewed thumbnails through the scanner
	s.scanner = scanner.New(s.cfg, db, s.log, nil)
	high := addDBTestThumbnail(t, db, "high.mp4", 0)
	mid := addDBTestThumbnail(t, db, "mid.mp4", 0)
	low := addDBTestThumbnail(t, db, "low.mp4", 0)
	db.ReplaceScores([]models.Score{
		{ThumbnailID: high.ID, PDelete: 0.9},
		{ThumbnailID: mid.ID, PDelete: 0.6},
		{ThumbnailID: low.ID, PDelete: 0.1},
	}, nil)

	rr := httptest.NewRecorder()
	s.handleSlideshow(rr, httptest.NewRequest("GET", "/slideshow?new=true&order=likely-delete", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("slideshow = %d %s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if !strings.Contains(body, "high.mp4") || !strings.Contains(body, "Delete 90%") || !strings.Contains(body, "likely delete first") {
		t.Errorf("first slide does not show the top score, its badge and the order")
	}
	session, cookie := sessionFrom(t, rr)
	if session.Order != models.OrderLikelyDelete || session.CurrentID != high.ID || session.NextID != mid.ID {
		t.Fatalf("session = %+v; want current %d, next %d", session, high.ID, mid.ID)
	}

	// Skip the top one: it must not come back in this session even though it ranks first
	req := httptest.NewRequest("GET", "/slideshow/next?skip=true", nil)
	req.AddCookie(cookie)
	rr = httptest.NewRecorder()
	s.handleSlideshowNext(rr, req)
	session, cookie = sessionFrom(t, rr)
	if session.CurrentID != mid.ID || len(session.Skipped) != 1 || session.Skipped[0] != high.ID {
		t.Fatalf("after skip session = %+v", session)
	}
	if session.NextID != low.ID {
		t.Errorf("after skip NextID = %d; want %d (skipped one excluded)", session.NextID, low.ID)
	}

	// The keep badge shows the confidence in keeping
	db.MarkAsViewedByID(high.ID)
	db.MarkAsViewedByID(mid.ID)
	rr = httptest.NewRecorder()
	s.handleSlideshow(rr, httptest.NewRequest("GET", "/slideshow?new=true&order=likely-keep", nil))
	if !strings.Contains(rr.Body.String(), "Keep 90%") {
		t.Errorf("keep badge missing")
	}

	// Invalid orders are rejected; "random" is the default order
	rr = httptest.NewRecorder()
	s.handleSlideshow(rr, httptest.NewRequest("GET", "/slideshow?new=true&order=sideways", nil))
	if rr.Code != http.StatusBadRequest {
		t.Errorf("invalid order = %d; want 400", rr.Code)
	}
	rr = httptest.NewRecorder()
	s.handleSlideshow(rr, httptest.NewRequest("GET", "/slideshow?new=true&order=random", nil))
	if session, _ := sessionFrom(t, rr); session.Order != "" {
		t.Errorf("random order stored as %q; want empty", session.Order)
	}
}

func TestPlayerDecisionsAreRecorded(t *testing.T) {
	s, db := newDBTestServer(t)
	watched := addDBTestThumbnail(t, db, "watched.mp4", 1)
	disliked := addDBTestThumbnail(t, db, "disliked.mp4", 1)

	rr := mlRequest(t, s.handleAPIArchiveVideo, "POST", "/api/v1/video/archive", VideoRequest{Filename: "watched.mp4"})
	if rr.Code != http.StatusOK {
		t.Fatalf("archive = %d %s", rr.Code, rr.Body.String())
	}
	rr = mlRequest(t, s.handleAPIDeleteVideo, "POST", "/api/v1/video/delete", VideoRequest{Filename: "disliked.mp4"})
	if rr.Code != http.StatusOK {
		t.Fatalf("delete = %d %s", rr.Code, rr.Body.String())
	}

	decisions, _ := db.ListDecisions(0, 10)
	want := map[int64]string{watched.ID: "player/archive", disliked.ID: "player/delete"}
	if len(decisions) != 2 {
		t.Fatalf("decisions = %+v", decisions)
	}
	for _, d := range decisions {
		if want[d.ThumbnailID] != d.Stage+"/"+d.Action {
			t.Errorf("decision %+v; want %s", d, want[d.ThumbnailID])
		}
	}
}

func TestScoreBadge(t *testing.T) {
	for _, tc := range []struct {
		p      float64
		delete bool
		pct    int
	}{
		{0.9, true, 90},
		{0.5, true, 50},
		{0.2, false, 80},
		{0.996, true, 99}, // never a rounded 100%
		{0.002, false, 99},
	} {
		b := newScoreBadge(&models.Score{PDelete: tc.p})
		if b.Delete != tc.delete || b.Percent != tc.pct {
			t.Errorf("p_delete %v -> %+v; want delete=%v %d%%", tc.p, b, tc.delete, tc.pct)
		}
	}
	if newScoreBadge(nil) != nil {
		t.Error("unscored thumbnail got a badge")
	}
}
