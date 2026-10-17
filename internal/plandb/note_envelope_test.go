package plandb

// Tests for the envelope page. Each runs on a real store and the real read
// handle. The ones that matter compare it with NotesPage on the SAME rows: a
// note NotesPage refuses for a column the envelope does not read is delivered
// by the envelope, a note either refuses for a column both read is refused by
// both, and everything else — the cursor, the task, the bounds, the lock — is
// the same answer from either, because both are one page over different columns.

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func envelopeIDs(page NoteEnvelopePage) []string {
	var ids []string
	for _, note := range page.Notes {
		ids = append(ids, note.ID)
	}
	return ids
}

// The tags and the time are not read: a project or chat a hundred kilobytes
// long, or a time that is not one, does not stop the envelope page, and the
// full page on the same rows is the control that it stops there.
func TestNoteEnvelopesPageDeliversPastWhatItDoesNotRead(t *testing.T) {
	for _, c := range []struct {
		column string
		value  string
	}{
		{"project", strings.Repeat("p", 100<<10)},
		{"chat", strings.Repeat("c", 100<<10)},
		{"at", strings.Repeat("a", NotePageFixedMax+1)},
		{"at", "not a time"},
	} {
		path := filepath.Join(t.TempDir(), "plan.db")
		store := planOpen(t, path)
		ids := seedThree(t, store)
		rawExec(t, path, `UPDATE notes SET `+c.column+` = ? WHERE id = ?`, c.value, ids[1])

		if _, err := store.NotesPage(context.Background(), NotePageAsk{TaskID: "a", After: ids[0], Limit: 5}); err == nil {
			t.Fatalf("%s: the full page read the damaged row; the control proves nothing", c.column)
		}
		page, err := store.NoteEnvelopesPage(context.Background(), NoteEnvelopeAsk{TaskID: "a", Limit: 5})
		if err != nil || !sameIDs(envelopeIDs(page), ids) || page.More || page.Cursor != ids[2] {
			t.Fatalf("%s: envelope page %+v err %v; want all of %v", c.column, page, err, ids)
		}
		store.Close()
	}
}

// The envelope carries the note's identity apart from the sender's key, its
// direction, its author and its words, and the caller's own task id.
func TestNoteEnvelopesPageCarriesWhatAnInboxReads(t *testing.T) {
	store := planOpen(t, "")
	defer store.Close()
	planAdd(t, store, planSpec("a", "Alpha"))
	person, err := store.AddPersonNoteKeyed("a", "direction", "surface-key")
	if err != nil {
		t.Fatal(err)
	}
	worker, err := store.AddNote("a", "agent-7", "a worker's finding")
	if err != nil {
		t.Fatal(err)
	}
	page, err := store.NoteEnvelopesPage(context.Background(), NoteEnvelopeAsk{TaskID: "a", Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	want := []NoteEnvelope{
		{ID: person.ID, TaskID: "a", Agent: "", From: NoteFromPerson, Body: "direction", ClientID: "surface-key"},
		{ID: worker.ID, TaskID: "a", Agent: "agent-7", From: NoteFromWorker, Body: "a worker's finding"},
	}
	if len(page.Notes) != len(want) || page.Notes[0] != want[0] || page.Notes[1] != want[1] {
		t.Fatalf("envelopes = %+v, want %+v", page.Notes, want)
	}
	if person.ID == "surface-key" {
		t.Fatal("the store's note id is the sender's key")
	}
}

// A column both pages read, over its ceiling, is refused by both the same way:
// the notes before it first, the cursor before it, then the typed refusal on
// every call from there, and never a truncated or skipped row.
func TestNoteEnvelopesPageRefusesEachColumnItReadsOverItsCeiling(t *testing.T) {
	for _, c := range []struct {
		column string
		size   int
		field  string
	}{
		{"body", NotePageBodyMax + 1, "body"},
		{"agent", NotePageMetaMax + 1, "agent"},
		{"client_id", NotePageMetaMax + 1, "client_id"},
		{`"from"`, NotePageFixedMax + 1, "from"},
	} {
		path := filepath.Join(t.TempDir(), "plan.db")
		store := planOpen(t, path)
		ids := seedThree(t, store)
		rawExec(t, path, `UPDATE notes SET `+c.column+` = ? WHERE id = ?`, strings.Repeat("z", c.size), ids[1])

		first, err := store.NoteEnvelopesPage(context.Background(), NoteEnvelopeAsk{TaskID: "a", Limit: 5})
		if err != nil || !sameIDs(envelopeIDs(first), ids[:1]) || !first.More || first.Cursor != ids[0] {
			t.Fatalf("%s: first page %+v err %v; want the note before it, More and the cursor on it", c.column, first, err)
		}
		for call := 0; call < 3; call++ {
			page, err := store.NoteEnvelopesPage(context.Background(), NoteEnvelopeAsk{TaskID: "a", After: first.Cursor, Limit: 5})
			var refused *NoteTooLargeError
			if !errors.As(err, &refused) || refused.Field != c.field || refused.Size != int64(c.size) || len(page.Notes) != 0 || page.Cursor != "" || len(err.Error()) > 200 {
				t.Fatalf("%s call %d: page %+v err %v; want a bounded refusal naming it and no page", c.column, call, page, err)
			}
		}
		_, full := store.NotesPage(context.Background(), NotePageAsk{TaskID: "a", After: ids[0], Limit: 5})
		if !errors.Is(full, ErrNoteTooLarge) {
			t.Fatalf("%s: the full page answered %v; want the same refusal", c.column, full)
		}
		store.Close()
	}

	path := filepath.Join(t.TempDir(), "plan.db")
	store := planOpen(t, path)
	defer store.Close()
	ids := seedThree(t, store)
	rawExec(t, path, `UPDATE notes SET id = ? WHERE id = ?`, strings.Repeat("i", 100<<10), ids[2])
	_, err := store.NoteEnvelopesPage(context.Background(), NoteEnvelopeAsk{TaskID: "a", After: ids[1], Limit: 5})
	var refused *NoteTooLargeError
	if !errors.As(err, &refused) || refused.Field != "id" || refused.NoteID != "" || len(err.Error()) > 200 {
		t.Fatalf("an oversized id: err = %v, want a refusal that does not echo it", err)
	}
}

// NoteEnvelopeRowMax is the sum of the ceilings of the columns the envelope
// reads, and a row at every one of them fits a budget of exactly that and no
// less, so the page always makes progress.
func TestNoteEnvelopeRowMaxIsTheSumOfItsColumnCeilings(t *testing.T) {
	sum := 0
	for _, col := range envelopeCols {
		switch col.name {
		case "project", "chat", "at":
			t.Fatalf("the envelope reads %s", col.name)
		}
		sum += col.max
	}
	if sum != NoteEnvelopeRowMax {
		t.Fatalf("NoteEnvelopeRowMax = %d, the column ceilings sum to %d", NoteEnvelopeRowMax, sum)
	}

	path := filepath.Join(t.TempDir(), "plan.db")
	store := planOpen(t, path)
	defer store.Close()
	ids := seedThree(t, store)
	rawExec(t, path, `UPDATE notes SET id = ?, agent = ?, body = ?, "from" = ?, client_id = ? WHERE id = ?`,
		strings.Repeat("i", NotePageFixedMax), strings.Repeat("a", NotePageMetaMax), strings.Repeat("b", NotePageBodyMax),
		strings.Repeat("f", NotePageFixedMax), strings.Repeat("k", NotePageMetaMax), ids[0])
	page, err := store.NoteEnvelopesPage(context.Background(), NoteEnvelopeAsk{TaskID: "a", Limit: 1, MaxBytes: NoteEnvelopeRowMax})
	if err != nil || len(page.Notes) != 1 || len(page.Notes[0].Body) != NotePageBodyMax {
		t.Fatalf("a row at every ceiling under a budget of its own size: page %+v err %v", page, err)
	}
	if _, err := store.NoteEnvelopesPage(context.Background(), NoteEnvelopeAsk{TaskID: "a", Limit: 1, MaxBytes: NoteEnvelopeRowMax - 1}); err == nil {
		t.Fatal("a budget below the largest row was accepted")
	}
}

// The envelope page is the full page over fewer columns: the same answer to the
// same ask for every cursor, task, bound and Skip, the same errors and the same
// More. A page that drifted from NotesPage in any of them would show here.
func TestNoteEnvelopesPageAnswersLikeNotesPage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plan.db")
	store := planOpen(t, path)
	defer store.Close()
	planAdd(t, store, planSpec("a", "Alpha"), planSpec("b", "Beta"))
	ids := seedInterleaved(t, store, 12)
	other, err := store.AddNote("b", "w", "elsewhere")
	if err != nil {
		t.Fatal(err)
	}
	skipEven := func(id string) bool {
		return strings.HasSuffix(id, "0") || strings.HasSuffix(id, "2") || strings.HasSuffix(id, "4")
	}

	for name, ask := range map[string]NotePageAsk{
		"first page":       {TaskID: "a", Limit: 5},
		"after a note":     {TaskID: "a", After: ids[4], Limit: 5},
		"the last":         {TaskID: "a", After: ids[11], Limit: 5},
		"small budget":     {TaskID: "a", Limit: 100, MaxBytes: NotePageRowMax},
		"scan bound":       {TaskID: "a", Limit: 5, ScanMax: 2, Skip: func(Note) bool { return true }},
		"a skip":           {TaskID: "a", Limit: 3, Skip: func(n Note) bool { return skipEven(n.ID) }},
		"unknown cursor":   {TaskID: "a", After: "n-nope", Limit: 5},
		"foreign cursor":   {TaskID: "a", After: other.ID, Limit: 5},
		"long cursor":      {TaskID: "a", After: strings.Repeat("c", NotePageFixedMax+1), Limit: 5},
		"unknown task":     {TaskID: "nope", Limit: 5},
		"empty task":       {Limit: 5},
		"padded task":      {TaskID: strings.Repeat(" ", NotePageTaskIDMax+1), Limit: 5},
		"limit zero":       {TaskID: "a"},
		"limit too large":  {TaskID: "a", Limit: NotePageMax + 1},
		"scan max too big": {TaskID: "a", Limit: 5, ScanMax: NotePageScanMax + 1},
	} {
		full, fullErr := store.NotesPage(context.Background(), ask)
		envelopeAsk := NoteEnvelopeAsk{TaskID: ask.TaskID, After: ask.After, Limit: ask.Limit, MaxBytes: ask.MaxBytes, ScanMax: ask.ScanMax, ScanBytes: ask.ScanBytes}
		if ask.Skip != nil {
			skip := ask.Skip
			envelopeAsk.Skip = func(n NoteEnvelope) bool { return skip(Note{ID: n.ID, Agent: n.Agent, Body: n.Body, From: n.From}) }
		}
		page, err := store.NoteEnvelopesPage(context.Background(), envelopeAsk)
		if (fullErr == nil) != (err == nil) || (fullErr != nil && fullErr.Error() != err.Error()) {
			t.Fatalf("%s: full page err %v, envelope page err %v; want the same answer", name, fullErr, err)
		}
		var fullIDs []string
		for _, note := range full.Notes {
			fullIDs = append(fullIDs, note.ID)
		}
		if !sameIDs(envelopeIDs(page), fullIDs) || page.Cursor != full.Cursor || page.More != full.More {
			t.Fatalf("%s: envelope %v cursor %q more %v; full %v cursor %q more %v", name, envelopeIDs(page), page.Cursor, page.More, fullIDs, full.Cursor, full.More)
		}
	}
}

// A closed store, a cancelled context and a duplicated id are the same
// non-pages from the envelope page as from the full one.
func TestNoteEnvelopesPageSharesTheLockContextAndDuplicateRules(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plan.db")
	store := planOpen(t, path)
	ids := seedThree(t, store)

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if page, err := store.NoteEnvelopesPage(cancelled, NoteEnvelopeAsk{TaskID: "a", Limit: 5}); !errors.Is(err, context.Canceled) || len(page.Notes) != 0 {
		t.Fatalf("a cancelled context: page %+v err %v", page, err)
	}

	rawExec(t, path, `INSERT INTO notes (seq, id, task_id, agent, body, at, project, chat, "from", client_id)
		SELECT 100, id, task_id, agent, body, at, project, chat, "from", client_id FROM notes WHERE id = ?`, ids[1])
	if page, err := store.NoteEnvelopesPage(context.Background(), NoteEnvelopeAsk{TaskID: "a", After: ids[1], Limit: 5}); !errors.Is(err, ErrDuplicateNoteID) || !errors.Is(err, ErrNoteCursor) || len(page.Notes) != 0 {
		t.Fatalf("a duplicate cursor: page %+v err %v", page, err)
	}

	store.Close()
	if page, err := store.NoteEnvelopesPage(context.Background(), NoteEnvelopeAsk{TaskID: "a", Limit: 5}); !errors.Is(err, ErrClosed) || len(page.Notes) != 0 {
		t.Fatalf("a closed store: page %+v err %v; want ErrClosed and no page", page, err)
	}
}
