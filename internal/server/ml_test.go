package server

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"
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
	if !strings.Contains(rr.Body.String(), "Keep 90%") || !strings.Contains(rr.Body.String(), "likely archive first") {
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
	p := func(v float64) *float64 { return &v }
	for _, tc := range []struct {
		pDelete  float64
		pArchive *float64
		label    string
		pct      int
	}{
		// Scores without an archive probability: delete vs keep
		{0.9, nil, "Delete", 90},
		{0.5, nil, "Delete", 50},
		{0.2, nil, "Keep", 80},
		{0.996, nil, "Delete", 99}, // never a rounded 100%
		{0.002, nil, "Keep", 99},
		// Final-outcome scores: the likelier of delete and archive
		{0.1, p(0.4), "Archive", 40},
		{0.3, p(0.1), "Delete", 30},
		{0.05, p(0.999), "Archive", 99},
	} {
		b := newScoreBadge(&models.Score{PDelete: tc.pDelete, PArchive: tc.pArchive})
		if b.Label != tc.label || b.Percent != tc.pct || b.Delete != (tc.label == "Delete") {
			t.Errorf("p_delete %v p_archive %v -> %+v; want %s %d%%", tc.pDelete, tc.pArchive, b, tc.label, tc.pct)
		}
	}
	if newScoreBadge(nil) != nil {
		t.Error("unscored thumbnail got a badge")
	}
}

func TestVideoStatusPrediction(t *testing.T) {
	s, db := newDBTestServer(t)
	scored := addDBTestThumbnail(t, db, "scored.mp4", 0)
	addDBTestThumbnail(t, db, "unscored.mp4", 0)
	pArchive := 0.1
	db.ReplaceScores([]models.Score{{ThumbnailID: scored.ID, PDelete: 0.87, PArchive: &pArchive}}, nil)

	status := func(filename string) VideoStatusResponse {
		t.Helper()
		req := mux.SetURLVars(httptest.NewRequest("GET", "/api/v1/video/status/"+filename, nil), map[string]string{"filename": filename})
		rr := httptest.NewRecorder()
		s.handleAPIVideoStatus(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("status %s = %d %s", filename, rr.Code, rr.Body.String())
		}
		var resp VideoStatusResponse
		json.NewDecoder(rr.Body).Decode(&resp)
		return resp
	}

	if resp := status("scored.mp4"); resp.Prediction != "Delete 87%" || resp.Score == nil || resp.Score.PDelete != 0.87 {
		t.Errorf("scored = %+v", resp)
	}
	if resp := status("unscored.mp4"); resp.Prediction != "" || resp.Score != nil {
		t.Errorf("unscored = %+v", resp)
	}
}
