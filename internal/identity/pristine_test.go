package identity

import (
	"database/sql"
	"github.com/Agent-Field/codeaf/internal/sqliteuri"
	"os"
	"path/filepath"
	"testing"
)

// TestK3AFileWhereTheChatDirectoryBelongsIsUse proves K3: a non-directory
// named v3 is never disposable first-start state, including when Windows
// reports its listing failure as path-not-found.
func TestK3AFileWhereTheChatDirectoryBelongsIsUse(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "v3"), []byte("keep my bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	if fresh, err := Pristine(home); fresh {
		t.Fatalf("K3 existing bytes were called pristine: %v", err)
	}
}

// TestK3BareGraphUsesItsLiteralNativePath proves K3: a startup-only graph is
// still unused when its Windows profile or Unix home has URI metacharacters.
// Calling the pairing preflight must not mistake a URI parsing error for work.
func TestK3BareGraphUsesItsLiteralNativePath(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home %20 #")
	if err := os.MkdirAll(home, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, "graph.db")
	uri, err := sqliteuri.FromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE nodes(id TEXT);INSERT INTO nodes VALUES ('root');CREATE TABLE facts(kind TEXT,channel TEXT)`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if fresh, err := Pristine(home); !fresh || err != nil {
		t.Fatalf("K3 untouched graph called used: fresh=%v err=%v", fresh, err)
	}
}
