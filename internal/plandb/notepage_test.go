package plandb

// Tests for the bounded note page. Every one runs against a real store on a
// temp file and the real read handle; there is no model, no sleep and no
// stubbed query. The cases are the ones the 50-note window hid: the fifty-first
// and two-hundred-first note, other tasks' notes between them, a task deleted
// under a cursor, a store without the client_id column, a cancelled or closed
// store, and notes as large as the store allows.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// seedInterleaved adds n notes to task a, each followed by one to task b, and
// returns a's ids in mint order. The interleave is the point: a's rows are
// never adjacent in the table.
func seedInterleaved(t *testing.T, store *Store, n int) []string {
	t.Helper()
	ids := make([]string, 0, n)
	for i := 0; i < n; i++ {
		note, err := store.AddNote("a", "w", fmt.Sprintf("a-%d", i))
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, note.ID)
		if _, err := store.AddNote("b", "w", fmt.Sprintf("b-%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	return ids
}

// walkNotePages follows Cursor until More is false and returns every id seen.
// The iteration bound makes a cursor that does not advance a failure and not a
// hang.
func walkNotePages(t *testing.T, store *Store, ask NotePageAsk) []string {
	t.Helper()
	var ids []string
	for i := 0; i < 2000; i++ {
		page, err := store.NotesPage(context.Background(), ask)
		if err != nil {
			t.Fatalf("page %d after %q: %v", i, ask.After, err)
		}
		for _, note := range page.Notes {
			ids = append(ids, note.ID)
		}
		if !page.More {
			return ids
		}
		if page.Cursor == ask.After {
			t.Fatalf("page %d did not advance past %q", i, ask.After)
		}
		ask.After = page.Cursor
	}
	t.Fatal("the walk did not end")
	return nil
}

func sameIDs(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// The fifty-first and two-hundred-first note are reached, every note exactly
// once and in mint order, whatever the page size, with another task's notes
// between them.
func TestNotesPageReachesNote51And201OnceInOrderBesideAnotherTasksNotes(t *testing.T) {
	for _, n := range []int{51, 201} {
		for _, limit := range []int{1, 5, NotePageMax} {
			t.Run(fmt.Sprintf("%d notes, pages of %d", n, limit), func(t *testing.T) {
				store := planOpen(t, "")
				defer store.Close()
				planAdd(t, store, planSpec("a", "Alpha"), planSpec("b", "Beta"))
				want := seedInterleaved(t, store, n)
				got := walkNotePages(t, store, NotePageAsk{TaskID: "a", Limit: limit})
				if !sameIDs(got, want) {
					t.Fatalf("walked %d ids, want %d in mint order\ngot  %v\nwant %v", len(got), len(want), got, want)
				}
				if last := got[len(got)-1]; last != want[n-1] {
					t.Fatalf("last note = %q, want %q", last, want[n-1])
				}
			})
		}
	}
}

// A page carries the whole note, including the sender's key, and the last page
// says there is nothing more.
func TestNotesPageCarriesTheWholeNoteAndEndsWithMoreFalse(t *testing.T) {
	store := planOpen(t, "")
	defer store.Close()
	planAdd(t, store, planSpec("a", "Alpha"))
	keyed, err := store.AddPersonNoteKeyed("a", "hello", "cid-1")
	if err != nil {
		t.Fatal(err)
	}
	page, err := store.NotesPage(context.Background(), NotePageAsk{TaskID: "a", Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Notes) != 1 || page.More || page.Cursor != keyed.ID {
		t.Fatalf("page = %#v, want one note, no more, cursor %q", page, keyed.ID)
	}
	got := page.Notes[0]
	if got.ID != keyed.ID || got.Body != "hello" || got.ClientID != "cid-1" || got.From != NoteFromPerson || got.TaskID != "a" || got.At.IsZero() {
		t.Fatalf("note = %#v, want the stored person note %#v", got, keyed)
	}
	tail, err := store.NotesPage(context.Background(), NotePageAsk{TaskID: "a", After: page.Cursor, Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(tail.Notes) != 0 || tail.More || tail.Cursor != page.Cursor {
		t.Fatalf("page after the last note = %#v, want empty, no more, the cursor sent", tail)
	}
}

// A Skip drops notes before they count toward the limit: a hundred and ninety
// own notes cannot starve the five foreign ones behind them.
func TestNotesPageSkipRunsBeforeTheBatchBound(t *testing.T) {
	store := planOpen(t, "")
	defer store.Close()
	planAdd(t, store, planSpec("a", "Alpha"), planSpec("b", "Beta"))
	ids := seedInterleaved(t, store, 201)
	own := map[string]bool{}
	for _, id := range ids[:190] {
		own[id] = true
	}
	page, err := store.NotesPage(context.Background(), NotePageAsk{TaskID: "a", Limit: 5, Skip: func(n Note) bool { return own[n.ID] }})
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(page.Notes))
	for _, note := range page.Notes {
		got = append(got, note.ID)
	}
	if !sameIDs(got, ids[190:195]) || page.Cursor != ids[194] || !page.More {
		t.Fatalf("page = %v cursor %q more %v, want notes 191..195", got, page.Cursor, page.More)
	}
}

// A call that examines only skipped rows still moves the cursor, and no call
// examines more than ScanMax rows, so the two-hundred-first note arrives by
// continuation and never by an unbounded call.
func TestNotesPageScanBoundMovesTheCursorThroughSkippedRows(t *testing.T) {
	store := planOpen(t, "")
	defer store.Close()
	planAdd(t, store, planSpec("a", "Alpha"), planSpec("b", "Beta"))
	ids := seedInterleaved(t, store, 201)
	drop := map[string]bool{}
	for _, id := range ids[:200] {
		drop[id] = true
	}
	var examined int
	ask := NotePageAsk{TaskID: "a", Limit: 5, ScanMax: 50, Skip: func(n Note) bool { examined++; return drop[n.ID] }}
	var got []string
	calls := 0
	for ; calls < 20; calls++ {
		examined = 0
		page, err := store.NotesPage(context.Background(), ask)
		if err != nil {
			t.Fatal(err)
		}
		if examined > 50 {
			t.Fatalf("call %d examined %d rows, ScanMax is 50", calls, examined)
		}
		for _, note := range page.Notes {
			got = append(got, note.ID)
		}
		if !page.More {
			break
		}
		if len(page.Notes) == 0 && page.Cursor == ask.After {
			t.Fatalf("call %d returned nothing and did not advance", calls)
		}
		ask.After = page.Cursor
	}
	if !sameIDs(got, ids[200:]) {
		t.Fatalf("got %v, want only note 201 %v", got, ids[200:])
	}
	if calls+1 != 5 {
		t.Fatalf("took %d calls, want 5 (four of 50 skipped rows, then note 201)", calls+1)
	}
}

// Skip runs after the read, outside the store's lock, so it may call the store.
// A Skip run under the lock would deadlock here.
func TestNotesPageSkipMayCallTheStore(t *testing.T) {
	store := planOpen(t, "")
	defer store.Close()
	planAdd(t, store, planSpec("a", "Alpha"))
	if _, err := store.AddNote("a", "w", "one"); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := store.NotesPage(context.Background(), NotePageAsk{TaskID: "a", Limit: 5, Skip: func(Note) bool {
			_ = store.Tasks()
			return false
		}})
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("NotesPage did not return: Skip ran under the store's lock")
	}
}

// A task deleted between pages renumbers seq in the table. The cursor is an
// id, so the next page continues exactly where the last stopped.
func TestNotesPageSurvivesATaskDeleteBetweenPages(t *testing.T) {
	store := planOpen(t, "")
	defer store.Close()
	planAdd(t, store, planSpec("b", "Beta"), planSpec("a", "Alpha"))
	for i := 0; i < 3; i++ {
		if _, err := store.AddNote("b", "w", fmt.Sprintf("b-%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	var want []string
	for i := 0; i < 9; i++ {
		note, err := store.AddNote("a", "w", fmt.Sprintf("a-%d", i))
		if err != nil {
			t.Fatal(err)
		}
		want = append(want, note.ID)
	}
	first, err := store.NotesPage(context.Background(), NotePageAsk{TaskID: "a", Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.DeleteSubtree("b"); err != nil {
		t.Fatal(err)
	}
	got := []string{first.Notes[0].ID, first.Notes[1].ID, first.Notes[2].ID}
	got = append(got, walkNotePages(t, store, NotePageAsk{TaskID: "a", After: first.Cursor, Limit: 3})...)
	if !sameIDs(got, want) {
		t.Fatalf("notes across the delete = %v, want %v", got, want)
	}
}

// The cursor and the rows come from one snapshot. The page's own read helpers
// run here on ONE real SQLite read transaction while another handle commits a
// task delete between them, the one moment a split read would skip notes
// because the delete renumbers every position. The control reads the same
// position in a later snapshot and must come out different, or the test would
// prove nothing.
func TestNotesPageReadsCursorAndRowsInOneSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plan.db")
	store := planOpen(t, path)
	defer store.Close()
	planAdd(t, store, planSpec("b", "Beta"), planSpec("a", "Alpha"))
	for i := 0; i < 3; i++ {
		if _, err := store.AddNote("b", "w", fmt.Sprintf("b-%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	var want []string
	for i := 0; i < 9; i++ {
		note, err := store.AddNote("a", "w", fmt.Sprintf("a-%d", i))
		if err != nil {
			t.Fatal(err)
		}
		want = append(want, note.ID)
	}
	writer := planReopen(t, path)
	defer writer.Close()
	db, err := openReadDatabase(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	var live int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM tasks WHERE id = 'a'`).Scan(&live); err != nil {
		t.Fatal(err)
	}
	floor, err := resolveNoteCursor(ctx, tx, "a", want[2])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.DeleteSubtree("b"); err != nil {
		t.Fatal(err)
	}
	rows, _, refused, err := queryNoteRows(ctx, tx, "a", floor, 3, NotePageScanBytesMax)
	tx.Rollback()
	if err != nil || refused != nil {
		t.Fatalf("rows after the commit: err %v refused %v", err, refused)
	}
	var got []string
	for _, row := range rows {
		got = append(got, row.note.ID)
	}
	if !sameIDs(got, want[3:6]) {
		t.Fatalf("one snapshot read %v, want notes 4..6 %v", got, want[3:6])
	}

	later, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer later.Rollback()
	stale, _, _, err := queryNoteRows(ctx, later, "a", floor, 3, NotePageScanBytesMax)
	if err != nil {
		t.Fatal(err)
	}
	var skipped []string
	for _, row := range stale {
		skipped = append(skipped, row.note.ID)
	}
	if sameIDs(skipped, want[3:6]) {
		t.Fatal("the control did not see the renumbering, so the test cannot tell a split read from a whole one")
	}
}

// A cursor survives closing and reopening the store, because it is a note id.
func TestNotesPageCursorSurvivesAReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plan.db")
	store := planOpen(t, path)
	planAdd(t, store, planSpec("a", "Alpha"))
	var want []string
	for i := 0; i < 8; i++ {
		note, err := store.AddNote("a", "w", fmt.Sprintf("a-%d", i))
		if err != nil {
			t.Fatal(err)
		}
		want = append(want, note.ID)
	}
	first, err := store.NotesPage(context.Background(), NotePageAsk{TaskID: "a", Limit: 4})
	if err != nil {
		t.Fatal(err)
	}
	store.Close()
	reopened := planReopen(t, path)
	defer reopened.Close()
	got := walkNotePages(t, reopened, NotePageAsk{TaskID: "a", After: first.Cursor, Limit: 4})
	if !sameIDs(got, want[4:]) {
		t.Fatalf("after the reopen = %v, want %v", got, want[4:])
	}
}

// A cursor that is not a note of this task is an error, never an empty page
// that a delivery loop would read as the end.
func TestNotesPageRefusesACursorThatIsNotThisTasksNote(t *testing.T) {
	store := planOpen(t, "")
	defer store.Close()
	planAdd(t, store, planSpec("a", "Alpha"), planSpec("b", "Beta"), planSpec("c", "Gamma"))
	own, err := store.AddNote("a", "w", "mine")
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := store.AddNote("b", "w", "theirs")
	if err != nil {
		t.Fatal(err)
	}
	gone, err := store.AddNote("c", "w", "deleted with its task")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.DeleteSubtree("c"); err != nil {
		t.Fatal(err)
	}
	for name, cursor := range map[string]string{
		"another task's note": foreign.ID,
		"a deleted note":      gone.ID,
		"an id never minted":  "n-ffffffff",
		"free text":           "after the third one",
	} {
		page, err := store.NotesPage(context.Background(), NotePageAsk{TaskID: "a", After: cursor, Limit: 5})
		if !errors.Is(err, ErrNoteCursor) {
			t.Fatalf("%s: err = %v, want ErrNoteCursor", name, err)
		}
		if len(page.Notes) != 0 || page.More || page.Cursor != "" {
			t.Fatalf("%s: an error came with a page %#v", name, page)
		}
	}
	if _, err := store.NotesPage(context.Background(), NotePageAsk{TaskID: "a", After: own.ID, Limit: 5}); err != nil {
		t.Fatalf("the task's own note is a valid cursor: %v", err)
	}
}

// A task the database does not hold is an error, whether it never existed or
// was deleted, and a cursor does not change that.
func TestNotesPageRefusesAnUnknownOrDeletedTask(t *testing.T) {
	store := planOpen(t, "")
	defer store.Close()
	planAdd(t, store, planSpec("a", "Alpha"), planSpec("c", "Gamma"))
	note, err := store.AddNote("c", "w", "x")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.DeleteSubtree("c"); err != nil {
		t.Fatal(err)
	}
	for name, ask := range map[string]NotePageAsk{
		"never existed":         {TaskID: "ghost", Limit: 5},
		"deleted":               {TaskID: "c", Limit: 5},
		"deleted with a cursor": {TaskID: "c", After: note.ID, Limit: 5},
		"no task named":         {Limit: 5},
	} {
		if _, err := store.NotesPage(context.Background(), ask); !errors.Is(err, ErrTaskNotFound) {
			t.Fatalf("%s: err = %v, want ErrTaskNotFound", name, err)
		}
	}
}

// A page of a closed store is ErrClosed, not the empty page its memory would
// answer.
func TestNotesPageOnAClosedStoreIsAnErrorNotAnEmptyPage(t *testing.T) {
	store := planOpen(t, "")
	planAdd(t, store, planSpec("a", "Alpha"))
	if _, err := store.AddNote("a", "w", "x"); err != nil {
		t.Fatal(err)
	}
	store.Close()
	page, err := store.NotesPage(context.Background(), NotePageAsk{TaskID: "a", Limit: 5})
	if !errors.Is(err, ErrClosed) {
		t.Fatalf("err = %v, want ErrClosed", err)
	}
	if len(page.Notes) != 0 || page.More {
		t.Fatalf("a closed store answered a page %#v", page)
	}
}

// A cancelled context is an error before the read and an error after a Skip
// that cancelled it, and neither returns the rows read so far.
func TestNotesPageCancelledContextReturnsAnErrorAndNoPartialPage(t *testing.T) {
	store := planOpen(t, "")
	defer store.Close()
	planAdd(t, store, planSpec("a", "Alpha"))
	for i := 0; i < 6; i++ {
		if _, err := store.AddNote("a", "w", fmt.Sprintf("a-%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	done, cancel := context.WithCancel(context.Background())
	cancel()
	page, err := store.NotesPage(done, NotePageAsk{TaskID: "a", Limit: 5})
	if !errors.Is(err, context.Canceled) || len(page.Notes) != 0 {
		t.Fatalf("pre-cancelled: page %#v err %v", page, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	page, err = store.NotesPage(ctx, NotePageAsk{TaskID: "a", Limit: 5, Skip: func(Note) bool {
		if calls++; calls == 3 {
			cancel()
		}
		return false
	}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled in Skip: err = %v, want context.Canceled", err)
	}
	if len(page.Notes) != 0 || page.Cursor != "" || page.More {
		t.Fatalf("cancelled in Skip: a partial page came back: %#v", page)
	}
}

// A store written before the client_id column is read without migrating it,
// its notes with no key, and a store that has the column returns the key.
func TestNotesPageReadsAStoreWithoutTheClientIDColumn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plan.db")
	store := planOpen(t, path)
	defer store.Close()
	planAdd(t, store, planSpec("a", "Alpha"))
	var want []string
	for i := 0; i < 3; i++ {
		note, err := store.AddPersonNoteKeyed("a", fmt.Sprintf("p-%d", i), fmt.Sprintf("cid-%d", i))
		if err != nil {
			t.Fatal(err)
		}
		want = append(want, note.ID)
	}
	keyedPage, err := store.NotesPage(context.Background(), NotePageAsk{TaskID: "a", Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(keyedPage.Notes) != 3 || keyedPage.Notes[2].ClientID != "cid-2" {
		t.Fatalf("keyed page = %#v", keyedPage.Notes)
	}

	raw, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`ALTER TABLE notes DROP COLUMN client_id`); err != nil {
		t.Fatal(err)
	}
	raw.Close()

	page, err := store.NotesPage(context.Background(), NotePageAsk{TaskID: "a", Limit: 5})
	if err != nil {
		t.Fatalf("a store without client_id failed: %v", err)
	}
	var got []string
	for _, note := range page.Notes {
		got = append(got, note.ID)
		if note.ClientID != "" {
			t.Fatalf("note %q has key %q, want unknown", note.ID, note.ClientID)
		}
	}
	if !sameIDs(got, want) {
		t.Fatalf("legacy page = %v, want %v", got, want)
	}
	if columnPresent(t, path) {
		t.Fatal("reading a page added the client_id column")
	}
}

// Notes as large as the store admits: a page never exceeds its byte budget,
// every page has a note, and every note still arrives once. A note weighs a
// little over its 32 KiB body, so a budget holds at most budget/32 KiB of them.
func TestNotesPageHoldsTheByteBudgetWithMaximumSizeNotes(t *testing.T) {
	store := planOpen(t, "")
	defer store.Close()
	planAdd(t, store, planSpec("a", "Alpha"))
	body := strings.Repeat("x", NotePageBodyMax)
	var want []string
	for i := 0; i < 40; i++ {
		note, err := store.AddNote("a", "w", body)
		if err != nil {
			t.Fatal(err)
		}
		want = append(want, note.ID)
	}
	for _, budget := range []int{NotePageRowMax, 100 << 10, NotePageBytesMax} {
		ask := NotePageAsk{TaskID: "a", Limit: NotePageMax, MaxBytes: budget}
		var got []string
		for i := 0; i < 100; i++ {
			page, err := store.NotesPage(context.Background(), ask)
			if err != nil {
				t.Fatal(err)
			}
			if len(page.Notes) == 0 || len(page.Notes) > budget/NotePageBodyMax {
				t.Fatalf("budget %d: a page of %d notes", budget, len(page.Notes))
			}
			for _, note := range page.Notes {
				got = append(got, note.ID)
			}
			if !page.More {
				break
			}
			ask.After = page.Cursor
		}
		if !sameIDs(got, want) {
			t.Fatalf("budget %d: got %d notes, want %d, once and in order", budget, len(got), len(want))
		}
	}
}

// The scan byte bound stops a call that skips everything after a few
// maximum-size rows, and the cursor still moves, so no call is as long as the
// table and no row is passed over.
func TestNotesPageScanBytesBoundsASkipEverythingCall(t *testing.T) {
	store := planOpen(t, "")
	defer store.Close()
	planAdd(t, store, planSpec("a", "Alpha"))
	body := strings.Repeat("x", NotePageBodyMax)
	for i := 0; i < 40; i++ {
		if _, err := store.AddNote("a", "w", body); err != nil {
			t.Fatal(err)
		}
	}
	var examined, total int
	ask := NotePageAsk{TaskID: "a", Limit: 5, ScanBytes: 100 << 10, Skip: func(Note) bool { examined++; return true }}
	for i := 0; i < 100; i++ {
		examined = 0
		page, err := store.NotesPage(context.Background(), ask)
		if err != nil {
			t.Fatal(err)
		}
		if examined < 1 || examined > 3 {
			t.Fatalf("call %d examined %d rows, want 1..3 under a 100 KiB scan budget", i, examined)
		}
		if len(page.Notes) != 0 {
			t.Fatalf("a Skip that drops everything returned %d notes", len(page.Notes))
		}
		total += examined
		if !page.More {
			break
		}
		ask.After = page.Cursor
	}
	if total != 40 {
		t.Fatalf("examined %d rows across the walk, want all 40 exactly once", total)
	}
}

// rawExec changes a store file behind its back, the way an older or foreign
// writer could: through a plain connection that none of the store's write
// bounds stand in front of.
func rawExec(t *testing.T, path, query string, args ...any) {
	t.Helper()
	raw, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path))
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	if _, err := raw.Exec(query, args...); err != nil {
		t.Fatal(err)
	}
}

func seedThree(t *testing.T, store *Store) []string {
	t.Helper()
	planAdd(t, store, planSpec("a", "Alpha"))
	var ids []string
	for i := 0; i < 3; i++ {
		note, err := store.AddNote("a", "w", fmt.Sprintf("a-%d", i))
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, note.ID)
	}
	return ids
}

// A legacy note far beyond what the writer admits is refused, never truncated,
// skipped or read. The notes before it arrive first with More true and the
// cursor before it; the call that starts at it fails with the note named; and a
// Skip that would drop everything does not get to drop it.
func TestNotesPageRefusesAnOversizedLegacyNoteAndNeverMovesPastIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plan.db")
	store := planOpen(t, path)
	defer store.Close()
	ids := seedThree(t, store)
	rawExec(t, path, `UPDATE notes SET body = ? WHERE id = ?`, strings.Repeat("y", 8<<20), ids[1])

	first, err := store.NotesPage(context.Background(), NotePageAsk{TaskID: "a", Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Notes) != 1 || first.Notes[0].ID != ids[0] || first.Cursor != ids[0] || !first.More {
		t.Fatalf("page before the refused note = %#v, want only note 1, More, cursor before the refusal", first)
	}
	skipped := 0
	page, err := store.NotesPage(context.Background(), NotePageAsk{TaskID: "a", After: ids[0], Limit: 5, Skip: func(Note) bool { skipped++; return true }})
	if !errors.Is(err, ErrNoteTooLarge) {
		t.Fatalf("err = %v, want ErrNoteTooLarge", err)
	}
	var tooLarge *NoteTooLargeError
	if !errors.As(err, &tooLarge) || tooLarge.NoteID != ids[1] || tooLarge.Field != "body" || tooLarge.Size != 8<<20 {
		t.Fatalf("refusal = %#v, want note %q field body 8 MiB", tooLarge, ids[1])
	}
	if len(err.Error()) > 200 || len(page.Notes) != 0 || page.More || skipped != 0 {
		t.Fatalf("refusal came with %d error bytes, page %#v, %d Skip calls", len(err.Error()), page, skipped)
	}
}

// The refused body is not copied out by the driver: one such read allocates a
// small fraction of the body. It measures Go allocation, which is where the
// driver's copy lands; what SQLite does inside the engine is not counted.
func TestNotesPageDoesNotAllocateAnOversizedBody(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plan.db")
	store := planOpen(t, path)
	defer store.Close()
	ids := seedThree(t, store)
	const huge = 8 << 20
	rawExec(t, path, `UPDATE notes SET body = ? WHERE id = ?`, strings.Repeat("y", huge), ids[1])
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	_, err := store.NotesPage(context.Background(), NotePageAsk{TaskID: "a", After: ids[0], Limit: 5})
	runtime.ReadMemStats(&after)
	if !errors.Is(err, ErrNoteTooLarge) {
		t.Fatalf("err = %v, want ErrNoteTooLarge", err)
	}
	if grew := after.TotalAlloc - before.TotalAlloc; grew > huge/8 {
		t.Fatalf("the refused read allocated %d bytes for an %d byte body", grew, huge)
	}
}

// Each field has its own ceiling, exactly: the body at 32 KiB passes and one
// byte more is refused, and a refusal names a field and a size only, however
// large the stored value, an id included.
func TestNotesPageFieldCeilingsAreExactAndRefusalsStayBounded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plan.db")
	store := planOpen(t, path)
	defer store.Close()
	ids := seedThree(t, store)
	for _, c := range []struct {
		column string
		size   int
		want   string
	}{
		{"body", NotePageBodyMax + 1, "body"},
		{"agent", NotePageMetaMax + 1, "agent"},
		{"client_id", NotePageMetaMax + 1, "client_id"},
		{"project", 100 << 10, "project"},
		{"chat", 100 << 10, "chat"},
		{"at", NotePageFixedMax + 1, "at"},
	} {
		rawExec(t, path, `UPDATE notes SET `+c.column+` = ? WHERE id = ?`, strings.Repeat("z", c.size), ids[1])
		_, err := store.NotesPage(context.Background(), NotePageAsk{TaskID: "a", After: ids[0], Limit: 5})
		var tooLarge *NoteTooLargeError
		if !errors.As(err, &tooLarge) || tooLarge.Field != c.want || tooLarge.Size != int64(c.size) || len(err.Error()) > 200 {
			t.Fatalf("%s at %d bytes: err = %v, want a bounded refusal naming it", c.column, c.size, err)
		}
		rawExec(t, path, `UPDATE notes SET `+c.column+` = ? WHERE id = ?`, map[string]string{"at": "2026-01-01T00:00:00Z"}[c.column], ids[1])
	}
	rawExec(t, path, `UPDATE notes SET body = ? WHERE id = ?`, strings.Repeat("z", NotePageBodyMax), ids[1])
	page, err := store.NotesPage(context.Background(), NotePageAsk{TaskID: "a", After: ids[0], Limit: 5})
	if err != nil || len(page.Notes) != 2 || len(page.Notes[0].Body) != NotePageBodyMax {
		t.Fatalf("a body of exactly %d bytes: page %#v err %v", NotePageBodyMax, page, err)
	}

	rawExec(t, path, `UPDATE notes SET id = ? WHERE id = ?`, strings.Repeat("i", 100<<10), ids[2])
	_, err = store.NotesPage(context.Background(), NotePageAsk{TaskID: "a", After: ids[1], Limit: 5})
	var tooLarge *NoteTooLargeError
	if !errors.As(err, &tooLarge) || tooLarge.Field != "id" || tooLarge.NoteID != "" || len(err.Error()) > 200 {
		t.Fatalf("an oversized id: err = %v, want a refusal that does not echo it", err)
	}
}

// A cursor or a task id longer than any id can be is refused before the
// database is touched, and the error does not echo it.
func TestNotesPageRefusesOverlongCursorAndTaskWithoutEchoingThem(t *testing.T) {
	store := planOpen(t, "")
	defer store.Close()
	planAdd(t, store, planSpec("a", "Alpha"))
	long := strings.Repeat("n", 1<<20)
	if _, err := store.NotesPage(context.Background(), NotePageAsk{TaskID: "a", After: long, Limit: 5}); !errors.Is(err, ErrNoteCursor) || len(err.Error()) > 200 {
		t.Fatalf("overlong cursor: err = %v", err)
	}
	if _, err := store.NotesPage(context.Background(), NotePageAsk{TaskID: long, Limit: 5}); !errors.Is(err, ErrTaskNotFound) || len(err.Error()) > 200 {
		t.Fatalf("overlong task: err = %v", err)
	}
}

// Bounds the caller got wrong are errors; none is quietly replaced by a
// default.
func TestNotesPageRefusesOutOfRangeBounds(t *testing.T) {
	store := planOpen(t, "")
	defer store.Close()
	planAdd(t, store, planSpec("a", "Alpha"))
	for name, ask := range map[string]NotePageAsk{
		"limit zero":           {TaskID: "a"},
		"limit negative":       {TaskID: "a", Limit: -1},
		"limit over ceiling":   {TaskID: "a", Limit: NotePageMax + 1},
		"bytes negative":       {TaskID: "a", Limit: 1, MaxBytes: -1},
		"bytes under a row":    {TaskID: "a", Limit: 1, MaxBytes: NotePageRowMax - 1},
		"bytes over":           {TaskID: "a", Limit: 1, MaxBytes: NotePageBytesMax + 1},
		"scan negative":        {TaskID: "a", Limit: 1, ScanMax: -1},
		"scan over":            {TaskID: "a", Limit: 1, ScanMax: NotePageScanMax + 1},
		"scan bytes under row": {TaskID: "a", Limit: 1, ScanBytes: NotePageRowMax - 1},
		"scan bytes over":      {TaskID: "a", Limit: 1, ScanBytes: NotePageScanBytesMax + 1},
	} {
		if _, err := store.NotesPage(context.Background(), ask); err == nil || errors.Is(err, ErrNoteCursor) || errors.Is(err, ErrTaskNotFound) {
			t.Fatalf("%s: err = %v, want a bound error", name, err)
		}
	}
}

// A page is read from the database, not from the handle's memory: a note
// another handle just committed is on the page, and the reading handle's own
// state was neither hydrated nor refreshed to find it.
func TestNotesPageReadsTheDatabaseAndLeavesTheHandlesMemoryAlone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plan.db")
	reader := planOpen(t, path)
	defer reader.Close()
	planAdd(t, reader, planSpec("a", "Alpha"))
	writer := planReopen(t, path)
	defer writer.Close()
	note, err := writer.AddNote("a", "w", "from the other handle")
	if err != nil {
		t.Fatal(err)
	}
	if len(reader.data.Notes) != 0 {
		t.Fatalf("the reader's memory already holds %d notes", len(reader.data.Notes))
	}
	page, err := reader.NotesPage(context.Background(), NotePageAsk{TaskID: "a", Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Notes) != 1 || page.Notes[0].ID != note.ID {
		t.Fatalf("page = %#v, want the other handle's note %q", page.Notes, note.ID)
	}
	if len(reader.data.Notes) != 0 {
		t.Fatal("NotesPage hydrated the handle's memory")
	}
}
