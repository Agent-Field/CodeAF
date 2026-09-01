package tui3

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// THE SETTLE QUESTION FOLLOWS THE PERSON INTO THE ROOM.
//
// A node that lands needing a look invites the person into its room — the
// roster says `finished — look it over`, enter opens the page — and for a while
// the page they arrived on had nothing to answer with: `task finished — esc to
// return` at the foot, and the only choices back in the conversation on a card
// that had to be walked to and selected. Everything here is about the room
// asking the same question, answering to the same keys, and sharing ONE state
// with the card in the conversation (tasksettle.go).

// roomSettleFake is a room fake that can also be answered (tasksettle.go's
// [settleAgent]). It is composed here rather than by embedding [settleFake]
// beside [roomFake], because both of those embed the same tasker and Go would
// promote none of its methods through two doors at once.
type roomSettleFake struct {
	*roomFake
	resolved []settleCall
	handed   []uint64
	refuse   error
}

func (f *roomSettleFake) ResolveUnverified(id uint64, resolution session.TaskResolution, why string) error {
	if f.refuse != nil {
		return f.refuse
	}
	f.resolved = append(f.resolved, settleCall{id: id, answer: resolution})
	return nil
}

func (f *roomSettleFake) HandUnverifiedToModel(id uint64) error {
	if f.refuse != nil {
		return f.refuse
	}
	f.handed = append(f.handed, id)
	return nil
}

// roomSettleApp is [roomApp] with a resolver under it and a profile of its own,
// standing in the room of node 7 after it landed in the given state.
func roomSettleApp(t *testing.T, state session.TaskState) (*app, *roomSettleFake) {
	t.Helper()
	base, fake, _ := roomApp(t)
	agent := &roomSettleFake{roomFake: fake}
	base.agent = agent
	base.profileDir = t.TempDir()
	notice := unverifiedNotice("finished, but needs your look — nothing came back either way")
	if state == session.TaskDone {
		notice = session.TaskNotice{Elapsed: notice.Elapsed, Merge: mergeWordMerged}
	}
	drive(t, base, streamEventMsg{gen: base.gen, ev: update(7, "Fix the nil-map crash", state, notice)})
	base.openRoom(7, "Fix the nil-map crash")
	drive(t, base, roomClosedMsg{gen: base.room.gen})
	return base, agent
}

// THE ROOM ASKS, in the place the foot used to say "finished": the ask line and
// the four answers, and not the plain foot.
func TestTheRoomOfANodeThatNeedsALookAsks(t *testing.T) {
	a, _ := roomSettleApp(t, session.TaskUnverified)

	page := roomText(a)
	for _, want := range []string{
		settleAskWord,
		settleTakeKey + settleTakeWord,
		settleAgainKey + settleAgainWord,
		settleNotRightKey + settleNotRightWord,
		settleAlwaysKey + settleAlwaysWord,
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("the room is missing %q:\n%s", want, page)
		}
	}
	if strings.Contains(page, roomFinishedRefusal.what) {
		t.Fatalf("the room says %q under a question it is asking:\n%s", roomFinishedRefusal.what, page)
	}
	if got := a.roomHint(); got != roomSettleHint {
		t.Fatalf("the hint slot reads %q, want %q", got, roomSettleHint)
	}
}

// AND AN ORDINARY LANDING KEEPS THE FOOT IT HAS. Only the unverified case
// changes: a done node's room still says finished, and offers nothing.
func TestADoneNodeRoomKeepsThePlainFoot(t *testing.T) {
	a, _ := roomSettleApp(t, session.TaskDone)

	page := roomText(a)
	if !strings.Contains(page, roomFinishedRefusal.what) {
		t.Fatalf("a finished room lost its foot:\n%s", page)
	}
	if strings.Contains(page, settleAskWord) || strings.Contains(page, settleTakeKey) {
		t.Fatalf("a finished room asks to be decided about:\n%s", page)
	}
	if got := a.roomHint(); got != "" {
		t.Fatalf("the hint slot offers %q on a node with nothing to decide", got)
	}
}

// `a` IN THE ROOM ANSWERS THE ROOM'S OWN NODE — nothing selected, the room is
// the selection — and the foot becomes the receipt. The card back in the
// conversation is the same card, so it is decided too.
func TestAcceptingFromTheRoomResolvesTheNodeAndTheCard(t *testing.T) {
	a, agent := roomSettleApp(t, session.TaskUnverified)
	_ = roomText(a)

	drive(t, a, key("a"))

	if len(agent.resolved) != 1 || agent.resolved[0].id != 7 ||
		agent.resolved[0].answer != session.TaskAccept {
		t.Fatalf("the accept reached the engine as %+v", agent.resolved)
	}
	page := roomText(a)
	if !strings.Contains(page, settleTookLine) {
		t.Fatalf("the room does not say what was decided:\n%s", page)
	}
	for _, gone := range []string{settleAskWord, settleTakeKey + settleTakeWord, roomFinishedRefusal.what} {
		if strings.Contains(page, gone) {
			t.Fatalf("an answered room still shows %q:\n%s", gone, page)
		}
	}
	if got := a.roomHint(); got != "" {
		t.Fatalf("the hint slot still offers %q after the answer", got)
	}
	card := a.doneCardFor(7)
	if card == nil || card.decided != settleTookLine {
		t.Fatalf("the conversation's card was not marked decided: %+v", card)
	}
	// A second letter finds nothing to answer.
	drive(t, a, key("n"))
	if len(agent.resolved) != 1 {
		t.Fatalf("an answered room was answered again: %+v", agent.resolved)
	}
}

// AND THE OTHER WAY ROUND: the card answered from the conversation marks the
// room's foot, because there is one state and not two.
func TestAnsweringTheCardMarksTheRoomDecided(t *testing.T) {
	a, agent := roomSettleApp(t, session.TaskUnverified)
	_ = roomText(a)

	a.settleCard(a.doneCardFor(7), settleAgain)

	if len(agent.resolved) != 1 || agent.resolved[0].answer != session.TaskReaudit {
		t.Fatalf("the re-check reached the engine as %+v", agent.resolved)
	}
	page := roomText(a)
	if !strings.Contains(page, settleAgainLine) || strings.Contains(page, settleAskWord) {
		t.Fatalf("the room does not read as sent back:\n%s", page)
	}
}

// THE LETTERS NEVER FIRE WITH A SENTENCE IN THE BOX. The room's box steers the
// worker, and a letter typed into it stays a letter.
func TestTheLettersDoNothingWithASentenceInTheRoomBox(t *testing.T) {
	a, agent := roomSettleApp(t, session.TaskUnverified)

	drive(t, a, key("h"), key("a"))

	if len(agent.resolved) != 0 {
		t.Fatalf("typing answered the node: %+v", agent.resolved)
	}
	if got := a.input.String(); got != "ha" {
		t.Fatalf("the box holds %q, want the letters that were typed", got)
	}
	if !strings.Contains(roomText(a), settleAskWord) {
		t.Fatal("the room stopped asking while a sentence was being typed")
	}
}

// THE POINTER ANSWERS THE SAME ROW, resolved to the room's node through the
// same columns the card records.
func TestAClickOnTheRoomsAnswersRowDecides(t *testing.T) {
	a, agent := roomSettleApp(t, session.TaskUnverified)
	_ = roomText(a)
	card := a.doneCardFor(7)
	if len(card.chips) != 4 {
		t.Fatalf("the room's row recorded %d pressable answers, want 4", len(card.chips))
	}
	if !a.settlePress(-1, card.chips[2].span.from+1) {
		t.Fatal("a press on the room's answers row was not taken")
	}
	if len(agent.resolved) != 1 || agent.resolved[0].answer != session.TaskRefute {
		t.Fatalf("the third chip answered %+v", agent.resolved)
	}
}

// SOMEBODY ELSE GOT THERE FIRST: the engine's refusal is the quiet refresh, in
// the room exactly as on the card.
func TestAnAlreadyAnsweredNodeRefreshesQuietlyInTheRoom(t *testing.T) {
	a, agent := roomSettleApp(t, session.TaskUnverified)
	agent.refuse = fmt.Errorf("task 7 is done, and only a task that needs a look is waiting on somebody to decide: %w",
		session.ErrTaskDecided)

	drive(t, a, key("a"))

	page := roomText(a)
	if !strings.Contains(page, settleGoneLine) || strings.Contains(page, settleAskWord) {
		t.Fatalf("the room does not read as already answered:\n%s", page)
	}
	if strings.Contains(page, "waiting on somebody to decide") {
		t.Fatalf("the engine's refusal was put on screen:\n%s", page)
	}
}

// AND A NODE THAT RE-SETTLED WHILE THE ROOM WAS OPEN stops asking on the next
// draw: the engine's second landing is the latest card, and it has no question
// on it, so the foot is the plain one — never a dead answers row.
func TestANodeSettledElsewhereStopsAskingInTheRoom(t *testing.T) {
	a, _ := roomSettleApp(t, session.TaskUnverified)
	if !strings.Contains(roomText(a), settleAskWord) {
		t.Fatal("the room is not asking to begin with")
	}

	drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, "Fix the nil-map crash", session.TaskDone,
		session.TaskNotice{Elapsed: 400 * time.Second, Merge: mergeWordMerged})})
	a.room.dirty = true

	page := roomText(a)
	if strings.Contains(page, settleAskWord) || strings.Contains(page, settleTakeKey) {
		t.Fatalf("the room still asks about work that has settled:\n%s", page)
	}
	if !strings.Contains(page, roomFinishedRefusal.what) {
		t.Fatalf("the settled room lost its foot:\n%s", page)
	}
}
