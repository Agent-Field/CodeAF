package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// idleFloor is a floor of n jobs, the first managed by the returned
// conversation, with its folder dated an hour back so the index may trust it.
func idleFloor(t *testing.T, n int) (*store.Store, string, int) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "factory"))
	if err != nil {
		t.Fatal(err)
	}
	talk := filepath.Join(t.TempDir(), "manager.jsonl")
	if err := os.WriteFile(talk, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	managed := 0
	for i := range n {
		it := factory.Item{Title: fmt.Sprintf("job %d", i), Repo: "acme/api", Kind: factory.KindIssue, State: factory.StateNew}
		if i == 0 {
			it.Talk = talk
		}
		made, err := st.Create(it)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			managed = made.ID
		}
	}
	settleFolder(t, st)
	return st, talk, managed
}

// settleFolder dates the floor's folder an hour back: what an idle floor looks
// like once its last write is older than a file system's clock can blur.
func settleFolder(t *testing.T, st *store.Store) {
	t.Helper()
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(st.Root(), old, old); err != nil {
		t.Fatal(err)
	}
}

// THE IDLE-COST LAW (PERF.md): the person-Stop watch every job chat runs ten
// times a second (session.watchJobBudget) reads the floor only when the floor
// changed. It used to decode every job on the floor twice per look, and on a
// machine with seven hundred jobs that alone kept more than a core busy.
func TestAnIdleManagerWatchReadsNoJobs(t *testing.T) {
	const jobs, ticks = 60, 100
	st, talk, _ := idleFloor(t, jobs)
	door := seamRunDoor{st: st}
	stranger := filepath.Join(t.TempDir(), "not-a-manager.jsonl")
	for _, conversation := range []string{talk, stranger} {
		if got := door.AutomaticRefusal(conversation); got != "" {
			t.Fatalf("a running job refused automatic work: %q", got)
		}
		before := st.Reads()
		for range ticks {
			door.AutomaticRefusal(conversation)
		}
		got := st.Reads() - before
		t.Logf("%d idle looks over %d jobs read %d documents", ticks, jobs, got)
		if got != 0 {
			t.Fatalf("%d idle looks over %d jobs read %d documents, want 0", ticks, jobs, got)
		}
	}
}

// A person's Stop is still seen on the very next look, and so is a job that
// names a conversation as its manager after the watch began.
func TestTheManagerWatchSeesEveryChange(t *testing.T) {
	st, talk, id := idleFloor(t, 20)
	door := seamRunDoor{st: st}
	if got := door.AutomaticRefusal(talk); got != "" {
		t.Fatalf("before the Stop: %q", got)
	}
	if err := st.Update(id, func(it *factory.Item) error { factory.MarkPersonStopped(it); return nil }); err != nil {
		t.Fatal(err)
	}
	if got := door.AutomaticRefusal(talk); got != factory.PersonStopReason {
		t.Fatalf("the look after a person's Stop answered %q", got)
	}

	later := filepath.Join(t.TempDir(), "later.jsonl")
	if door.Manages(later) {
		t.Fatal("a conversation no job names was a manager")
	}
	settleFolder(t, st)
	made, err := st.Create(factory.Item{Title: "late", Repo: "acme/api", Kind: factory.KindIssue, State: factory.StateNew, Talk: later})
	if err != nil {
		t.Fatal(err)
	}
	if !door.Manages(later) {
		t.Fatal("a job made after the first look was not found on the next")
	}
	// The manager moving to another conversation is seen too.
	moved := filepath.Join(t.TempDir(), "moved.jsonl")
	settleFolder(t, st)
	if err := st.Update(made.ID, func(it *factory.Item) error { it.Talk = moved; return nil }); err != nil {
		t.Fatal(err)
	}
	if door.Manages(later) || !door.Manages(moved) {
		t.Fatal("a manager's move to another conversation was not seen on the next look")
	}
}
