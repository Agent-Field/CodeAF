package sqliteuri

import (
	"database/sql"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	_ "modernc.org/sqlite"
)

func TestFilePreservesPOSIXURIs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX path spelling is a contract on POSIX hosts")
	}
	for _, path := range []string{
		"/tmp/memory.db", "/tmp/with spaces.db", "/tmp/hash#1.db",
		"/tmp/a&b.db", "/tmp/100%.db", "/tmp/literal%20.db",
		"/tmp/query?mode=ro.db", "/tmp/記憶-é.db", "/tmp/back\\slash.db",
		"//server/share/db", "/tmp/../tmp/db",
	} {
		t.Run(path, func(t *testing.T) {
			before := url.URL{Scheme: "file", Path: path}
			after := File(path)
			q := url.Values{"mode": {"rw"}, "_pragma": {"busy_timeout(5000)", "foreign_keys(1)"}, "_txlock": {"immediate"}}
			before.RawQuery, after.RawQuery = q.Encode(), q.Encode()
			if after.String() != before.String() {
				t.Fatalf("URI changed: got %q, dev produced %q", after.String(), before.String())
			}
		})
	}
}

func TestFileWindowsPaths(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("filepath.ToSlash follows the host filesystem")
	}
	for _, test := range []struct{ path, want string }{
		{`C:\x\y.db`, "file:///C:/x/y.db"},
		{`C:/x/y.db`, "file:///C:/x/y.db"},
		{`C:\x\with space #&%.db`, "file:///C:/x/with%20space%20%23&%25.db"},
		{`C:\記憶\é.db`, "file:///C:/%E8%A8%98%E6%86%B6/%C3%A9.db"},
		{`\\server\share\db`, "file:////server/share/db"},
		{`\\server\share\with space #&%.db`, "file:////server/share/with%20space%20%23&%25.db"},
	} {
		t.Run(test.path, func(t *testing.T) {
			u := File(test.path)
			if u.String() != test.want || u.Host != "" {
				t.Fatalf("URI = %q, host = %q; want %q with no authority", u.String(), u.Host, test.want)
			}
			q := url.Values{"mode": {"ro"}, "_pragma": {"busy_timeout(5000)", "foreign_keys(1)"}, "_txlock": {"immediate"}}
			u.RawQuery = q.Encode()
			if u.String() != test.want+"?"+q.Encode() {
				t.Fatalf("caller query changed: %q", u.String())
			}
		})
	}
}

func TestFileOpensTheNamedDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory #1 & 100%.db")
	u := File(path)
	q := u.Query()
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "busy_timeout(5000)")
	q.Set("_txlock", "immediate")
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
	var busyTimeout int
	if err := db.QueryRow("PRAGMA busy_timeout").Scan(&busyTimeout); err != nil || busyTimeout != 5000 {
		db.Close()
		t.Fatalf("connection pragma was lost: busy_timeout=%d, err=%v", busyTimeout, err)
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

func TestFileReadOnlyDoesNotCreateAMissingDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing #&%.db")
	u := File(path)
	u.RawQuery = "mode=ro"
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Ping(); err == nil {
		t.Fatal("read-only open accepted a missing database")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("read-only open created the database: %v", err)
	}
}
