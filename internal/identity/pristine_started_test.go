package identity

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite" // the memory graph the started home holds
)

const (
	sessionDir = "v3/projects/-work-notes/01SESSION"
	cellDir    = sessionDir + "/.cell"
)

// startedFiles is the layout a fresh install leaves after the person ran
// codeaf, looked at the start screen and quit (read off a real run): files that
// are made again on demand, and one chat that has a header line and no turn.
var startedFiles = map[string]string{
	File: "{}", DeviceFile: "{}", "identity-solo": "", "config.json": "{}",
	"notices.json": "{}", "model-catalog.json": "{}", "chat.log": "", "credits.json": "{}",
	"bin/furrow": "x", "cas/.keep-dir": "", "logs/run.log": "x",
	"pool/doc.json": "{}", "pool/outbox.jsonl": "", "pool/meta.json": "{}",
	"telemetry/install_id": "x", "telemetry/first_run": "x",
	"graph.db-wal": "x", "graph.db-shm": "x",
	"v3/history.jsonl": `{"text":"/quit","cwd":"/w","ts":"2026-01-01T00:00:00Z"}` + "\n",
	"v3/models.json":   "{}", "v3/lanes.json": "{}", "v3/lanes.json.lock": "",
	"v3/draft-ab-0-12.json":           `{"version":1,"owner":"o","slots":[{"owner":"o","caret":0}]}`,
	"v3/hosts/ab/host.sock":           "",
	"v3/hosts/ab/host.log":            "",
	"v3/standing/tick.lock":           "",
	"v3/stores/sweep.stamp":           "",
	"v3/stores/01SESSION/budget.json": "{}",
	"v3/stores/01SESSION/budget.lock": "",
	sessionDir + "/meta.json":         "{}",
	sessionDir + "/presence.json":     "{}",
	cellDir + "/meta.json":            "{}",
	cellDir + "/session.json":         "{}",
	cellDir + "/env/.keep-dir":        "",
	cellDir + "/memories.jsonl":       "",
	cellDir + "/transcript.jsonl":     `{"type":"session","version":1,"id":"01SESSION"}` + "\n",
}

// startedHome lays the started files out, then applies the change a test makes.
func startedHome(t *testing.T, change func(home string)) string {
	t.Helper()
	home := t.TempDir()
	for name, body := range startedFiles {
		path := filepath.Join(home, name)
		if strings.HasSuffix(name, ".keep-dir") {
			path = filepath.Dir(path)
			if err := os.MkdirAll(path, 0o700); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	makeGraph(t, home, nil)
	if change != nil {
		change(home)
	}
	return home
}

// makeGraph writes the memory graph the way a start leaves it: the root node
// and one event, plus whatever rows a test adds.
func makeGraph(t *testing.T, home string, extra []string) {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(home, "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, q := range append([]string{
		`CREATE TABLE IF NOT EXISTS nodes (id TEXT)`, `CREATE TABLE IF NOT EXISTS events (id TEXT)`,
		`CREATE TABLE IF NOT EXISTS messages (id TEXT)`, `CREATE TABLE IF NOT EXISTS memories (id TEXT)`,
		`INSERT INTO nodes VALUES ('root')`, `INSERT INTO events VALUES ('boot')`,
	}, extra...) {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
}

func put(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// A computer that was only started holds nothing to lose.
func TestPristineAfterOnlyStarting(t *testing.T) {
	home := startedHome(t, nil)
	ok, err := Pristine(home)
	if err != nil || !ok {
		t.Fatalf("a home that was only started is pristine, got %v %v", ok, err)
	}
}

// Anything the person made keeps the home from being pristine, however little
// else changed. When in doubt the answer stays no.
func TestPristineRefusesEveryKindOfUse(t *testing.T) {
	cases := map[string]func(home string){
		"a chat turn": func(h string) {
			put(t, filepath.Join(h, cellDir, "transcript.jsonl"),
				startedFiles[cellDir+"/transcript.jsonl"]+`{"type":"message","role":"user"}`+"\n")
		},
		"a memory": func(h string) { put(t, filepath.Join(h, cellDir, "memories.jsonl"), `{"m":1}`) },
		"a typed draft": func(h string) {
			put(t, filepath.Join(h, "v3/draft-ab-0-12.json"), `{"version":1,"slots":[{"text":"hi"}]}`)
		},
		"a typed request": func(h string) {
			put(t, filepath.Join(h, "v3/history.jsonl"), `{"text":"/quit"}`+"\n"+`{"text":"fix the bug"}`+"\n")
		},
		"a stored blob":    func(h string) { put(t, filepath.Join(h, "cas/ab/blob"), "x") },
		"a synced cell":    func(h string) { put(t, filepath.Join(h, "v3/stores/01SESSION/store-v1/packs/p"), "x") },
		"a task history":   func(h string) { put(t, filepath.Join(h, "v3/projects/-work-notes/tasks.jsonl"), "{}") },
		"a secret":         func(h string) { put(t, filepath.Join(h, "provider-keys.enc"), "x") },
		"an unknown file":  func(h string) { put(t, filepath.Join(h, "v3/notes.txt"), "x") },
		"a stored message": func(h string) { makeGraph(t, h, []string{`INSERT INTO messages VALUES ('m')`}) },
		"a second node":    func(h string) { makeGraph(t, h, []string{`INSERT INTO nodes VALUES ('task')`}) },
		"a broken graph":   func(h string) { put(t, filepath.Join(h, "graph.db"), "not a database") },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			if ok, _ := Pristine(startedHome(t, change)); ok {
				t.Fatalf("a home holding %s is used", name)
			}
		})
	}
}
