package tui3

import (
	"github.com/Agent-Field/aforge-v2/internal/session"
	"strings"
	"testing"
)

func TestLargePasteBecomesOneEditableChipAndSendsWhole(t *testing.T) {
	agent, a := wired(nil)
	pasted := "alpha\nbeta\ngamma\ndelta"
	a.paste(pasted)
	if got := a.input.String(); !strings.Contains(got, "[paste 1 · 4 lines]") || strings.Contains(got, "alpha") {
		t.Fatalf("large paste landed as %q", got)
	}

	a.input.left() // Cross the trailing space and select the atomic token.
	if _, _, ok := a.selectedPaste(); !ok {
		t.Fatal("left did not select the paste token")
	}
	a.enter()
	if !a.pasteEdit.open {
		t.Fatal("enter on the selected token did not open its editor")
	}
	a.pasteEditorKey(key("x"))
	a.pasteEditorKey(key("esc"))
	if !strings.Contains(a.input.String(), "[paste 1 · 4 lines]") {
		t.Fatalf("editing lost the token: %q", a.input.String())
	}

	a.input.end()
	drive(t, a, key("enter"))
	if len(agent.sent) != 1 || !strings.Contains(agent.sent[0], "paste 1:\n```text\n") || !strings.Contains(agent.sent[0], pasted) {
		t.Fatalf("model received %#v", agent.sent)
	}
	if got := a.entries[len(a.entries)-1].text; !strings.Contains(got, "[paste 1 · 4 lines]") || strings.Contains(got, "beta") {
		t.Fatalf("transcript kept %q", got)
	}
}

func TestSmallAndSlashPastesStayTextAndBackspaceDropsAChip(t *testing.T) {
	_, a := wired(nil)
	a.paste("one\ntwo")
	if got := a.input.String(); got != "one\ntwo" {
		t.Fatalf("small paste became %q", got)
	}
	a.input.setText("/ask ")
	a.paste("one\ntwo\nthree")
	if strings.Contains(a.input.String(), pasteTokenHead) {
		t.Fatalf("slash paste became a chip: %q", a.input.String())
	}

	a.input.reset()
	a.paste("one\ntwo\nthree")
	a.input.left()
	a.pasteChipKey(key("backspace"))
	if strings.Contains(a.input.String(), pasteTokenHead) || len(a.pastes) != 0 {
		t.Fatalf("backspace left draft %q and %d held pastes", a.input.String(), len(a.pastes))
	}
}

func TestPasteThresholdIsTheManualsThreshold(t *testing.T) {
	if pasteChipLines != 3 {
		t.Fatalf("manual says three lines; threshold is %d", pasteChipLines)
	}
}

// THE MODEL READS THE PASTE WHEREVER THE MESSAGE GOES. A message parked over a
// running turn and then steered into it used to reach the model as its tag —
// `[paste 2 · 20 lines]` — and the model said, honestly, that it could not see
// the paste. The screen keeps the tag; the wire carries the lines.
func TestAPasteParkedAndSteeredReachesTheModelWholeAndTheRowKeepsTheTag(t *testing.T) {
	a, agent := steerableTurn(t, "reading the tree. ")
	pasted := "alpha\nbeta\ngamma\ndelta"
	a.paste(pasted)
	parkLine(t, a, "look at this")
	if len(a.parks) != 1 || !strings.Contains(a.parks[0].text, "[paste 1 · 4 lines]") || len(a.parks[0].pastes) != 1 {
		t.Fatalf("the message did not park with its chip: %+v", a.parks)
	}
	a.input.reset()
	drive(t, a, key("right"))
	if len(agent.steered) != 1 || !strings.Contains(agent.steered[0], "paste 1:\n```text\n") || !strings.Contains(agent.steered[0], pasted) {
		t.Fatalf("the steer reached the model as %q", agent.steered)
	}
	for _, e := range a.entries {
		if strings.Contains(e.text, "beta") {
			t.Fatalf("the screen unfolded the paste: %q", e.text)
		}
	}
}

// A STEER THE SESSION REFUSED GOES BACK WHOLE, chip and all, so the next door it
// takes still has the lines to send.
func TestARefusedSteerPutsTheMessageBackWithItsPaste(t *testing.T) {
	a, agent := steerableTurn(t, "reading the tree. ")
	agent.steerErr = session.ErrNothingToSteer
	a.paste("alpha\nbeta\ngamma\ndelta")
	parkLine(t, a, "look at this")
	a.input.reset()
	drive(t, a, key("right"))
	if len(a.parks) != 1 || len(a.parks[0].pastes) != 1 || !strings.Contains(a.parks[0].text, "[paste 1") {
		t.Fatalf("the refused message came back without its chip: %+v", a.parks)
	}
}

// AND A TASK'S ROOM IS ANOTHER DOOR ON THE SAME BOX: the worker reads the lines,
// the page keeps the tag.
func TestAPasteSteeredIntoARoomReachesTheWorkerWhole(t *testing.T) {
	a, agent, _ := roomApp(t)
	clickRail(t, a, 0)
	pasted := "alpha\nbeta\ngamma\ndelta"
	a.paste(pasted)
	typeInto(t, a, "fix this")
	drive(t, a, key("enter"))
	if len(agent.steered) != 1 || !strings.Contains(agent.steered[0].text, pasted) || !strings.Contains(agent.steered[0].text, "paste 1:\n```text\n") {
		t.Fatalf("the room's steer reached the worker as %+v", agent.steered)
	}
	if got := roomText(a); !strings.Contains(got, "[paste 1 · 4 lines]") || strings.Contains(got, "beta") {
		t.Fatalf("the room drew %q", got)
	}
	if len(a.pastes) != 0 {
		t.Fatal("the chip was not spent with the line")
	}
}
