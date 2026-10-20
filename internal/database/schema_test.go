package database

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestTagsTableIsDropped(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")

	// A database from before v0.9.0, with tags
	old, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := old.Exec(`
		CREATE TABLE thumbnail_tags (thumbnail_id INTEGER NOT NULL, tag TEXT NOT NULL, PRIMARY KEY (thumbnail_id, tag));
		CREATE INDEX idx_thumbnail_tags_tag ON thumbnail_tags(tag);
		INSERT INTO thumbnail_tags VALUES (1, 'ai-reviewed');
	`); err != nil {
		t.Fatal(err)
	}
	old.Close()

	db, err := New(path)
	if err != nil {
		t.Fatalf("opening a pre-v0.9.0 database: %v", err)
	}
	defer db.Close()

	var n int
	if err := db.db.QueryRow(
		"SELECT COUNT(*) FROM sqlite_master WHERE name IN ('thumbnail_tags', 'idx_thumbnail_tags_tag')",
	).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("%d tag objects left in the schema; want 0", n)
	}
}
