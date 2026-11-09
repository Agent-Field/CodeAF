package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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

// THE BUSY-FOLDER CASE, by reads alone: a folder whose clock moves under every
// look must not make the watch read the floor. It read every job on every look
// before the index stopped trusting the folder's clock.
func TestABusyFloorFolderReadsNoJobs(t *testing.T) {
	const jobs, ticks = 60, 100
	st, talk, _ := idleFloor(t, jobs)
	door := seamRunDoor{st: st}
	door.AutomaticRefusal(talk)
	before := st.Reads()
	for i := range ticks {
		if err := st.SetBusyAll(fmt.Sprintf("tick %d", i)); err != nil {
			t.Fatal(err)
		}
		door.AutomaticRefusal(talk)
	}
	got := st.Reads() - before
	t.Logf("%d looks over %d jobs in a busy folder read %d documents", ticks, jobs, got)
	if got != 0 {
		t.Fatalf("%d looks over %d jobs in a busy folder read %d documents, want 0", ticks, jobs, got)
	}
}

// THE BUSY-FOLDER CASE. A live engine rewrites the floor folder's busy, poll
// and runner records every second or two, so the folder is never still. The
// first manager index trusted the folder's clock and was built again, from a
// full read of the floor, on every look of every job chat; on the owner's
// machine that was a fifth of an idle engine's time. Now an idle look reads no
// job and checks the floor at most once a managerRecheck, while a write this
// process makes is still seen on the next look and one another process makes
// within managerRecheck.
func TestABusyFloorFolderCostsTheManagerWatchNothing(t *testing.T) {
	const jobs, ticks = 60, 100
	st, talk, id := idleFloor(t, jobs)
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	managerClock = func() time.Time { return now }
	t.Cleanup(func() { managerClock = time.Now })
	door := seamRunDoor{st: st}
	if got := door.AutomaticRefusal(talk); got != "" {
		t.Fatalf("a running job refused automatic work: %q", got)
	}
	x := managerIndexOf(st)
	reads, checks := st.Reads(), x.checks
	for i := range ticks {
		// The folder moves under every look, as the factory's own records do.
		if err := st.SetBusyAll(fmt.Sprintf("tick %d", i)); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(st.Root(), now, now); err != nil {
			t.Fatal(err)
		}
		door.AutomaticRefusal(talk)
		now = now.Add(100 * time.Millisecond)
	}
	gotReads, gotChecks := st.Reads()-reads, x.checks-checks
	t.Logf("%d looks over %d jobs in a busy folder: %d documents read, %d checks of the floor", ticks, jobs, gotReads, gotChecks)
	if gotReads != 0 {
		t.Fatalf("%d idle looks over %d jobs in a busy folder read %d documents, want 0", ticks, jobs, gotReads)
	}
	if limit := ticks/10 + 1; gotChecks > limit {
		t.Fatalf("%d idle looks over ten seconds checked the floor %d times, want at most %d", ticks, gotChecks, limit)
	}

	// Another process names this conversation's job elsewhere: seen once the
	// recheck window has passed, without any write by this process.
	path := st.ItemPath(id)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	elsewhere := filepath.Join(t.TempDir(), "elsewhere.jsonl")
	moved := strings.Replace(string(data), strconv.Quote(talk), strconv.Quote(elsewhere), 1)
	if moved == string(data) {
		t.Fatal("the fixture's document does not name its manager")
	}
	if err := os.WriteFile(path+".tmp", []byte(moved), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path+".tmp", path); err != nil {
		t.Fatal(err)
	}
	now = now.Add(managerRecheck)
	if door.Manages(talk) || !door.Manages(elsewhere) {
		t.Fatal("a manager moved by another process was not seen after the recheck window")
	}
	// And a Stop written here is seen on the very next look.
	if err := st.Update(id, func(it *factory.Item) error { factory.MarkPersonStopped(it); return nil }); err != nil {
		t.Fatal(err)
	}
	if got := door.AutomaticRefusal(elsewhere); got != factory.PersonStopReason {
		t.Fatalf("the look after a person's Stop answered %q", got)
	}
}
