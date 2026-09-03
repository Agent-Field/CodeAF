package tui3

// ANSWERING A DESIGN WHERE YOU ARE STANDING.
//
// The card in the conversation was the only door onto a design's one question,
// and the room underneath it — the place people actually are when the page lands
// — had no way to answer at all. These tests hold the second door: it is up only
// while something is waiting, it answers through the same method the card does,
// and it takes exactly two chords and lets every other key through to the box.
//
// The last one is about the tally, which was lying in the other direction: a
// design whose only remaining step is a person's counted as work in progress.

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// awaitingDesign is a surface standing in a design's room with a card up: the
// node at the phase where nothing is running, and the design's own card in the
// feed under the same id (session mints one number for both).
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

// THE ROW IS UP WHILE THE DESIGN IS WAITING, and it says all three things a
// person can do with a page — two chords, and the box they are already looking
// at.
func TestTheApprovalRowIsDrawnWhileADesignWaits(t *testing.T) {
	a, _ := awaitingDesign(t)
	drawn := chromeText(a)
	for _, want := range []string{
		roomApprovalWord,
		roomApprovalSaveKey + " save it",
		roomApprovalDropKey + " drop it",
		roomApprovalSayWord,
	} {
		if !strings.Contains(drawn, want) {
			t.Fatalf("the approval row does not say %q:\n%s", want, drawn)
		}
	}
	if a.roomApprovalHeight() != 2 {
		t.Fatalf("the block is %d rows, want the question and its answers", a.roomApprovalHeight())
	}
}

// AND IT IS DOWN EVERYWHERE ELSE, which is the half that keeps it honest. The
// PHASE is the test and never the card: a card with a page on it stays in the
// feed forever, so a row drawn off the page alone would still be asking a week
// later.
func TestTheApprovalRowIsDownWhenNothingIsWaiting(t *testing.T) {
	t.Run("out in the conversation", func(t *testing.T) {
		a, _ := awaitingDesign(t)
		a.room = nil
		if a.roomApprovalHeight() != 0 {
			t.Fatal("the room's approval row is drawn with no room open")
		}
	})
	t.Run("while the page is being rewritten", func(t *testing.T) {
		a, _ := awaitingDesign(t)
		// The person asked for a change: the phase goes back to designing, and at
		// that moment there is nothing to approve.
		a.taskUpdate(update(4, "harness · flake triage", session.TaskRunning, session.TaskNotice{
			Kind: session.TaskKindHarness, Doing: "designing",
		}))
		if a.roomApprovalHeight() != 0 {
			t.Fatalf("the row still asks for approval while the page is being rewritten:\n%s", chromeText(a))
		}
	})
	t.Run("in an ordinary task's room", func(t *testing.T) {
		a, _ := awaitingDesign(t)
		a.tasks[4].kind = ""
		if a.roomApprovalHeight() != 0 {
			t.Fatal("an ordinary task's room drew a design's approval row")
		}
	})
	t.Run("after it is answered", func(t *testing.T) {
		a, _ := awaitingDesign(t)
		a.roomApprovalKey(tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl})
		if a.roomApprovalHeight() != 1 {
			t.Fatalf("an answered design does not report what the answer was: %d rows", a.roomApprovalHeight())
		}
		if drawn := chromeText(a); !strings.Contains(drawn, "saved as research-helper v1") {
			t.Fatalf("the row does not say what it saved:\n%s", drawn)
		}
	})
}

// THE CHORDS ANSWER, and they answer through the SAME method the card in the
// conversation does — one design, one id, one recorded state, so the card out
// there shows the answer given in here.
func TestTheApprovalChordsResolveTheSameQuestionTheCardDoes(t *testing.T) {
	for _, tc := range []struct {
		name  string
		key   rune
		run   bool
		state string
	}{
		{"save", 'k', true, "saved as research-helper v1"},
		{"drop", 'x', false, "dropped"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, agent := awaitingDesign(t)
			if !a.roomApprovalKey(tea.KeyPressMsg{Code: tc.key, Mod: tea.ModCtrl}) {
				t.Fatal("the chord was not taken")
			}
			if len(agent.answers) != 1 || agent.answers[0].run != tc.run || agent.answers[0].id != 4 {
				t.Fatalf("the engine was told %+v", agent.answers)
			}
			card, _ := a.harnessCardOf(4)
			if card == nil || card.state != tc.state {
				t.Fatalf("the card in the conversation does not show the answer given in the room: %+v", card)
			}
			// AND A SECOND PRESS ANSWERS NOTHING. The question is gone; a chord that
			// resolved twice would be a second answer to a design that already has one.
			a.roomApprovalKey(tea.KeyPressMsg{Code: tc.key, Mod: tea.ModCtrl})
			if len(agent.answers) != 1 {
				t.Fatalf("the design was answered twice: %+v", agent.answers)
			}
		})
	}
}

// AND THE ROW NEVER SWALLOWS THE BOX. This is the difference between it and
// every modal question on this surface: the third answer to it is a sentence
// typed under it, so a row that owned the keyboard would make the box it points
// at unusable.
func TestTheApprovalRowLetsEveryOtherKeyThroughToTheBox(t *testing.T) {
	a, agent := awaitingDesign(t)
	for _, key := range []tea.KeyPressMsg{
		{Code: 'x', Text: "x"},
		{Code: 'k', Text: "k"},
		{Code: 'e', Text: "e"},
		{Code: tea.KeyEnter},
		{Code: tea.KeyEscape},
		{Code: 's', Mod: tea.ModCtrl},
	} {
		if a.roomApprovalKey(key) {
			t.Fatalf("the approval row swallowed %q", key.String())
		}
	}
	if len(agent.answers) != 0 {
		t.Fatalf("typing into the box answered the design: %+v", agent.answers)
	}
	// The plain letters are still letters, all the way through the room's own
	// keyboard and into the draft.
	for _, key := range []tea.KeyPressMsg{{Code: 'x', Text: "x"}, {Code: 'k', Text: "k"}} {
		if _, taken := a.roomKey(key); taken {
			t.Fatalf("the room took %q away from the message box", key.String())
		}
	}
}

// AND THE ROW IS PRESSABLE, like the card's own columns are: the chord and the
// word beside it are one target.
func TestTheApprovalRowAnswersThePointer(t *testing.T) {
	a, agent := awaitingDesign(t)
	// Laying the chrome out is what writes the spans; reading them before it
	// would be reading where the answers were drawn on the previous frame.
	chromeText(a)
	if len(a.roomApprovalTaps) != 2 {
		t.Fatalf("the row recorded %d targets, want the two chords", len(a.roomApprovalTaps))
	}
	save := a.roomApprovalTaps[0]
	if !save.save {
		t.Fatal("the first target is not the save")
	}
	a.answerRoomApproval(save.save)
	if len(agent.answers) != 1 || !agent.answers[0].run {
		t.Fatalf("the pointer's answer did not save: %+v", agent.answers)
	}
}

// ── the tally ───────────────────────────────────────────────────────────────

// A DESIGN WAITING ON YOU IS NOT RUNNING WORK. Its state is `running` on the
// wire for as long as its card is up — which is right, the node is open and its
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
	if glyph := plain(a.railGlyph(node)); glyph != glyphUnverified {
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
