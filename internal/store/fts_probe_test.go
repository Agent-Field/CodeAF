package store

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func TestFTS5Available(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE VIRTUAL TABLE f USING fts5(body)`); err != nil {
		t.Fatalf("FTS5 unavailable: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO f (body) VALUES ('seedance clips cap at ten seconds')`); err != nil {
		t.Fatal(err)
	}
	var body string
	var rank float64
	if err := db.QueryRow(`SELECT body, bm25(f) FROM f WHERE f MATCH 'seedance'`).Scan(&body, &rank); err != nil {
		t.Fatalf("bm25 match failed: %v", err)
	}
	t.Logf("FTS5 OK: %q rank=%.3f", body, rank)
}
