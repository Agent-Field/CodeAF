//go:build !windows

package run_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/run"
)

// THE PROGRAM'S OWN CLOCK IS WRITTEN DOWN. A run's wall time had nothing to
// stand on: the store is seeded before the copy is cut and the row settles
// after the landing, so every surface reconstructed a span from a different
// pair of instants and none of them was the program's. The worker now stamps
// the instant it started the process and the instant the process was gone on
// the program record — keeping the hello's name, stages and ceiling — and on
// the trajectory's ending line.
func TestDelegateWorkerStampsTheProgramsOwnClock(t *testing.T) {
	store := runOpenStore(t)
	storeDir := filepath.Dir(store.Path())
	m, setup := fakeDelegate(t, "sleep 0.2\n"+passLine("tests are green"))
	worker := run.NewDelegateWorker(store, t.TempDir(), m, setup, 2.5, 0)
	before := time.Now()
	if _, err := worker.Run(runContext(t), *store.Task(store.RootID())); err != nil {
		t.Fatalf("the delegate's run failed: %v", err)
	}
	after := time.Now()

	record, ok := delegate.ReadProgram(plandb.TaskDir(storeDir, store.RootID()))
	if !ok || record.Name != "fake" || strings.Join(record.Stages, ",") != "implement,verify" || record.CeilingUSD != 2.5 {
		t.Fatalf("program record = %+v %v, want the hello's name and stages and the run's ceiling kept", record, ok)
	}
	if record.StartedAt.IsZero() || record.EndedAt.IsZero() {
		t.Fatalf("program record carries no clock: started %v ended %v", record.StartedAt, record.EndedAt)
	}
	if record.StartedAt.Before(before) || record.EndedAt.After(after) || record.EndedAt.Sub(record.StartedAt) < 200*time.Millisecond {
		t.Fatalf("program clock %v → %v is not the process's life inside the run's %v → %v", record.StartedAt, record.EndedAt, before, after)
	}
	end := endLine(t, rawTrajectory(t, storeDir, store.RootID()))
	if !end.StartedAt.Equal(record.StartedAt) || !end.EndedAt.Equal(record.EndedAt) {
		t.Fatalf("the ending line's clock %v → %v is not the record's %v → %v", end.StartedAt, end.EndedAt, record.StartedAt, record.EndedAt)
	}
}

// A PROGRAM THAT DIED BEFORE ITS HELLO STILL HAS ITS TIMES. The record used to
// be written at the hello and nowhere else, so a program that fell over on its
// first line left no record at all, and its page and its row had nothing to
// measure it by.
func TestDelegateWorkerRecordsTheClockOfAProgramThatNeverSaidHello(t *testing.T) {
	store := runOpenStore(t)
	storeDir := filepath.Dir(store.Path())
	script := filepath.Join(t.TempDir(), "dies.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho 'no such flag' >&2\nexit 3\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	worker := run.NewDelegateWorker(store, t.TempDir(), delegate.Delegate{Name: "fake", Default: "run"}, run.DelegateSetup{Exe: script}, 0, 0)
	if _, err := worker.Run(runContext(t), *store.Task(store.RootID())); err == nil || !strings.Contains(err.Error(), "fake exited 3 without a terminal record") {
		t.Fatalf("err = %v, want the exit named", err)
	}
	record, ok := delegate.ReadProgram(plandb.TaskDir(storeDir, store.RootID()))
	if !ok || record.Name != "fake" || len(record.Stages) != 0 {
		t.Fatalf("program record = %+v %v, want the program named with no stages it never said", record, ok)
	}
	if record.StartedAt.IsZero() || record.EndedAt.IsZero() || record.EndedAt.Before(record.StartedAt) {
		t.Fatalf("program clock = %v → %v, want both instants in order", record.StartedAt, record.EndedAt)
	}
}
