package tui3

import (
	"context"
	"testing"

	"github.com/Agent-Field/codeaf/internal/chatlist"
)

// rosterFleet is a fleet that lists its devices when asked, the way a directory
// read does, and counts the asks.
type rosterFleet struct {
	devices []chatlist.Device
	asks    int
}

func (f *rosterFleet) FleetSize(context.Context) (int, error) { return len(f.devices), nil }

func (f *rosterFleet) Roster(context.Context) ([]chatlist.Device, error) {
	f.asks++
	return f.devices, nil
}

func moveCardRaised(a *app) bool {
	ask, ok := a.homeAsking()
	return ok && ask.question.Head == moveAsk
}

// R3 (the open the rest stands on): Enter on a chat another computer holds opens
// a live view, never the Move card, when that computer is one of the person's
// own. The listing of chats and the list of computers are two reads begun
// together, so a chat can be on screen, and entered, before the computers have
// answered; that Enter used to be refused with a Move card nobody asked for.
func TestR3EnterOnAHeldChatBeforeTheRosterAnswersStillOpensItLive(t *testing.T) {
	a, taker := liveHomeRig(t, "dev_tower")
	calls, _ := wireFakeOpenLive(t, a, nil)
	fleet := &rosterFleet{devices: rosterOf}
	a.fleet = fleet
	a.devRow.devices = nil // the listing has landed; the roster has not

	standOn(t, a, "c1")

	if moveCardRaised(a) {
		t.Fatal("Enter on a chat held by one of the person's own computers raised the Move card before the roster could say so")
	}
	if len(*calls) != 1 || (*calls)[0].Cell != "c1" || a.liveFront() == nil || a.at(pageHome) {
		t.Fatalf("the Enter did not end on a live view: opens=%d live=%v home=%v", len(*calls), a.liveFront(), a.at(pageHome))
	}
	if len(taker.cells) != 0 {
		t.Fatalf("a live open moved the chat: %v", taker.cells)
	}
	if fleet.asks != 1 {
		t.Fatalf("the roster was asked %d times, want once", fleet.asks)
	}
}

// R3: a roster read that was already out when Enter came may predate the
// computer it is looking for, so it does not decide the Enter: a read begun
// after it does.
func TestR3AnAnswerThatPredatesTheEnterDoesNotDecideIt(t *testing.T) {
	a, _ := liveHomeRig(t, "dev_tower")
	calls, _ := wireFakeOpenLive(t, a, nil)
	fleet := &rosterFleet{devices: rosterOf}
	a.fleet = fleet
	a.devRow.devices = nil
	a.devRow.asking, a.devRow.asked = true, 3

	standOn(t, a, "c1")
	if moveCardRaised(a) || len(*calls) != 0 {
		t.Fatalf("the Enter was decided before any roster answered: card=%v opens=%d", moveCardRaised(a), len(*calls))
	}

	again := a.tookRoster(rosterMsg{seq: 3})
	if again == nil {
		t.Fatal("an answer from before the Enter ended the wait without a fresh read")
	}
	if moveCardRaised(a) {
		t.Fatal("the old answer, which lacks the computer, raised the Move card")
	}
	drain(t, a, again)
	if len(*calls) != 1 || a.liveFront() == nil {
		t.Fatalf("the fresh read did not open the chat live: opens=%d live=%v", len(*calls), a.liveFront())
	}
	if fleet.asks != 1 {
		t.Fatalf("fresh reads = %d, want one", fleet.asks)
	}
}

// R3, the other half: a computer the fresh roster does not list is not one of
// the person's own, and the Enter ends in the Move card as it always did.
func TestR3AComputerTheRosterDoesNotListStillGetsTheMoveCard(t *testing.T) {
	a, _ := liveHomeRig(t, "dev_tower")
	calls, _ := wireFakeOpenLive(t, a, nil)
	fleet := &rosterFleet{devices: rosterOf[:1]}
	a.fleet = fleet
	a.devRow.devices = nil

	standOn(t, a, "c1")

	if !moveCardRaised(a) || len(*calls) != 0 {
		t.Fatalf("an unlisted computer did not end in the Move card: card=%v opens=%d", moveCardRaised(a), len(*calls))
	}
}

// R3: a second Enter while the first waits for the roster asks for nothing more.
func TestR3AnEnterThatIsAlreadyWaitingForTheRosterIsNotQueuedTwice(t *testing.T) {
	a, _ := liveHomeRig(t, "dev_tower")
	calls, _ := wireFakeOpenLive(t, a, nil)
	fleet := &rosterFleet{devices: rosterOf}
	a.fleet = fleet
	a.devRow.devices = nil
	a.devRow.asking, a.devRow.asked = true, 1

	standOn(t, a, "c1")
	standOn(t, a, "c1")
	drain(t, a, a.tookRoster(rosterMsg{seq: 1}))
	drain(t, a, a.tookRoster(rosterMsg{devices: rosterOf, seq: 2}))

	if len(*calls) != 1 {
		t.Fatalf("two Enters opened the chat %d times, want once", len(*calls))
	}
}
