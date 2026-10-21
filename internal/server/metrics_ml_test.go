package server

import (
	"math"
	"testing"
	"time"

	"github.com/pandino/movie-thumbnailer-go/internal/metrics"
	"github.com/pandino/movie-thumbnailer-go/internal/models"
	"github.com/prometheus/client_golang/prometheus"
)

// gauge returns the value of the series of the named metric with the given model label ("" for
// an unlabelled metric), as served on /metrics, and how many series the metric has
func gauge(t *testing.T, name, model string) (float64, int) {
	t.Helper()
	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, family := range families {
		if family.GetName() != name {
			continue
		}
		value := math.NaN()
		for _, series := range family.GetMetric() {
			label := ""
			for _, l := range series.GetLabel() {
				if l.GetName() == "model" {
					label = l.GetValue()
				}
			}
			if label == model {
				value = series.GetGauge().GetValue()
			}
		}
		return value, len(family.GetMetric())
	}
	return math.NaN(), 0
}

func value(t *testing.T, name, model string) float64 {
	v, _ := gauge(t, name, model)
	return v
}

func TestModelStatusMetrics(t *testing.T) {
	s, db := newDBTestServer(t)
	s.metrics = sharedTestMetrics()

	a := addDBTestThumbnail(t, db, "a.mp4", 0)
	b := addDBTestThumbnail(t, db, "b.mp4", 0)
	model := "ViT-B-16-SigLIP2-256/webli/f16"
	if err := db.PutEmbeddings(model, []models.Embedding{
		{ThumbnailID: a.ID, Vector: []float32{1, 0}},
		{ThumbnailID: b.ID, Vector: []float32{0, 1}},
	}); err != nil {
		t.Fatal(err)
	}
	pArchive := 0.4
	meta := map[string]string{
		"model": model, "trained_at": "2026-10-21 12:41",
		"auc_delete": "0.88", "brier_delete": "0.059", "auc_archive": "0.75", "brier_archive": "0.123",
	}
	if err := db.ReplaceScores([]models.Score{{ThumbnailID: a.ID, PDelete: 0.2, PArchive: &pArchive}}, meta); err != nil {
		t.Fatal(err)
	}

	s.UpdateMetricsFromStats()
	want := time.Date(2026, 10, 21, 12, 41, 0, 0, time.Local).Unix()
	checks := []struct {
		name      string
		got, want float64
	}{
		{"trained timestamp", value(t, "movie_thumbnailer_ml_trained_timestamp_seconds", ""), float64(want)},
		{"delete AUC", value(t, "movie_thumbnailer_ml_cv_auc", "delete"), 0.88},
		{"archive AUC", value(t, "movie_thumbnailer_ml_cv_auc", "archive"), 0.75},
		{"delete Brier", value(t, "movie_thumbnailer_ml_brier", "delete"), 0.059},
		{"archive Brier", value(t, "movie_thumbnailer_ml_brier", "archive"), 0.123},
		{"embedded", value(t, "movie_thumbnailer_ml_embedded_movies", ""), 2},
		{"scored", value(t, "movie_thumbnailer_ml_scored_movies", ""), 1},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %v; want %v", c.name, c.got, c.want)
		}
	}

	// A model the worker no longer reports disappears instead of keeping its last value
	delete(meta, "auc_archive")
	delete(meta, "brier_archive")
	if err := db.ReplaceScores(nil, meta); err != nil {
		t.Fatal(err)
	}
	s.UpdateMetricsFromStats()
	if _, n := gauge(t, "movie_thumbnailer_ml_cv_auc", ""); n != 1 {
		t.Errorf("AUC series = %d; want only the delete model", n)
	}
	if n := value(t, "movie_thumbnailer_ml_scored_movies", ""); n != 0 {
		t.Errorf("scored = %v after replacing the scores with none", n)
	}
}

func TestTrainedUnix(t *testing.T) {
	if got := metrics.TrainedUnix(""); got != 0 {
		t.Errorf("empty = %d", got)
	}
	if got := metrics.TrainedUnix("yesterday"); got != 0 {
		t.Errorf("malformed = %d", got)
	}
	if got, want := metrics.TrainedUnix("2026-10-21 12:41"), time.Date(2026, 10, 21, 12, 41, 0, 0, time.Local).Unix(); got != want {
		t.Errorf("got %d; want %d", got, want)
	}
}
