package run

// The note inbox's own tests: the fifty-first and two-hundred-first note
// reached by a real worker's decision, one page read per decision, own notes
// recognised only by the step that wrote them, and every error — a busy store,
// a cancelled context, a refused splice, a note too large to read — a decision
// that delivers and marks nothing. Every one runs against a real store on a
// temp file; the only stand-in is a pager that counts its calls and can answer
// busy, because the store's lock is its own and a test here cannot hold it.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/plandb"
)

func openNoteStore(t *testing.T) *plandb.Store {
	t.Helper()
	store, err := plandb.Open(filepath.Join(t.TempDir(), "plan.json"), "run-test", "root", "The run", "drive")
	if err != nil {
		t.Fatalf("open the store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// seedNotes writes n notes on the root through a plain connection, each body
// the format with its index, and answers their ids in order. The store's own
// writer rewrites the whole table per note, so volume goes in this way; the
// next write through the store reads these back as its own.
func seedNotes(t *testing.T, store *plandb.Store, n int, from, agent, format string) []string {
	t.Helper()
	db, err := sql.Open("sqlite", store.Path())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	ids := make([]string, 0, n)
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("n-seed-%s-%04d", from, i)
		if _, err := tx.Exec(`INSERT INTO notes (seq, id, task_id, agent, body, at, project, chat, "from", client_id)
			SELECT COALESCE(MAX(seq), -1) + 1, ?, ?, ?, ?, '2026-10-17T00:00:00Z', '', '', ?, '' FROM notes`,
			id, store.RootID(), agent, fmt.Sprintf(format, i), from); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return ids
}

func rawNoteExec(t *testing.T, store *plandb.Store, query string, args ...any) {
	t.Helper()
	db, err := sql.Open("sqlite", store.Path())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatal(err)
	}
}

// countingPager is the store, counted, and busy for its first busy calls.
type countingPager struct {
	store *plandb.Store
	busy  int
	calls int
	asks  []plandb.NotePageAsk
}

func (p *countingPager) NotesPage(ctx context.Context, ask plandb.NotePageAsk) (plandb.NotePage, error) {
	p.calls++
	p.asks = append(p.asks, ask)
	if p.busy > 0 {
		p.busy--
		return plandb.NotePage{}, plandb.ErrNoteBusy
	}
	return p.store.NotesPage(ctx, ask)
}

// takes is a steer that always lands and keeps what it was handed.
func takes(words *[]string) func(string) error {
	return func(w string) error {
		*words = append(*words, w)
		return nil
	}
}

// The fifty-first and the two-hundred-first note: with the first fifty, or
// the first two hundred, already had — read back off the task's record the
// way a restarted worker reads them — the next note is in the first steer,
// and every note not had arrives exactly once, at most a delivery's worth per
// steer, in order. A worker started again on the record that wrote does not
// hand any of them over a second time.
func TestBashNotesReachNote51And201AfterTheHadOnes(t *testing.T) {
	for _, hadCount := range []int{50, 200} {
		store := openNoteStore(t)
		dir := filepath.Dir(store.Path())
		ids := seedNotes(t, store, 250, plandb.NoteFromPerson, "", "direction-%03d")
		if err := appendTrajectory(dir, store.RootID(), Step{Kind: trajectoryNotesKind, Notes: ids[:hadCount]}); err != nil {
			t.Fatal(err)
		}
		notes := newBashNotes(store, store.RootID(), notesAlreadyHad(dir, store.RootID()))
		var words, got []string
		for boundary := 0; boundary < 100 && len(got) < len(ids)-hadCount; boundary++ {
			had := notes.boundary(context.Background(), "echo working", true, takes(&words))
			if len(had) > notesPerDelivery {
				t.Fatalf("%d had: one boundary handed over %d notes, more than %d", hadCount, len(had), notesPerDelivery)
			}
			got = append(got, had...)
			if len(had) > 0 {
				if err := appendTrajectory(dir, store.RootID(), Step{Kind: trajectoryNotesKind, Notes: had}); err != nil {
					t.Fatal(err)
				}
			}
		}
		if len(words) == 0 || !strings.Contains(words[0], fmt.Sprintf("direction-%03d", hadCount)) {
			t.Fatalf("%d had: the first steer did not carry note %d: %q", hadCount, hadCount+1, words)
		}
		if strings.Join(got, ",") != strings.Join(ids[hadCount:], ",") {
			t.Fatalf("%d had: handed over %d notes, want the %d after the had ones, once and in order", hadCount, len(got), len(ids)-hadCount)
		}
		again := newBashNotes(store, store.RootID(), notesAlreadyHad(dir, store.RootID()))
		var repeated []string
		for boundary := 0; boundary < 3; boundary++ {
			again.boundary(context.Background(), "echo working", true, takes(&repeated))
		}
		if len(repeated) != 0 {
			t.Fatalf("%d had: a worker started again on the record was handed notes a second time: %q", hadCount, repeated)
		}
	}
}

// One boundary is one page read, and its whole budget is that page's.
func TestBashNotesOneBoundaryIsOnePageRead(t *testing.T) {
	store := openNoteStore(t)
	seedNotes(t, store, 12, plandb.NoteFromPerson, "", "direction-%03d")
	pager := &countingPager{store: store}
	notes := newBashNotes(pager, store.RootID(), map[string]bool{})
	var words []string
	notes.boundary(context.Background(), "echo working", true, takes(&words))
	if pager.calls != 1 || pager.asks[0].Limit != notesPerDelivery || pager.asks[0].ScanMax != notesExaminedPerDecision {
		t.Fatalf("one boundary made %d reads %+v, want one of Limit %d and ScanMax %d", pager.calls, pager.asks, notesPerDelivery, notesExaminedPerDecision)
	}
}

// A note this step's own command wrote is marked had and never handed back.
func TestBashNotesOwnNoteOfThisStepIsNotHandedBack(t *testing.T) {
	store := openNoteStore(t)
	const own = "the parser lives in cmd/parse and not in internal"
	note, err := store.AddNote(store.RootID(), "w", own)
	if err != nil {
		t.Fatal(err)
	}
	notes := newBashNotes(store, store.RootID(), map[string]bool{})
	var words []string
	had := notes.boundary(context.Background(), "plandb task note t-root '"+own+"'", true, takes(&words))
	if len(words) != 0 || len(had) != 1 || had[0] != note.ID || !notes.read[note.ID] {
		t.Fatalf("own note: steered %q, had %v; want it marked had and not handed back", words, had)
	}
}

// AN EARLIER COMMAND IS NOT EVIDENCE. The worker once ran a command carrying
// some words; a colleague later leaves a note with the same words. At the
// next boundary, under a different command, that note is a colleague's and is
// handed over — matching it against the earlier command would hide it.
func TestBashNotesAColleaguesNoteMatchingAnEarlierCommandIsDelivered(t *testing.T) {
	store := openNoteStore(t)
	const said = "the deploy is blocked on review"
	notes := newBashNotes(store, store.RootID(), map[string]bool{})
	var words []string
	if had := notes.boundary(context.Background(), "echo '"+said+"'", true, takes(&words)); len(had) != 0 || len(words) != 0 {
		t.Fatalf("an empty inbox handed over %q, had %v", words, had)
	}
	colleague, err := store.AddNote(store.RootID(), "sibling", said)
	if err != nil {
		t.Fatal(err)
	}
	had := notes.boundary(context.Background(), "echo two", true, takes(&words))
	if len(words) != 1 || !strings.Contains(words[0], said) || len(had) != 1 || had[0] != colleague.ID {
		t.Fatalf("the colleague's note: steered %q, had %v; want it handed over", words, had)
	}
}

// A splice that refuses marks nothing and moves nothing: the same notes are
// offered at the next boundary, and taken there.
func TestBashNotesRefusedSteerMarksNothingAndOffersAgain(t *testing.T) {
	store := openNoteStore(t)
	ids := seedNotes(t, store, 3, plandb.NoteFromPerson, "", "direction-%03d")
	notes := newBashNotes(store, store.RootID(), map[string]bool{})
	refused := 0
	had := notes.boundary(context.Background(), "echo working", true, func(string) error {
		refused++
		return errors.New("session: nothing to steer")
	})
	if refused != 1 || len(had) != 0 || len(notes.read) != 0 || notes.inbox.after != "" {
		t.Fatalf("a refused splice: %d tries, had %v, read %v, mark %q; want one try and nothing moved", refused, had, notes.read, notes.inbox.after)
	}
	var words []string
	had = notes.boundary(context.Background(), "echo working", true, takes(&words))
	if strings.Join(had, ",") != strings.Join(ids, ",") {
		t.Fatalf("after the refusal the next boundary handed over %v, want %v", had, ids)
	}
}

// A busy store and a cancelled context are decisions that deliver nothing and
// mark nothing; the next boundary delivers.
func TestBashNotesBusyOrCancelledDeliverNothing(t *testing.T) {
	store := openNoteStore(t)
	ids := seedNotes(t, store, 2, plandb.NoteFromPerson, "", "direction-%03d")
	pager := &countingPager{store: store, busy: 1}
	notes := newBashNotes(pager, store.RootID(), map[string]bool{})
	var words []string
	if had := notes.boundary(context.Background(), "echo working", true, takes(&words)); len(had) != 0 || len(words) != 0 {
		t.Fatalf("a busy store handed over %q, had %v", words, had)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if had := notes.boundary(cancelled, "echo working", true, takes(&words)); len(had) != 0 || len(words) != 0 {
		t.Fatalf("a cancelled context handed over %q, had %v", words, had)
	}
	if len(notes.read) != 0 || notes.inbox.after != "" {
		t.Fatalf("an error moved the mark %q or marked %v", notes.inbox.after, notes.read)
	}
	if had := notes.boundary(context.Background(), "echo working", true, takes(&words)); strings.Join(had, ",") != strings.Join(ids, ",") {
		t.Fatalf("after the errors the next boundary handed over %v, want %v", had, ids)
	}
}

// A turn that is ending is not handed notes and they are not marked; the
// step's own note is still marked, and the next live boundary hands the
// person's note over.
func TestBashNotesEndingTurnIsNotHandedNotes(t *testing.T) {
	store := openNoteStore(t)
	person, err := store.AddPersonNote(store.RootID(), "use the staging bucket")
	if err != nil {
		t.Fatal(err)
	}
	const own = "found the flaky test in run_test.go"
	mine, err := store.AddNote(store.RootID(), "w", own)
	if err != nil {
		t.Fatal(err)
	}
	notes := newBashNotes(store, store.RootID(), map[string]bool{})
	steered := false
	had := notes.boundary(context.Background(), "plandb task note t-root '"+own+"'", false, func(string) error {
		steered = true
		return nil
	})
	if steered || len(had) != 1 || had[0] != mine.ID || notes.read[person.ID] {
		t.Fatalf("an ending turn: steered %v, had %v; want only the own note marked", steered, had)
	}
	var words []string
	if had := notes.boundary(context.Background(), "echo next", true, takes(&words)); len(had) != 1 || had[0] != person.ID {
		t.Fatalf("the next live boundary handed over %v, want the person's note", had)
	}
}

// A NOTE TOO LARGE TO READ IS NEVER SKIPPED. The note before it is handed
// over; then every boundary is one read that answers the refusal, hands over
// nothing and marks nothing, and the note after it waits behind it with the
// mark held on the last note handed over.
func TestBashNotesATooLargeNoteHoldsTheCursorAndIsNeverSkipped(t *testing.T) {
	store := openNoteStore(t)
	ids := seedNotes(t, store, 3, plandb.NoteFromPerson, "", "direction-%03d")
	rawNoteExec(t, store, `UPDATE notes SET body = ? WHERE id = ?`, strings.Repeat("x", 2<<20), ids[1])
	pager := &countingPager{store: store}
	notes := newBashNotes(pager, store.RootID(), map[string]bool{})
	var words []string
	if had := notes.boundary(context.Background(), "echo working", true, takes(&words)); len(had) != 1 || had[0] != ids[0] {
		t.Fatalf("the note before the large one: had %v, want %v", had, ids[:1])
	}
	for boundary := 0; boundary < 3; boundary++ {
		before := pager.calls
		if had := notes.boundary(context.Background(), "echo working", true, takes(&words)); len(had) != 0 {
			t.Fatalf("boundary %d past a note too large to read handed over %v", boundary, had)
		}
		if pager.calls-before != 1 {
			t.Fatalf("boundary %d made %d reads, want one", boundary, pager.calls-before)
		}
	}
	if len(words) != 1 || notes.read[ids[1]] || notes.read[ids[2]] || notes.inbox.after != ids[0] {
		t.Fatalf("after the refusals: %d steers, read %v, mark %q; want the mark held on %q", len(words), notes.read, notes.inbox.after, ids[0])
	}
}

// listeningSink is a delegate sink on a real store whose inbox is a plain file.
func listeningSink(t *testing.T, store *plandb.Store) *delegateSink {
	t.Helper()
	inbox := filepath.Join(t.TempDir(), "inbox")
	if err := os.WriteFile(inbox, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	sink := &delegateSink{worker: &DelegateWorker{store: store}, taskID: store.RootID(), storeDir: filepath.Dir(store.Path()),
		name: "fake", inboxPath: inbox, ctx: context.Background(), sent: map[string]bool{}, had: map[string]bool{}}
	sink.record.Listening = true
	return sink
}

// reports answers the unread reports the sink left, walking every note.
func reports(t *testing.T, store *plandb.Store) []string {
	t.Helper()
	var out []string
	ask := plandb.NotePageAsk{TaskID: store.RootID(), Limit: plandb.NotePageMax}
	for i := 0; i < 100; i++ {
		page, err := store.NotesPage(context.Background(), ask)
		if err != nil {
			t.Fatal(err)
		}
		for _, note := range page.Notes {
			if note.Agent == "fake" {
				out = append(out, note.Body)
			}
		}
		if !page.More {
			return out
		}
		ask.After = page.Cursor
	}
	t.Fatal("the walk did not end")
	return nil
}

// A note too large to read is not published, and the notes after it wait:
// nothing is sent, heard or refused-for-its-words on its account.
func TestDelegateForwardTooLargeHoldsAndPublishesNothingPastIt(t *testing.T) {
	store := openNoteStore(t)
	ids := seedNotes(t, store, 3, plandb.NoteFromPerson, "", "direction-%03d")
	rawNoteExec(t, store, `UPDATE notes SET body = ? WHERE id = ?`, strings.Repeat("x", 2<<20), ids[1])
	sink := listeningSink(t, store)
	f := sink.newForwarding(store)
	for tick := 0; tick < 3; tick++ {
		sink.forwardOnce(f)
	}
	raw, err := os.ReadFile(sink.inboxPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), ids[0]) || strings.Contains(string(raw), ids[1]) || strings.Contains(string(raw), ids[2]) {
		t.Fatalf("the inbox held %q; want only the note before the large one", raw)
	}
	if len(sink.sent) != 1 || len(sink.had) != 0 || len(f.rejected) != 0 || f.inbox.after != ids[0] {
		t.Fatalf("sent %v had %v rejected %v mark %q; want only the first sent and the mark on it", sink.sent, sink.had, f.rejected, f.inbox.after)
	}
}

// The unread report names the two-hundred-first message like any other, and a
// walk that reached the last note reads as complete.
func TestDelegateUnreadReportNamesNote201(t *testing.T) {
	store := openNoteStore(t)
	ids := seedNotes(t, store, 250, plandb.NoteFromPerson, "", "direction-%03d")
	sink := listeningSink(t, store)
	for _, id := range ids[:200] {
		sink.had[id] = true
	}
	sink.reportUnheardFrom(store)
	got := reports(t, store)
	if len(got) != 1 || !strings.Contains(got[0], "direction-200") || strings.Contains(got[0], "direction-199") || strings.Contains(got[0], "could not") {
		t.Fatalf("reports = %q; want one complete report quoting note 201", got)
	}
}

// A walk that could not finish says so, once, and never counts what it did
// not see: a busy store from the first page is one report with no count; a
// note too large to read is the end of what was checked.
func TestDelegateUnreadReportSaysWhenItCouldNotCheck(t *testing.T) {
	store := openNoteStore(t)
	ids := seedNotes(t, store, 3, plandb.NoteFromPerson, "", "direction-%03d")
	sink := listeningSink(t, store)
	sink.reportUnheardFrom(&countingPager{store: store, busy: unheardPagesMost + 1})
	got := reports(t, store)
	if len(got) != 1 || !strings.Contains(got[0], "may not have read every message") || !strings.Contains(got[0], "the plan was busy") || strings.Contains(got[0], "more message") {
		t.Fatalf("a busy walk: reports = %q; want one report that says it could not check", got)
	}

	other := openNoteStore(t)
	ids = seedNotes(t, other, 3, plandb.NoteFromPerson, "", "direction-%03d")
	rawNoteExec(t, other, `UPDATE notes SET body = ? WHERE id = ?`, strings.Repeat("x", 2<<20), ids[1])
	sink = listeningSink(t, other)
	sink.reportUnheardFrom(other)
	got = reports(t, other)
	if len(got) != 1 || !strings.Contains(got[0], "direction-000") || strings.Contains(got[0], "direction-002") ||
		!strings.Contains(got[0], "could not be checked (a message there is too large to read") {
		t.Fatalf("a walk stopped by a large note: reports = %q", got)
	}
}

// A receipt marks only a message this launch sent: one that arrives before
// the send is not a receipt for it, and one after it is, on the record.
func TestDelegateLateAckMarksOnlyWhatWasSent(t *testing.T) {
	store := openNoteStore(t)
	note, err := store.AddPersonNote(store.RootID(), "also add a line saying bye")
	if err != nil {
		t.Fatal(err)
	}
	sink := listeningSink(t, store)
	sink.Heard([]string{note.ID})
	if sink.had[note.ID] || notesAlreadyHad(sink.storeDir, store.RootID())[note.ID] {
		t.Fatal("a receipt for a message never sent marked it heard")
	}
	sink.forwardOnce(sink.newForwarding(store))
	if !sink.sent[note.ID] {
		t.Fatal("the message was not sent")
	}
	sink.Heard([]string{note.ID})
	if !sink.had[note.ID] || !notesAlreadyHad(sink.storeDir, store.RootID())[note.ID] {
		t.Fatal("a receipt for a sent message was not marked heard on the record")
	}
}

// A note the writer accepts is a note the readers deliver. An author of
// exactly the page's ceiling, 4096 bytes, is the longest the writer admits;
// it is handed to a bash worker at a boundary and published to a listening
// program, not held as though it were too large to read. One byte more is
// refused by the writer, so no note can hold the readers.
func TestANoteWithTheLongestAuthorTheWriterAdmitsIsDelivered(t *testing.T) {
	store := openNoteStore(t)
	longest := strings.Repeat("a", plandb.NotePageMetaMax)
	if _, err := store.AddNote(store.RootID(), longest+"x", "one byte too many"); err == nil {
		t.Fatal("the writer admitted an author one byte over the page's ceiling")
	}
	mine, err := store.AddNote(store.RootID(), longest, "from the longest author")
	if err != nil {
		t.Fatalf("the writer refused an author of exactly %d bytes: %v", plandb.NotePageMetaMax, err)
	}
	later, err := store.AddPersonNote(store.RootID(), "a person's direction after it")
	if err != nil {
		t.Fatal(err)
	}

	notes := newBashNotes(store, store.RootID(), map[string]bool{})
	var words []string
	had := notes.boundary(context.Background(), "echo working", true, takes(&words))
	if strings.Join(had, ",") != mine.ID+","+later.ID || len(words) != 1 ||
		!strings.Contains(words[0], "from the longest author") || !strings.Contains(words[0], "a person's direction after it") {
		t.Fatalf("a bash boundary handed over %v %q; want both notes in order", had, words)
	}

	sink := listeningSink(t, store)
	sink.forwardOnce(sink.newForwarding(store))
	raw, err := os.ReadFile(sink.inboxPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), mine.ID) || !strings.Contains(string(raw), later.ID) {
		t.Fatalf("the inbox held %d bytes without both notes", len(raw))
	}
}
