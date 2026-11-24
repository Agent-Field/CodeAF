package store

import (
	"database/sql"
	"path/filepath"
	"testing"
)

// P2-8
// Opening an older ledger adds the asker column once and keeps the old charges intact.
func TestP2_8_SpendAskerMigrationIsIdempotent(t *testing.T) {
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(st.Root(), WorkstreamsDB)
	old, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = old.Exec(`CREATE TABLE spend (
		seq INTEGER PRIMARY KEY AUTOINCREMENT, ws TEXT NOT NULL, at TEXT NOT NULL, by TEXT NOT NULL,
		usd REAL NOT NULL, job INTEGER NOT NULL DEFAULT 0, chat TEXT NOT NULL DEFAULT '', key TEXT NOT NULL DEFAULT ''
	);
	INSERT INTO spend (ws, at, by, usd, chat, key) VALUES ('space', '2026-11-24', 'owner', 1, 'hosted:chat', 'old')`)
	closeErr := old.Close()
	if err != nil || closeErr != nil {
		t.Fatalf("create P1 ledger: %v, close: %v", err, closeErr)
	}
	for _, key := range []string{"first", "second"} {
		if _, err := st.AddSpend("space", Spend{At: "2026-11-24", By: "owner", Asker: "guest", USD: 1, Chat: "hosted:chat", Key: key}); err != nil {
			t.Fatal(err)
		}
		// Drop the process cache so the next write opens and migrates the same ledger again.
		workstreamDBs.Lock()
		err := workstreamDBs.open[path].Close()
		delete(workstreamDBs.open, path)
		workstreamDBs.Unlock()
		if err != nil {
			t.Fatal(err)
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, want := range []struct{ key, asker string }{{"old", ""}, {"first", "guest"}, {"second", "guest"}} {
		var by, asker string
		if err := db.QueryRow(`SELECT by, asker FROM spend WHERE key = ?`, want.key).Scan(&by, &asker); err != nil || by != "owner" || asker != want.asker {
			t.Fatalf("spend %s by %q, asker %q, error %v", want.key, by, asker, err)
		}
	}
}
