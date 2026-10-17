package sqliteuri

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestFileOpensTheNamedDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory #1 & 100%.db")
	u := File(path)
	q := u.Query()
	q.Add("_pragma", "foreign_keys(1)")
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		t.Fatal(err)
	}
	var foreignKeys int
	if err := db.QueryRow("PRAGMA foreign_keys").Scan(&foreignKeys); err != nil || foreignKeys != 1 {
		db.Close()
		t.Fatalf("connection pragma was lost: foreign_keys=%d, err=%v", foreignKeys, err)
	}
	if _, err := db.Exec("CREATE TABLE probe(value TEXT); INSERT INTO probe VALUES ('persisted')"); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	q.Set("mode", "ro")
	u.RawQuery = q.Encode()
	db, err = sql.Open("sqlite", u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var value string
	if err := db.QueryRow("SELECT value FROM probe").Scan(&value); err != nil || value != "persisted" {
		t.Fatalf("wrong database: value=%q, err=%v", value, err)
	}
	if _, err := db.Exec("INSERT INTO probe VALUES ('unexpected')"); err == nil {
		t.Fatal("mode=ro opened a writable database")
	}
}
