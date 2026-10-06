package session

// standing_inbox_ack_test.go proves the durable at-least-once handoff of the
// standing inbox: the fold is offered live ON TOP of a durable note, a delivery
// whose identity the inbox has already durably drained is NOT offered live or
// steered again (the PR1777 external review's defect 4), and an unacknowledged
// drain is handed over again rather than lost.

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/standing"
)

// drainStanding reads the inbox and acknowledges every file in one step: the
// shape a test that just wants the notes uses. It returns the notes and the join
// of the read and the acknowledgement failures.
func drainStanding(t *testing.T, dir string) ([]standing.Note, error) {
	t.Helper()
	files, err := standing.Drain(dir)
	var notes []standing.Note
	for _, file := range files {
		notes = append(notes, file.Notes...)
		if ackErr := file.Ack(); ackErr != nil {
			err = errors.Join(err, ackErr)
		}
	}
	sort.SliceStable(notes, func(i, j int) bool { return notes[i].At.Before(notes[j].At) })
	return notes, err
}

// A RETRIED ONE-SHOT IS NOT OFFERED LIVE AFTER THE INBOX HAS DURABLY DRAINED IT.
// The line already reached the person through the fold, so the retry must settle
// the intent without putting the same sentence in front of them again. THE
// REVIEW'S DEFECT 4.
func TestASpentDeliveryIsNotOfferedLiveAgain(t *testing.T) {
	workspace := t.TempDir()
	room := standingLiveAgent(t, workspace, nil)
	dir := t.TempDir()
	item := standing.Item{
		ID:        "item-dup",
		Words:     "tell me once when ready",
		Workspace: workspace,
		Origin:    standing.Origin{SessionID: room.id, Transcript: filepath.Join(dir, "transcript.jsonl")},
	}
	runner := &standingRunner{}
	pending := standing.Pending{ID: "dup-1", Kind: standing.ActionSay, Text: "the last run failed"}

	if _, err := runner.Deliver(context.Background(), item, pending); err != nil {
		t.Fatalf("first Deliver: %v", err)
	}
	if queued := standingQueued(room); len(queued) != 1 {
		t.Fatalf("first delivery queued %d lines, wanted one: %v", len(queued), queued)
	}
	// The person opens the window, the fold is built, and its line is durably
	// recorded: the file is acknowledged, so the identity is spent.
	if notes, err := drainStanding(t, dir); err != nil || len(notes) != 1 {
		t.Fatalf("drain answered %+v (err %v)", notes, err)
	}
	// The process died before the ledger line; the intent is retried. The inbox
	// refuses the append, and NO live row or steering line is made.
	if _, err := runner.Deliver(context.Background(), item, pending); err != nil {
		t.Fatalf("retried Deliver: %v", err)
	}
	if queued := standingQueued(room); len(queued) != 1 {
		t.Fatalf("a spent delivery was offered live again: %v", queued)
	}
}

// BEFORE THE ACKNOWLEDGEMENT THE IDENTITY IS NOT SPENT: a retry is still
// accepted (the file may never have reached a surviving caller), and one
// identity is still one note.
func TestAnUnacknowledgedDrainLeavesTheIdentityUnspent(t *testing.T) {
	dir := t.TempDir()
	item := offlineItem(dir)
	runner := &standingRunner{}
	pending := standing.Pending{ID: "unack-1", Kind: standing.ActionSay, Text: "the last run failed"}
	if _, err := runner.Deliver(context.Background(), item, pending); err != nil {
		t.Fatalf("first Deliver: %v", err)
	}
	files, err := standing.Drain(dir)
	if err != nil {
		t.Fatalf("Drain: %v", err)
	}
	if len(files) != 1 || len(files[0].Notes) != 1 {
		t.Fatalf("drain answered %+v", files)
	}
	// No acknowledgement yet: the same delivery is still accepted.
	if _, err := runner.Deliver(context.Background(), item, pending); err != nil {
		t.Fatalf("retried Deliver before ack: %v", err)
	}
	// One identity across the whole drain, however many times it was appended.
	again, err := standing.Drain(dir)
	if err != nil {
		t.Fatalf("second Drain: %v", err)
	}
	count := 0
	for _, file := range again {
		count += len(file.Notes)
		if ackErr := file.Ack(); ackErr != nil {
			t.Fatalf("ack: %v", ackErr)
		}
	}
	if count != 1 {
		t.Fatalf("an unacknowledged identity became %d notes", count)
	}
}

// A REOPENED WINDOW THAT NEVER TYPED STILL SHOWS THE NOTICE ONCE, AND THE
// SECOND OPEN IS QUIET. This is the whole point of recording the fold at
// ARRIVAL rather than at the next turn: the delivery receipt reaches the
// journal the moment the inbox is drained, so the second open finds the file
// acknowledged instead of replaying the one-time notice until somebody types.
// No model call is made and no turn runs.
func TestAReopenedWindowShowsTheNoticeOnceAndTheSecondOpenIsQuiet(t *testing.T) {
	dir := t.TempDir()
	config := func() Config {
		return Config{
			Workspace:   t.TempDir(),
			Model:       "test/model",
			System:      "SYSTEM",
			SessionFile: filepath.Join(dir, "session.jsonl"),
			Standing:    &Standing{},
		}
	}
	agent, err := newAgent(config(), &scriptedCompleter{})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	const spoken = "the last run failed on main"
	if err := standing.Deliver(dir, standing.Note{
		At: time.Now(), ItemID: "item-once", Words: "tell me when CI goes red",
		Kind: "said", Text: spoken, ID: "once-1",
	}); err != nil {
		t.Fatalf("Deliver: %v", err)
	}

	// The window opens: the drain records the fold and retires the file, with
	// nothing submitted and no turn running.
	agent.drainStandingInbox()
	if got := foldOccurrences(agent, spoken); got != 1 {
		t.Fatalf("the fold reached the transcript %d time(s), want once", got)
	}
	agent.mu.Lock()
	queued := len(agent.steering)
	running := agent.running
	agent.mu.Unlock()
	if queued != 0 {
		t.Fatalf("the recorded fold was ALSO left on the steering queue (%d line(s)); a later turn would repeat it", queued)
	}
	if running {
		t.Fatal("draining the inbox started a turn")
	}
	if staged := countStagedInboxFiles(t, dir); staged != 0 {
		t.Fatalf("the arrival left %d staged file(s); the receipt did not precede the ack", staged)
	}
	if err := agent.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Tomorrow, the SAME journal, a fresh process, and again nobody types. The
	// content is still there, exactly once.
	resumed, err := newAgent(config(), &scriptedCompleter{})
	if err != nil {
		t.Fatalf("newAgent (resume): %v", err)
	}
	defer func() { _ = resumed.Close() }()
	if got := foldOccurrences(resumed, spoken); got != 1 {
		t.Fatalf("after reconstructing the same journal the fold appears %d time(s), want once", got)
	}
	if staged := countStagedInboxFiles(t, dir); staged != 0 {
		t.Fatalf("the second open found %d staged file(s); a one-time notice was replayed without a turn", staged)
	}
}

// A CRASH BETWEEN THE RECORD AND THE ACK LOSES NOTHING AND REPEATS NOTHING.
// The journal already holds the receipt, so the residual staged file is retired
// without a second fold: the recovery reads the record, not the file.
func TestACrashBetweenTheFoldRecordAndTheAckIsRetiredQuietly(t *testing.T) {
	dir := t.TempDir()
	config := func() Config {
		return Config{
			Workspace:   t.TempDir(),
			Model:       "test/model",
			System:      "SYSTEM",
			SessionFile: filepath.Join(dir, "session.jsonl"),
			Standing:    &Standing{},
		}
	}
	agent, err := newAgent(config(), &scriptedCompleter{})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	const spoken = "the backup job finished"
	if err := standing.Deliver(dir, standing.Note{
		At: time.Now(), ItemID: "item-crash", Words: "tell me when the backup ends",
		Kind: "landed", Text: spoken, ID: "crash-1",
	}); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	live := standing.InboxPath(dir)
	stagedBytes, err := os.ReadFile(live)
	if err != nil {
		t.Fatalf("read the live inbox: %v", err)
	}
	agent.drainStandingInbox()
	if err := agent.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// The crash state: the record was written and the file was never removed.
	residual := filepath.Join(dir, filepath.Base(live)+".crash.draining")
	if err := os.WriteFile(residual, stagedBytes, 0o600); err != nil {
		t.Fatalf("write the residual staged file: %v", err)
	}

	reopened, err := newAgent(config(), &scriptedCompleter{})
	if err != nil {
		t.Fatalf("newAgent (resume): %v", err)
	}
	defer func() { _ = reopened.Close() }()
	if got := foldOccurrences(reopened, spoken); got != 1 {
		t.Fatalf("the crash recovery folded the notice %d time(s), want once", got)
	}
	if staged := countStagedInboxFiles(t, dir); staged != 0 {
		t.Fatalf("the residual file was not retired: %d staged file(s) remain", staged)
	}
}

// foldOccurrences counts transcript messages that carry one unique string.
func foldOccurrences(agent *Agent, want string) int {
	agent.mu.Lock()
	defer agent.mu.Unlock()
	n := 0
	for _, message := range agent.messages {
		if strings.Contains(messageContentText(message), want) {
			n++
		}
	}
	return n
}

// countStagedInboxFiles counts this conversation folder's staged inbox files.
func countStagedInboxFiles(t *testing.T, dir string) int {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, filepath.Base(standing.InboxPath(dir))) {
			n++
		}
	}
	return n
}

// transcriptLine answers the drawn entry that carries want, and whether one
// exists, so a test can read the LANE a line lands in — "aside" is a line the
// session wrote, and a person's own words never are ([Agent.Transcript]).
func transcriptLine(agent *Agent, want string) (DisplayEntry, bool) {
	for _, entry := range agent.Transcript() {
		if strings.Contains(entry.Text, want) {
			return entry, true
		}
	}
	return DisplayEntry{}, false
}

// journalNoteLines answers the message entries a journal holds that carry the
// note mark, so a test can read WHAT the fold was written as rather than only how
// many times it replays.
func journalNoteLines(t *testing.T, path string) []sessionEntry {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the journal: %v", err)
	}
	var notes []sessionEntry
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var entry sessionEntry
		if json.Unmarshal([]byte(line), &entry) != nil {
			continue
		}
		if entry.Type == "message" && entry.Note {
			notes = append(notes, entry)
		}
	}
	return notes
}

// THE FOLD IS THE SESSION'S OWN NOTE, AND ITS DELIVERY IDENTITY IS DURABLE BEFORE
// THE FILE IS ACKNOWLEDGED. The journal line carries the note mark and the inbox
// identity together (so hasRecorded answers), and the person's folder learns
// nothing: no last-spoke stamp and no placeholder title. THE REVIEW'S F1.
func TestTheFoldIsADurableSessionNoteBeforeTheAck(t *testing.T) {
	dir := t.TempDir()
	place := Place{Dir: dir, Workspace: t.TempDir()}
	config := func() Config {
		return Config{
			Workspace: place.Workspace, Model: "test/model", System: "SYSTEM",
			SessionFile: place.Transcript(), Place: place, Standing: &Standing{},
		}
	}
	agent, err := newAgent(config(), &scriptedCompleter{})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	const spoken = "the backup job finished"
	if err := standing.Deliver(dir, standing.Note{
		At: time.Now(), ItemID: "item-note", Words: "tell me when the backup ends",
		Kind: "landed", Text: spoken, ID: "note-1",
	}); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	agent.drainStandingInbox()

	// The identity the delivery id names is IN THE JOURNAL, which is what makes
	// a crash-before-ack recoverable without a second fold.
	want := deliveryID(standingDeliveryID("note-1"))
	if !agent.hasRecorded(want) {
		t.Fatalf("the fold's delivery identity %q is NOT in the journal", want)
	}
	notes := journalNoteLines(t, place.Transcript())
	if len(notes) != 1 {
		t.Fatalf("the journal holds %d note line(s), want one", len(notes))
	}
	if notes[0].Role != "user" || !strings.Contains(notes[0].Content, spoken) {
		t.Fatalf("the fold line reads role=%q content=%q, want the user-role session note",
			notes[0].Role, notes[0].Content)
	}
	if got := notes[0].Deliveries; len(got) != 1 || got[0] != string(want) {
		t.Fatalf("the note carries deliveries %v, want [%s]", got, want)
	}
	// The session spoke, not the person: the folder says nobody did.
	agent.SettleWrites()
	meta, err := LoadMeta(dir)
	if err != nil {
		t.Fatalf("LoadMeta: %v", err)
	}
	if !meta.LastUserAt.IsZero() {
		t.Fatalf("the fold stamped LastUserAt %s; the person did not speak", meta.LastUserAt)
	}
	if meta.Title != "" {
		t.Fatalf("the fold named the conversation %q; no person typed those words", meta.Title)
	}
	// The live surface draws it in the session's own lane, not as a person's
	// message — the same role the e2e fold check waits for.
	if entry, found := transcriptLine(agent, spoken); !found || entry.Role != "aside" {
		t.Fatalf("the fold was drawn as %+v, want the session's own aside", entry)
	}
	if err := agent.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// THE TRUE CRASH STATE: the fold's journal line exists, the drained-identity
// record does NOT, and the file is still staged. The JOURNAL's own identity
// retires the file without folding again — which the ack-then-restage test above
// cannot prove, because its spent id sits in the drained-identity record. THE
// REVIEW'S F1, reproduced against a real journal reconstruction.
func TestTheJournalsIdentityRetiresACrashBeforeTheAck(t *testing.T) {
	dir := t.TempDir()
	place := Place{Dir: dir, Workspace: t.TempDir()}
	config := func() Config {
		return Config{
			Workspace: place.Workspace, Model: "test/model", System: "SYSTEM",
			SessionFile: place.Transcript(), Place: place, Standing: &Standing{},
		}
	}
	agent, err := newAgent(config(), &scriptedCompleter{})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	const spoken = "the deployment finished"
	if err := standing.Deliver(dir, standing.Note{
		At: time.Now(), ItemID: "item-gap", Words: "tell me when the deploy ends",
		Kind: "landed", Text: spoken, ID: "gap-1",
	}); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	staged, err := os.ReadFile(standing.InboxPath(dir))
	if err != nil {
		t.Fatalf("read the live inbox: %v", err)
	}
	agent.drainStandingInbox()
	if err := agent.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// UNDO THE ACK AS A CRASH WOULD HAVE: the record is durable, the file was
	// never removed and the drained-identity record was never written.
	if err := os.Remove(filepath.Join(dir, "inbox.seen")); err != nil {
		t.Fatalf("drop the drained-identity record: %v", err)
	}
	residual := filepath.Join(dir, filepath.Base(standing.InboxPath(dir))+".crash.draining")
	if err := os.WriteFile(residual, staged, 0o600); err != nil {
		t.Fatalf("re-stage the residual file: %v", err)
	}

	reopened, err := newAgent(config(), &scriptedCompleter{})
	if err != nil {
		t.Fatalf("newAgent (resume): %v", err)
	}
	defer func() { _ = reopened.Close() }()
	if got := foldOccurrences(reopened, spoken); got != 1 {
		t.Fatalf("after a crash before the ack the notice was folded %d time(s), want once", got)
	}
	if !reopened.hasRecorded(deliveryID(standingDeliveryID("gap-1"))) {
		t.Fatal("the reconstructed journal lost the fold's delivery identity")
	}
	if entry, found := transcriptLine(reopened, spoken); !found || entry.Role != "aside" {
		t.Fatalf("the reconstructed history drew the fold as %+v, want the session's own aside", entry)
	}
	if staged := countStagedInboxFiles(t, dir); staged != 0 {
		t.Fatalf("the crash recovery left %d staged file(s)", staged)
	}
}
