package session

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// R8 recovery splits must retain authoritative abandonment on the local fallthrough path.
func TestReopeningKeepsTheAbandonmentSavedBeforeItsCheckpoint(t *testing.T) {
	taskComputerChoiceHome(t)
	t.Setenv("CODEAF_TASK_BELT", "bash")
	local := newBeltRunDouble("done here")
	local.real = true
	local.honoursStop = true
	registerBeltRunEngine(t, local)
	ready := make(chan struct{})
	var starts, resumes atomic.Int32
	remote := &scriptedTaskEngine{
		start: func(ctx context.Context, spec RunSpec) RunSummary {
			starts.Add(1)
			r := remoteRecordFor(spec)
			if err := spec.SaveRemote(r, true); err != nil {
				t.Error(err)
			}
			close(ready)
			<-ctx.Done()
			return RunSummary{Outcome: "interrupted", KeepCopy: true}
		},
		resume: func(ctx context.Context, spec RunSpec) RunSummary {
			resumes.Add(1)
			return RunSummary{Outcome: "interrupted", KeepCopy: true}
		},
	}
	place, repo, profile := t.TempDir(), beltRunCommittedRepo(t), t.TempDir()
	open := func() *Agent {
		a, _ := newTestAgent(t, beltRunCompleter{text: "done"}, func(c *Config) {
			c.Workspace = repo
			c.Place = Place{Dir: place}
			c.SessionFile = c.Place.Transcript()
			c.ProfileDir = profile
			c.TaskComputers = testTaskComputers{engine: remote}
		})
		return a
	}
	first := open()
	if _, _, _, err := first.StartTaskOn(t.Context(), "Repair export", false, "work computer"); err != nil {
		t.Fatal(err)
	}
	<-ready
	_ = first.Close()
	waitRemoteRun(t, first)
	store, err := openRemoteTaskPlan(first.graph(), 1)
	if err != nil {
		t.Fatal(err)
	}
	row, _ := runRowOf(first.graph(), 1)
	record := readRemoteRecord(store, row.Remote)
	record.Abandoned = true
	record.AckPending = false
	if err := writeRemoteRecord(store, *record); err != nil {
		t.Fatal(err)
	}
	if err := store.CompleteRoot("the local replacement finished"); err != nil {
		t.Fatal(err)
	}
	_ = store.Close()
	second := open()
	events, stop := second.WatchTaskUpdates()
	defer stop()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case event := <-events:
			if event.Kind != EventTaskUpdate || event.Task == nil || event.Task.ID != 1 {
				continue
			}
			if event.Task.Remote == nil || !event.Task.Remote.Abandoned {
				t.Fatalf("reopen forgot the saved abandonment: %+v", event.Task.Remote)
			}
			if starts.Load() != 1 || resumes.Load() != 0 || local.didRun() {
				t.Fatal("settled recovery started another worker")
			}
			return
		case <-deadline:
			t.Fatal("reopened task was not replayed")
		}
	}
}
