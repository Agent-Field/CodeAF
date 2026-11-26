package identity

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/catalog"
	"github.com/Agent-Field/codeaf/internal/service"
	"github.com/Agent-Field/codeaf/internal/skills"
	"github.com/Agent-Field/codeaf/internal/sqliteuri"

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
	// Exact startup names outrank directory globs; Go map order must never
	// decide whether an existing file is mistaken for a directory (C30.4).
	if exact, ok := r[name]; ok {
		return exact
	}
	for glob, j := range r {
		if ok, _ := filepath.Match(glob, name); ok {
			return j
		}
	}
	return unknown
}

// never is for what a start makes again on demand: keys, caches, locks, logs.
func never(string) (bool, error) { return false, nil }

func emptyFile(path string) (bool, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return true, err
	}
	return !info.Mode().IsRegular() || info.Size() != 0, nil
}

// appStartupState recognizes the part of the app's saved state written by
// onboarding and initial unread tracking. Custom settings, chat references,
// unknown fields and duplicate fields still count as use.
func appStartupState(path string) (bool, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 1024 {
		return true, err
	}
	raw, err := skills.ReadWholeRegular(path, 1024)
	if err != nil {
		return true, err
	}
	var state struct {
		Setup *struct {
			Step         json.RawMessage `json:"step"`
			Provider     string          `json:"provider,omitempty"`
			DefaultModel string          `json:"default_model,omitempty"`
			DayCapUSD    *float64        `json:"day_cap_usd,omitempty"`
			Start        string          `json:"start,omitempty"`
		} `json:"setup,omitempty"`
		SetupSteps int       `json:"setup_steps,omitempty"`
		UnreadFrom time.Time `json:"unread_from,omitzero"`
	}
	if json.Unmarshal(raw, &state) != nil || state.SetupSteps < 0 || state.SetupSteps > 4 {
		return true, nil
	}
	if setup := state.Setup; setup != nil {
		switch string(setup.Step) {
		case `"done"`, "1", "2", "3", "4": // Older builds kept four setup steps.
		default:
			return true, nil
		}
		if setup.Start != "" && setup.Start != "agents" {
			return true, nil
		}
	}
	return noncanonicalJSON(raw, state)
}

// bundledSkill recognizes a managed skill by its own name and version marker
// used by WorkAF. A custom skill, or extra files in this skill's folder, still
// count as use. Ordinary pairing leaves the managed skill in place.
func bundledSkill(name string) judge {
	return func(path string) (bool, error) {
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() {
			return true, err
		}
		raw, err := skills.ReadRegularHead(path, 4096)
		if err != nil {
			return true, err
		}
		text := strings.TrimPrefix(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\ufeff")
		front, body, found := strings.Cut(text, "\n---\n")
		if !found || !strings.HasPrefix(front, "---\nname: "+name+"\n") {
			return true, nil
		}
		line, _, _ := strings.Cut(body, "\n")
		version, prefix := strings.CutPrefix(strings.TrimSpace(line), "<!-- "+name+" v")
		version, suffix := strings.CutSuffix(version, " -->")
		_, err = strconv.ParseUint(version, 10, 32)
		return !prefix || !suffix || err != nil, nil
	}
}

func serviceStartup(path string) (bool, error) {
	known, err := service.IsStartupStore(path)
	return !known, err
}

// catalogCache delegates cache naming to its owner instead of treating every
// model-catalog-prefixed file as disposable startup state.
func catalogCache(path string) (bool, error) {
	return !catalog.IsCacheName(filepath.Base(path)), nil
}

// pickerCache uses the same filename boundary as the window that writes it.
func pickerCache(path string) (bool, error) {
	return !catalog.IsPickerCacheName(filepath.Base(path)), nil
}

// unknown is the judge of a name nobody declared, and it is always use.
func unknown(string) (bool, error) { return true, nil }

// credentialDigestHistory accepts only the canonical metadata written by
// config.writeCrewCredentials: provider slugs, sixteen hex digits of a hash,
// and an optional change time. Unknown content, including key fields and
// duplicate JSON fields, stays protected as local work.
func credentialDigestHistory(path string) (bool, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return true, err
	}
	var history map[string]struct {
		Print string    `json:"print"`
		Since time.Time `json:"since,omitzero"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&history) != nil || history == nil || len(history) > 64 || decoder.Decode(new(json.RawMessage)) != io.EOF {
		return true, nil
	}
	for provider, seen := range history {
		if provider == "" || len(provider) > 64 || len(seen.Print) != 16 {
			return true, nil
		}
		for _, char := range provider {
			if !(char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || char == '-') {
				return true, nil
			}
		}
		if _, err := hex.DecodeString(seen.Print); err != nil {
			return true, nil
		}
	}
	return noncanonicalJSON(raw, history)
}

func noncanonicalJSON(raw []byte, known any) (bool, error) {
	canonical, err := json.Marshal(known)
	if err != nil {
		return true, err
	}
	var compact bytes.Buffer
	if json.Compact(&compact, raw) != nil {
		return true, nil
	}
	return !bytes.Equal(canonical, compact.Bytes()), nil
}

// startupInventory admits the platform and locations recorded before any call.
// Observed tools, notes and all other fields still count as work.
func startupInventory(path string) (bool, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return true, err
	}
	var inv struct {
		V         uint16
		Platform  struct{ OS, Arch string }
		Workspace string
		ChatDir   string `json:"chat_dir"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&inv) != nil || inv.V != 1 {
		return true, nil
	}
	return decoder.Decode(new(json.RawMessage)) != io.EOF, nil
}

// unopenedRepository keeps even deleted or staged project work from being
// mistaken for the empty repository made when an owned chat first opens.
//
// ANY NAME GIT PRINTS IS WORK. The names are read NUL-terminated and never
// trimmed: a file named only with spaces, staged and then deleted, printed a
// blank line that trimming discarded, and the repository passed as untouched.
// For the startup repository both listings print nothing at all.
func unopenedRepository(path string) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, args := range [][]string{{"log", "--all", "--format=", "--name-only", "-z"}, {"ls-files", "--cached", "-z"}} {
		cmd := exec.CommandContext(ctx, "git", append([]string{"--no-optional-locks"}, args...)...) //codeaf:plumbing An empty owned repository contains no saved or staged files.
		cmd.Dir = filepath.Dir(path)
		raw, err := cmd.Output()
		if err != nil || len(raw) > 0 {
			return true, err
		}
	}
	return false, nil
}

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
	if err != nil {
		return true, err
	}
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

// bareGraph is for the memory graph. A start seeds its root node and its
// inferred skill facts and logs events about them; every other table (sessions,
// messages, memories, charters, ...), node and fact is work the person did. It
// is opened read-only so judging a home never changes it.
func bareGraph(path string) (bool, error) {
	if sound, err := soundDatabase(path); !sound || err != nil {
		return true, err
	}
	// K3: this is a filesystem path, including a Windows drive or literal %/#.
	uri, err := sqliteuri.FromPath(path)
	if err != nil {
		return true, err
	}
	uri.RawQuery = "mode=ro"
	db, err := sql.Open("sqlite", uri.String())
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

// sqliteMagic opens every SQLite file. The driver reads a file shorter than its
// header as an empty database, which would call a stray byte "nothing", so the
// header is checked first and a file that is not one is use.
const sqliteMagic = "SQLite format 3\x00"

func soundDatabase(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	head := make([]byte, len(sqliteMagic))
	n, err := io.ReadFull(f, head)
	if n == 0 {
		return true, nil // a file with no bytes is an empty database
	}
	return err == nil && string(head) == sqliteMagic, nil
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

// workRows names, per table, the rows that are the person's: what is left once
// what a start writes itself is taken out. A start seeds the root node and one
// inferred skill fact for each skill installed on the computer (read from the
// disk again at every start). A table not named here is all work.
var workRows = map[string]string{
	"nodes": "id <> '" + graphRoot + "'",
	"facts": "NOT (kind = 'skill' AND channel = 'inferred')",
}

// holdsWork says whether a table has a row that is the person's work.
func holdsWork(db *sql.DB, table string) (bool, error) {
	query := `SELECT count(*) FROM "` + table + `"`
	if only, ok := workRows[table]; ok {
		query += " WHERE " + only
	}
	var n int
	err := db.QueryRow(query).Scan(&n)
	return n > 0, err
}
