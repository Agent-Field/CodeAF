package identity_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/automation"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/knowledge"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/sqliteuri"
	"github.com/Agent-Field/codeaf/internal/store"
)

const startupChat = "v3/projects/project/chat/.cell"

func startupFile(t *testing.T, home, name, text string) {
	t.Helper()
	path := filepath.Join(home, name)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
}

// firstStart uses the actual database owners and history reader, including the
// worker's empty-chat cursor and waiting job rather than an invented SQLite shape.
func firstStart(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	if _, err := identity.EnsureSolo(home); err != nil {
		t.Fatal(err)
	}
	graph, err := store.Open(filepath.Join(home, "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.Close(); err != nil {
		t.Fatal(err)
	}
	autos, err := automation.Open(filepath.Join(home, "v3/automations"))
	if err != nil {
		t.Fatal(err)
	}
	if err := autos.Close(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"teams.json.lock", "v3/automations/clock.lock", "v3/automations/clock.log", "v3/automations/environment.salt", "v3/automations/windows/window", "v3/projects/project/chat/meta.lock", startupChat + "/.furrowpolicy"} {
		startupFile(t, home, name, "")
	}
	startupFile(t, home, "model-catalog-a88839f814a18779.json", "{}")
	startupFile(t, home, "v3/projects/project/chat/meta.json", `{"id":"chat","workspace":"/project"}`)
	startupFile(t, home, startupChat+"/transcript.jsonl", "{\"type\":\"session\",\"id\":\"chat\",\"cwd\":\"/project\"}\n")
	for _, name := range []string{"meta.json", "session.json", "memories.jsonl"} {
		startupFile(t, home, startupChat+"/"+name, "")
	}
	vault := knowledge.Open(home)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	guard.Go("identity/startup-fixture", func() {
		done <- vault.Run(ctx, session.KnowledgeHistory(filepath.Join(home, "v3/projects")), func() string { return "" })
	})
	for {
		snapshot, err := vault.Status()
		if err != nil {
			cancel()
			<-done
			t.Fatal(err)
		}
		if snapshot.Build.Phase == knowledge.BuildWaiting {
			break
		}
		if ctx.Err() != nil {
			t.Fatal("startup knowledge worker did not finish", <-done)
		}
		runtime.Gosched()
	}
	cancel()
	<-done
	return home
}

func startupSQL(t *testing.T, home, path, query string) {
	t.Helper()
	uri := sqliteuri.File(filepath.Join(home, path))
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(query); err != nil {
		t.Fatal(err)
	}
}

// L4-H2, fix round 2: a real first-start shape can join, while every form of
// submitted work, stored knowledge and unknown state retains the refusal.
func TestL4Fix2FirstStartCanJoin(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*testing.T, string)
	}{
		{"only opened", nil},
		{"sent turn", func(t *testing.T, h string) {
			startupFile(t, h, startupChat+"/transcript.jsonl", "{\"type\":\"message\",\"role\":\"user\",\"content\":\"hello\"}\n")
		}},
		{"tracked item", func(t *testing.T, h string) { startupFile(t, h, startupChat+"/state.json", `{"tracked":["result"]}`) }},
		{"chat memory", func(t *testing.T, h string) { startupFile(t, h, startupChat+"/memories.jsonl", `{"text":"remember"}`) }},
		{"automation", func(t *testing.T, h string) {
			autos, err := automation.Open(filepath.Join(h, "v3/automations"))
			if err != nil {
				t.Fatal(err)
			}
			defer autos.Close()
			_, err = autos.Create(automation.Automation{Title: "remind", Schedule: automation.Schedule{At: time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)}, Action: automation.Action{Say: "hello"}, Workspace: "/project"})
			if err != nil {
				t.Fatal(err)
			}
		}},
		{"knowledge page", func(t *testing.T, h string) {
			startupSQL(t, h, "knowledge/knowledge.sqlite", `INSERT INTO pages VALUES ('page','My note','profile','{}')`)
		}},
		{"titled session", func(t *testing.T, h string) {
			startupSQL(t, h, "graph.db", `INSERT INTO sessions(id,title,created_at,last_active_at) VALUES('sent','My work','now','now')`)
		}},
		{"unknown file", func(t *testing.T, h string) { startupFile(t, h, "my-work", "keep") }},
		{"unknown catalog filename", func(t *testing.T, h string) { startupFile(t, h, "model-catalog-my-notes.json", "keep my work") }},
		{"unknown graph table", func(t *testing.T, h string) { startupSQL(t, h, "graph.db", `CREATE TABLE future_work (data TEXT)`) }},
		{"unknown knowledge state", func(t *testing.T, h string) {
			startupSQL(t, h, "knowledge/knowledge.sqlite", `INSERT INTO state VALUES('future-work','{}')`)
		}},
		{"unknown automation table", func(t *testing.T, h string) {
			startupSQL(t, h, "v3/automations/automations.db", `CREATE TABLE future_work (data TEXT)`)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := firstStart(t)
			if ok, err := identity.Joinable(home); err != nil || !ok {
				t.Fatalf("first start cannot join: %v %v", ok, err)
			}
			if tc.change != nil {
				tc.change(t, home)
				if ok, err := identity.Joinable(home); ok {
					t.Fatalf("work was called empty: %v", err)
				}
			}
		})
	}
}

// L4-H2: Narrow startup rows must not block the base memory merge, and an
// unknown graph event must still refuse a move even when its projections are empty.
func TestL4Fix2GraphMemoryRemainsJoinable(t *testing.T) {
	home := firstStart(t)
	graph, err := store.Open(filepath.Join(home, "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = graph.AddMemory(store.Memory{Owner: store.OwnerUser, Text: "My hand-edited memory"})
	if err != nil {
		graph.Close()
		t.Fatal(err)
	}
	if err := graph.Close(); err != nil {
		t.Fatal(err)
	}
	if ok, err := identity.Joinable(home); !ok || err != nil {
		t.Fatalf("memory merge refused: %v %v", ok, err)
	}
	startupSQL(t, home, "graph.db", `INSERT INTO events(ts,node_id,kind,payload) VALUES('now','root','unknown_work','{}')`)
	if ok, err := identity.Joinable(home); ok || err != nil {
		t.Fatalf("unknown work event: %v %v", ok, err)
	}
}
