package session

// standing_fold_test.go holds round 3b of the review of the fold — the
// "while you were away" note an inbox becomes — and its settlement: the files
// it came from are removed only once the conversation's record holds it.
//
// The third review (c7ec2566f..4701f270d) found the settlement going ahead
// after the record's sync had failed (B4), and the agent's memory of what it
// had folded growing for ever and being scanned whole on every surface attach
// (B5). It also named two receipts the round had claimed without: a process
// that dies after the record and before the removal, and a sync that fails.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/standing"
)

// foldHome is a session folder with its journal, and n notes waiting in its
// inbox.
func foldHome(t *testing.T, n int) (string, func(*Config)) {
	t.Helper()
	dir := t.TempDir()
	for i := 0; i < n; i++ {
		if err := standing.Deliver(dir, standing.Note{At: time.Now().Add(time.Duration(i) * time.Second), ItemID: "aaaaaaaaaaaaaaaa", Words: "keep the inbox report", Kind: "landed", Text: "report updated: reports/r.md"}); err != nil {
			t.Fatal(err)
		}
	}
	place := Place{Dir: dir}
	return dir, func(config *Config) {
		config.Place = place
		config.SessionFile = place.Transcript()
	}
}

// inboxFilesIn is every inbox file in dir, live or staged.
func inboxFilesIn(dir string) []string {
	files, _ := filepath.Glob(filepath.Join(dir, "inbox.jsonl*"))
	return files
}

// foldsQueued counts the "while you were away" notes an agent has waiting.
func foldsQueued(agent *Agent) int {
	agent.mu.Lock()
	defer agent.mu.Unlock()
	count := 0
	for _, message := range agent.steering {
		if strings.HasPrefix(messageContentText(message.message), "while you were away") {
			count++
		}
	}
	return count
}

// B5, THE MEMORY OF WHAT WAS FOLDED IS BOUNDED BY WHAT IS STILL OWED. The set
// of folded notes only ever grew, and every surface attach walked all of it
// under the agent's lock — work that grew with every note the conversation had
// ever been told. Once a fold is in the record and settled, nothing of it is
// left to look through.
func TestASettledFoldLeavesNothingPending(t *testing.T) {
	dir, home := foldHome(t, 3)
	model := &scriptedCompleter{steps: []step{saying("Noted.")}}
	agent, _ := newTestAgent(t, model, home)
	collect(t, mustSubmit(t, agent, "what happened overnight?"))
	if files := inboxFilesIn(dir); len(files) != 0 {
		t.Fatalf("the fold was recorded and its inbox not consumed: %v", files)
	}
	agent.mu.Lock()
	pending := len(agent.foldPending)
	agent.mu.Unlock()
	if pending != 0 {
		t.Fatalf("a settled fold left %d notes for every later attach to look through", pending)
	}
	// And an attach after it finds nothing to fold and nothing to scan.
	agent.drainStandingInbox()
	if foldsQueued(agent) != 0 {
		t.Fatal("an attach after the settlement folded something again")
	}
}

// RECEIPT, THE PROCESS THAT DIES AFTER THE RECORD AND BEFORE THE REMOVAL. The
// fold is in the journal, on disk, and the staged inbox it came from is still
// there because the process was killed between the two. The next open reads
// the staged file again, finds every note already in the record by its
// delivery id, folds nothing, and consumes the file.
func TestAFoldRecordedBeforeACrashIsNotFoldedAgainAndItsInboxIsConsumed(t *testing.T) {
	dir, home := foldHome(t, 2)
	raw, err := os.ReadFile(standing.InboxPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	model := &scriptedCompleter{steps: []step{saying("Noted.")}}
	first, _ := newTestAgent(t, model, home)
	collect(t, mustSubmit(t, first, "what happened overnight?"))
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	// The crash, exactly there: the record is on disk and the removal never
	// happened, so the staged file is back where the reader left it.
	staged := standing.InboxPath(dir) + ".crashed.draining"
	if err := os.WriteFile(staged, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	next, _ := newTestAgent(t, &scriptedCompleter{}, home)
	if folds := foldsQueued(next); folds != 0 {
		t.Fatalf("a fold the record already held was folded again (%d)", folds)
	}
	if files := inboxFilesIn(dir); len(files) != 0 {
		t.Fatalf("a staged inbox the record already held was left behind: %v", files)
	}
}

// B4, A SYNC THAT FAILED IS NOT A RECORD. The journal's sync is what makes
// the fold's line survive a power cut, and the settlement that removes the
// inbox ran whether the sync had worked or not — so an I/O error and a power
// cut could leave the inbox gone and the record never written. The settlement
// stops at a failed sync; the next open, whose record is what the disk
// actually holds, settles it.
func TestAFailedJournalSyncStopsTheSettlement(t *testing.T) {
	dir, home := foldHome(t, 1)
	failing := errors.New("injected: the disk would not sync")
	restore := syncJournal
	syncJournal = func(*os.File) error { return failing }
	defer func() { syncJournal = restore }()
	model := &scriptedCompleter{steps: []step{saying("Noted.")}}
	agent, _ := newTestAgent(t, model, home)
	collect(t, mustSubmit(t, agent, "what happened overnight?"))
	_ = agent.Close()
	if files := inboxFilesIn(dir); len(files) == 0 {
		t.Fatal("the inbox was consumed after the record's sync failed")
	}
	// The disk works again. The next open finds the line the journal does
	// hold, folds nothing twice, and consumes the inbox.
	syncJournal = restore
	next, _ := newTestAgent(t, &scriptedCompleter{}, home)
	if folds := foldsQueued(next); folds != 0 {
		t.Fatalf("a fold whose line reached the journal was folded again (%d)", folds)
	}
	if files := inboxFilesIn(dir); len(files) != 0 {
		t.Fatalf("the next open did not settle the inbox: %v", files)
	}
}

// And a journal that could not be synced does not consume an inbox on a later
// attach either, when every staged note is already in its memory of the record.
func TestAnAttachAfterAFailedSyncDoesNotConsumeTheInbox(t *testing.T) {
	dir, home := foldHome(t, 1)
	restore := syncJournal
	syncJournal = func(*os.File) error { return errors.New("injected: the disk would not sync") }
	defer func() { syncJournal = restore }()
	model := &scriptedCompleter{steps: []step{saying("Noted.")}}
	agent, _ := newTestAgent(t, model, home)
	collect(t, mustSubmit(t, agent, "what happened overnight?"))
	agent.drainStandingInbox()
	if files := inboxFilesIn(dir); len(files) == 0 {
		t.Fatal("an attach consumed an inbox whose record never synced")
	}
}
