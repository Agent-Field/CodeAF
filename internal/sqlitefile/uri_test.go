package sqlitefile

import (
	"database/sql"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	_ "modernc.org/sqlite"
)

// R2 pins Unix URI bytes, including quoting and query ordering, to the former
// url.URL expression rather than a second account of its escaping rules.
func TestUnixURIBytes(t *testing.T) {
	for _, path := range []string{"/tmp/graph.db", "/tmp/dir with space é/graph.db", "/tmp/?#%é.db", "relative/graph.db", "/tmp/back\\slash.db", "//server/share/graph.db"} {
		for _, query := range []string{"", "mode=ro&_pragma=busy_timeout(2000)", "z=%23&a=one+two&_pragma=foreign_keys%281%29"} {
			want := (&url.URL{Scheme: "file", Path: path, RawQuery: query}).String()
			for _, goos := range []string{"linux", "darwin"} {
				if got := uri(goos, path, query); got != want {
					t.Fatalf("%s URI(%q, %q) = %q, want %q", goos, path, query, got, want)
				}
			}
		}
	}
}

// R2 keeps UNC names in an empty-authority URI, which the bundled SQLite parser
// accepts, and keeps the caller's query separate from the escaped Windows path.
func TestWindowsURIForms(t *testing.T) {
	for _, tc := range []struct{ path, want string }{
		{`C:\Users\dir with space é\graph.db`, "file:///C:/Users/dir%20with%20space%20%C3%A9/graph.db"},
		{"d:/dir/#?%.db", "file:///d:/dir/%23%3F%25.db"},
		{`\\server\share\dir with space é\graph.db`, "file:////server/share/dir%20with%20space%20%C3%A9/graph.db"},
		{"//server/share/#?%.db", "file:////server/share/%23%3F%25.db"},
	} {
		const query = "mode=ro&_pragma=busy_timeout(2000)&custom=one%20two"
		if got := uri("windows", tc.path, query); got != tc.want+"?"+query {
			t.Fatalf("URI(%q) = %q, want %q", tc.path, got, tc.want+"?"+query)
		}
	}
}

// R2 proves the real driver opens precisely the escaped filename, with a query
// still enforcing read-only access. Windows filenames cannot contain a question
// mark, so that character is exercised by the pure cases above and on Unix here.
func TestURIReachesTheNamedDatabase(t *testing.T) {
	name := "graph #%é.db"
	if runtime.GOOS != "windows" {
		name = "graph ?#%é.db"
	}
	path := filepath.Join(t.TempDir(), name)
	db, err := sql.Open("sqlite", URI(path, ""))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TABLE words (word TEXT); INSERT INTO words VALUES ('héllo')"); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	db, err = sql.Open("sqlite", URI(path, "mode=ro"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var word string
	if err := db.QueryRow("SELECT word FROM words").Scan(&word); err != nil || word != "héllo" {
		t.Fatalf("read = %q, %v", word, err)
	}
	if _, err := db.Exec("INSERT INTO words VALUES ('wrong')"); err == nil {
		t.Fatal("the query lost its read-only mode")
	}
}
