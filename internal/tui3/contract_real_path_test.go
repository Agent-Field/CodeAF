package tui3

import (
	"github.com/Agent-Field/codeaf/internal/chatlist"
	"path/filepath"
	"testing"
)

// TestC5DisplacedCellWindowOffersContinuation proves C5: moving a chat preserves its history and its single holder.
func TestC5DisplacedCellWindowOffersContinuation(t *testing.T) {
	a, agent := heldLab(t, &flipping{status: chatlist.Running}, &fakeTaker{})
	a.file = filepath.Join(filepath.Dir(a.file), ".cell", "transcript.jsonl")
	a.closeHome()
	typeText(t, a, "hello")
	drive(t, a, key("enter"))
	if len(agent.sent) != 0 {
		t.Fatalf("cell-layout stale window sent %v instead of offering continue here; file=%s", agent.sent, a.file)
	}
	if !a.at(pageHome) {
		t.Fatal("no move door was shown")
	}
}

// TestC5DisplacementIsShownWhileTheChatIsOpen proves C5: an attended cell
// window learns the new holder, keeps typed words and sends no model message.
func TestC5DisplacementIsShownWhileTheChatIsOpen(t *testing.T) {
	src := &flipping{status: chatlist.Here, mine: true}
	a, agent := heldLab(t, src, &fakeTaker{})
	a.file = filepath.Join(filepath.Dir(a.file), ".cell", "transcript.jsonl")
	a.closeHome()
	src.mu.Lock()
	src.status, src.mine = chatlist.Running, false
	src.mu.Unlock()
	a.machinePoll.hurry()
	drain(t, a, a.homeBeat(a.homeGen))
	if got := plain(lastNote(t, a)); got != chatlist.Superseded("host-a") {
		t.Fatalf("no displacement sentence: %q", got)
	}
	typeText(t, a, "keep these words")
	drive(t, a, key("enter"))
	if len(agent.sent) != 0 || a.input.String() != "keep these words" || !a.at(pageHome) {
		t.Fatalf("displaced send=%v draft=%q home=%v", agent.sent, a.input.String(), a.at(pageHome))
	}
}

// TestC6ReleasedRemoteChatStillOffersContinuation proves C6: releasing the
// other computer's lease does not make the stale local copy writable again.
func TestC6ReleasedRemoteChatStillOffersContinuation(t *testing.T) {
	a, agent := heldLab(t, &flipping{status: chatlist.Idle}, &fakeTaker{})
	a.file = filepath.Join(filepath.Dir(a.file), ".cell", "transcript.jsonl")
	a.closeHome()
	typeText(t, a, "new words")
	drive(t, a, key("enter"))
	if len(agent.sent) != 0 || !a.at(pageHome) || a.input.String() != "new words" {
		t.Fatalf("released stale window sent=%v home=%v words=%q", agent.sent, a.at(pageHome), a.input.String())
	}
}

// TestC5ASubmissionChecksOwnershipBeforeTheChangeFrameArrives proves C5:
// taking a chat between the last reading and enter never sends the stale draft.
func TestC5ASubmissionChecksOwnershipBeforeTheChangeFrameArrives(t *testing.T) {
	src := &flipping{status: chatlist.Here, mine: true}
	a, agent := heldLab(t, src, &fakeTaker{})
	a.file = filepath.Join(filepath.Dir(a.file), ".cell", "transcript.jsonl")
	a.closeHome()
	src.mu.Lock()
	src.status, src.mine = chatlist.Running, false
	src.mu.Unlock()
	typeText(t, a, "keep my words")
	drive(t, a, key("enter"))
	if len(agent.sent) != 0 || !a.at(pageHome) || a.input.String() != "keep my words" {
		t.Fatalf("stale send=%v home=%v draft=%q", agent.sent, a.at(pageHome), a.input.String())
	}
}
