package store

import (
	"context"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// THE STORE WRITES THE ACTIVITY: a read the triage made, a conversation made
// for the item and stages the plan changed each append their event on the
// write that made them, and a write that changed none of them appends nothing.
func TestTheStoreNotesReadsTalksAndStageChanges(t *testing.T) {
	st := open(t)
	id, err := st.Add(context.Background(), factory.Item{Title: "budget caps", Repo: "codeaf"})
	if err != nil {
		t.Fatal(err)
	}
	steps := []func(*factory.Item){
		func(it *factory.Item) { it.Cap = 4 },
		func(it *factory.Item) { it.Triage.Read, it.Triage.TriagedAt = "a read", time.Now() },
		func(it *factory.Item) { it.Talk = "/tmp/talk.jsonl" },
		func(it *factory.Item) { it.Talk = "/tmp/talk.jsonl" },
		func(it *factory.Item) { it.Adapted = append(it.Adapted, "plan added security", "why: auth") },
	}
	for i, step := range steps {
		write := st.Update
		if i == 1 {
			write = st.Annotate
		}
		if err := write(id, func(it *factory.Item) error { step(it); return nil }); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := st.Get(id)
	var whats []string
	for _, e := range got.Activity {
		if e.At.IsZero() {
			t.Fatalf("an event has no time: %+v", e)
		}
		whats = append(whats, e.What)
	}
	want := []string{factory.EventRead, factory.EventTalked, "stages changed by plan"}
	if !reflect.DeepEqual(whats, want) {
		t.Fatalf("activity %q, want %q", whats, want)
	}
}

// AT MOST TWENTY EVENTS ARE KEPT, and the oldest go first.
func TestActivityKeepsTheLastTwenty(t *testing.T) {
	var it factory.Item
	at := time.Date(2026, 10, 8, 9, 0, 0, 0, time.UTC)
	for i := 0; i < factory.ActivityMost+5; i++ {
		it.Note(at.Add(time.Duration(i)*time.Minute), "event "+strconv.Itoa(i))
	}
	it.Note(at, "  ")
	if len(it.Activity) != factory.ActivityMost {
		t.Fatalf("kept %d events", len(it.Activity))
	}
	if it.Activity[0].What != "event 5" || it.Activity[factory.ActivityMost-1].What != "event 24" {
		t.Fatalf("kept %q .. %q, want the newest twenty", it.Activity[0].What, it.Activity[factory.ActivityMost-1].What)
	}
}

// The fixture's new fields (page, comments, files, check runs, activity) come
// back from the store exactly as they went in.
func TestTheFixtureRoundTripsTheSourceFields(t *testing.T) {
	st := open(t)
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	checked := 0
	for _, it := range factory.Fixture(now).Items {
		want := it
		it.ID = 0
		made, err := st.Create(it)
		if err != nil {
			t.Fatal(err)
		}
		got, err := st.Get(made.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.URL != want.URL || !sameJSON(t, got.Comments, want.Comments) || !sameJSON(t, got.Files, want.Files) ||
			!sameJSON(t, got.CheckRuns, want.CheckRuns) || !sameJSON(t, got.Activity, want.Activity) {
			t.Fatalf("%s did not round-trip:\n got %+v\nwant %+v", want.Ref(), got, want)
		}
		if want.Num == 1662 {
			if len(got.Files) == 0 || len(got.CheckRuns) == 0 || len(got.Comments) != 2 || len(got.Activity) != 4 || got.URL == "" {
				t.Fatalf("#1662 lost what the page draws: %+v", got)
			}
			checked++
		}
		if want.Num == 1538 {
			if got.URL == "" || len(got.Comments) != 1 {
				t.Fatalf("#1538 lost its page or comment: %+v", got)
			}
			checked++
		}
	}
	if checked != 2 {
		t.Fatalf("the fixture has %d of the two enriched items", checked)
	}
}

func sameJSON[T any](t *testing.T, a, b []T) bool {
	t.Helper()
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !reflect.DeepEqual(normal(a[i]), normal(b[i])) {
			return false
		}
	}
	return true
}

// normal strips a time's location and monotonic reading, which a JSON round
// trip does not keep and nobody draws.
func normal(v any) any {
	switch x := v.(type) {
	case factory.Comment:
		x.At = x.At.UTC().Round(0)
		return x
	case factory.Event:
		x.At = x.At.UTC().Round(0)
		return x
	}
	return v
}

// WHAT IS IN FLIGHT is written and cleared per item and for the floor, an
// entry whose process died reads as nothing, and a process starting clears
// the dead ones.
func TestBusyMarksAndTheirDeadProcesses(t *testing.T) {
	st := open(t)
	if items, all, err := st.Busy(); err != nil || items != nil || all != "" {
		t.Fatalf("a fresh floor is busy: %v %q %v", items, all, err)
	}
	_ = st.SetBusy(3, factory.BusyReading)
	_ = st.SetBusy(4, factory.BusyRefreshing)
	_ = st.SetBusyAll("refreshing 8 items · 3 done")
	items, all, _ := st.Busy()
	if items[3] != "reading" || items[4] != "refreshing" || all != "refreshing 8 items · 3 done" {
		t.Fatalf("busy read %v %q", items, all)
	}
	_ = st.SetBusy(3, "")
	_ = st.SetBusyAll("")
	if items, all, _ = st.Busy(); len(items) != 1 || all != "" {
		t.Fatalf("clearing left %v %q", items, all)
	}

	// The process that wrote item 4's mark dies.
	alive = func(int) bool { return false }
	t.Cleanup(func() { alive = defaultAlive })
	if items, _, _ = st.Busy(); len(items) != 0 {
		t.Fatalf("a dead process's mark is drawn: %v", items)
	}
	alive = defaultAlive
	if err := st.ClearBusy(); err != nil {
		t.Fatal(err)
	}
	if items, _, _ = st.Busy(); len(items) != 0 {
		t.Fatalf("a process starting kept its own stale mark: %v", items)
	}
}

// The read's cost is the last one and the average of every priced one, and
// the local seam's Load carries the last one and what is in flight.
func TestReadCostAndBusyReachTheSnapshot(t *testing.T) {
	st := open(t)
	if last, avg, _ := st.ReadCost(); last != 0 || avg != 0 {
		t.Fatalf("no read priced, yet %v %v", last, avg)
	}
	_ = st.NoteReadCost(0.0002)
	_ = st.NoteReadCost(0.0004)
	last, avg, _ := st.ReadCost()
	if last != 0.0004 || avg < 0.000299 || avg > 0.000301 {
		t.Fatalf("cost %v avg %v", last, avg)
	}
	id, _ := st.Add(context.Background(), factory.Item{Title: "x", Repo: "r"})
	_ = st.SetBusy(id, factory.BusyReading)
	snap, err := factory.LocalSeam(st, time.Now()).Load()
	if err != nil {
		t.Fatal(err)
	}
	if snap.LastReadCost != 0.0004 || snap.Busy[id] != "reading" || snap.BusyAll != "" {
		t.Fatalf("snapshot %v %v %q", snap.LastReadCost, snap.Busy, snap.BusyAll)
	}
}
