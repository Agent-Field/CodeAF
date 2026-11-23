package plandb

import (
	"database/sql"
	"path/filepath"
	"testing"
)

// A LEDGER FROM BEFORE ANSWERED CHARGES EXISTED READS AS NOT ANSWERED. Its rows
// named the model a seat was built on, and the migration may not promote one.
func TestOldSpendRowsAreNotAnsweredAfterTheMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plan.db")
	store, err := Open(path, "p", "root", "Fix", "Fix it")
	if err != nil {
		t.Fatal(err)
	}
	store.Close()
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`ALTER TABLE spend DROP COLUMN answered`,
		`INSERT INTO spend (task_id, model, role, usd, in_tokens, out_tokens, at) VALUES ('root', 'm/built', 'work', 0.02, 1, 1, '2026-10-01T00:00:00Z')`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
	db.Close()

	store, err = Open(path, "p", "root", "Fix", "Fix it")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.AddAnsweredSpend("root", "m/answered", RoleWork, 0.01, 1, 1); err != nil {
		t.Fatal(err)
	}
	if err := store.AddAnsweredSpend("root", " ", RoleWork, 0.01, 1, 1); err == nil {
		t.Fatal("an answered charge with no model was written")
	}
	if err := store.AddSpend("root", "m/configured", RoleWork, 0.01, 1, 1); err != nil {
		t.Fatal(err)
	}
	read, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer read.Close()
	rows, err := read.Query(`SELECT model, answered FROM spend ORDER BY model`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	got := map[string]int{}
	for rows.Next() {
		var model string
		var answered int
		if err := rows.Scan(&model, &answered); err != nil {
			t.Fatal(err)
		}
		got[model] = answered
	}
	if len(got) != 3 || got["m/built"] != 0 || got["m/configured"] != 0 || got["m/answered"] != 1 {
		t.Fatalf("answered marks %v", got)
	}
}
