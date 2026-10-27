package database

import (
	"fmt"
	"testing"
	"time"

	"github.com/pandino/movie-thumbnailer-go/internal/models"
)

// decisionsOf returns the stage/action pairs recorded for a thumbnail, oldest first
func decisionsOf(t *testing.T, db *DB, id int64) []string {
	t.Helper()
	all, err := db.ListDecisions(0, 1000)
	if err != nil {
		t.Fatalf("ListDecisions failed: %v", err)
	}
	var got []string
	for _, d := range all {
		if d.ThumbnailID == id {
			got = append(got, d.Stage+"/"+d.Action)
		}
	}
	return got
}

func assertDecisions(t *testing.T, db *DB, id int64, want ...string) {
	t.Helper()
	got := decisionsOf(t, db, id)
	if len(got) != len(want) {
		t.Fatalf("decisions of %d = %v; want %v", id, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("decisions of %d = %v; want %v", id, got, want)
		}
	}
}

func TestDecisionsRecorded(t *testing.T) {
	db := newTestDB(t)
	passed := addTestThumbnail(t, db, "passed.mp4", models.StatusSuccess, 0)
	deleted := addTestThumbnail(t, db, "deleted.mp4", models.StatusSuccess, 0)
	watched := addTestThumbnail(t, db, "watched.mp4", models.StatusSuccess, 1)

	if err := db.MarkAsViewedByID(passed.ID); err != nil {
		t.Fatal(err)
	}
	assertDecisions(t, db, passed.ID, "slideshow/pass")

	// Delete, undo, delete again: the log keeps all three
	if err := db.MarkForDeletionByID(deleted.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.RestoreFromDeletionByID(deleted.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.MarkForDeletionByID(deleted.ID); err != nil {
		t.Fatal(err)
	}
	assertDecisions(t, db, deleted.ID, "slideshow/delete", "slideshow/undo", "slideshow/delete")

	if err := db.MarkForArchival(watched.ID, models.StagePlayer); err != nil {
		t.Fatal(err)
	}
	assertDecisions(t, db, watched.ID, "player/archive")

	// Unknown IDs and no-op restores record nothing
	if err := db.MarkForDeletionByID(9999); err != nil {
		t.Fatal(err)
	}
	assertDecisions(t, db, 9999)
	if err := db.RestoreFromDeletionByID(passed.ID); err != nil {
		t.Fatal(err)
	}
	assertDecisions(t, db, passed.ID, "slideshow/pass")
}

func TestDecisionCopiesScore(t *testing.T) {
	db := newTestDB(t)
	th := addTestThumbnail(t, db, "scored.mp4", models.StatusSuccess, 0)
	if err := db.ReplaceScores([]models.Score{{ThumbnailID: th.ID, PDelete: 0.8}}, nil); err != nil {
		t.Fatal(err)
	}
	if err := db.MarkForDeletionByID(th.ID); err != nil {
		t.Fatal(err)
	}

	all, err := db.ListDecisions(0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 || all[0].PDeleteAtDecision == nil || *all[0].PDeleteAtDecision != 0.8 {
		t.Fatalf("decision = %+v; want p_delete_at_decision 0.8", all)
	}
	if all[0].MovieFilename != "scored.mp4" || all[0].Source != models.DecisionLive {
		t.Errorf("decision = %+v", all[0])
	}
}

func TestDecisionsSurviveCleanup(t *testing.T) {
	db := newTestDB(t)
	gone := addTestThumbnail(t, db, "gone.mp4", models.StatusSuccess, 0)
	queued := addTestThumbnail(t, db, "queued.mp4", models.StatusSuccess, 0)
	db.MarkAsViewedByID(gone.ID)
	db.MarkForDeletionByID(queued.ID)

	if err := db.DeleteThumbnail(gone.MoviePath); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CleanupOrphans(); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ResetViewedStatus(); err != nil {
		t.Fatal(err)
	}

	assertDecisions(t, db, gone.ID, "slideshow/pass")
	assertDecisions(t, db, queued.ID, "slideshow/delete")
}

func TestResetViewedUndoesPasses(t *testing.T) {
	db := newTestDB(t)
	passed := addTestThumbnail(t, db, "passed.mp4", models.StatusSuccess, 0)
	twice := addTestThumbnail(t, db, "twice.mp4", models.StatusSuccess, 0)
	legacy := addTestThumbnail(t, db, "legacy.mp4", models.StatusSuccess, 1)
	fresh := addTestThumbnail(t, db, "fresh.mp4", models.StatusSuccess, 0)

	db.MarkAsViewedByID(passed.ID)
	db.MarkForArchival(passed.ID, models.StagePlayer)
	db.MarkAsViewedByID(fresh.ID)
	// A pass, a reset from before resets logged undos, and a second pass
	db.MarkAsViewedByID(twice.ID)
	if _, err := db.db.Exec("UPDATE thumbnails SET viewed = 0 WHERE id = ?", twice.ID); err != nil {
		t.Fatal(err)
	}
	db.MarkAsViewedByID(twice.ID)
	// Back to New by an old reset: not To Watch any more, so left alone
	if _, err := db.db.Exec("UPDATE thumbnails SET viewed = 0 WHERE id = ?", fresh.ID); err != nil {
		t.Fatal(err)
	}

	count, err := db.ResetViewedStatus()
	if err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Errorf("reset %d thumbnails; want 3", count)
	}

	// The player stage keeps its own decisions
	assertDecisions(t, db, passed.ID, "slideshow/pass", "player/archive", "slideshow/undo")
	assertDecisions(t, db, twice.ID, "slideshow/pass", "slideshow/pass", "slideshow/undo", "slideshow/undo")
	assertDecisions(t, db, legacy.ID)
	assertDecisions(t, db, fresh.ID, "slideshow/pass")

	// A second reset finds nothing to undo
	if _, err := db.ResetViewedStatus(); err != nil {
		t.Fatal(err)
	}
	assertDecisions(t, db, twice.ID, "slideshow/pass", "slideshow/pass", "slideshow/undo", "slideshow/undo")
}

func TestRestoreUndoesEachStage(t *testing.T) {
	db := newTestDB(t)
	watched := addTestThumbnail(t, db, "watched.mp4", models.StatusSuccess, 0)
	both := addTestThumbnail(t, db, "both.mp4", models.StatusSuccess, 0)
	legacy := addTestThumbnail(t, db, "legacy.mp4", models.StatusDeleted, 0)

	// Passed, then deleted after watching: both are undone
	db.MarkAsViewedByID(watched.ID)
	db.MarkForDeletion(watched.ID, models.StagePlayer)
	// Deleted in the slideshow and again in the player
	db.MarkForDeletionByID(both.ID)
	db.MarkForDeletion(both.ID, models.StagePlayer)

	for _, id := range []int64{watched.ID, both.ID, legacy.ID} {
		if err := db.RestoreFromDeletionByID(id); err != nil {
			t.Fatal(err)
		}
		th, err := db.GetByID(id)
		if err != nil {
			t.Fatal(err)
		}
		if th.Status != models.StatusSuccess || th.Viewed != 0 {
			t.Errorf("restored %s is %s/viewed=%d; want success/0", th.MovieFilename, th.Status, th.Viewed)
		}
	}

	assertDecisions(t, db, watched.ID, "slideshow/pass", "player/delete", "player/undo", "slideshow/undo")
	got := decisionsOf(t, db, both.ID)
	if len(got) != 4 || got[2] == got[3] || got[2][len(got[2])-5:] != "/undo" || got[3][len(got[3])-5:] != "/undo" {
		t.Errorf("decisions of both = %v; want both deletes undone, one per stage", got)
	}
	// Deleted before decisions were logged: nothing to undo
	assertDecisions(t, db, legacy.ID)
}

func TestUndoStaleDecisions(t *testing.T) {
	db := newTestDB(t)
	stale := addTestThumbnail(t, db, "stale.mp4", models.StatusSuccess, 0)
	toWatch := addTestThumbnail(t, db, "towatch.mp4", models.StatusSuccess, 0)
	queued := addTestThumbnail(t, db, "queued.mp4", models.StatusSuccess, 0)
	restored := addTestThumbnail(t, db, "restored.mp4", models.StatusSuccess, 0)
	db.MarkAsViewedByID(stale.ID)
	db.MarkAsViewedByID(toWatch.ID)
	db.MarkAsViewedByID(queued.ID)
	db.MarkForDeletion(queued.ID, models.StagePlayer)
	// An old reset: back to New with the pass still standing
	if _, err := db.db.Exec("UPDATE thumbnails SET viewed = 0 WHERE id IN (?, ?)", stale.ID, queued.ID); err != nil {
		t.Fatal(err)
	}
	// An old restore of a movie passed, then deleted after watching: it undid the pass
	db.MarkAsViewedByID(restored.ID)
	db.MarkForDeletion(restored.ID, models.StagePlayer)
	if _, err := db.db.Exec("UPDATE thumbnails SET status = 'success', viewed = 0 WHERE id = ?", restored.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.Exec(`INSERT INTO decisions (thumbnail_id, movie_filename, stage, action)
		VALUES (?, 'restored.mp4', 'slideshow', 'undo')`, restored.ID); err != nil {
		t.Fatal(err)
	}

	for _, want := range []int{2, 0} {
		undone, err := db.UndoStaleDecisions()
		if err != nil {
			t.Fatal(err)
		}
		if undone != want {
			t.Errorf("undid %d passes; want %d", undone, want)
		}
	}
	assertDecisions(t, db, stale.ID, "slideshow/pass", "slideshow/undo")
	all, err := db.ListDecisions(0, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if last := all[len(all)-1]; last.Source != models.DecisionBackfill {
		t.Errorf("undo source = %q; want backfill", last.Source)
	}
	assertDecisions(t, db, toWatch.ID, "slideshow/pass")
	assertDecisions(t, db, restored.ID, "slideshow/pass", "player/delete", "slideshow/undo", "player/undo")
	assertDecisions(t, db, queued.ID, "slideshow/pass", "player/delete")
}

func TestMLStatusLiveFavoriteAUC(t *testing.T) {
	db := newTestDB(t)
	// Favorites scored 0.6..1.0; deleted and To Watch movies 0.1..0.5: AUC = 1
	var scores []models.Score
	var favorites, toWatch, deleted []int64
	p := func(v float64) *float64 { return &v }
	for i := 0; i < 5; i++ {
		f := addTestThumbnail(t, db, fmt.Sprintf("f%d.mp4", i), models.StatusSuccess, 0)
		o := addTestThumbnail(t, db, fmt.Sprintf("o%d.mp4", i), models.StatusSuccess, 0)
		scores = append(scores,
			models.Score{ThumbnailID: f.ID, PDelete: 0.1, PArchive: p(0.6 + 0.1*float64(i))},
			models.Score{ThumbnailID: o.ID, PDelete: 0.1, PArchive: p(0.1 + 0.1*float64(i))})
		favorites = append(favorites, f.ID)
		if i < 3 {
			toWatch = append(toWatch, o.ID)
		} else {
			deleted = append(deleted, o.ID)
		}
	}
	// Scored without an archive probability: left out
	old := addTestThumbnail(t, db, "old.mp4", models.StatusSuccess, 0)
	scores = append(scores, models.Score{ThumbnailID: old.ID, PDelete: 0.9})
	db.ReplaceScores(scores, nil)

	// Favorites are passed in the slideshow first: those passes are superseded, not negatives
	for _, id := range favorites {
		db.MarkAsViewedByID(id)
		db.MarkForArchival(id, models.StagePlayer)
	}
	for _, id := range toWatch {
		db.MarkAsViewedByID(id)
	}
	for _, id := range deleted {
		db.MarkForDeletion(id, models.StagePlayer)
	}
	db.MarkForArchival(old.ID, models.StagePlayer)

	status, err := db.GetMLStatus()
	if err != nil {
		t.Fatal(err)
	}
	if status.LiveAUCArchiveDecisions != 10 || status.LiveAUCArchive != 1 {
		t.Errorf("live favorite AUC = %v on %d decisions; want 1 on 10",
			status.LiveAUCArchive, status.LiveAUCArchiveDecisions)
	}

	all, err := db.ListDecisions(0, 1000)
	if err != nil {
		t.Fatal(err)
	}
	last := all[len(all)-1]
	if last.ThumbnailID != old.ID || last.PArchiveAtDecision != nil || last.PDeleteAtDecision == nil {
		t.Errorf("unscored archive probability recorded: %+v", last)
	}
	if a := all[0].PArchiveAtDecision; a == nil || *a != 0.6 {
		t.Errorf("p_archive_at_decision = %v; want 0.6", a)
	}
}

func TestOrderedSelection(t *testing.T) {
	db := newTestDB(t)
	high := addTestThumbnail(t, db, "high.mp4", models.StatusSuccess, 0)
	low := addTestThumbnail(t, db, "low.mp4", models.StatusSuccess, 0)
	mid := addTestThumbnail(t, db, "mid.mp4", models.StatusSuccess, 0)
	unscored := addTestThumbnail(t, db, "unscored.mp4", models.StatusSuccess, 0)
	viewed := addTestThumbnail(t, db, "viewed.mp4", models.StatusSuccess, 1)

	err := db.ReplaceScores([]models.Score{
		{ThumbnailID: high.ID, PDelete: 0.9},
		{ThumbnailID: low.ID, PDelete: 0.1},
		{ThumbnailID: mid.ID, PDelete: 0.55},
		{ThumbnailID: viewed.ID, PDelete: 0.99},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	pick := func(order string, exclude ...int64) int64 {
		t.Helper()
		th, err := db.GetUnviewedThumbnailOrdered(order, exclude...)
		if err != nil {
			t.Fatal(err)
		}
		if th == nil {
			return 0
		}
		return th.ID
	}

	if got := pick(models.OrderLikelyDelete); got != high.ID {
		t.Errorf("likely-delete = %d; want %d (viewed ones are skipped)", got, high.ID)
	}
	if got := pick(models.OrderLikelyDelete, high.ID); got != mid.ID {
		t.Errorf("likely-delete excluding top = %d; want %d", got, mid.ID)
	}
	if got := pick(models.OrderLikelyKeep); got != low.ID {
		t.Errorf("likely-keep = %d; want %d", got, low.ID)
	}
	if got := pick(models.OrderUncertain); got != mid.ID {
		t.Errorf("uncertain = %d; want %d", got, mid.ID)
	}
	// Scored ones exhausted: fall back to an unscored thumbnail
	if got := pick(models.OrderLikelyDelete, high.ID, mid.ID, low.ID); got != unscored.ID {
		t.Errorf("fallback = %d; want unscored %d", got, unscored.ID)
	}
	if got := pick(models.OrderLikelyDelete, high.ID, mid.ID, low.ID, unscored.ID); got != 0 {
		t.Errorf("nothing left = %d; want 0", got)
	}
}

func TestListMLItems(t *testing.T) {
	db := newTestDB(t)
	live := addTestThumbnail(t, db, "live.mp4", models.StatusSuccess, 0)
	decided := addTestThumbnail(t, db, "decided.mp4", models.StatusDeleted, 0)
	addTestThumbnail(t, db, "broken.mp4", models.StatusError, 0)

	geometry := models.GridGeometry{Cols: 8, Rows: 4, TileWidth: 320, TileHeight: 180, Padding: 4}
	db.SetGeometry(live.ID, geometry)
	db.SetGeometry(decided.ID, geometry)

	// Cleanup keeps the decided grid and removes the row
	if err := db.SaveDecidedMovie(decided, "7.jpg", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteThumbnail(decided.MoviePath); err != nil {
		t.Fatal(err)
	}
	// A decided movie without a kept grid is not listed
	noGrid := &models.Thumbnail{ID: 500, MovieFilename: "nogrid.mp4"}
	db.SaveDecidedMovie(noGrid, "", time.Now())

	items, err := db.ListMLItems("", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("items = %+v; want live and decided", items)
	}
	if items[0].ID != live.ID || !items[0].Live || items[0].GridURL != "live.mp4.jpg" || items[0].Geometry != geometry {
		t.Errorf("live item = %+v", items[0])
	}
	if items[1].ID != decided.ID || items[1].Live || items[1].GridURL != "decided/7.jpg" || items[1].Geometry != geometry {
		t.Errorf("decided item = %+v", items[1])
	}

	// Paging and the missing-embedding filter
	if items, _ := db.ListMLItems("", live.ID, 10); len(items) != 1 || items[0].ID != decided.ID {
		t.Errorf("after_id page = %+v", items)
	}
	if err := db.PutEmbeddings("m", []models.Embedding{{ThumbnailID: live.ID, Vector: []float32{1}}}); err != nil {
		t.Fatal(err)
	}
	if items, _ := db.ListMLItems("m", 0, 10); len(items) != 1 || items[0].ID != decided.ID {
		t.Errorf("missing_model items = %+v", items)
	}
	if items, _ := db.ListMLItems("other", 0, 10); len(items) != 2 {
		t.Errorf("missing other model = %+v; want both", items)
	}
}

func TestEmbeddingsRoundTrip(t *testing.T) {
	db := newTestDB(t)
	want := []float32{0.25, -1.5, 3e-7}
	if err := db.PutEmbeddings("siglip", []models.Embedding{{ThumbnailID: 3, Vector: want}}); err != nil {
		t.Fatal(err)
	}
	// Replacing keeps one row per thumbnail and model
	if err := db.PutEmbeddings("siglip", []models.Embedding{{ThumbnailID: 3, Vector: want}}); err != nil {
		t.Fatal(err)
	}

	got, err := db.ListEmbeddings("siglip", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ThumbnailID != 3 || len(got[0].Vector) != 3 {
		t.Fatalf("embeddings = %+v", got)
	}
	for i := range want {
		if got[0].Vector[i] != want[i] {
			t.Errorf("vector[%d] = %v; want %v", i, got[0].Vector[i], want[i])
		}
	}
	if other, _ := db.ListEmbeddings("clip", 0, 10); len(other) != 0 {
		t.Errorf("other model returned %+v", other)
	}
}

func TestAllocateID(t *testing.T) {
	db := newTestDB(t)
	th := addTestThumbnail(t, db, "a.mp4", models.StatusSuccess, 0)

	id1, err := db.AllocateID()
	if err != nil {
		t.Fatal(err)
	}
	id2, err := db.AllocateID()
	if err != nil {
		t.Fatal(err)
	}
	if id1 <= th.ID || id2 <= id1 {
		t.Errorf("allocated %d, %d after thumbnail %d; want strictly increasing", id1, id2, th.ID)
	}
	all, _ := db.GetAllThumbnails()
	if len(all) != 1 {
		t.Errorf("allocation left rows behind: %d thumbnails", len(all))
	}
	// New thumbnails never reuse an allocated ID
	next := addTestThumbnail(t, db, "b.mp4", models.StatusSuccess, 0)
	if next.ID <= id2 {
		t.Errorf("new thumbnail got ID %d; want above %d", next.ID, id2)
	}
}

func TestBackfillDecisions(t *testing.T) {
	db := newTestDB(t)
	viewed := addTestThumbnail(t, db, "viewed.mp4", models.StatusSuccess, 1)
	addTestThumbnail(t, db, "unviewed.mp4", models.StatusSuccess, 0)
	decidedLive := addTestThumbnail(t, db, "decided.mp4", models.StatusSuccess, 1)
	db.MarkForDeletionByID(decidedLive.ID)

	n, err := db.BackfillViewedAsPassed()
	if err != nil || n != 1 {
		t.Fatalf("BackfillViewedAsPassed = %d, %v; want 1", n, err)
	}
	if n, _ := db.BackfillViewedAsPassed(); n != 0 {
		t.Errorf("second run recorded %d; want 0", n)
	}
	assertDecisions(t, db, viewed.ID, "slideshow/pass")

	at := time.Date(2026, 6, 9, 21, 38, 0, 0, time.UTC)
	ok, err := db.RecordBackfillDecision(777, "old.mp4", models.StageSlideshow, models.ActionDelete, at)
	if err != nil || !ok {
		t.Fatalf("RecordBackfillDecision = %v, %v", ok, err)
	}
	if ok, _ := db.RecordBackfillDecision(777, "old.mp4", models.StageSlideshow, models.ActionDelete, at); ok {
		t.Error("second backfill of the same stage was recorded")
	}
	all, _ := db.ListDecisions(0, 100)
	last := all[len(all)-1]
	if last.ThumbnailID != 777 || last.Source != models.DecisionBackfill || !last.CreatedAt.Equal(at) {
		t.Errorf("backfilled decision = %+v", last)
	}
}

func TestMLStatus(t *testing.T) {
	db := newTestDB(t)
	a := addTestThumbnail(t, db, "a.mp4", models.StatusSuccess, 0)
	b := addTestThumbnail(t, db, "b.mp4", models.StatusSuccess, 0)
	addTestThumbnail(t, db, "unscored.mp4", models.StatusSuccess, 0)
	db.ReplaceScores([]models.Score{{ThumbnailID: a.ID, PDelete: 0.9}, {ThumbnailID: b.ID, PDelete: 0.2}},
		map[string]string{"genre_auc": "0.81"})
	db.MarkForDeletionByID(a.ID)
	db.MarkAsViewedByID(b.ID)
	db.PutEmbeddings("m", []models.Embedding{{ThumbnailID: a.ID, Vector: []float32{1}}})

	status, err := db.GetMLStatus()
	if err != nil {
		t.Fatal(err)
	}
	if status.Decisions["slideshow/delete"] != 1 || status.Decisions["slideshow/pass"] != 1 {
		t.Errorf("decision counts = %v", status.Decisions)
	}
	if status.Scored != 2 || status.Embedded != 1 || status.Meta["genre_auc"] != "0.81" {
		t.Errorf("status = %+v", status)
	}
	// Too few scored decisions for a live AUC
	if status.LiveAUCDecisions != 0 {
		t.Errorf("live AUC reported on %d decisions; want none yet", status.LiveAUCDecisions)
	}
}

func TestMLStatusLiveAUC(t *testing.T) {
	db := newTestDB(t)
	// Deleted movies scored 0.5..0.9, kept ones 0.1..0.5: only the 0.5 pair ties, so
	// AUC = (25 pairs - 0.5) / 25 = 0.98
	var scores []models.Score
	var deleted, kept []int64
	for i := 0; i < 5; i++ {
		d := addTestThumbnail(t, db, fmt.Sprintf("d%d.mp4", i), models.StatusSuccess, 0)
		k := addTestThumbnail(t, db, fmt.Sprintf("k%d.mp4", i), models.StatusSuccess, 0)
		scores = append(scores,
			models.Score{ThumbnailID: d.ID, PDelete: 0.5 + 0.1*float64(i)},
			models.Score{ThumbnailID: k.ID, PDelete: 0.1 + 0.1*float64(i)})
		deleted, kept = append(deleted, d.ID), append(kept, k.ID)
	}
	// A low-scored delete that was undone must not count
	undone := addTestThumbnail(t, db, "undone.mp4", models.StatusSuccess, 0)
	scores = append(scores, models.Score{ThumbnailID: undone.ID, PDelete: 0.01})
	db.ReplaceScores(scores, nil)

	for i := range deleted {
		db.MarkForDeletionByID(deleted[i])
		db.MarkAsViewedByID(kept[i])
	}
	db.MarkForDeletionByID(undone.ID)
	db.RestoreFromDeletionByID(undone.ID)

	status, err := db.GetMLStatus()
	if err != nil {
		t.Fatal(err)
	}
	if status.LiveAUCDecisions != 10 || status.LiveAUC < 0.979 || status.LiveAUC > 0.981 {
		t.Errorf("live AUC = %v on %d decisions; want 0.98 on 10", status.LiveAUC, status.LiveAUCDecisions)
	}
}

func TestOrderedSelectionWithArchiveScores(t *testing.T) {
	db := newTestDB(t)
	liked := addTestThumbnail(t, db, "liked.mp4", models.StatusSuccess, 0)
	disliked := addTestThumbnail(t, db, "disliked.mp4", models.StatusSuccess, 0)
	unsure := addTestThumbnail(t, db, "unsure.mp4", models.StatusSuccess, 0)
	undecided := addTestThumbnail(t, db, "undecided.mp4", models.StatusSuccess, 0)
	p := func(v float64) *float64 { return &v }
	err := db.ReplaceScores([]models.Score{
		{ThumbnailID: liked.ID, PDelete: 0.1, PArchive: p(0.8)},       // likeliest outcome 0.8
		{ThumbnailID: disliked.ID, PDelete: 0.6, PArchive: p(0.1)},    // 0.6
		{ThumbnailID: unsure.ID, PDelete: 0.3, PArchive: p(0.3)},      // 0.4: least sure
		{ThumbnailID: undecided.ID, PDelete: 0.05, PArchive: p(0.05)}, // 0.9 undecided
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	for order, want := range map[string]int64{
		models.OrderLikelyDelete: disliked.ID,
		models.OrderLikelyKeep:   liked.ID,
		models.OrderUncertain:    unsure.ID,
	} {
		th, err := db.GetUnviewedThumbnailOrdered(order)
		if err != nil {
			t.Fatal(err)
		}
		if th == nil || th.ID != want {
			t.Errorf("%s picked %+v; want %d", order, th, want)
		}
	}
}

func TestReplaceScoresReplacesMeta(t *testing.T) {
	db := newTestDB(t)
	db.ReplaceScores(nil, map[string]string{"genre_auc": "0.80", "model": "a"})
	db.ReplaceScores(nil, map[string]string{"auc_delete": "0.81", "model": "b"})

	status, err := db.GetMLStatus()
	if err != nil {
		t.Fatal(err)
	}
	if _, stale := status.Meta["genre_auc"]; stale || status.Meta["auc_delete"] != "0.81" || status.Meta["model"] != "b" {
		t.Errorf("meta = %v; want only the latest run's keys", status.Meta)
	}
}
