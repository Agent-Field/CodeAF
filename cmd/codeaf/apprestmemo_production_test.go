package main

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

// The production opener restores both dials and recovers a nonempty checkpoint.
// A warm open canonicalizes the fixture; all later opens must preserve identity.
func productionRestBackend(t *testing.T, cell bool) (*appBackend, *appAgent, *atomic.Int32) {
	t.Helper()
	_, workspace := wakeEnvironment(t, resolvedTempDir(t))
	bucket, err := v3ProjectDir(workspace)
	if err != nil {
		t.Fatal(err)
	}
	id := "00000000000000e3"
	dir := writeV3Session(t, bucket, id, "Saved settled work", time.Now())
	if cell {
		if err := os.Mkdir(filepath.Join(dir, ".cell"), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(filepath.Join(dir, "transcript.jsonl"), filepath.Join(dir, ".cell", "transcript.jsonl")); err != nil {
			t.Fatal(err)
		}
	}
	if err := session.SaveMeta(dir, session.Meta{ID: id, Workspace: workspace, Created: time.Now(), Effort: "high", Approval: session.PostureAsk}); err != nil {
		t.Fatal(err)
	}
	place := session.Place{Dir: dir, Workspace: workspace}
	checkpoint := `{"type":"tasks","version":1,"seq":1,"nodes":[{"id":1,"title":"Saved settled task","brief":"Already finished","acceptance":"Task finished","state":"done","report":"Finished","noted":true}]}`
	if err := os.WriteFile(place.Tasks(), []byte(checkpoint), 0600); err != nil {
		t.Fatal(err)
	}
	conn, err := appOpenInProcess(appOpenAsk{Workspace: workspace, Transcript: place.Transcript()})
	if err != nil {
		t.Fatal(err)
	}
	conn.Release()
	opens := &atomic.Int32{}
	b := newFakeBackend(newAppRecorder(), func(ask appOpenAsk) (*appConn, error) {
		opens.Add(1)
		conn, err := appOpenInProcess(ask)
		if err == nil {
			agent, ok := session.LiveAgentFor(conn.Transcript)
			if !ok || agent.ConversationEffort() != "high" || agent.ApprovalPosture() != session.PostureAsk {
				t.Error("production opener did not restore both live dials")
			}
		}
		return conn, err
	})
	b.now = func() time.Time { return time.Now().Add(time.Hour) }
	// First-run daily-limit migration is a real configuration change, outside
	// this unchanged-session regression. Settle it before taking any snapshot.
	if err := b.settleDayLimit(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { b.locked(func() { b.crewRun = nil }); b.Close() })
	a, err := b.find(id)
	if err != nil {
		t.Fatal(err)
	}
	b.locked(func() { b.crewRun = &appCrewRun{held: map[string][]string{id: {"crew"}}} })
	return b, a, opens
}

func TestAppRestProductionSavedSettingsFillBothCaches(t *testing.T) {
	for _, cell := range []bool{false, true} {
		name := "legacy"
		if cell {
			name = "cell"
		}
		t.Run(name, func(t *testing.T) {
			b, a, opens := productionRestBackend(t, cell)
			// Every persistent writer on this settled open: recoverTasks writes the
			// checkpoint; restored approval and effort each call SaveMeta, which writes
			// meta.json and, for cells, session.json. Check each destination independently.
			paths := []string{filepath.Join(filepath.Dir(a.transcript), "tasks.json"), filepath.Join(session.DirOf(a.transcript), "meta.json")}
			if cell {
				paths = append(paths, filepath.Join(filepath.Dir(a.transcript), "session.json"))
			}
			before := map[string]os.FileInfo{}
			for _, path := range paths {
				info, err := os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}
				before[path] = info
			}
			for pass := range 3 {
				tasks, err := b.ListTasks(a.view.ID)
				if err != nil || len(tasks) != 1 || tasks[0].Title != "Saved settled task" {
					t.Fatalf("listing %d: %+v, %v", pass+1, tasks, err)
				}
				if b.liveOf(a) != nil {
					t.Fatal("idle conversation stayed held")
				}
				for _, path := range paths {
					after, err := os.Stat(path)
					if err != nil {
						t.Fatal(err)
					}
					if !os.SameFile(before[path], after) || !before[path].ModTime().Equal(after.ModTime()) {
						t.Errorf("listing %d replaced unchanged %s", pass+1, path)
					}
				}
				if got := opens.Load(); got != 1 {
					t.Errorf("listing %d: %d opens, want 1", pass+1, got)
				}
				if pass == 1 {
					b.restMemo = appRestMemo{}
				}
			}
			meta, err := session.LoadMeta(session.DirOf(a.transcript))
			if err != nil || meta.Effort != "high" || meta.Approval != session.PostureAsk {
				t.Fatalf("saved settings changed: %+v, %v", meta, err)
			}
		})
	}
}

func TestAppRestTombstoneCommitInvalidatesBothCaches(t *testing.T) {
	for _, cache := range []string{"memory", "disk"} {
		t.Run(cache, func(t *testing.T) {
			b, a, _ := productionRestBackend(t, false)
			index := session.TaskIndexPath(a.transcript)
			row := `{"id":"2","sessionId":"` + a.view.ID + `","title":"Index-only task","status":"done"}` + "\n"
			if err := os.WriteFile(index, []byte(row), 0600); err != nil {
				t.Fatal(err)
			}
			tasks, err := b.ListTasks(a.view.ID)
			if err != nil || len(tasks) != 2 {
				t.Fatalf("prime cache: %+v, %v", tasks, err)
			}
			stamp := b.restStamp(a)
			if _, ok := b.restMemo.get(a.view.ID, stamp); !ok {
				t.Fatal("memory cache not primed")
			}
			if _, ok := appRestRead(a.view.ID, stamp); !ok {
				t.Fatal("disk cache not primed")
			}
			paths := []string{filepath.Join(filepath.Dir(a.transcript), "tasks.json"), index}
			before := map[string]os.FileInfo{}
			for _, path := range paths {
				info, err := os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}
				before[path] = info
			}
			// This is the durable state after commitTaskDeletions, with the process
			// lost before finishTaskCleanup can touch the checkpoint or index.
			tombstone := filepath.Join(filepath.Dir(a.transcript), ".deleted-tasks.json")
			if err := os.WriteFile(tombstone, []byte(`{"1":true,"2":true}`), 0600); err != nil {
				t.Fatal(err)
			}
			for _, path := range paths {
				after, err := os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}
				if !os.SameFile(before[path], after) || !before[path].ModTime().Equal(after.ModTime()) {
					t.Fatalf("crash fixture changed %s", path)
				}
			}
			if got := session.ReadSessionTasks(a.transcript); len(got) != 0 {
				t.Fatalf("uncached checkpoint did not filter deletion: %+v", got)
			}
			if got := session.ReadTaskIndex(index); len(got) != 0 {
				t.Fatalf("uncached index did not filter deletion: %+v", got)
			}
			if cache == "disk" {
				b.restMemo = appRestMemo{}
			}
			tasks, err = b.ListTasks(a.view.ID)
			if err != nil || len(tasks) != 0 {
				t.Fatalf("%s cache resurrected deleted tasks: %+v, %v", cache, tasks, err)
			}
		})
	}
}
