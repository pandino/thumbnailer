package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gorilla/mux"
	"github.com/pandino/movie-thumbnailer-go/internal/config"
	"github.com/sirupsen/logrus"
)

func TestStaticFilesAreRevalidated(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "css"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "css", "styles.css"), []byte("body{}"), 0644); err != nil {
		t.Fatal(err)
	}
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	s := &Server{
		cfg:     &config.Config{StaticDir: dir, ThumbnailsDir: t.TempDir()},
		log:     logger,
		router:  mux.NewRouter(),
		metrics: sharedTestMetrics(),
	}
	s.routes()

	rr := httptest.NewRecorder()
	s.router.ServeHTTP(rr, httptest.NewRequest("GET", "/static/css/styles.css", nil))
	if rr.Code != http.StatusOK || rr.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("status %d, Cache-Control %q; want 200 and no-cache", rr.Code, rr.Header().Get("Cache-Control"))
	}

	// Revalidation of an unchanged file is a cheap 304
	req := httptest.NewRequest("GET", "/static/css/styles.css", nil)
	req.Header.Set("If-Modified-Since", rr.Header().Get("Last-Modified"))
	rr = httptest.NewRecorder()
	s.router.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotModified {
		t.Errorf("revalidation status = %d; want 304", rr.Code)
	}
}
