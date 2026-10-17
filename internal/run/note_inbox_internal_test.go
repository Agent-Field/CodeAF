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
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/delegate"
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
	asks  []plandb.NoteEnvelopeAsk
}

func (p *countingPager) NoteEnvelopesPage(ctx context.Context, ask plandb.NoteEnvelopeAsk) (plandb.NoteEnvelopePage, error) {
	p.calls++
	p.asks = append(p.asks, ask)
	if p.busy > 0 {
		p.busy--
		return plandb.NoteEnvelopePage{}, plandb.ErrNoteBusy
	}
	return p.store.NoteEnvelopesPage(ctx, ask)
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
		ids := seedNotes(t, store, 250, plandb.NoteFromPerson, "", "direction-%03d")
		bashIsHandedEveryNoteAfterTheHadOnes(t, store, ids, hadCount)
	}
}

// bashIsHandedEveryNoteAfterTheHadOnes is the body of the fifty-first and
// two-hundred-first proofs, so the same assertions run on a plan of any name:
// the first hadCount ids are written on the task's record as had, a worker
// started on that record is handed every other note exactly once and in order,
// and a worker started again on the record it wrote is handed none.
func bashIsHandedEveryNoteAfterTheHadOnes(t *testing.T, store *plandb.Store, ids []string, hadCount int) {
	t.Helper()
	dir := filepath.Dir(store.Path())
	if err := appendTrajectory(dir, store.RootID(), Step{Kind: trajectoryNotesKind, Notes: ids[:hadCount]}); err != nil {
		t.Fatal(err)
	}
	notes := newBashNotes(store, store.RootID(), notesAlreadyHad(dir, store.RootID()))
	// The most boundaries the walk can need, derived and not guessed: a
	// delivery hands over at most notesPerDelivery notes, so the notes after the
	// had ones take at most ceil(rest/notesPerDelivery) boundaries; passing the
	// had ones costs at most one more per notesExaminedPerDecision examined,
	// and one spare shows the walk ended. A worker that needs more is stuck.
	rest := len(ids) - hadCount
	most := (rest+notesPerDelivery-1)/notesPerDelivery + (hadCount+notesExaminedPerDecision-1)/notesExaminedPerDecision + 1
	var words, got []string
	for boundary := 0; boundary < most && len(got) < rest; boundary++ {
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

// reports answers the unread reports the sink left, in the order they were
// written. It reads the table directly, because the pages refuse the very
// notes some of these tests leave behind and the report is what is under test.
func reports(t *testing.T, store *plandb.Store) []string {
	t.Helper()
	db, err := sql.Open("sqlite", store.Path())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.Query(`SELECT body FROM notes WHERE agent = 'fake' ORDER BY seq`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var body string
		if err := rows.Scan(&body); err != nil {
			t.Fatal(err)
		}
		out = append(out, body)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
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

// The writer's author ceiling fits the readers' author ceiling. An author of
// exactly the page's ceiling, 4096 bytes, is the longest the writer admits;
// it is handed to a bash worker at a boundary and published to a listening
// program, not held as though it were too large to read. One byte more is
// refused by the writer. A long plan name holds nothing, which the long-plan
// tests below cover; this test covers the author field only.
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

// openLongNamedStore is a plan whose name and conversation tag are both over
// the page's ceiling, the way a long first task title makes one, and proves
// the full page refuses it: the control that makes every test below about the
// envelope and not about a plan that was never a problem.
func openLongNamedStore(t *testing.T) *plandb.Store {
	t.Helper()
	long := strings.Repeat("p", plandb.NotePageMetaMax+1)
	store, err := plandb.Open(filepath.Join(t.TempDir(), "plan.json"), long, "root", "The run", "drive", long)
	if err != nil {
		t.Fatalf("open the long-named store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// tagLikeTheWriter gives every seeded note the plan's own tags, which is what
// the writer does to a note it adds, and checks the full page now refuses the
// first of them for its project.
func tagLikeTheWriter(t *testing.T, store *plandb.Store) {
	t.Helper()
	long := strings.Repeat("p", plandb.NotePageMetaMax+1)
	rawNoteExec(t, store, `UPDATE notes SET project = ?, chat = ?`, long, long)
	_, err := store.NotesPage(context.Background(), plandb.NotePageAsk{TaskID: store.RootID(), Limit: 1})
	var refused *plandb.NoteTooLargeError
	if !errors.As(err, &refused) || refused.Field != "project" {
		t.Fatalf("the full page on a long-named plan answered %v; want a refusal of the project", err)
	}
}

// The control above is the failure this lane removes: with every note tagged
// by a plan name over the ceiling, a bash worker is still handed the
// fifty-first and the two-hundred-first note.
func TestBashNotesReachNote51And201OnAPlanNamedOverTheCeiling(t *testing.T) {
	for _, hadCount := range []int{50, 200} {
		store := openLongNamedStore(t)
		ids := seedNotes(t, store, 250, plandb.NoteFromPerson, "", "direction-%03d")
		tagLikeTheWriter(t, store)
		bashIsHandedEveryNoteAfterTheHadOnes(t, store, ids, hadCount)
	}
}

// Passing the had notes spans more than two examining windows: 1100 had notes
// are three decisions of the walk (500, 500 and 100) before the first of the
// last hundred is handed over. It is the only case that needs 1200 tagged
// rows, so it is the only one that pays for them.
func TestBashNotesReachTheNoteAfterThreeExaminingWindowsOnAPlanNamedOverTheCeiling(t *testing.T) {
	store := openLongNamedStore(t)
	ids := seedNotes(t, store, 1200, plandb.NoteFromPerson, "", "direction-%03d")
	tagLikeTheWriter(t, store)
	bashIsHandedEveryNoteAfterTheHadOnes(t, store, ids, 1100)
}

// The unread report on a long-named plan names note 201 like any other and
// reads as complete: the walk reaches the last note, so it says nothing of a
// check that could not finish.
func TestDelegateUnreadReportNamesNote201OnAPlanNamedOverTheCeiling(t *testing.T) {
	store := openLongNamedStore(t)
	ids := seedNotes(t, store, 250, plandb.NoteFromPerson, "", "direction-%03d")
	tagLikeTheWriter(t, store)
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

// publishedMessages is the messages a listening program's inbox file holds, in
// order, read back the way the program reads them.
func publishedMessages(t *testing.T, path string) []delegate.Message {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out []delegate.Message
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if line == "" {
			continue
		}
		var message delegate.Message
		if err := json.Unmarshal([]byte(line), &message); err != nil {
			t.Fatalf("inbox line %q: %v", line, err)
		}
		out = append(out, message)
	}
	return out
}

// The forwarder publishes the fifty-first and the two-hundred-first note of a
// long-named plan, each once, in order, from the store's own ids.
func TestDelegateForwarderReachesNote51And201OnAPlanNamedOverTheCeiling(t *testing.T) {
	for _, hadCount := range []int{50, 200} {
		store := openLongNamedStore(t)
		ids := seedNotes(t, store, 250, plandb.NoteFromPerson, "", "direction-%03d")
		tagLikeTheWriter(t, store)
		sink := listeningSink(t, store)
		for _, id := range ids[:hadCount] {
			sink.had[id] = true
		}
		f := sink.newForwarding(store)
		for tick := 0; tick < 100 && len(publishedMessages(t, sink.inboxPath)) < len(ids)-hadCount; tick++ {
			sink.forwardOnce(f)
		}
		var got []string
		for _, message := range publishedMessages(t, sink.inboxPath) {
			got = append(got, message.ID)
			if message.From != delegate.FromPerson {
				t.Fatalf("%d had: message %s is from %q, want the person", hadCount, message.ID, message.From)
			}
		}
		if strings.Join(got, ",") != strings.Join(ids[hadCount:], ",") {
			t.Fatalf("%d had: published %d messages, want the %d after the had ones, once and in order", hadCount, len(got), len(ids)-hadCount)
		}
	}
}

// A note's own id, the one an acknowledgement is matched by, and the sender's
// key, which only the sender's surface reads, are two names. A keyed note on a
// long-named plan keeps its key on the stored note and reaches the program
// under the store's id, without the key: the inbox message carries no client
// key, and none is made up for it.
func TestAKeyedNoteOnALongNamedPlanIsPublishedUnderItsNoteIDNotItsKey(t *testing.T) {
	store := openLongNamedStore(t)
	const key = "surface-key-0001"
	const words = "ship it behind the flag"
	note, err := store.AddPersonNoteKeyed(store.RootID(), words, key)
	if err != nil {
		t.Fatal(err)
	}
	if note.ID == key {
		t.Fatalf("the store's id for the note is the sender's key %q", key)
	}
	if _, err := store.NotesPage(context.Background(), plandb.NotePageAsk{TaskID: store.RootID(), Limit: 1}); !errors.Is(err, plandb.ErrNoteTooLarge) {
		t.Fatalf("the full page on the writer's own note answered %v; want the long plan to refuse it", err)
	}
	page, err := store.NoteEnvelopesPage(context.Background(), plandb.NoteEnvelopeAsk{TaskID: store.RootID(), Limit: 1})
	if err != nil || len(page.Notes) != 1 || page.Notes[0].ID != note.ID || page.Notes[0].ClientID != key {
		t.Fatalf("envelope page %+v err %v; want the note under id %s with key %q", page, err, note.ID, key)
	}

	sink := listeningSink(t, store)
	sink.forwardOnce(sink.newForwarding(store))
	got := publishedMessages(t, sink.inboxPath)
	if len(got) != 1 || got[0].ID != note.ID || got[0].From != delegate.FromPerson || got[0].Text != words {
		t.Fatalf("published %+v; want id %s from the person saying %q", got, note.ID, words)
	}
	if raw, _ := os.ReadFile(sink.inboxPath); strings.Contains(string(raw), key) {
		t.Fatalf("the inbox carries the sender's key: %s", raw)
	}
	sink.Heard([]string{note.ID})
	if !sink.had[note.ID] || sink.had[key] {
		t.Fatalf("the acknowledgement marked %v; want the note id only", sink.had)
	}
}

// A note whose words are over their ceiling is a corrupt row for both readers:
// the fifty-one before it are delivered, and then nothing moves — not the
// mark, not what was read, sent or heard, not the inbox file — however many
// more decisions are made, and nothing after it is delivered in its place.
func TestARelevantCorruptRowHoldsBothReadersAndMovesNothing(t *testing.T) {
	store := openNoteStore(t)
	ids := seedNotes(t, store, 201, plandb.NoteFromPerson, "", "direction-%03d")
	rawNoteExec(t, store, `UPDATE notes SET body = ? WHERE id = ?`, strings.Repeat("x", plandb.NotePageBodyMax+1), ids[51])

	notes := newBashNotes(store, store.RootID(), map[string]bool{})
	var words, handed []string
	for boundary := 0; boundary < 30; boundary++ {
		handed = append(handed, notes.boundary(context.Background(), "echo working", true, takes(&words))...)
	}
	if strings.Join(handed, ",") != strings.Join(ids[:51], ",") || notes.inbox.after != ids[50] {
		t.Fatalf("the bash worker was handed %d notes with the mark on %q; want the 51 before the corrupt one and the mark on %q", len(handed), notes.inbox.after, ids[50])
	}
	readBefore, stepsBefore := len(notes.read), len(words)
	for boundary := 0; boundary < 3; boundary++ {
		if had := notes.boundary(context.Background(), "echo working", true, takes(&words)); len(had) != 0 {
			t.Fatalf("a boundary past the corrupt row handed over %v", had)
		}
	}
	if len(notes.read) != readBefore || len(words) != stepsBefore || notes.inbox.after != ids[50] || notes.read[ids[51]] || notes.read[ids[52]] {
		t.Fatalf("the corrupt row moved the bash worker: read %d->%d, steers %d->%d, mark %q", readBefore, len(notes.read), stepsBefore, len(words), notes.inbox.after)
	}

	sink := listeningSink(t, store)
	f := sink.newForwarding(store)
	for tick := 0; tick < 30; tick++ {
		sink.forwardOnce(f)
	}
	published := publishedMessages(t, sink.inboxPath)
	if len(published) != 51 || published[50].ID != ids[50] || f.inbox.after != ids[50] || len(sink.sent) != 51 || len(sink.had) != 0 || len(f.rejected) != 0 {
		t.Fatalf("published %d, mark %q, sent %d, had %d, rejected %d; want 51 published and the mark on %q", len(published), f.inbox.after, len(sink.sent), len(sink.had), len(f.rejected), ids[50])
	}
	before, err := os.ReadFile(sink.inboxPath)
	if err != nil {
		t.Fatal(err)
	}
	for tick := 0; tick < 3; tick++ {
		sink.forwardOnce(f)
	}
	after, err := os.ReadFile(sink.inboxPath)
	if err != nil || string(before) != string(after) || f.inbox.after != ids[50] || len(sink.sent) != 51 || len(sink.had) != 0 {
		t.Fatalf("the corrupt row moved the forwarder: inbox %d->%d bytes, mark %q, sent %d, err %v", len(before), len(after), f.inbox.after, len(sink.sent), err)
	}

	sink.reportUnheardFrom(store)
	got := reports(t, store)
	if len(got) != 1 || !strings.Contains(got[0], "could not be checked (a message there is too large to read") || strings.Contains(got[0], "direction-052") {
		t.Fatalf("reports = %q; want one that says the check stopped at a message too large to read", got)
	}
}
