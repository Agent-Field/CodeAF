package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

// letGo makes the backend treat the agent as not held, reading its history
// from transcript, and puts the conversation back for the backend's close.
func letGo(t *testing.T, backend *appBackend, id, transcript string) {
	t.Helper()
	a, err := backend.find(id)
	if err != nil {
		t.Fatal(err)
	}
	var held *appLive
	var was string
	backend.locked(func() { held, was = a.live, a.transcript; a.live, a.transcript = nil, transcript })
	t.Cleanup(func() { backend.locked(func() { a.live, a.transcript = held, was }) })
}

// seedRecords writes a conversation's process store the way the engine
// writes it: one record per job and a head.
func seedRecords(t *testing.T, dir string, rows ...session.JobNotice) {
	t.Helper()
	store := filepath.Join(dir, "processes")
	if err := os.MkdirAll(store, 0o700); err != nil {
		t.Fatal(err)
	}
	high := 0
	for _, n := range rows {
		raw, _ := json.Marshal(n)
		if err := os.WriteFile(filepath.Join(store, strconv.Itoa(n.ID)+".json"), raw, 0o600); err != nil {
			t.Fatal(err)
		}
		high = max(high, n.ID)
	}
	head, _ := json.Marshal(map[string]any{"epoch": "e1", "high": high})
	if err := os.WriteFile(filepath.Join(store, "head.json"), head, 0o600); err != nil {
		t.Fatal(err)
	}
}

// A CONVERSATION NOBODY HERE HOLDS IS READ OFF ITS STORE, and a record saved as
// running is unknown — no exit code, no ending — while a settled one keeps its
// folder, its ending and who started it, a task by the app's own task id.
func TestProcessPageOfANotHeldConversationIsTruthfulAndPaged(t *testing.T) {
	backend, _, agent := workBackend(t, &fakeConversation{})
	dir := t.TempDir()
	transcript := filepath.Join(dir, "transcript.jsonl")
	log := filepath.Join(dir, "2.log")
	_ = os.WriteFile(log, []byte("compiling\nlistening on localhost:3000\n"), 0o600)
	started := time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)
	var rows []session.JobNotice
	for i := 3; i <= 60; i++ {
		rows = append(rows, session.JobNotice{ID: i, Kind: session.JobKindCommand, Command: "make " + strconv.Itoa(i), State: session.JobDone, Started: started.Add(time.Duration(i) * time.Minute), Ended: started.Add(time.Duration(i)*time.Minute + time.Second)})
	}
	rows = append(rows,
		session.JobNotice{ID: 2, Kind: session.JobKindCommand, Command: "npm run dev", State: session.JobRunning, Started: started, Elapsed: time.Hour, LogPath: log, Dir: "/work/site", StartedBy: session.JobStartedByTask, Task: 4},
		session.JobNotice{ID: 1, Kind: session.JobKindCommand, Command: "go test", State: session.JobFailed, ExitCode: 1, Started: started, Ended: started.Add(time.Minute), StartedBy: session.JobStartedByChat, LogPath: filepath.Join(dir, "1.log")},
	)
	seedRecords(t, dir, rows...)
	letGo(t, backend, agent.ID, transcript)

	ctx := context.Background()
	first, err := backend.PageProcesses(ctx, appwire.ProcessPageAsk{Agent: agent.ID})
	if err != nil || len(first.Processes) != appwire.ProcessPageLimitDefault || first.Next == "" || first.Revision != "e1" {
		t.Fatalf("first page: n=%d next=%q err=%v", len(first.Processes), first.Next, err)
	}
	second, err := backend.PageProcesses(ctx, appwire.ProcessPageAsk{Agent: agent.ID, Cursor: first.Next})
	if err != nil || second.Next != "" || len(second.Processes) != 10 {
		t.Fatalf("second page: n=%d next=%q err=%v", len(second.Processes), second.Next, err)
	}
	running, failed := second.Processes[8], second.Processes[9]
	if running.State != appwire.ProcessUnknown || running.UnknownReason != appwire.ProcessUnknownNotHeld || running.EndedAt != nil || running.ExitCode != nil {
		t.Fatalf("saved running row: %+v", running)
	}
	if running.Dir != "/work/site" || running.LogPath != log || running.LastLine != "listening on localhost:3000" || running.StartedBy == nil || running.StartedBy.Task != agent.ID+":4" {
		t.Fatalf("running facts: %+v starter=%+v", running, running.StartedBy)
	}
	if failed.State != appwire.ProcessFailed || failed.ExitCode == nil || *failed.ExitCode != 1 || failed.EndedAt == nil || !failed.EndedAt.Equal(started.Add(time.Minute)) || failed.StartedBy.Kind != "chat" || !failed.LogGone {
		t.Fatalf("failed row: %+v", failed)
	}
	if _, err := os.Stat(filepath.Join(dir, "processes.json")); !os.IsNotExist(err) {
		t.Fatal("a read wrote the legacy shelf")
	}

	// The log is read from the record by exact id, bounded, by offset.
	out, err := backend.ReadProcessLog(ctx, appwire.ProcessLogAsk{ID: running.ID})
	if err != nil || out.Text != "compiling\nlistening on localhost:3000\n" || !out.EOF || out.Next != out.Size {
		t.Fatalf("log tail: %+v %v", out, err)
	}
	ten := 10
	zero := int64(0)
	part, err := backend.ReadProcessLog(ctx, appwire.ProcessLogAsk{ID: running.ID, Offset: &zero, LimitBytes: &ten})
	if err != nil || part.Text != "compiling\n" || part.Next != 10 || part.EOF {
		t.Fatalf("log window: %+v %v", part, err)
	}
	gone, err := backend.ReadProcessLog(ctx, appwire.ProcessLogAsk{ID: failed.ID})
	if err != nil || !gone.LogGone {
		t.Fatalf("gone log: %+v %v", gone, err)
	}
	other := agent.ID + ":p2.zzz"
	if _, err := backend.ReadProcessLog(ctx, appwire.ProcessLogAsk{ID: other}); !isCode(err, appwire.CodeNotFound) {
		t.Fatalf("another run of the same number: %v", err)
	}
	_ = os.Remove(log)
	_ = os.Symlink("/etc/passwd", log)
	if out, err := backend.ReadProcessLog(ctx, appwire.ProcessLogAsk{ID: running.ID}); err == nil {
		t.Fatalf("followed a link the record names: %+v", out)
	}
}

func isCode(err error, code string) bool {
	wire, ok := err.(*appwire.Error)
	return ok && wire.Code == code
}

// A HELD CONVERSATION WHOSE ENGINE CANNOT PAGE is paged here from its list,
// within the same bounds, and a stale cursor says so.
func TestProcessPageOfAnOlderHeldEngineIsPagedFromItsList(t *testing.T) {
	spool := filepath.Join(t.TempDir(), "9.log")
	_ = os.WriteFile(spool, []byte(strings.Repeat("x\n", 10)), 0o600)
	var jobs []session.JobNotice
	for i := 1; i <= 9; i++ {
		jobs = append(jobs, session.JobNotice{ID: i, Kind: session.JobKindCommand, Command: "c", State: session.JobDone, Started: time.Now(), LogPath: spool})
	}
	conv := &fakeConversation{jobs: jobs}
	backend, _, agent := workBackend(t, conv)
	ctx := context.Background()
	four := 4
	page, err := backend.PageProcesses(ctx, appwire.ProcessPageAsk{Agent: agent.ID, Limit: &four})
	if err != nil || len(page.Processes) != 4 || page.Processes[0].ID != appProcessID(agent.ID, jobs[8]) || page.Next == "" {
		t.Fatalf("list page: %+v %v", page, err)
	}
	conv.mu.Lock()
	conv.jobs = append(conv.jobs, session.JobNotice{ID: 10, Kind: session.JobKindCommand, State: session.JobRunning, Started: time.Now()})
	conv.mu.Unlock()
	if _, err := backend.PageProcesses(ctx, appwire.ProcessPageAsk{Agent: agent.ID, Cursor: page.Next}); !isCode(err, appwire.CodeStaleCursor) {
		t.Fatalf("stale list cursor: %v", err)
	}
	out, err := backend.ReadProcessLog(ctx, appwire.ProcessLogAsk{ID: appProcessID(agent.ID, jobs[8])})
	if err != nil || out.Text != strings.Repeat("x\n", 10) {
		t.Fatalf("log through the list: %+v %v", out, err)
	}
	if _, err := backend.PageProcesses(ctx, appwire.ProcessPageAsk{Agent: agent.ID, Crew: "c1"}); !isCode(err, appwire.CodeBadParams) {
		t.Fatalf("crew scope: %v", err)
	}
}
