package store

import (
	"database/sql"
	"path/filepath"
	"testing"
)

// A page_versions table made before based_on existed gains the column once,
// keeping its rows, and a second open changes nothing.
func TestPageVersionsGainBasedOn(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "old.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE page_versions (ws TEXT NOT NULL, page TEXT NOT NULL, version INTEGER NOT NULL,
		at TEXT NOT NULL, by TEXT NOT NULL, title TEXT NOT NULL, reason TEXT NOT NULL,
		restored_from INTEGER NOT NULL DEFAULT 0, body_md TEXT NOT NULL, state BLOB, PRIMARY KEY (ws, page, version));
		INSERT INTO page_versions VALUES ('ws','pg',1,'at','abir','Plan','idle',0,'# Plan',NULL);`); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := addPageVersionsBasedOn(db); err != nil {
			t.Fatal(err)
		}
	}
	var basedOn int
	if err := db.QueryRow(`SELECT based_on FROM page_versions WHERE version = 1`).Scan(&basedOn); err != nil || basedOn != 0 {
		t.Fatalf("based_on = %d, %v", basedOn, err)
	}
}
