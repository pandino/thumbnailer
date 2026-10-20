package database

import (
	"database/sql"
	"encoding/binary"
	"fmt"
	"math"
	"time"

	"github.com/pandino/movie-thumbnailer-go/internal/models"
)

// initMLSchema creates the tables behind the preference model. None of them are touched by
// cleanup or reset-views: decisions and kept grids are the training history.
func initMLSchema(db *sql.DB) error {
	_, err := db.Exec(`
		-- Append-only log of user decisions (see models.Stage*/Action*)
		CREATE TABLE IF NOT EXISTS decisions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			thumbnail_id INTEGER NOT NULL,
			movie_filename TEXT NOT NULL,
			stage TEXT NOT NULL,
			action TEXT NOT NULL,
			source TEXT NOT NULL DEFAULT 'live',
			p_delete_at_decision REAL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_decisions_thumbnail ON decisions(thumbnail_id);

		-- Tile layout of each grid; cols = 0 means checked but unknown
		CREATE TABLE IF NOT EXISTS grid_geometry (
			thumbnail_id INTEGER PRIMARY KEY,
			cols INTEGER NOT NULL,
			rows INTEGER NOT NULL,
			tile_width INTEGER NOT NULL,
			tile_height INTEGER NOT NULL,
			padding INTEGER NOT NULL
		);

		-- Movies that left the library after a decision; their grid is kept in decided/
		CREATE TABLE IF NOT EXISTS decided_movies (
			thumbnail_id INTEGER PRIMARY KEY,
			movie_filename TEXT NOT NULL,
			grid_file TEXT NOT NULL DEFAULT '',
			duration REAL DEFAULT 0,
			width INTEGER DEFAULT 0,
			height INTEGER DEFAULT 0,
			file_size INTEGER DEFAULT 0,
			source TEXT DEFAULT 'generated',
			decided_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_decided_movies_filename ON decided_movies(movie_filename);

		-- Image embeddings computed by the desktop worker (a cache: recomputable from the grids)
		CREATE TABLE IF NOT EXISTS embeddings (
			thumbnail_id INTEGER NOT NULL,
			model TEXT NOT NULL,
			vector BLOB NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (thumbnail_id, model)
		);

		-- Latest predictions for live thumbnails, replaced as a whole on every worker run
		CREATE TABLE IF NOT EXISTS scores (
			thumbnail_id INTEGER PRIMARY KEY,
			p_delete REAL NOT NULL,
			p_archive REAL,
			scored_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);

		-- Model status reported by the worker (label counts, CV AUC, model version, ...)
		CREATE TABLE IF NOT EXISTS ml_meta (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		);
	`)
	return err
}

// recordDecision appends a decision for a live thumbnail, copying its current p_delete so the
// model's ranking of the user's decisions can be measured later (live AUC)
func recordDecision(tx *sql.Tx, id int64, stage, action, source string) error {
	_, err := tx.Exec(`
		INSERT INTO decisions (thumbnail_id, movie_filename, stage, action, source, p_delete_at_decision)
		SELECT t.id, t.movie_filename, ?, ?, ?, (SELECT p_delete FROM scores WHERE thumbnail_id = t.id)
		FROM thumbnails t
		WHERE t.id = ?`,
		stage, action, source, id,
	)
	return err
}

// RecordBackfillDecision appends a reconstructed decision, which may refer to a thumbnail that no
// longer exists. It does nothing if the thumbnail already has a decision for that stage.
func (d *DB) RecordBackfillDecision(id int64, movieFilename, stage, action string, at time.Time) (bool, error) {
	result, err := d.db.Exec(`
		INSERT INTO decisions (thumbnail_id, movie_filename, stage, action, source, created_at)
		SELECT ?, ?, ?, ?, ?, ?
		WHERE NOT EXISTS (SELECT 1 FROM decisions WHERE thumbnail_id = ? AND stage = ?)`,
		id, movieFilename, stage, action, models.DecisionBackfill, at.UTC(), id, stage,
	)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	return affected > 0, err
}

// BackfillViewedAsPassed records a slideshow "pass" for every viewed thumbnail that has no
// slideshow decision yet, returning how many were recorded
func (d *DB) BackfillViewedAsPassed() (int64, error) {
	result, err := d.db.Exec(`
		INSERT INTO decisions (thumbnail_id, movie_filename, stage, action, source, created_at)
		SELECT id, movie_filename, ?, ?, ?, updated_at
		FROM thumbnails
		WHERE status = 'success' AND viewed = 1
		AND id NOT IN (SELECT thumbnail_id FROM decisions WHERE stage = ?)`,
		models.StageSlideshow, models.ActionPass, models.DecisionBackfill, models.StageSlideshow,
	)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// SetGeometry stores the tile layout of a thumbnail's grid
func (d *DB) SetGeometry(id int64, g models.GridGeometry) error {
	_, err := d.db.Exec(`
		INSERT OR REPLACE INTO grid_geometry (thumbnail_id, cols, rows, tile_width, tile_height, padding)
		VALUES (?, ?, ?, ?, ?, ?)`,
		id, g.Cols, g.Rows, g.TileWidth, g.TileHeight, g.Padding,
	)
	return err
}

// DecidedGrid is a kept grid of a decided movie, by thumbnail ID and file name inside decided/
type DecidedGrid struct {
	ID       int64
	GridFile string
}

// GetDecidedGridsWithoutGeometry returns kept grids of decided movies whose layout hasn't been
// checked yet (e.g. movies that left the library before their layout was recorded)
func (d *DB) GetDecidedGridsWithoutGeometry() ([]DecidedGrid, error) {
	rows, err := d.db.Query(`
		SELECT thumbnail_id, grid_file
		FROM decided_movies
		WHERE grid_file != ''
		AND thumbnail_id NOT IN (SELECT thumbnail_id FROM grid_geometry)
		ORDER BY thumbnail_id ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var grids []DecidedGrid
	for rows.Next() {
		var g DecidedGrid
		if err := rows.Scan(&g.ID, &g.GridFile); err != nil {
			return nil, err
		}
		grids = append(grids, g)
	}
	return grids, rows.Err()
}

// HasGeometry reports whether a thumbnail's grid layout has been recorded (known or unknown)
func (d *DB) HasGeometry(id int64) (bool, error) {
	var exists bool
	err := d.db.QueryRow("SELECT EXISTS (SELECT 1 FROM grid_geometry WHERE thumbnail_id = ?)", id).Scan(&exists)
	return exists, err
}

// GetThumbnailsWithoutGeometry returns thumbnails with a grid whose layout hasn't been checked yet
func (d *DB) GetThumbnailsWithoutGeometry() ([]*models.Thumbnail, error) {
	rows, err := d.db.Query(`
		SELECT
			id, movie_path, movie_filename, thumbnail_path,
			created_at, updated_at, status, viewed,
			width, height, duration, file_size, error_message, source
		FROM thumbnails
		WHERE status IN ('success', 'deleted', 'archived') AND thumbnail_path != ''
		AND id NOT IN (SELECT thumbnail_id FROM grid_geometry)
		ORDER BY id ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanThumbnails(rows)
}

// SaveDecidedMovie snapshots a thumbnail that is about to leave the library after a decision.
// gridFile is the kept grid's name inside decided/, or "" if no grid could be kept.
func (d *DB) SaveDecidedMovie(t *models.Thumbnail, gridFile string, decidedAt time.Time) error {
	_, err := d.db.Exec(`
		INSERT OR REPLACE INTO decided_movies
		(thumbnail_id, movie_filename, grid_file, duration, width, height, file_size, source, decided_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.MovieFilename, gridFile, t.Duration, t.Width, t.Height, t.FileSize, t.Source, decidedAt.UTC(),
	)
	return err
}

// DecidedMovieExists reports whether a decided movie with this filename is already recorded
func (d *DB) DecidedMovieExists(movieFilename string) (bool, error) {
	var exists bool
	err := d.db.QueryRow(
		"SELECT EXISTS (SELECT 1 FROM decided_movies WHERE movie_filename = ?)", movieFilename,
	).Scan(&exists)
	return exists, err
}

// DecidedMovieExistsByID reports whether a decided movie with this thumbnail ID is recorded
func (d *DB) DecidedMovieExistsByID(id int64) (bool, error) {
	var exists bool
	err := d.db.QueryRow(
		"SELECT EXISTS (SELECT 1 FROM decided_movies WHERE thumbnail_id = ?)", id,
	).Scan(&exists)
	return exists, err
}

// AllocateID reserves a fresh thumbnail ID without leaving a thumbnail row behind, for grids of
// movies that are recorded only as decided (e.g. backfilled archived movies). AUTOINCREMENT
// guarantees the ID is never handed out again.
func (d *DB) AllocateID() (int64, error) {
	tx, err := d.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	result, err := tx.Exec(`
		INSERT INTO thumbnails (movie_path, movie_filename, status)
		VALUES ('__allocate_id__', '__allocate_id__', 'pending')`)
	if err != nil {
		return 0, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}
	if _, err := tx.Exec("DELETE FROM thumbnails WHERE id = ?", id); err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

// ListMLItems returns live and decided grids ordered by ID for cursor paging. GridURL holds the
// grid's path relative to the thumbnails directory. If missingModel is set, only grids without
// an embedding for that model are returned.
func (d *DB) ListMLItems(missingModel string, afterID int64, limit int) ([]models.MLItem, error) {
	condition := ""
	args := []interface{}{afterID}
	if missingModel != "" {
		condition = " AND NOT EXISTS (SELECT 1 FROM embeddings e WHERE e.thumbnail_id = i.id AND e.model = ?)"
		args = append(args, missingModel)
	}
	args = append(args, limit)

	rows, err := d.db.Query(`
		SELECT i.id, i.movie_filename, i.grid, i.live,
			COALESCE(g.cols, 0), COALESCE(g.rows, 0), COALESCE(g.tile_width, 0),
			COALESCE(g.tile_height, 0), COALESCE(g.padding, 0)
		FROM (
			SELECT id, movie_filename, thumbnail_path AS grid, 1 AS live
			FROM thumbnails
			WHERE status IN ('success', 'deleted', 'archived') AND thumbnail_path != ''
			UNION ALL
			SELECT thumbnail_id, movie_filename, 'decided/' || grid_file, 0
			FROM decided_movies
			WHERE grid_file != '' AND thumbnail_id NOT IN (SELECT id FROM thumbnails)
		) i
		LEFT JOIN grid_geometry g ON g.thumbnail_id = i.id
		WHERE i.id > ?`+condition+`
		ORDER BY i.id ASC
		LIMIT ?`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []models.MLItem{}
	for rows.Next() {
		var item models.MLItem
		g := &item.Geometry
		if err := rows.Scan(&item.ID, &item.MovieFilename, &item.GridURL, &item.Live,
			&g.Cols, &g.Rows, &g.TileWidth, &g.TileHeight, &g.Padding); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// PutEmbeddings stores embeddings computed by the given model, replacing earlier ones
func (d *DB) PutEmbeddings(model string, embeddings []models.Embedding) error {
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, e := range embeddings {
		if _, err := tx.Exec(`
			INSERT OR REPLACE INTO embeddings (thumbnail_id, model, vector, created_at)
			VALUES (?, ?, ?, CURRENT_TIMESTAMP)`,
			e.ThumbnailID, model, encodeVector(e.Vector),
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ListEmbeddings returns embeddings of the given model ordered by thumbnail ID for cursor paging
func (d *DB) ListEmbeddings(model string, afterID int64, limit int) ([]models.Embedding, error) {
	rows, err := d.db.Query(`
		SELECT thumbnail_id, vector
		FROM embeddings
		WHERE model = ? AND thumbnail_id > ?
		ORDER BY thumbnail_id ASC
		LIMIT ?`,
		model, afterID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	embeddings := []models.Embedding{}
	for rows.Next() {
		var e models.Embedding
		var blob []byte
		if err := rows.Scan(&e.ThumbnailID, &blob); err != nil {
			return nil, err
		}
		if e.Vector, err = decodeVector(blob); err != nil {
			return nil, fmt.Errorf("embedding of thumbnail %d: %w", e.ThumbnailID, err)
		}
		embeddings = append(embeddings, e)
	}
	return embeddings, rows.Err()
}

// ListDecisions returns the decision log ordered by ID for cursor paging
func (d *DB) ListDecisions(afterID int64, limit int) ([]models.Decision, error) {
	rows, err := d.db.Query(`
		SELECT id, thumbnail_id, movie_filename, stage, action, source, p_delete_at_decision, created_at
		FROM decisions
		WHERE id > ?
		ORDER BY id ASC
		LIMIT ?`,
		afterID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	decisions := []models.Decision{}
	for rows.Next() {
		var dec models.Decision
		var p sql.NullFloat64
		if err := rows.Scan(&dec.ID, &dec.ThumbnailID, &dec.MovieFilename, &dec.Stage, &dec.Action,
			&dec.Source, &p, &dec.CreatedAt); err != nil {
			return nil, err
		}
		if p.Valid {
			dec.PDeleteAtDecision = &p.Float64
		}
		decisions = append(decisions, dec)
	}
	return decisions, rows.Err()
}

// ReplaceScores replaces all scores and the model status reported by the worker
func (d *DB) ReplaceScores(scores []models.Score, meta map[string]string) error {
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec("DELETE FROM scores"); err != nil {
		return err
	}
	for _, s := range scores {
		if _, err := tx.Exec(`
			INSERT OR REPLACE INTO scores (thumbnail_id, p_delete, p_archive)
			VALUES (?, ?, ?)`,
			s.ThumbnailID, s.PDelete, s.PArchive,
		); err != nil {
			return err
		}
	}
	// The worker reports its full status on every run; keys of an earlier model must not linger
	if _, err := tx.Exec("DELETE FROM ml_meta"); err != nil {
		return err
	}
	for k, v := range meta {
		if _, err := tx.Exec("INSERT OR REPLACE INTO ml_meta (key, value) VALUES (?, ?)", k, v); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// GetScore returns the scores of a thumbnail, or nil if it hasn't been scored
func (d *DB) GetScore(id int64) (*models.Score, error) {
	s := &models.Score{ThumbnailID: id}
	var pArchive sql.NullFloat64
	err := d.db.QueryRow(
		"SELECT p_delete, p_archive FROM scores WHERE thumbnail_id = ?", id,
	).Scan(&s.PDelete, &pArchive)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if pArchive.Valid {
		s.PArchive = &pArchive.Float64
	}
	return s, nil
}

// liveAUCWindow is how many recent scored slideshow decisions the live AUC covers, and
// liveAUCMinPerClass how many deletes and keeps it needs before it is reported
const (
	liveAUCWindow      = 300
	liveAUCMinPerClass = 5
)

// GetMLStatus summarizes decisions, embeddings, scores and the worker's model status
func (d *DB) GetMLStatus() (*models.MLStatus, error) {
	status := &models.MLStatus{Decisions: map[string]int{}, Meta: map[string]string{}}

	rows, err := d.db.Query(`
		SELECT stage, action, COUNT(*)
		FROM decisions
		WHERE action != ?
		GROUP BY stage, action`,
		models.ActionUndo,
	)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var stage, action string
		var count int
		if err := rows.Scan(&stage, &action, &count); err != nil {
			rows.Close()
			return nil, err
		}
		status.Decisions[stage+"/"+action] = count
	}
	rows.Close()

	rows, err = d.db.Query("SELECT key, value FROM ml_meta")
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			rows.Close()
			return nil, err
		}
		status.Meta[k] = v
	}
	rows.Close()

	// Count embeddings of the model the worker last trained with, if it reported one
	embedded := "SELECT COUNT(DISTINCT thumbnail_id) FROM embeddings"
	embeddedArgs := []interface{}{}
	if model := status.Meta["model"]; model != "" {
		embedded += " WHERE model = ?"
		embeddedArgs = append(embeddedArgs, model)
	}
	if err := d.db.QueryRow(embedded, embeddedArgs...).Scan(&status.Embedded); err != nil {
		return nil, err
	}
	if err := d.db.QueryRow("SELECT COUNT(*) FROM scores").Scan(&status.Scored); err != nil {
		return nil, err
	}

	if err := d.liveAUC(status); err != nil {
		return nil, err
	}
	return status, nil
}

// liveAUC measures how well the scores shown at decision time ranked the user's recent
// slideshow decisions: the probability that a deleted movie had a higher delete score than a
// kept (passed or archived) one. Being rank-based, it is unaffected by how the scores were
// calibrated. Decisions that were later undone are left out.
func (d *DB) liveAUC(status *models.MLStatus) error {
	rows, err := d.db.Query(`
		SELECT p_delete_at_decision, action
		FROM decisions dec
		WHERE stage = ? AND action IN (?, ?, ?) AND p_delete_at_decision IS NOT NULL
		AND NOT EXISTS (
			SELECT 1 FROM decisions u
			WHERE u.thumbnail_id = dec.thumbnail_id AND u.stage = dec.stage AND u.action = ? AND u.id > dec.id
		)
		ORDER BY id DESC
		LIMIT ?`,
		models.StageSlideshow, models.ActionDelete, models.ActionPass, models.ActionArchive,
		models.ActionUndo, liveAUCWindow,
	)
	if err != nil {
		return err
	}
	defer rows.Close()

	var deleted, kept []float64
	for rows.Next() {
		var p float64
		var action string
		if err := rows.Scan(&p, &action); err != nil {
			return err
		}
		if action == models.ActionDelete {
			deleted = append(deleted, p)
		} else {
			kept = append(kept, p)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(deleted) < liveAUCMinPerClass || len(kept) < liveAUCMinPerClass {
		return nil
	}

	var wins float64
	for _, pd := range deleted {
		for _, pk := range kept {
			switch {
			case pd > pk:
				wins++
			case pd == pk:
				wins += 0.5
			}
		}
	}
	status.LiveAUC = wins / float64(len(deleted)*len(kept))
	status.LiveAUCDecisions = len(deleted) + len(kept)
	return nil
}

// encodeVector stores a float32 vector as little-endian bytes
func encodeVector(v []float32) []byte {
	buf := make([]byte, 4*len(v))
	for i, f := range v {
		binary.LittleEndian.PutUint32(buf[4*i:], math.Float32bits(f))
	}
	return buf
}

// decodeVector is the inverse of encodeVector
func decodeVector(buf []byte) ([]float32, error) {
	if len(buf)%4 != 0 {
		return nil, fmt.Errorf("vector blob length %d is not a multiple of 4", len(buf))
	}
	v := make([]float32, len(buf)/4)
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(buf[4*i:]))
	}
	return v, nil
}
