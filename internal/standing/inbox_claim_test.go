package standing

// inbox_claim_test.go is the focused proof of the claim/ack handoff and of the
// clipped-reading contract the PR1777 external review found broken. It also
// carries the two helpers the older inbox tests use to keep their fire-and-forget
// shape now that nothing is spent until an acknowledgement.

import (
	"errors"
	"os"
	"sort"
	"strings"
	"testing"
	"time"
)

// drainAndAck reads and acknowledges in one step: the shape a caller that only
// wants the notes uses. It returns the notes and the join of the read and the
// acknowledgement failures, so a test can still assert the package reported one.
func drainAndAck(t *testing.T, dir string) ([]Note, error) {
	t.Helper()
	files, err := Drain(dir)
	var notes []Note
	for _, file := range files {
		notes = append(notes, file.Notes...)
		if ackErr := file.Ack(); ackErr != nil {
			err = errors.Join(err, ackErr)
		}
	}
	sort.SliceStable(notes, func(i, j int) bool { return notes[i].At.Before(notes[j].At) })
	return notes, err
}

// drainProjectAndAck is [drainAndAck] at a project's address.
func drainProjectAndAck(t *testing.T, root, workspace string) ([]Note, error) {
	t.Helper()
	files, err := DrainProject(root, workspace)
	var notes []Note
	for _, file := range files {
		notes = append(notes, file.Notes...)
		if ackErr := file.Ack(); ackErr != nil {
			err = errors.Join(err, ackErr)
		}
	}
	sort.SliceStable(notes, func(i, j int) bool { return notes[i].At.Before(notes[j].At) })
	return notes, err
}

// AN UNACKNOWLEDGED DRAIN IS HANDED OVER AGAIN. The file stays, the record stays
// untouched, and a crash between the read and the acknowledgement is a duplicate
// rather than a loss. Acknowledging it is what retires the file and spends the
// identity. THE REVIEW'S DEFECT 5.
func TestAnUnacknowledgedDrainIsHandedOverAgain(t *testing.T) {
	dir := t.TempDir()
	note := Note{At: time.Now(), ItemID: "i", Kind: "said", Text: "the fix landed", ID: "handed-twice"}
	if err := Deliver(dir, note); err != nil {
		t.Fatalf("deliver: %v", err)
	}
	first, err := Drain(dir)
	if err != nil {
		t.Fatalf("first drain: %v", err)
	}
	if len(first) != 1 || len(first[0].Notes) != 1 || first[0].Notes[0].ID != note.ID {
		t.Fatalf("the first drain answered %+v", first)
	}
	path := first[0].Path()
	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatalf("the staged file was removed before the caller acknowledged: %v", statErr)
	}
	// A CRASH HERE IS THE WINDOW THE OLD CODE LOST: no acknowledgement ran, so
	// the same note is handed over again.
	second, err := Drain(dir)
	if err != nil {
		t.Fatalf("second drain: %v", err)
	}
	if len(second) != 1 || len(second[0].Notes) != 1 || second[0].Notes[0].ID != note.ID {
		t.Fatalf("an unacknowledged note was not recovered: %+v", second)
	}
	if err := second[0].Ack(); err != nil {
		t.Fatalf("ack: %v", err)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("the acknowledged file survived: %v", statErr)
	}
	if err := Deliver(dir, note); !errors.Is(err, ErrAlreadyDrained) {
		t.Fatalf("after an ack the identity must be spent; got %v", err)
	}
}

// A RESIDUAL FILE AFTER A SUCCESSFUL ACK IS RETIRED WITHOUT A REPLAY. The record
// is written before the file is removed, so a removal that failed leaves a file
// whose identity is already spent; the next drain skips the note and removes the
// file rather than putting the line in front of the person again.
func TestAResidualFileAfterAckIsRetiredWithoutReplay(t *testing.T) {
	dir := t.TempDir()
	note := Note{At: time.Now(), ItemID: "i", Kind: "said", Text: "the fix landed", ID: "residual-after-ack"}
	if err := Deliver(dir, note); err != nil {
		t.Fatalf("deliver: %v", err)
	}
	files, err := Drain(dir)
	if err != nil {
		t.Fatalf("drain: %v", err)
	}
	if err := files[0].Ack(); err != nil {
		t.Fatalf("ack: %v", err)
	}
	// THE REMOVAL FAILED, SO THE FILE IS STILL THERE: re-stage an identical note.
	stageFile(t, dir, "residual", noteLine(t, note))
	again, err := Drain(dir)
	if err != nil {
		t.Fatalf("drain after a residual file: %v", err)
	}
	if len(again) != 1 || len(again[0].Notes) != 0 {
		t.Fatalf("a spent identity was handed over again: %+v", again)
	}
	if err := again[0].Ack(); err != nil {
		t.Fatalf("ack the residual file: %v", err)
	}
}

// THE ACK, NOT THE DRAIN, IS WHAT MAKES AN IDENTITY SPENT. Before it the same
// identity can still be appended (the note may never have reached anyone), after
// it a retry is refused with [ErrAlreadyDrained].
func TestAckIsWhatMarksAnIdentitySpent(t *testing.T) {
	dir := t.TempDir()
	note := Note{At: time.Now(), ItemID: "i", Kind: "said", Text: "the fix landed", ID: "spend-me"}
	if err := Deliver(dir, note); err != nil {
		t.Fatalf("deliver: %v", err)
	}
	files, err := Drain(dir)
	if err != nil {
		t.Fatalf("drain: %v", err)
	}
	if len(files) != 1 || len(files[0].Notes) != 1 {
		t.Fatalf("drain answered %+v", files)
	}
	// UNACKNOWLEDGED: the identity is not spent, so a delivery is still accepted.
	if err := Deliver(dir, note); err != nil {
		t.Fatalf("deliver before ack: %v", err)
	}
	if err := files[0].Ack(); err != nil {
		t.Fatalf("ack: %v", err)
	}
	// ACKNOWLEDGED: a retry is refused, and the caller is told why.
	if err := Deliver(dir, note); !errors.Is(err, ErrAlreadyDrained) {
		t.Fatalf("a replayed delivery after an ack answered %v, wanted ErrAlreadyDrained", err)
	}
}

// A CLIPPED READING CANNOT CERTIFY AN UNCHANGED STATE. The production probe
// keeps only the tail and says so ([standingProbeReading]); a short text that
// came from a cut must carry no identity, or two readings that share a suffix
// suppress each other. THE REVIEW'S DEFECT 3.
func TestAClippedProbeReadingCannotCertifyAnIdentity(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	// The shape a production probe hands up: a tail of a much longer reading,
	// already under the clip.
	tail := "\u2026\n" + strings.Repeat("a log line\n", 20) + "the old failure"
	if len(tail) > ProbeClip {
		t.Fatalf("the repro tail is %d bytes, wanted under the clip", len(tail))
	}
	runner := &fakeRunner{evidence: tail, clipped: true}
	made, err := store.Create(newProbe("tell me when the test log shows a new failure"))
	if err != nil {
		t.Fatal(err)
	}
	yes, _ := countingSentinel(VerdictYes)
	if pass := mustTick(t, probeTicker(store, runner, yes, now)); pass.Fired != 1 {
		t.Fatalf("the clipped reading did not fire: %+v", pass)
	}
	after, _ := store.Get(made.ID)
	if after.Positive != "" {
		t.Fatalf("a clipped tail was stored as a full reading's identity: %q", after.Positive)
	}
	// WITH NO IDENTITY, a second look at the same tail is judged afresh rather
	// than passed off as "the same state".
	at := now.Add(6 * time.Minute)
	if pass := mustTick(t, probeTicker(store, runner, yes, at)); pass.Fired != 1 {
		t.Fatalf("a clipped reading was suppressed as unchanged: %+v", pass)
	}
}

// AN EDIT NEVER CLEARS RUNTIME STATE IT DID NOT SEE, AND NEVER RESURRECTS WHAT IT
// DID NOT SEE SETTLED. The row a surface hands back was drawn before the ticker
// committed the delivery intent or the task's in-flight marker; the person's save
// takes both from DISK, so the intent committed after the read is carried forward
// (this test) and one settled after the read is not brought back (store_test's
// TestSaveNeverResurrectsSettledRuntimeState). THE REVIEW'S DEFECT 6 and F4.
func TestSaveKeepsRuntimePendingAndInflight(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	made, err := store.Create(reminder("remind me at 6 to leave", now.Add(time.Hour)))
	if err != nil {
		t.Fatal(err)
	}
	stale, _ := store.Get(made.ID)
	live, _ := store.Get(made.ID)
	live.Pending = append(live.Pending, Pending{ID: "pending-runtime", Kind: ActionSay, Text: "a line", At: now})
	live.TaskInflight = &TaskInflight{RunDir: "runs/0001", Started: now, Attempts: 1}
	if err := store.saveActive(&live); err != nil {
		t.Fatalf("ticker write: %v", err)
	}
	stale.Words = "remind me at seven"
	if err := store.Save(stale); err != nil {
		t.Fatalf("person edit: %v", err)
	}
	back, _ := store.Get(made.ID)
	if len(back.Pending) != 1 || back.Pending[0].ID != "pending-runtime" {
		t.Fatalf("an edit dropped the delivery intent: %+v", back.Pending)
	}
	if back.TaskInflight == nil {
		t.Fatal("an edit dropped the in-flight marker")
	}
	if back.Words != "remind me at seven" {
		t.Fatalf("the edit itself was lost: %q", back.Words)
	}
}

// SETTING THE EFFORT MOVES ONE FIELD ON THE DOCUMENT ON DISK, so an intent or a
// marker committed while a surface held the row is not published away.
func TestSetStandingEffortKeepsRuntimeState(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	made, err := store.Create(reminder("remind me at 6 to leave", now.Add(time.Hour)))
	if err != nil {
		t.Fatal(err)
	}
	live, _ := store.Get(made.ID)
	live.Pending = append(live.Pending, Pending{ID: "effort-pending", Kind: ActionSay, Text: "a line", At: now})
	live.TaskInflight = &TaskInflight{RunDir: "runs/0002", Started: now, Attempts: 1}
	if err := store.saveActive(&live); err != nil {
		t.Fatalf("ticker write: %v", err)
	}
	if err := store.SetStandingEffort(made.ID, "high"); err != nil {
		t.Fatalf("SetStandingEffort: %v", err)
	}
	back, _ := store.Get(made.ID)
	if len(back.Pending) != 1 || back.TaskInflight == nil {
		t.Fatalf("moving the effort dropped runtime state: pending=%d inflight=%v", len(back.Pending), back.TaskInflight)
	}
	if back.Does.Effort != "high" {
		t.Fatalf("the effort did not move: %q", back.Does.Effort)
	}
}
