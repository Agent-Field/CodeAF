package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// These are the persistence destinations of a settled opener: task recovery,
// metadata summary and cell truth. The production opener test also exercises
// the approval/effort setters that reach SaveMeta.
func TestSessionPersistenceWritersPreserveUnchangedFiles(t *testing.T) {
	for _, writer := range []string{"checkpoint", "metadata", "cell metadata"} {
		t.Run(writer, func(t *testing.T) {
			dir := t.TempDir()
			place := Place{Dir: dir, Workspace: dir}
			if writer == "cell metadata" {
				if err := os.Mkdir(filepath.Join(dir, ".cell"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			meta := Meta{ID: "saved", Workspace: dir, Created: time.Unix(100, 0), Title: "high", Effort: "high", Approval: PostureAsk}
			document := taskDocument{Type: taskDocumentType, Version: taskFileVersion, Seq: 1, Nodes: []taskRecord{{ID: 1, Title: "high", Brief: "finished", Acceptance: "finished", State: TaskDone, Noted: true}}}
			paths := []string{place.MetaPath()}
			write := func() error { return SaveMeta(dir, meta) }
			if writer == "checkpoint" {
				paths = []string{place.Tasks()}
				store := newTaskStore(place.Tasks())
				write = func() error { return store.writeLocked(document) }
			}
			if writer == "cell metadata" {
				paths = append(paths, filepath.Join(dir, ".cell", "session.json"))
			}
			if err := write(); err != nil {
				t.Fatal(err)
			}
			// A fixed old timestamp makes even an in-place rewrite detectable without
			// sleeps or relying on the filesystem timestamp resolution.
			old := time.Unix(200, 0)
			before := map[string]os.FileInfo{}
			for _, path := range paths {
				if err := os.Chtimes(path, old, old); err != nil {
					t.Fatal(err)
				}
				info, err := os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}
				before[path] = info
			}
			for range 2 {
				if err := write(); err != nil {
					t.Fatal(err)
				}
				for _, path := range paths {
					info, err := os.Stat(path)
					if err != nil {
						t.Fatal(err)
					}
					if !os.SameFile(before[path], info) || !info.ModTime().Equal(old) {
						t.Errorf("unchanged writer replaced %s", path)
					}
				}
			}
			// Both same-size content changes and size changes must still be persisted.
			for _, title := range []string{"auto", "a longer title"} {
				meta.Title, meta.Effort, document.Nodes[0].Title = title, title, title
				if err := write(); err != nil {
					t.Fatal(err)
				}
				if writer == "checkpoint" {
					got, ok := loadTaskCheckpoint(place.Tasks())
					if !ok || len(got.Nodes) != 1 || got.Nodes[0].Title != title {
						t.Fatalf("changed checkpoint missing: %+v", got)
					}
				} else {
					got, err := LoadMeta(dir)
					if err != nil || got.Title != title || got.Effort != title {
						t.Fatalf("changed metadata missing: %+v, %v", got, err)
					}
				}
				for _, path := range paths {
					info, err := os.Stat(path)
					if err != nil {
						t.Fatal(err)
					}
					if os.SameFile(before[path], info) {
						t.Errorf("changed writer did not replace %s", path)
					}
					if info.Mode().Perm() != 0600 {
						t.Errorf("%s permissions = %o, want 600", path, info.Mode().Perm())
					}
					before[path] = info
				}
			}
		})
	}
}

// List the settled opener's session writers, rather than relying on one
// checkpoint regression to stand for every write performed during restoration.
// Task recovery writes the recovered graph's snapshot. Pending-run/index/answer
// reconciliation writes only for a state transition; list those too. Presence
// has a changing heartbeat and is removed on close, rather than persisted state.
func TestSessionOpenerWritersAreIdempotent(t *testing.T) {
	for _, layout := range []string{"legacy", "cell"} {
		t.Run(layout, func(t *testing.T) {
			dir := t.TempDir()
			if layout == "cell" {
				if err := os.Mkdir(filepath.Join(dir, ".cell"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			place := Place{Dir: dir, Workspace: dir}
			agent, _ := newTestAgent(t, &scriptedCompleter{}, func(cfg *Config) {
				cfg.SessionFile, cfg.Place = place.Transcript(), place
				cfg.ApprovalGate = &fakeApprovalGate{standing: PostureAsk}
			})
			agent.SetConversationEffort("high")
			if err := agent.SetApprovalPosture(PostureAsk); err != nil {
				t.Fatal(err)
			}
			agent.writeSpend(dir, 1, 10)
			agent.SettleWrites()
			document := taskDocument{Type: taskDocumentType, Version: taskFileVersion, Seq: 1, Nodes: []taskRecord{{ID: 1, Title: "Saved settled task", Brief: "finished", Acceptance: "finished", State: TaskDone, Noted: true}}}
			if err := newTaskStore(place.Tasks()).writeLocked(document); err != nil {
				t.Fatal(err)
			}
			agent.recoverTasks()
			writers := []struct {
				name  string
				write func()
			}{
				{"recoverTasks/graph.checkpoint", func() { agent.graph().checkpoint() }},
				{"recoverBeltRun", agent.recoverBeltRun},
				{"endInterruptedProgramRun", agent.endInterruptedProgramRun},
				{"closeInflightTaskIndexRows", agent.closeInflightTaskIndexRows},
				{"drainAnswers", agent.drainAnswers},
				{"restore approval/SaveMeta", func() {
					if err := agent.SetApprovalPosture(PostureAsk); err != nil {
						t.Fatal(err)
					}
				}},
				{"restore effort/SaveMeta", func() {
					if !agent.SetConversationEffort("high") {
						t.Fatal("effort rejected")
					}
				}},
				{"restore spend/SaveMeta", func() { agent.stampRestoredSpend(Usage{CostUSD: 1, Input: 10}) }},
			}
			paths := []string{place.Tasks(), place.MetaPath()}
			if layout == "cell" {
				paths = append(paths, filepath.Join(dir, ".cell", "session.json"))
			}
			for _, writer := range writers {
				t.Run(writer.name, func(t *testing.T) {
					before := map[string]os.FileInfo{}
					for _, path := range paths {
						info, err := os.Stat(path)
						if err != nil {
							t.Fatal(err)
						}
						before[path] = info
					}
					writer.write()
					agent.SettleWrites()
					for _, path := range paths {
						after, err := os.Stat(path)
						if err != nil {
							t.Fatal(err)
						}
						if !os.SameFile(before[path], after) || !before[path].ModTime().Equal(after.ModTime()) {
							t.Errorf("%s replaced unchanged %s", writer.name, path)
						}
					}
				})
			}
		})
	}
}
