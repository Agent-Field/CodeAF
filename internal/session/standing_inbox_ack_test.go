package session

// standing_inbox_ack_test.go proves the durable at-least-once handoff of the
// standing inbox: the fold is offered live ON TOP of a durable note, a delivery
// whose identity the inbox has already durably drained is NOT offered live or
// steered again (the PR1777 external review's defect 4), and an unacknowledged
// drain is handed over again rather than lost.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

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

// THE FOLD ACKNOWLEDGES ITS INBOX ONLY AFTER ITS JOURNAL LINE SETTLES. This is
// the production crash stage: with the fold merely QUEUED (volatile) the staged
// file stays, so a crash loses nothing; the acknowledgement runs from the
// delivery's settle callback, which the journal invokes only once the fold line
// is written down.
func TestTheFoldAcknowledgesItsInboxOnlyAfterTheJournalSettles(t *testing.T) {
	workspace := t.TempDir()
	agent := standingLiveAgent(t, workspace, nil)
	dir := t.TempDir()
	agent.mu.Lock()
	agent.config.Place = Place{Dir: dir, Workspace: workspace}
	agent.mu.Unlock()

	// Deliver a note to that conversation's inbox as a firing would.
	item := standing.Item{
		ID:        "item-settle",
		Words:     "tell me when CI goes red",
		Workspace: workspace,
		Origin:    standing.Origin{SessionID: "closed", Transcript: filepath.Join(dir, "transcript.jsonl")},
	}
	runner := &standingRunner{}
	if _, err := runner.Deliver(context.Background(), item, standing.Pending{ID: "settle-1", Kind: standing.ActionSay, Text: "the last run failed"}); err != nil {
		t.Fatalf("Deliver: %v", err)
	}

	agent.drainStandingInbox()

	// The fold is queued with one durable delivery per note, and the staged file
	// is STILL THERE: a crash now re-hands the fold rather than losing it.
	deliveries := 0
	agent.mu.Lock()
	for _, note := range agent.steering {
		deliveries += len(note.delivered)
	}
	agent.mu.Unlock()
	if deliveries == 0 {
		t.Fatal("the queued fold carries no durable delivery")
	}
	if staged := countStagedInboxFiles(t, dir); staged == 0 {
		t.Fatal("the inbox file was acknowledged while the fold was still only queued")
	}

	// THE JOURNAL LINE IS WRITTEN: settle the deliveries the way recordUserLocked
	// would, and only now is the file acknowledged.
	agent.mu.Lock()
	var settle []durableDelivery
	for _, note := range agent.steering {
		settle = append(settle, note.delivered...)
	}
	agent.mu.Unlock()
	for _, delivery := range settle {
		if delivery.settled != nil {
			delivery.settled()
		}
	}
	if staged := countStagedInboxFiles(t, dir); staged != 0 {
		t.Fatalf("the acknowledged inbox left %d staged file(s)", staged)
	}
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
