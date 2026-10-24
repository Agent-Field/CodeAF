package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

// replayConversation sends the same restored rows a real session sends on open.
type replayConversation struct{ fakeConversation }

func (c *replayConversation) WatchTaskUpdates() (<-chan session.Event, func()) {
	lane, stop := c.fakeConversation.WatchTaskUpdates()
	for _, row := range c.jobs {
		job := row
		c.tasks <- session.Event{Kind: session.EventJobUpdate, Job: &job}
	}
	return lane, stop
}

func TestAppKeptJobReplayOpenListAndHistoryAgree(t *testing.T) {
	for _, road := range []string{"new", "old", "no_tool"} {
		t.Run(road, func(t *testing.T) {
			const command = "printf 'full command\\n'\nexit 17"
			transcript := appProcessJournal(t, "bash_28", command, nil)
			if road == "no_tool" {
				if err := os.WriteFile(transcript, []byte("{\"type\":\"session\",\"version\":1,\"id\":\"process-history\",\"cwd\":\".\",\"model\":\"test/model\"}\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			start := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
			row := map[string]any{"id": 8, "kind": "job", "title": "build", "state": "failed", "report": "job 3 · log /missing/3.log", "elapsed_ms": 2000}
			if road == "new" {
				row["job_command"], row["job_call_id"], row["job_exit_code"] = command, "bash_28", 17
				row["startedAt"], row["endedAt"] = start, start.Add(2*time.Second)
			}
			doc, _ := json.Marshal(map[string]any{"type": "tasks", "version": 1, "seq": 8, "runs": []any{row}})
			if err := os.WriteFile(filepath.Join(filepath.Dir(transcript), "tasks.json"), doc, 0600); err != nil {
				t.Fatal(err)
			}
			rows, err := session.ReadProcessNotices(transcript)
			if err != nil || len(rows) != 1 {
				t.Fatalf("rows = %+v, %v", rows, err)
			}
			conv := &replayConversation{fakeConversation: fakeConversation{jobs: rows}}
			rec := newAppRecorder()
			backend := newFakeBackend(rec, func(ask appOpenAsk) (*appConn, error) {
				return &appConn{Conv: conv, Transcript: transcript, Workspace: ask.Workspace, Release: func() {}}, nil
			})
			backend.mint = func(string) (string, error) { return transcript, nil }
			t.Cleanup(func() { backend.Close() })
			agent, err := mustCreate(t, backend, appwire.CreateAsk{Workspace: t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := backend.OpenAgent(agent.ID); err != nil {
				t.Fatal(err)
			}
			i := rec.waitFor(t, 2*time.Second, func(event string, data any) bool { return event == appwire.EventProcessChanged })
			rec.mu.Lock()
			announced := rec.data[i].(appwire.ProcessChanged).Process
			rec.mu.Unlock()
			list, err := backend.ListProcesses(agent.ID)
			if err != nil || !reflect.DeepEqual(list, []appwire.Process{announced}) {
				t.Fatalf("list = %+v, %v; announced = %+v", list, err, announced)
			}
			if announced.State != appwire.ProcessFailed || announced.Title != "build" {
				t.Fatalf("process = %+v", announced)
			}
			if road == "new" {
				raw, _ := json.Marshal(announced)
				t.Logf("replayed process: %s", raw)
				live := session.JobNotice{ID: 3, Started: start}
				if announced.ID != appProcessID(agent.ID, live) || announced.StartedAt != start || announced.EndedAt == nil || !announced.EndedAt.Equal(start.Add(2*time.Second)) || announced.ExitCode == nil || *announced.ExitCode != 17 {
					t.Fatalf("new process = %+v", announced)
				}
			} else if announced.ExitCode != nil || !announced.StartedAt.IsZero() || announced.EndedAt != nil {
				t.Fatalf("invented facts = %+v", announced)
			}
			if road != "no_tool" && announced.Command != command {
				t.Fatalf("command = %q", announced.Command)
			}
			entries, _, err := backend.Entries(agent.ID, "", appwire.MaxEntries)
			if err != nil {
				t.Fatal(err)
			}
			linked := false
			for _, e := range entries {
				if e.Kind == appwire.EntryTool && e.Call == "bash_28" {
					linked = e.Process == announced.ID
				}
			}
			if road != "no_tool" && !linked {
				t.Fatalf("history lacks process: %+v", entries)
			}
			if road == "no_tool" {
				raw, _ := json.Marshal(announced)
				var wire map[string]any
				_ = json.Unmarshal(raw, &wire)
				// THE CONTRACT KEEPS command AND started_at ON EVERY PROCESS, as ""
				// and the zero time when unknown, because existing apps read them
				// unconditionally; only the optional fields are left out.
				if wire["command"] != "" || wire["started_at"] != "0001-01-01T00:00:00Z" {
					t.Fatalf("unknown command or start not in the kept contract: %s", raw)
				}
				for _, key := range []string{"ended_at", "exit_code"} {
					if _, ok := wire[key]; ok {
						t.Fatalf("unknown %s present: %s", key, raw)
					}
				}
			}
		})
	}
}

func TestAppReplayDerivesEndOnlyForSettledJobs(t *testing.T) {
	start := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	n := session.JobNotice{ID: 3, Started: start, Elapsed: 2 * time.Second, State: session.JobFailed, ExitCodeUnknown: true}
	p := appProcessOf("agent", n)
	if p.EndedAt == nil || !p.EndedAt.Equal(start.Add(2*time.Second)) || p.ExitCode != nil || p.State != appwire.ProcessFailed {
		t.Fatalf("settled = %+v", p)
	}
	n.State = session.JobRunning
	if p := appProcessOf("agent", n); p.EndedAt != nil || p.ExitCode != nil {
		t.Fatalf("running = %+v", p)
	}
}
