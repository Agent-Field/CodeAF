package tui3

// ANSWERING A DESIGN WHERE YOU ARE STANDING.
//
// The card in the conversation was once the only door onto a design's one
// question, and the room underneath it — the place people actually are when the
// page lands — had no way to answer at all. A second row was grown in there to
// fix that (roomapproval.go), with two chords of its own, and for a while one
// decision had two drawings and two grammars.
//
// It has one of each now. A finished page is a question, and every question this
// engine hands a person is drawn once, above the box, by the block
// (question.go) — which is pinned to the frame and therefore already in the room.
// These tests hold that: the answers are there while the design waits, they are
// the same three digits they are out in the conversation, they resolve the same
// design, and the two keys the room owns are still the room's.
//
// The last one is about the tally, which was lying in the other direction: a
// design whose only remaining step is a person's counted as work in progress.

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// awaitingDesign is a surface standing in a design's room with a page waiting on
// it: the node at the phase where nothing is running, and the design's own card
// in the feed under the same id (session mints one number for both).
func awaitingDesign(t *testing.T) (*app, *designingRoomAgent) {
	t.Helper()
	agent := &designingRoomAgent{roomFake: &roomFake{
		taskFake: &taskFake{fakeAgent: &fakeAgent{model: "m"}},
		lanes:    map[uint64]chan session.Event{},
	}}
	a := newTestApp(agent)
	a.width, a.height = 100, 30
	a.taskUpdate(update(4, "harness · flake triage", session.TaskRunning, session.TaskNotice{
		Kind:  session.TaskKindHarness,
		Doing: session.HarnessPhaseAsking,
	}))
	page := designedPage()
	a.finishHarnessCard(session.Event{ID: 4, Harness: &page, Task: &session.TaskNotice{ID: 4}})
	a.room = a.newRoom(4, "harness · flake triage")
	harnessSettled(t, a)
	return a, agent
}

// chromeText is the block below the conversation as a reader sees it.
func chromeText(a *app) string {
	rows, _, _, _ := a.chrome(a.width)
	out := make([]string, len(rows))
	for i, row := range rows {
		out[i] = plain(row)
	}
	return strings.Join(out, "\n")
}

// THE ANSWERS ARE UP IN THE ROOM WHILE THE DESIGN IS WAITING, and they are the
// three the card in the conversation offers, spelled the same way.
func TestADesignsAnswersAreDrawnInsideItsRoom(t *testing.T) {
	a, _ := awaitingDesign(t)
	drawn := chromeText(a)
	for _, want := range []string{
		"research-helper",
		session.HarnessSaveKey + "  save it",
		session.HarnessChangeKey + "  change it",
		session.HarnessDropKey + "  drop it",
	} {
		if !strings.Contains(drawn, want) {
			t.Fatalf("the block in the room does not say %q:\n%s", want, drawn)
		}
	}
}

// AND THEY ARE DOWN EVERYWHERE ELSE, which is the half that keeps it honest.
func TestADesignsAnswersAreDownWhenNothingIsWaiting(t *testing.T) {
	t.Run("while the page is being rewritten", func(t *testing.T) {
		a, _ := awaitingDesign(t)
		// The person asked for a change and the designer took the page back:
		// there is nothing to judge until the next one lands.
		a.withdrawHarnessCard(session.Event{ID: 4, Text: "run it on the failing tests only"})
		if a.questionOpenOn(session.QuestionHarness, 4) != nil {
			t.Fatalf("the block still asks about a page that is being written again:\n%s", chromeText(a))
		}
		if drawn := chromeText(a); strings.Contains(drawn, session.HarnessSaveKey+"  save it") {
			t.Fatalf("the answers outlived the page they were about:\n%s", drawn)
		}
	})
	t.Run("after it is answered", func(t *testing.T) {
		a, _ := awaitingDesign(t)
		drive(t, a, key(session.HarnessSaveKey))
		if a.questionOpenOn(session.QuestionHarness, 4) != nil {
			t.Fatal("an answered design is still being asked about")
		}
		// AND THE CARD IN THE FEED SAYS WHAT BECAME OF IT, which is where a
		// decision lives once it has been made ([app.harnessPageRows]).
		card, _ := a.harnessCardOf(4)
		if card == nil || card.state != "saved as research-helper v1" {
			t.Fatalf("the card does not say what it saved: %+v", card)
		}
	})
}

// THE DIGITS ANSWER FROM IN HERE, and they answer through the SAME door the
// conversation's do — one design, one id, one recorded state, so the card out
// there shows the answer given in here.
func TestADesignsDigitsResolveItFromInsideItsRoom(t *testing.T) {
	for _, tc := range []struct {
		name, key, state string
		run              bool
	}{
		{"save", session.HarnessSaveKey, "saved as research-helper v1", true},
		{"drop", session.HarnessDropKey, "dropped", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, agent := awaitingDesign(t)
			drive(t, a, key(tc.key))
			if len(agent.answers) != 1 || agent.answers[0].run != tc.run || agent.answers[0].id != 4 {
				t.Fatalf("the engine was told %+v", agent.answers)
			}
			card, _ := a.harnessCardOf(4)
			if card == nil || card.state != tc.state {
				t.Fatalf("the card in the conversation does not show the answer given in the room: %+v", card)
			}
			if !a.roomOpen() {
				t.Fatal("answering the design walked out of its room")
			}
			// AND A SECOND PRESS ANSWERS NOTHING. The question is gone; a digit
			// that resolved twice would be a second answer to a design that
			// already has one.
			drive(t, a, key(tc.key))
			if len(agent.answers) != 1 {
				t.Fatalf("the design was answered twice: %+v", agent.answers)
			}
		})
	}
}

// AND THE ROOM'S OWN TWO KEYS ARE STILL THE ROOM'S. This is where the collapse
// could have cost something: `esc` leaves a room and `enter` steers the worker
// in it, and both of those are read (app.go's [app.roomKey]) BEFORE the block —
// so a question standing in a room takes the digits and nothing else.
func TestADesignsQuestionLeavesTheRoomsOwnKeysAlone(t *testing.T) {
	a, agent := awaitingDesign(t)
	a.input.setText("try it against the flaky ones")
	_, took := a.roomKey(key("enter"))
	if !took {
		t.Fatal("enter in a room did not reach the room's own steer")
	}
	if len(agent.answers) != 0 {
		t.Fatalf("steering the room answered the design: %+v", agent.answers)
	}
	if a.questionOpenOn(session.QuestionHarness, 4) == nil {
		t.Fatal("steering the room closed the design's question")
	}
	if !a.roomOpen() {
		t.Fatal("steering the room left it")
	}
	a.input.setText("")
	drive(t, a, key("esc"))
	if a.roomOpen() {
		t.Fatal("esc did not walk out of the room")
	}
	if len(agent.answers) != 0 {
		t.Fatalf("leaving the room answered the design: %+v", agent.answers)
	}
}

// ── the tally ───────────────────────────────────────────────────────────────

// A DESIGN WAITING ON YOU IS NOT RUNNING WORK. Its state is `running` on the
// wire for as long as its page is up — which is right, the node is open and its
// room has to stay open with it — but the only remaining step is a person's, so
// every place that counts or draws it says so.
func TestADesignAwaitingYourLookCountsAsNeedsYouAndNotAsRunning(t *testing.T) {
	a, _ := awaitingDesign(t)
	node := a.tasks[4]

	if group := a.railGroupOf(node); group != railAttention {
		t.Fatalf("the roster files a waiting design under %q", railGroupWords[group])
	}
	if n := a.deckRunning(); n != 0 {
		t.Fatalf("the status row says %d running about a card that is waiting on somebody", n)
	}
	// NO SPINNER ON A WAITING ROW. It wears the same mark the other kind of
	// finished-and-waiting work wears.
	if glyph := plain(a.railGlyph(node)); glyph != glyphAsk {
		t.Fatalf("a waiting design's row is drawn with %q, not the waiting mark", glyph)
	}
	if a.tasksAnimating() {
		t.Fatal("the paint clock is kept alive for a row that is not moving")
	}

	// AND THE MOMENT IT IS ACTUALLY WORKING AGAIN, all four go back.
	a.taskUpdate(update(4, "harness · flake triage", session.TaskRunning, session.TaskNotice{
		Kind: session.TaskKindHarness, Doing: "designing",
	}))
	if group := a.railGroupOf(node); group != railRunning {
		t.Fatalf("a design that is writing a page is filed under %q", railGroupWords[group])
	}
	if n := a.deckRunning(); n != 1 {
		t.Fatalf("the status row says %d running about a design that is writing", n)
	}
}
