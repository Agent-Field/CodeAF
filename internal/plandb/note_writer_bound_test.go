package plandb

// Tests for the note writer keeping to what a page will read. A page refuses
// a note whose author or client key is over NotePageMetaMax and the readers
// hold every later note behind it, so the writer must refuse the same notes —
// refuse, never truncate — before anything is written.

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// The author and the client key have the page's ceiling exactly: the ceiling
// itself is written and read back whole, one byte more is refused with nothing
// stored, nothing truncated and no id spent, and the refusal holds across a
// reopen. An author is measured as it is stored, trimmed.
func TestNoteWriterRefusesWhatAPageWouldRefuse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plan.db")
	store := planOpen(t, path)
	planAdd(t, store, planSpec("a", "Alpha"))

	atCeiling := strings.Repeat("g", NotePageMetaMax)
	first, err := store.AddNote("a", " "+atCeiling+" ", "at the ceiling, padded")
	if err != nil {
		t.Fatalf("an author of exactly %d bytes (padded with spaces) was refused: %v", NotePageMetaMax, err)
	}
	keyed, err := store.AddPersonNoteKeyed("a", "keyed at the ceiling", atCeiling)
	if err != nil {
		t.Fatalf("a client key of exactly %d bytes was refused: %v", NotePageMetaMax, err)
	}

	over := strings.Repeat("g", NotePageMetaMax+1)
	if _, err := store.AddNote("a", over, "over"); err == nil {
		t.Fatalf("an author of %d bytes was accepted", len(over))
	}
	if _, err := store.AddNote("a", "  "+over+"  ", "over, padded"); err == nil {
		t.Fatalf("an author of %d bytes plus padding was accepted", len(over))
	}
	if _, err := store.AddPersonNoteKeyed("a", "over", over); err == nil {
		t.Fatalf("a client key of %d bytes was accepted", len(over))
	}
	next, err := store.AddNote("a", "w", "after the refusals")
	if err != nil {
		t.Fatal(err)
	}

	for name, handle := range map[string]*Store{"same handle": store, "reopened": nil} {
		if handle == nil {
			store.Close()
			handle = planReopen(t, path)
			defer handle.Close()
		}
		got := handle.Notes("a", 0)
		if len(got) != 3 || got[0].ID != first.ID || got[1].ID != keyed.ID || got[2].ID != next.ID {
			t.Fatalf("%s: notes = %#v, want only the three accepted ones", name, got)
		}
		if got[0].Agent != atCeiling || got[1].ClientID != atCeiling {
			t.Fatalf("%s: the ceiling values were cut: agent %d bytes, key %d bytes", name, len(got[0].Agent), len(got[1].ClientID))
		}
	}
}

// A refusal spends nothing: the next note's id follows the last accepted one
// with no gap, which a transaction begun and rolled back would not guarantee.
func TestNoteWriterRefusalSpendsNoNoteID(t *testing.T) {
	store := planOpen(t, "")
	defer store.Close()
	planAdd(t, store, planSpec("a", "Alpha"))
	before, err := store.AddNote("a", "w", "one")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AddNote("a", strings.Repeat("g", NotePageMetaMax+1), "refused"); err == nil {
		t.Fatal("the over-long author was accepted")
	}
	after, err := store.AddNote("a", "w", "two")
	if err != nil {
		t.Fatal(err)
	}
	var b, a int
	if _, err := fmt.Sscanf(before.ID, "n-%x", &b); err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Sscanf(after.ID, "n-%x", &a); err != nil {
		t.Fatal(err)
	}
	if a != b+1 {
		t.Fatalf("ids %q then %q: the refusal spent an id", before.ID, after.ID)
	}
}

// Every note the writer accepts is one a page reads: the boundary author and
// key come back through NotesPage whole and unrefused.
func TestNoteWriterCeilingNotesAreReadByAPage(t *testing.T) {
	store := planOpen(t, "")
	defer store.Close()
	planAdd(t, store, planSpec("a", "Alpha"))
	atCeiling := strings.Repeat("g", NotePageMetaMax)
	agent, err := store.AddNote("a", atCeiling, strings.Repeat("b", NotePageBodyMax))
	if err != nil {
		t.Fatal(err)
	}
	keyed, err := store.AddPersonNoteKeyed("a", "keyed", atCeiling)
	if err != nil {
		t.Fatal(err)
	}
	page, err := store.NotesPage(context.Background(), NotePageAsk{TaskID: "a", Limit: 5})
	if err != nil || len(page.Notes) != 2 || page.More || page.Notes[0].ID != agent.ID || page.Notes[1].ID != keyed.ID {
		t.Fatalf("a page of the ceiling notes: %#v err %v", page, err)
	}
}

// The CLI door is the one a model reaches: --agent and PLANDB_AGENT both go
// through the same writer, so an over-long author is an error and no note, and
// an author of exactly the ceiling is a note.
func TestPlandbCliNoteRefusesAnOverlongAgent(t *testing.T) {
	h := cliNewHarness(t)
	h.cliInitFresh()
	h.cliAdd("A", "a", "--description", "part a")
	atCeiling := strings.Repeat("g", NotePageMetaMax)
	over := strings.Repeat("g", NotePageMetaMax+1)

	cliWantCode(t, h.run("--db", h.db, "task", "note", "t-a", "at the ceiling", "--agent", atCeiling), 0)
	code := h.run("--db", h.db, "task", "note", "t-a", "over by flag", "--agent", over)
	cliWantError(t, h, code, "note author exceeds")

	t.Setenv("PLANDB_AGENT", over)
	code = h.run("--db", h.db, "task", "note", "t-a", "over by environment")
	cliWantError(t, h, code, "note author exceeds")
	t.Setenv("PLANDB_AGENT", "")

	cliWantCode(t, h.run("--db", h.db, "--json", "task", "notes", "t-a"), 0)
	if out := h.out.String(); !strings.Contains(out, "at the ceiling") || strings.Contains(out, "over by") {
		t.Fatalf("the notes after the refusals:\n%.300s", out)
	}
}
