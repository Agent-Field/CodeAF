package tui3

import (
	"strings"
	"testing"
	"time"
)

// withdrawnProposal is the engine taking one proposal back: the same notice,
// its clock gone, and the reason on it (session's TaskNotice.Withdrawn).
func withdrawnProposal(a *app, id uint64) streamEventMsg {
	ev := proposal(a, id, 0)
	ev.Task.Withdrawn = "the reply that proposed it did not go through"
	return streamEventMsg{gen: a.gen, ev: ev}
}

// A CARD STILL COUNTING IS SETTLED BY ITS WITHDRAWAL, and the question goes with
// it: the reply that proposed the work was cut off, the engine admitted nothing,
// and a countdown still running would be a question nothing is waiting on.
func TestAWithdrawnProposalSettlesItsCard(t *testing.T) {
	a, agent, _ := taskApp(t)
	agent.pending = []uint64{7}
	askTask(t, a, 7, 15*time.Second)
	if !a.awaitingTask() {
		t.Fatal("the proposal never started asking")
	}

	drive(t, a, withdrawnProposal(a, 7))
	if a.awaitingTask() {
		t.Fatalf("a withdrawn proposal is still asking:\n%s", taskText(a))
	}
	if a.questioning() {
		t.Fatalf("the block still asks about a withdrawn proposal:\n%s", taskAsk(a))
	}
	if text := taskText(a); !strings.Contains(text, taskWithdrawnWord) {
		t.Fatalf("the card does not say it was withdrawn:\n%s", text)
	}
}

// AND IT OUTRANKS THIS WINDOW'S OWN CLOCK. A card can run out on screen while
// the reply is still arriving — the countdown starts with the card — and read
// "approved · the clock"; if the reply is then cut off, that word is a promise
// about work that is not going to start.
func TestAWithdrawnProposalOverridesTheClocksVerdict(t *testing.T) {
	a, agent, advance := taskApp(t)
	agent.pending = []uint64{7}
	askTask(t, a, 7, 4*time.Second)
	advance(5 * time.Second)
	drive(t, a, frameMsg{})
	if !strings.Contains(taskText(a), taskClockWord) {
		t.Fatalf("the clock did not settle the card first:\n%s", taskText(a))
	}

	drive(t, a, withdrawnProposal(a, 7))
	text := taskText(a)
	if !strings.Contains(text, taskWithdrawnWord) || strings.Contains(text, taskClockWord) {
		t.Fatalf("the card still claims the clock started it:\n%s", text)
	}
}

// A WITHDRAWAL THIS WINDOW NEVER SAW THE CARD FOR DRAWS NOTHING: a card appearing
// only to say it is gone is news about nothing.
func TestAWithdrawalWithNoCardDrawsNothing(t *testing.T) {
	a, _, _ := taskApp(t)
	before := len(a.entries)
	drive(t, a, withdrawnProposal(a, 9))
	if len(a.entries) != before || a.awaitingTask() {
		t.Fatalf("a withdrawal with no card drew one:\n%s", taskText(a))
	}
}
