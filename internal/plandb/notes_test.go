package plandb

// Tests for the two additions to notes: the `from` column that tells a
// worker's note from the person's, and the Changed read a waiting worker's
// wake uses to learn which tasks moved.

import (
	"strings"
	"testing"
	"time"
)

// A note records who left it. AddNote is a worker's, AddPersonNote is the
// person's, and both read back through the one Notes door.
func TestPlandbCliPersonNotesReadBackBesideWorkerNotes(t *testing.T) {
	store := planOpen(t, "")
	planAdd(t, store, planSpec("a", "Alpha"))

	if _, err := store.AddNote("a", "w1", "worker handoff"); err != nil {
		t.Fatalf("worker note: %v", err)
	}
	if _, err := store.AddPersonNote("a", "please re-check the schema"); err != nil {
		t.Fatalf("person note: %v", err)
	}
	notes := store.Notes("a", 0)
	if len(notes) != 2 {
		t.Fatalf("notes = %#v, want a worker's then a person's", notes)
	}
	if notes[0].From != NoteFromWorker || notes[0].Agent != "w1" {
		t.Fatalf("worker note = %#v, want from worker with its agent", notes[0])
	}
	if notes[1].From != NoteFromPerson || notes[1].Agent != "" {
		t.Fatalf("person note = %#v, want from person with no agent", notes[1])
	}
	// A person note still names a real task.
	if _, err := store.AddPersonNote("ghost", "nowhere"); err == nil {
		t.Fatal("a person note on an unknown task was accepted")
	}
}

// The CLI prints the person's note with its `person:` prefix, in both the
// notes listing and the task card, beside the worker's attributed note.
func TestPlandbCliPrintsPersonNotesWithAPrefix(t *testing.T) {
	h := cliNewHarness(t)
	st, err := Open(h.db, "demo", "root", "demo", "")
	if err != nil {
		t.Fatalf("seed store: %v", err)
	}
	if _, err := st.AddMany([]TaskSpec{planSpec("a", "Alpha")}); err != nil {
		t.Fatalf("add: %v", err)
	}
	if _, err := st.AddNote("a", "w1", "worker handoff"); err != nil {
		t.Fatalf("worker note: %v", err)
	}
	if _, err := st.AddPersonNote("a", "please re-check the schema"); err != nil {
		t.Fatalf("person note: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	code := h.run("--db", h.db, "task", "notes", "t-a")
	cliWantCode(t, code, 0)
	if !strings.Contains(h.out.String(), "person: please re-check the schema") {
		t.Fatalf("task notes missed the person prefix:\n%s", h.out.String())
	}
	if !strings.Contains(h.out.String(), "[w1] worker handoff") {
		t.Fatalf("task notes lost the worker attribution:\n%s", h.out.String())
	}
	code = h.run("--db", h.db, "show", "t-a")
	cliWantCode(t, code, 0)
	if !strings.Contains(h.out.String(), "notes:") || !strings.Contains(h.out.String(), "person: please re-check the schema") {
		t.Fatalf("show missed the person note:\n%s", h.out.String())
	}
}

// Changed names every task whose row, notes or scoped context moved after a
// moment, and nothing else: it is the whole of the wake a waiting worker
// reads.
func TestPlandbCliChangedNamesTheTasksTouchedSinceAMoment(t *testing.T) {
	store := planOpen(t, "")
	clock := time.Now().UTC()
	store.now = func() time.Time { return clock }

	planAdd(t, store, planSpec("a", "A"), planSpec("b", "B"))
	// Changed is strict: exactly at the write moment, nothing has moved since.
	since := clock
	if got := store.Changed(since); len(got) != 0 {
		t.Fatalf("Changed at the write moment = %v, want nothing", got)
	}

	clock = clock.Add(time.Minute)
	if _, err := store.AddNote("a", "w", "handoff for the next worker"); err != nil {
		t.Fatalf("note: %v", err)
	}
	if _, err := store.AddContext("b", "decision", "chose sqlite"); err != nil {
		t.Fatalf("context: %v", err)
	}
	if got := store.Changed(since); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("Changed after a note and a scoped context = %v, want [a b]", got)
	}

	// A run-wide context has no task to name, so it moves nothing.
	prev := clock
	clock = clock.Add(time.Minute)
	if _, err := store.AddContext("", "discovery", "the run learned something"); err != nil {
		t.Fatalf("context: %v", err)
	}
	if got := store.Changed(prev); len(got) != 0 {
		t.Fatalf("Changed after a run-wide context = %v, want nothing", got)
	}
	// A task born after the moment counts, because its row is newer; and so
	// does its parent, because adding a child moves the parent's own row. The
	// two tasks that did not move are not named.
	if _, err := store.AddMany([]TaskSpec{planSpec("c", "C")}); err != nil {
		t.Fatalf("add: %v", err)
	}
	got := store.Changed(prev)
	if len(got) != 2 || got[1] != "c" || got[0] != "root" {
		t.Fatalf("Changed after a new task = %v, want the run root and [c]", got)
	}
}
