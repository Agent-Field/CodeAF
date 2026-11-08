package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// personStopFloor is a floor like the third live run's: a handful of jobs,
// each a document of tens of kilobytes (a long thread, a long activity), one
// of them managed by talk.
func personStopFloor(tb testing.TB, jobs int) (*store.Store, string) {
	tb.Helper()
	st, err := store.Open(tb.TempDir())
	if err != nil {
		tb.Fatal(err)
	}
	talk := filepath.Join(tb.TempDir(), "manager", ".cell", "transcript.jsonl")
	for n := range jobs {
		it := factory.Item{Title: fmt.Sprintf("job %d", n), Repo: "acme/api", State: factory.StateRunning, Talk: filepath.Join(tb.TempDir(), "other.jsonl")}
		if n == jobs-1 {
			it.Talk = talk
		}
		for i := range 120 {
			it.Thread = append(it.Thread, factory.Message{From: "manager", Text: strings.Repeat("a step said something worth keeping ", 6), At: time.Now(), ID: fmt.Sprintf("line_%d_%d", n, i)})
			it.Activity = append(it.Activity, factory.Event{At: time.Now(), What: strings.Repeat("activity ", 10)})
		}
		if _, err := st.Create(it); err != nil {
			tb.Fatal(err)
		}
	}
	return st, talk
}

// AN IDLE MANAGER READS NOTHING IT ALREADY READ. Every job's manager checks
// ten times a second whether its person stopped the job (watchJobBudget), and
// each check listed and decoded EVERY job document on the floor: the third live
// run's engine sat at 145–188% of a core with nothing running. A check whose
// job file has not changed since the last one decodes nothing; a write is seen
// at the next check.
func TestAnIdleManagersStopCheckDecodesNothingUnchanged(t *testing.T) {
	st, talk := personStopFloor(t, 6)
	door := seamRunDoor{st: st}
	// The first check finds the job by listing the floor; the second reads
	// that one job by id and remembers the file it came from.
	for range 2 {
		if why := door.AutomaticRefusal(talk); why != "" {
			t.Fatalf("a running job refuses: %q", why)
		}
	}
	before := factoryStopCheckDecodes.Load()
	for range 50 {
		if why := door.AutomaticRefusal(talk); why != "" {
			t.Fatalf("a running job refuses: %q", why)
		}
	}
	if got := factoryStopCheckDecodes.Load() - before; got != 0 {
		t.Fatalf("50 checks of an unchanged job decoded %d job documents", got)
	}
	// A person's Stop is seen at the very next check.
	it := managedBy(t, st, talk)
	if err := st.Update(it.ID, func(it *factory.Item) error { factory.MarkPersonStopped(it); return nil }); err != nil {
		t.Fatal(err)
	}
	if why := door.AutomaticRefusal(talk); why != factory.PersonStopReason {
		t.Fatalf("the stop was not seen: %q", why)
	}
	// A conversation that manages nothing is not a reason to read the floor
	// again on every check either.
	stranger := filepath.Join(t.TempDir(), "nobody.jsonl")
	_ = door.AutomaticRefusal(stranger)
	before = factoryStopCheckDecodes.Load()
	for range 50 {
		_ = door.AutomaticRefusal(stranger)
	}
	if got := factoryStopCheckDecodes.Load() - before; got != 0 {
		t.Fatalf("50 checks by a conversation that manages nothing decoded %d documents", got)
	}
}

func managedBy(t *testing.T, st *store.Store, talk string) factory.Item {
	t.Helper()
	items, err := st.List()
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		if it.Talk == talk {
			return it
		}
	}
	t.Fatal("no job is managed by talk")
	return factory.Item{}
}

// BenchmarkAutomaticRefusal is one idle manager's stop check on a six-job
// floor: PERF.md's "Idle factory manager checks" states the before and after.
func BenchmarkAutomaticRefusal(b *testing.B) {
	st, talk := personStopFloor(b, 6)
	door := seamRunDoor{st: st}
	_ = door.AutomaticRefusal(talk)
	_ = door.AutomaticRefusal(talk)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = door.AutomaticRefusal(talk)
	}
}
