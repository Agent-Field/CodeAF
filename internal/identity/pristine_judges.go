package identity

import (
	"bufio"
	"bytes"
	"database/sql"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite" // reads the memory graph, which is a SQLite file
)

// A judge says whether one entry of a home, a file or a folder, holds anything
// the person made. A judge that cannot tell answers with its error, and the
// caller treats that as use.
type judge func(path string) (used bool, err error)

// rules names the judge of each entry of a folder, by glob.
type rules map[string]judge

// within judges a folder by its entries: each must match a glob in the rules and
// be no use. An entry that no glob names is use, so an unknown file stays safe.
func within(r rules) judge {
	return func(path string) (bool, error) {
		entries, err := os.ReadDir(path)
		if err != nil {
			return true, err
		}
		for _, e := range entries {
			if used, err := r.of(e.Name())(filepath.Join(path, e.Name())); used || err != nil {
				return true, err
			}
		}
		return false, nil
	}
}

// of is the judge that owns one name, or the judge of an unknown name.
func (r rules) of(name string) judge {
	for glob, j := range r {
		if ok, _ := filepath.Match(glob, name); ok {
			return j
		}
	}
	return unknown
}

// never is for what a start makes again on demand: keys, caches, locks, logs.
func never(string) (bool, error) { return false, nil }

// unknown is the judge of a name nobody declared, and it is always use.
func unknown(string) (bool, error) { return true, nil }

// empty is for what holds the person's words and bytes when it holds any: a
// file with no bytes, or a folder of such files.
func empty(path string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return err != nil || info.Size() > 0, err
	}
	return emptyTree(path)
}

func emptyTree(dir string) (bool, error) {
	entries, err := os.ReadDir(dir)
	for _, e := range entries {
		if used, err := empty(filepath.Join(dir, e.Name())); used || err != nil {
			return true, err
		}
	}
	return err != nil, err
}

// noTurns is for a chat's transcript. A start writes the header line that
// names the session; any other line is something said in the chat.
func noTurns(path string) (bool, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return true, err
	}
	sc := bufio.NewScanner(bytes.NewReader(raw))
	sc.Buffer(nil, len(raw)+1)
	for sc.Scan() {
		if line := bytes.TrimSpace(sc.Bytes()); len(line) > 0 && !isHeader(line) {
			return true, nil
		}
	}
	return false, sc.Err()
}

func isHeader(line []byte) bool {
	var row struct{ Type string }
	return json.Unmarshal(line, &row) == nil && row.Type == "session"
}

// untypedDraft is for a composer's saved record. It names its owner and keeps a
// slot for each recipient, and a slot is only the person's once it holds words,
// a paste, an attachment or a sent line; an emptied slot is a tombstone.
func untypedDraft(path string) (bool, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return true, err
	}
	var rec struct {
		Slots []struct {
			Text                 string
			Pastes, Chips, Sends []json.RawMessage
		}
	}
	if err := json.Unmarshal(raw, &rec); err != nil {
		return true, nil
	}
	for _, slot := range rec.Slots {
		if slot.Text != "" || len(slot.Pastes)+len(slot.Chips)+len(slot.Sends) > 0 {
			return true, nil
		}
	}
	return false, nil
}

// onlyCommands is for the recall history. A line starting with a slash is a
// command said to codeaf (such as the /quit of a person leaving), not work; any
// other line is a request the person made.
func onlyCommands(path string) (bool, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return true, err
	}
	sc := bufio.NewScanner(bytes.NewReader(raw))
	sc.Buffer(nil, len(raw)+1)
	for sc.Scan() {
		var entry struct{ Text string }
		if line := bytes.TrimSpace(sc.Bytes()); len(line) > 0 &&
			(json.Unmarshal(line, &entry) != nil || !strings.HasPrefix(entry.Text, "/")) {
			return true, nil
		}
	}
	return false, sc.Err()
}

// graphRoot is the one node a start puts in the memory graph.
const graphRoot = "root"

// bareGraph is for the memory graph. A start seeds its root node and logs events
// about it; every other table (sessions, messages, memories, charters, ...) and
// every other node is work the person did. It is opened read-only so judging a
// home never changes it.
func bareGraph(path string) (bool, error) {
	db, err := sql.Open("sqlite", (&url.URL{Scheme: "file", Opaque: filepath.ToSlash(path), RawQuery: "mode=ro"}).String())
	if err != nil {
		return true, err
	}
	defer db.Close()
	tables, err := graphTables(db)
	if err != nil {
		return true, err
	}
	for _, table := range tables {
		if used, err := holdsWork(db, table); used || err != nil {
			return true, err
		}
	}
	return false, nil
}

// graphTables lists the tables that hold rows of their own. Full-text indexes
// are skipped because they only echo the tables they index, and the event log
// because it is written by a start and says nothing about use.
func graphTables(db *sql.DB) ([]string, error) {
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type = 'table'
		AND name NOT LIKE '%\_fts%' ESCAPE '\' AND name NOT LIKE 'sqlite\_%' ESCAPE '\' AND name <> 'events'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

// holdsWork says whether a table has a row beyond the root node.
func holdsWork(db *sql.DB, table string) (bool, error) {
	var n int
	query := `SELECT count(*) FROM "` + table + `"`
	if table == "nodes" {
		query += ` WHERE id <> '` + graphRoot + `'`
	}
	err := db.QueryRow(query).Scan(&n)
	return n > 0, err
}
