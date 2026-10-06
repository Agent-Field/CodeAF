package standing

// repair_test.go covers the independent/root review failures repaired after
// a6c2caf10: a truncated first look must never become the baseline or be
// compared with a complete one; a billed unknown whose accounting cannot be
// stored must keep its opportunity; List must report the documents it skips;
// and Drain must be transactional enough that a valid note is never lost to a
// sibling's failure. Every fixture here is the real store on disk.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// oversize writes a file larger than the per-file fingerprint cap, so the next
// scan that matches it is TRUNCATED by the core's own bound rather than by a
// test hook.
func oversize(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), fingerprintPerFile+4096), 0o600); err != nil {
		t.Fatal(err)
	}
}

// ── 1: a partial look is never a baseline ───────────────────────────────────

// A TRUNCATED FIRST LOOK SETS NO BASELINE AND KEEPS THE VISIBLE FLAG. The next
// COMPLETE look is the one that establishes the baseline and takes the flag
// down \u2014 quietly, because nothing changed that it could see.
func TestAPartialFirstLookNeverBecomesTheBaseline(t *testing.T) {
	now := time.Date(2026, 10, 5, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	workspace := t.TempDir()
	made, err := store.Create(newWatch(workspace, "*.sql"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// The ratifier could not arm it: the flag is visible on the item.
	if err := store.NoteNeedsPerson(made.ID, NeedsBaselineLead); err != nil {
		t.Fatalf("note: %v", err)
	}
	oversize(t, filepath.Join(workspace, "big.sql"))

	runner := &fakeRunner{}
	first := mustTick(t, newTicker(store, runner, now))
	if first.Fired != 0 {
		t.Fatalf("a truncated first look fired: %+v", first)
	}
	mid, err := store.Get(made.ID)
	if err != nil {
		t.Fatal(err)
	}
	if mid.Fingerprint != "" {
		t.Fatalf("a partial first look was adopted as the baseline: %q", mid.Fingerprint)
	}
	if !IsBaselineLine(mid.NeedsPerson) {
		t.Fatalf("the incomplete flag was lost: %q", mid.NeedsPerson)
	}

	// The complete scan: the oversized match is gone and a small file remains.
	if err := os.Remove(filepath.Join(workspace, "big.sql")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "schema.sql"), []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}
	store.clock = held(now.Add(time.Minute))
	second := mustTick(t, newTicker(store, runner, now.Add(time.Minute)))
	if second.Fired != 0 {
		t.Fatalf("the first complete look fired: %+v", second)
	}
	base, err := store.Get(made.ID)
	if err != nil {
		t.Fatal(err)
	}
	if base.Fingerprint == "" || strings.HasPrefix(base.Fingerprint, truncatedPrefix) {
		t.Fatalf("the first complete look did not establish a complete baseline: %q", base.Fingerprint)
	}
	if base.NeedsPerson != "" {
		t.Fatalf("the flag survived the complete look that set the baseline: %q", base.NeedsPerson)
	}

	// A real later change, complete, fires.
	if err := os.WriteFile(filepath.Join(workspace, "schema.sql"), []byte("two"), 0o600); err != nil {
		t.Fatal(err)
	}
	store.clock = held(now.Add(2 * time.Minute))
	if pass := mustTick(t, newTicker(store, runner, now.Add(2*time.Minute))); pass.Fired != 1 {
		t.Fatalf("a real change after the baseline did not fire: %+v", pass)
	}
}

// AN ESTABLISHED BASELINE SURVIVES A PARTIAL SCAN, AND THE SAME COMPLETE SCAN IS
// STILL QUIET. A truncated reading in between must not be compared with the
// complete baseline and must not be written over it.
func TestAnEstablishedBaselineSurvivesAPartialScan(t *testing.T) {
	now := time.Date(2026, 10, 5, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "schema.sql"), []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}
	made, err := store.Create(newWatch(workspace, "*.sql"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := store.Arm(made.ID); err != nil {
		t.Fatalf("arm: %v", err)
	}
	runner := &fakeRunner{}
	mustTick(t, newTicker(store, runner, now))
	armed, err := store.Get(made.ID)
	if err != nil {
		t.Fatal(err)
	}
	baseline := armed.Fingerprint
	if baseline == "" || strings.HasPrefix(baseline, truncatedPrefix) {
		t.Fatalf("the arm did not set a complete baseline: %q", baseline)
	}

	// A partial scan: an oversized match appears beside the watched file.
	oversize(t, filepath.Join(workspace, "big.sql"))
	store.clock = held(now.Add(time.Minute))
	if pass := mustTick(t, newTicker(store, runner, now.Add(time.Minute))); pass.Fired != 0 {
		t.Fatalf("a truncated scan against a complete baseline fired: %+v", pass)
	}
	afterPartial, err := store.Get(made.ID)
	if err != nil {
		t.Fatal(err)
	}
	if afterPartial.Fingerprint != baseline {
		t.Fatalf("a truncated scan overwrote the complete baseline: %q \u2192 %q", baseline, afterPartial.Fingerprint)
	}

	// The same complete set as before: still quiet, still the same baseline.
	if err := os.Remove(filepath.Join(workspace, "big.sql")); err != nil {
		t.Fatal(err)
	}
	store.clock = held(now.Add(2 * time.Minute))
	if pass := mustTick(t, newTicker(store, runner, now.Add(2*time.Minute))); pass.Fired != 0 {
		t.Fatalf("the same complete scan was misread as a change: %+v", pass)
	}
	afterComplete, err := store.Get(made.ID)
	if err != nil {
		t.Fatal(err)
	}
	if afterComplete.Fingerprint != baseline {
		t.Fatalf("the recovered complete scan changed the baseline: %q", afterComplete.Fingerprint)
	}

	// And a real change still fires.
	if err := os.WriteFile(filepath.Join(workspace, "schema.sql"), []byte("two"), 0o600); err != nil {
		t.Fatal(err)
	}
	store.clock = held(now.Add(3 * time.Minute))
	if pass := mustTick(t, newTicker(store, runner, now.Add(3*time.Minute))); pass.Fired != 1 {
		t.Fatalf("a real change after the partial scan did not fire: %+v", pass)
	}
}

// A PARTIAL DIGEST AN EARLIER BUILD WROTE IS READ AS NO BASELINE. The document
// carries a "t\u2026" fingerprint, and a complete scan must treat it as unset \u2014
// establishing the baseline quietly rather than firing on the fabricated change.
func TestAStoredPartialFingerprintIsTreatedAsNoBaseline(t *testing.T) {
	now := time.Date(2026, 10, 5, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "schema.sql"), []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}
	made, err := store.Create(newWatch(workspace, "*.sql"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	legacy, err := store.Get(made.ID)
	if err != nil {
		t.Fatal(err)
	}
	legacy.Fingerprint = truncatedPrefix + "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	if err := store.write(legacy); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{}
	if pass := mustTick(t, newTicker(store, runner, now)); pass.Fired != 0 {
		t.Fatalf("a stored partial digest was compared with a complete one: %+v", pass)
	}
	after, err := store.Get(made.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Fingerprint == "" || strings.HasPrefix(after.Fingerprint, truncatedPrefix) {
		t.Fatalf("the complete look did not replace the stored partial digest: %q", after.Fingerprint)
	}
}

// ── 4: a billed unknown whose accounting cannot be stored keeps its moment ──

// A LEDGER THAT CANNOT BE WRITTEN DOES NOT CONSUME THE OPPORTUNITY. The judge is
// billed and answers Unknown; the ledger append fails; the item's next-due on
// disk is exactly where it was, the cost that reached the item's own figure is
// kept, and the failure is counted.
func TestABilledUnknownWhoseLedgerCannotBeWrittenKeepsTheLook(t *testing.T) {
	now := time.Date(2026, 10, 5, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	made, err := store.Create(newProbe("tell me when CI is red"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// Give the item an opportunity it already had, in the past, so an advance
	// is unmistakable.
	original, err := store.Get(made.ID)
	if err != nil {
		t.Fatal(err)
	}
	original.NextDue = now.Add(-time.Minute)
	if err := store.write(original); err != nil {
		t.Fatal(err)
	}
	// THE LEDGER PATH IS A DANGLING LINK: reads see no file, an append cannot
	// create the missing target, and it works the same as any euid.
	if err := os.Symlink(filepath.Join(t.TempDir(), "gone", "ledger.jsonl"), store.LedgerPath(now)); err != nil {
		t.Fatalf("seed dangling ledger: %v", err)
	}

	const billed = 0.25
	refusing := func(context.Context, Judgment) (SentinelReading, string, float64, error) {
		return VerdictUnknown, "the provider refused", billed, errors.New("the provider refused")
	}
	ticker := newTicker(store, &fakeRunner{}, now)
	ticker.SentinelVerdict = refusing
	pass := mustTick(t, ticker)
	if pass.Errors == 0 {
		t.Fatalf("a ledger write failure was not counted: %+v", pass)
	}

	after, err := store.Get(made.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !after.NextDue.Equal(original.NextDue) {
		t.Fatalf("a billed accounting failure consumed the look: next-due %s \u2192 %s", original.NextDue, after.NextDue)
	}
	if after.Runs != 0 || after.Status != StatusActive {
		t.Fatalf("the errored look recorded a firing: %+v", after)
	}
	// THE COST THAT REACHED THE ITEM'S OWN FIGURE IS KEPT (NoteSpend succeeded);
	// the ledger line the daily rail reads could not be written, and that was
	// surfaced rather than swallowed.
	if after.SpentUSD != billed {
		t.Fatalf("the item's lifetime figure is %v, want the billed %v", after.SpentUSD, billed)
	}

	// With the ledger reachable again, the next billed unknown adds again and
	// still consumes nothing.
	if err := os.Remove(store.LedgerPath(now)); err != nil {
		t.Fatal(err)
	}
	store.clock = held(now.Add(time.Minute))
	laterTicker := newTicker(store, &fakeRunner{}, now.Add(time.Minute))
	laterTicker.SentinelVerdict = refusing
	later := mustTick(t, laterTicker)
	if later.Errors != 0 {
		t.Fatalf("a writable ledger still errored: %+v", later)
	}
	settled, err := store.Get(made.ID)
	if err != nil {
		t.Fatal(err)
	}
	if settled.SpentUSD != 2*billed {
		t.Fatalf("the second billed unknown lost the first: %v", settled.SpentUSD)
	}
	if !settled.NextDue.Equal(original.NextDue) {
		t.Fatalf("the undecided look consumed the opportunity: next-due %s", settled.NextDue)
	}
}

// ── 6: skipped documents are reported, never silent ─────────────────────────

// LISTCHECKED REPORTS WHAT LIST ERASES, AND LIST STILL ERASES IT. A doc from the
// future, a corrupt one and an unreadable one are all skipped; the readable item
// survives and the skips come back with a reason.
func TestListCheckedReportsSkippedDocuments(t *testing.T) {
	now := time.Date(2026, 10, 5, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	if _, err := store.Create(reminder("remind me at 6 to leave", now.Add(time.Hour))); err != nil {
		t.Fatal(err)
	}
	future := reminder("from the future", now.Add(2*time.Hour))
	future.ID = "ffffffffffffffff"
	future.Schema = Schema + 1
	raw, err := json.Marshal(future)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.ItemPath(future.ID), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.ItemPath("eeeeeeeeeeeeeeee"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(t.TempDir(), "gone.json"), store.ItemPath("dddddddddddddddd")); err != nil {
		t.Fatal(err)
	}

	items, skipped, err := store.ListChecked()
	if err != nil {
		t.Fatalf("ListChecked: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("readable items = %d, want the one", len(items))
	}
	if len(skipped) != 3 {
		t.Fatalf("skipped = %+v, want the future, the corrupt and the unreadable", skipped)
	}
	joined := ""
	for _, doc := range skipped {
		joined += doc.Name + " " + doc.Reason + "\n"
	}
	if !strings.Contains(joined, "ffffffffffffffff") || !strings.Contains(joined, "newer codeaf") {
		t.Fatalf("the future document was not reported with its reason: %s", joined)
	}
	if !strings.Contains(joined, "eeeeeeeeeeeeeeee") {
		t.Fatalf("the corrupt document was not reported: %s", joined)
	}
	if !strings.Contains(joined, "dddddddddddddddd") {
		t.Fatalf("the unreadable document was not reported: %s", joined)
	}

	// List keeps its erased shape for every existing caller.
	plain, err := store.List()
	if err != nil || len(plain) != 1 {
		t.Fatalf("List = %d items (%v), want the one", len(plain), err)
	}
}

// A PASS SAYS SO. The skip is in the pass, and it is in the wake log's own line
// \u2014 the place "last wake" and /status read \u2014 so a quiet-looking home is never
// hiding a document this build could not read.
func TestATickSurfacesSkippedDocumentsInTheWakeLog(t *testing.T) {
	now := time.Date(2026, 10, 5, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	if _, err := store.Create(reminder("remind me at 6 to leave", now.Add(time.Hour))); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.ItemPath("eeeeeeeeeeeeeeee"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	pass := mustTick(t, newTicker(store, &fakeRunner{}, now))
	if pass.Unread != 1 {
		t.Fatalf("the pass reported %d unread documents, want one: %q", pass.Unread, pass.UnreadWhy)
	}
	if !strings.Contains(pass.UnreadWhy, "eeeeeeeeeeeeeeee") {
		t.Fatalf("the pass's unread line does not name the document: %q", pass.UnreadWhy)
	}
	raw, err := os.ReadFile(store.WakeLogPath())
	if err != nil {
		t.Fatalf("wake log: %v", err)
	}
	if !strings.Contains(string(raw), "unread=1") || !strings.Contains(string(raw), "eeeeeeeeeeeeeeee") {
		t.Fatalf("the wake log does not carry the skip: %s", raw)
	}
}

// ── 2: the drain is transactional enough not to lose a note ─────────────────

// stageFile writes one raw staged inbox file, the shape a crashed or racing
// drain leaves behind.
func stageFile(t *testing.T, dir, tag, content string) string {
	t.Helper()
	path := filepath.Join(dir, "inbox.jsonl."+tag+".draining")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// noteLine is one staged note in the inbox's own JSONL shape.
func noteLine(t *testing.T, note Note) string {
	t.Helper()
	raw, err := json.Marshal(note)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw) + "\n"
}

// A DEDUP RECORD THAT CANNOT BE USED DELETES NOTHING. The payload is retained
// and the notes are still returned, so a retry re-hands them rather than losing
// them — the same path a write failure of the record takes, since neither may
// rewrite the record or remove a file.
func TestDrainKeepsThePayloadWhenTheDedupRecordCannotBeUsed(t *testing.T) {
	dir := t.TempDir()
	id := "keep-across-seen-failure"
	path := stageFile(t, dir, "aaa", noteLine(t, Note{At: time.Now(), ItemID: "i", Words: "keep main green", Kind: "said", Text: "the fix landed", ID: id}))
	// inbox.seen is a DIRECTORY, so the atomic rename of the seen record fails.
	if err := os.Mkdir(seenPath(dir), 0o700); err != nil {
		t.Fatal(err)
	}

	notes, err := drainAndAck(t, dir)
	if err == nil {
		t.Fatal("a seen-record write failure was silent")
	}
	if len(notes) != 1 || notes[0].ID != id {
		t.Fatalf("the valid note was lost: %+v (err %v)", notes, err)
	}
	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatalf("the payload was deleted after a failed seen write: %v", statErr)
	}

	// Retrying after the record can be written hands the same note again \u2014 a
	// duplicate, never a loss \u2014 and only then removes the file.
	if err := os.Remove(seenPath(dir)); err != nil {
		t.Fatal(err)
	}
	again, err := drainAndAck(t, dir)
	if err != nil {
		t.Fatalf("retry drain: %v", err)
	}
	if len(again) != 1 || again[0].ID != id {
		t.Fatalf("the payload was not re-handed on retry: %+v", again)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("the drained file survived a successful drain: %v", statErr)
	}
}

// A VALID FILE IS HANDED OUT EVEN WHEN A SIBLING CANNOT BE READ WHOLE, and the
// unreadable sibling is kept rather than deleted.
func TestDrainHandsValidNotesWhenASiblingFileCannotBeRead(t *testing.T) {
	dir := t.TempDir()
	id := "survives-a-malformed-sibling"
	keep := stageFile(t, dir, "aaa", noteLine(t, Note{At: time.Now(), ItemID: "i", Words: "keep main green", Kind: "said", Text: "the fix landed", ID: id}))
	// A line longer than the scanner's buffer: readInbox cannot read it whole.
	broken := stageFile(t, dir, "bbb", strings.Repeat("x", inboxMaxLine+1024))

	notes, err := drainAndAck(t, dir)
	if err == nil {
		t.Fatal("an unreadable inbox file was silent")
	}
	if len(notes) != 1 || notes[0].ID != id {
		t.Fatalf("the valid note was lost to a malformed sibling: %+v", notes)
	}
	if _, statErr := os.Stat(keep); !os.IsNotExist(statErr) {
		t.Fatalf("the read-whole file was not removed: %v", statErr)
	}
	if _, statErr := os.Stat(broken); statErr != nil {
		t.Fatalf("the unreadable file was deleted: %v", statErr)
	}
}

// ONE IDENTITY IS ONE NOTE ACROSS THE WHOLE DRAIN. The same id staged in two
// files \u2014 the shape a lost acknowledgement leaves \u2014 reaches the person once.
func TestDrainDedupsOneIdentityAcrossStagedFiles(t *testing.T) {
	dir := t.TempDir()
	note := Note{At: time.Now(), ItemID: "i", Words: "keep main green", Kind: "said", Text: "the fix landed", ID: "one-identity-two-files"}
	stageFile(t, dir, "aaa", noteLine(t, note))
	stageFile(t, dir, "bbb", noteLine(t, note))

	notes, err := drainAndAck(t, dir)
	if err != nil {
		t.Fatalf("drain: %v", err)
	}
	if len(notes) != 1 || notes[0].ID != note.ID {
		t.Fatalf("one identity across two files reached the person as %+v", notes)
	}
}

// AN UNREADABLE DEDUP RECORD IS SURFACED, NOT READ AS EMPTY. [Deliver] cannot
// verify the identity, so it appends anyway — a duplicate is the safe cost —
// and returns the failure; [Drain] returns the notes it read whole but refuses
// to rewrite the record or delete the payload, so a retry re-hands them.
func TestDeliverSurfacesAnUnreadableDedupRecordAndStillDelivers(t *testing.T) {
	dir := t.TempDir()
	// inbox.seen is a DIRECTORY: the scanner cannot read it.
	if err := os.Mkdir(seenPath(dir), 0o700); err != nil {
		t.Fatal(err)
	}
	note := Note{At: time.Now(), ItemID: "i", Words: "keep main green", Kind: "said", Text: "the fix landed", ID: "delivered-despite-the-record"}
	if err := Deliver(dir, note); err == nil {
		t.Fatal("an unreadable dedup record was a silent success")
	}

	// The note is on disk anyway, so the person is not made to wait for the
	// record to be fixed.
	notes, err := drainAndAck(t, dir)
	if err == nil {
		t.Fatal("Drain read the unreadable dedup record as empty")
	}
	if len(notes) != 1 || notes[0].ID != note.ID {
		t.Fatalf("the delivered note was lost: %+v", notes)
	}
	// AND NOTHING WAS REWRITTEN OR DELETED: the payload is retained.
	staged, err := drainFiles(dir)
	if err != nil || len(staged) != 1 {
		t.Fatalf("the payload was deleted under an unreadable record: %v (%v)", staged, err)
	}

	// Fix the record and retry: the note is handed over again — a duplicate,
	// never a loss — and only now is the file removed.
	if err := os.Remove(seenPath(dir)); err != nil {
		t.Fatal(err)
	}
	again, err := drainAndAck(t, dir)
	if err != nil {
		t.Fatalf("retry drain: %v", err)
	}
	if len(again) != 1 || again[0].ID != note.ID {
		t.Fatalf("the retained payload was not re-handed: %+v", again)
	}
}

// A CORRUPT LINE IS REPORTED AND THE DEDUP STATE BESIDE IT SURVIVES. The drain
// reads the record to skip what was already acknowledged, reports the corruption,
// and an acknowledged identity is still honoured: its staged file is retired
// without being shown again, and a later delivery of it is refused.
func TestDrainReportsACorruptDedupLineAndKeepsDedupState(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(seenPath(dir), []byte("already-drained\n\xff\xfe not an identity\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	stageFile(t, dir, "aaa", noteLine(t, Note{At: time.Now(), ItemID: "i", Words: "keep main green", Kind: "said", Text: "the fix landed", ID: "fresh-1"}))
	stageFile(t, dir, "bbb", noteLine(t, Note{At: time.Now(), ItemID: "i", Words: "keep main green", Kind: "said", Text: "already seen", ID: "already-drained"}))

	files, err := Drain(dir)
	if err == nil {
		t.Fatal("a corrupt dedup line was silent")
	}
	var handed []Note
	for _, file := range files {
		handed = append(handed, file.Notes...)
	}
	if len(handed) != 1 || handed[0].ID != "fresh-1" {
		t.Fatalf("the drain handed %+v, want only the fresh note", handed)
	}
	for _, file := range files {
		if ackErr := file.Ack(); ackErr != nil {
			t.Fatalf("ack: %v", ackErr)
		}
	}
	raw, err := os.ReadFile(seenPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "already-drained") || !strings.Contains(string(raw), "fresh-1") {
		t.Fatalf("the dedup state was lost in the rewrite: %q", raw)
	}
	if strings.Contains(string(raw), " not an identity") {
		t.Fatalf("the corrupt line was trusted into the rewritten record: %q", raw)
	}

	// The identity already in the record is still honoured: a later delivery of
	// it is refused, and the fold stays empty.
	if err := Deliver(dir, Note{At: time.Now(), ID: "already-drained", Text: "again"}); !errors.Is(err, ErrAlreadyDrained) {
		t.Fatalf("Deliver after the rewrite answered %v, wanted ErrAlreadyDrained", err)
	}
	after, err := drainAndAck(t, dir)
	if err != nil {
		t.Fatalf("Drain after the rewrite: %v", err)
	}
	if len(after) != 0 {
		t.Fatalf("a drained identity reached the person again: %+v", after)
	}
}

// A RECORD THAT CANNOT BE OPENED IS NOT "NOT DRAINED". A permission failure is
// surfaced the same way the scanner failure is.
func TestADedupRecordThatCannotBeOpenedIsSurfaced(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores the permission bit this test needs")
	}
	dir := t.TempDir()
	if err := os.WriteFile(seenPath(dir), []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(seenPath(dir), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(seenPath(dir), 0o600) })
	if err := Deliver(dir, Note{At: time.Now(), ID: "blocked-record", Text: "x"}); err == nil {
		t.Fatal("an unopenable dedup record was a silent success")
	}
}
