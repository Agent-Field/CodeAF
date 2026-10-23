package main

import (
	"database/sql"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/Agent-Field/codeaf/internal/sqliteuri"
)

func TestStoreReadersUseLiteralDatabasePath(t *testing.T) {
	name := "graph%23 #&.db"
	if runtime.GOOS != "windows" {
		name = "graph%23 #&?.db"
	}
	path := filepath.Join(t.TempDir(), name)
	uri := sqliteuri.File(path)
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE usage(cost REAL,prompt_tokens INT,completion_tokens INT);
 INSERT INTO usage VALUES (0.25,11,7);
 CREATE TABLE transcript(kind TEXT,tool TEXT,body TEXT);
 INSERT INTO transcript VALUES('tool_call','bash','plandb show root');`)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var got readings
	if err = applyStoreReadings(&got, path); err != nil {
		t.Fatal(err)
	}
	if got.CostUSD != .25 || got.InputTokens != 11 || got.OutputTokens != 7 || got.Calls != 1 {
		t.Fatalf("usage read wrong store: %+v", got)
	}
	bash, _, plans := storeBeltFingerprints(path)
	if bash != 1 || plans != 1 {
		t.Fatalf("read wrong store: bash=%d plan=%d", bash, plans)
	}
}
