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
	"v3/models.json":   "{}", "v3/lanes.json": "{}", "v3/lanes.json.lock": "", "v3/lanes.log": "{}\n",
	"v3/lanes/deepseek%2Fdeepseek-v4.1-flash.json": "{}",
	"v3/draft-ab-0-12.json":                        `{"version":1,"owner":"o","slots":[{"owner":"o","caret":0}]}`,
	"v3/hosts/ab/host.sock":                        "",
	"v3/hosts/ab/host.log":                         "",
	"v3/standing/tick.lock":                        "",
	"v3/stores/sweep.stamp":                        "",
	"v3/stores/01SESSION/budget.json":              "{}",
	"v3/stores/01SESSION/budget.lock":              "",
	sessionDir + "/meta.json":                      "{}",
	sessionDir + "/presence.json":                  "{}",
	cellDir + "/meta.json":                         "{}",
	cellDir + "/session.json":                      "{}",
	cellDir + "/env/.keep-dir":                     "",
	cellDir + "/memories.jsonl":                    "",
	cellDir + "/transcript.jsonl":                  `{"type":"session","version":1,"id":"01SESSION"}` + "\n",
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
		`CREATE TABLE IF NOT EXISTS facts (kind TEXT, channel TEXT)`,
		`INSERT INTO nodes VALUES ('root')`, `INSERT INTO events VALUES ('boot')`,
		`INSERT INTO facts VALUES ('skill', 'inferred')`, // one per skill installed on the computer, read again at every start
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

func TestQAStartupBookkeepingDoesNotPreventPairing(t *testing.T) {
	for _, name := range []string{"teams.db.lock", "factory-rooms-swept", "v3/usage.jsonl.remote-lock", "app", "services", ".codeaf", "v3/factory"} {
		t.Run(name, func(t *testing.T) {
			home := startedHome(t, func(home string) {
				path := filepath.Join(home, name)
				if strings.HasSuffix(name, "lock") || name == "factory-rooms-swept" {
					put(t, path, "startup bookkeeping")
				} else if err := os.MkdirAll(path, 0o700); err != nil {
					t.Fatal(err)
				}
			})
			if ok, err := Pristine(home); err != nil || !ok {
				t.Fatalf("unused startup entry %s prevented pairing: %v", name, err)
			}
		})
	}
}

func TestAppBuilderStartupDoesNotPreventPairing(t *testing.T) {
	for _, root := range []string{"apps", ".codeaf/apps"} {
		t.Run(root, func(t *testing.T) {
			home := startedHome(t, func(home string) {
				put(t, filepath.Join(home, root, ".lock"), "")
				put(t, filepath.Join(home, root, "_drafts", ".builder.lock"), "")
				put(t, filepath.Join(home, ".codeaf/skills/workaf-apps/SKILL.md"), "---\nname: workaf-apps\ndescription: Make an app\n---\n<!-- workaf-apps v3 -->\n")
			})
			if ok, err := Pristine(home); err != nil || !ok {
				t.Fatalf("builder startup prevented pairing: %v", err)
			}
			put(t, filepath.Join(home, root, "_drafts", "draft", "build.json"), "{}")
			if ok, _ := Pristine(home); ok {
				t.Fatal("pairing accepted a home with app work")
			}
		})
	}
}

func TestQAStartupFoldersStillProtectLocalWork(t *testing.T) {
	cases := []struct {
		name, path, body string
	}{
		{"unknown desktop state", "app/state.json", "person's work or credential"},
		{"archived chat after setup", "app/state.json", `{"setup":{"step":"done"},"archived":{"chat":"2026-01-01T00:00:00Z"}}`},
		{"unknown field in setup", "app/state.json", `{"setup":{"step":"done","key":"keep"}}`},
		{"duplicate setup field", "app/state.json", `{"setup":{"step":"done","step":"done"}}`},
		{"local service", "services/service.json", "person's work"},
		{"local settings file", ".codeaf/settings.json", "person's work"},
		{"custom skill with bundled name", ".codeaf/skills/workaf-canvas/SKILL.md", "---\nname: workaf-canvas\n---\n# My skill\n<!-- workaf-canvas v17 -->\n"},
		{"custom app authoring skill", ".codeaf/skills/workaf-apps/SKILL.md", "---\nname: workaf-apps\n---\n# My skill"},
		{"additional skill file", ".codeaf/skills/workaf-canvas/reference.md", "person's work"},
		{"factory job", "v3/factory/work/job.txt", "person's work"},
		{"credential", "crew-credentials.json", "person's credential"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := startedHome(t, func(home string) { put(t, filepath.Join(home, tc.path), tc.body) })
			if ok, _ := Joinable(home); ok {
				t.Fatalf("pairing would admit local work in %s", tc.path)
			}
		})
	}
}

func TestQACredentialHistoryRequiresOnlyKnownDigests(t *testing.T) {
	for _, body := range []string{
		`{}`,
		`{"aiand":{"print":"0123456789abcdef"}}`,
		`{"aiand":{"print":"0123456789abcdef","since":"2026-11-19T00:00:00Z"}}`,
	} {
		home := startedHome(t, func(home string) { put(t, filepath.Join(home, "crew-credentials.json"), body) })
		if ok, err := Pristine(home); err != nil || !ok {
			t.Fatalf("known digest bookkeeping prevented pairing: %v", err)
		}
	}
	for _, body := range []string{
		`{"aiand":{"key":"FAKE-local-key"}}`,
		`{"aiand":{"print":"FAKE-local-key"}}`,
		`{"aiand":{"print":"FAKE-local-key","print":"0123456789abcdef"}}`,
		`{"aiand":{"print":"0123456789abcdef","unknown":"keep this work"}}`,
		`null`, `{"aiand":null}`, `person's work or credential`,
	} {
		home := startedHome(t, func(home string) { put(t, filepath.Join(home, "crew-credentials.json"), body) })
		if ok, _ := Pristine(home); ok {
			t.Fatal("credential history admitted unknown or credential-bearing content")
		}
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
		"a stored blob":            func(h string) { put(t, filepath.Join(h, "cas/ab/blob"), "x") },
		"a synced cell":            func(h string) { put(t, filepath.Join(h, "v3/stores/01SESSION/store-v1/packs/p"), "x") },
		"a task history":           func(h string) { put(t, filepath.Join(h, "v3/projects/-work-notes/tasks.jsonl"), "{}") },
		"a secret":                 func(h string) { put(t, filepath.Join(h, "provider-keys.enc"), "x") },
		"an unknown file":          func(h string) { put(t, filepath.Join(h, "v3/notes.txt"), "x") },
		"a stored message":         func(h string) { makeGraph(t, h, []string{`INSERT INTO messages VALUES ('m')`}) },
		"a second node":            func(h string) { makeGraph(t, h, []string{`INSERT INTO nodes VALUES ('task')`}) },
		"a remembered fact":        func(h string) { makeGraph(t, h, []string{`INSERT INTO facts VALUES ('preference', 'stated')`}) },
		"a stray byte for a graph": func(h string) { put(t, filepath.Join(h, "graph.db"), "x") },
		"a broken graph":           func(h string) { put(t, filepath.Join(h, "graph.db"), "not a database") },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			if ok, _ := Pristine(startedHome(t, change)); ok {
				t.Fatalf("a home holding %s is used", name)
			}
		})
	}
}

func TestQAStartupServiceStateProtectsUnknownAndRealWork(t *testing.T) {
	for _, body := range []string{
		`{"version":1,"seq":1,"services":null}`,
		`{"version":1,"seq":0,"services":null,"unknown":true}`,
		`{"version":1,"seq":0,"seq":0,"services":null}`,
		`{"version":1,"seq":0,"services":null,"consent":{"future-work":{}}}`,
	} {
		home := startedHome(t, func(home string) { put(t, filepath.Join(home, "services", "services.json"), body) })
		if ok, _ := Joinable(home); ok {
			t.Fatalf("pairing admitted used or unknown service state: %s", body)
		}
	}
}
