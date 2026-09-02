package tui3

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/standing"
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

func TestSmallAndKnownCommandPastesStayTextAndBackspaceDropsAChip(t *testing.T) {
	_, a := wired(nil)
	a.paste("one\ntwo")
	if got := a.input.String(); got != "one\ntwo" {
		t.Fatalf("small paste became %q", got)
	}
	a.input.setText("/task ")
	a.paste("one\ntwo\nthree")
	if strings.Contains(a.input.String(), pasteTokenHead) {
		t.Fatalf("known-command paste became a chip: %q", a.input.String())
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

func TestALiteralPasteTokenIsNotTheHeldPaste(t *testing.T) {
	_, a := wired(nil)
	literal := pasteToken(1, 3)
	a.input.setText(literal)
	a.input.end()
	a.paste("alpha\nbeta\ngamma")
	held := a.pastes[0]
	a.input.cursor = held.from
	block, _, _ := a.pasteDraftBlock(200, draftRows)
	painted := strings.Join(block, "\n")
	// The ordinary ink run includes the separating space; the owned token
	// starts a different styled run at its exact source span.
	plainAt := strings.Index(painted, a.pal.ink(literal+" "))
	marked := a.pal.mark(a.pal.chip(literal), ansi.StringWidth(literal))
	markedAt := strings.Index(painted, marked)
	if plainAt < 0 || markedAt < 0 || plainAt >= markedAt {
		t.Fatalf("literal and owned token were painted in the wrong order: %q", painted)
	}
	spoken, _ := a.composed(a.input.String())
	if strings.Count(spoken, literal) != 1 || strings.Count(spoken, "paste 1:\n") != 1 {
		t.Fatalf("literal and held token lost their identities in %q", spoken)
	}
}

func TestPasteNumbersAreNeverReusedWhileAHigherChipRemains(t *testing.T) {
	_, a := wired(nil)
	a.paste("one\ntwo\nthree")
	a.paste("four\nfive\nsix")
	first := a.pasteSpans()[0]
	a.removePaste(first, a.pasteForSpan(first))
	a.input.end()
	a.paste("seven\neight\nnine")
	if len(a.pastes) != 2 || a.pastes[0].n != 2 || a.pastes[1].n != 3 {
		t.Fatalf("paste identities are %+v", a.pastes)
	}
}

func TestLeavingDraftOffsetsAndRenumbersParkedPastes(t *testing.T) {
	_, a := wired(nil)
	a.paste("parked alpha\nparked beta\nparked gamma")
	parkedText := a.input.String()
	a.parks = []parked{{text: parkedText, pastes: append([]pasteChip(nil), a.pastes...)}}
	a.input.reset()
	a.pastes = nil
	a.paste("draft alpha\ndraft beta\ndraft gamma")
	typeInto(t, a, " inspect")

	text, pastes := a.leavingDraftState()
	if len(pastes) != 2 || pastes[0].n == pastes[1].n {
		t.Fatalf("the folded draft has paste identities %+v", pastes)
	}
	for _, held := range pastes {
		value := []rune(text)
		if held.from < 0 || held.to > len(value) || string(value[held.from:held.to]) != pasteToken(held.n, pasteLineCount(held.text)) {
			t.Fatalf("paste %d points outside %q: %+v", held.n, text, held)
		}
	}
	spoken := unfoldPastes(text, pastes)
	for _, want := range []string{"parked alpha\nparked beta", "draft alpha\ndraft beta"} {
		if !strings.Contains(spoken, want) {
			t.Fatalf("the folded draft lost %q in %q", want, spoken)
		}
	}
}

func TestFailedRoomSteerGuardSendsPasteWholeToMainOrRevive(t *testing.T) {
	for _, choice := range []string{"m", "r"} {
		a, agent, _ := roomApp(t)
		agent.steerErr = errString("task 7 is done, not running")
		clickRail(t, a, 0)
		a.paste("alpha\nbeta\ngamma")
		typeInto(t, a, " inspect")
		drive(t, a, key("enter"))
		if !a.guarding() || len(a.pastes) != 1 {
			t.Fatalf("%s guard has pastes=%+v open=%v", choice, a.pastes, a.guarding())
		}
		drive(t, a, key(choice))
		if len(agent.sent) != 1 || !strings.Contains(agent.sent[0], "alpha\nbeta\ngamma") ||
			strings.Count(agent.sent[0], "paste 1:\n") != 1 {
			t.Fatalf("%s sent %q", choice, agent.sent)
		}
		if len(a.pastes) != 0 {
			t.Fatalf("%s left paste metadata %+v", choice, a.pastes)
		}
	}
}

func TestUnknownSlashProseStillFoldsALongPaste(t *testing.T) {
	_, a := wired(nil)
	a.input.setText("/api/v1 is broken ")
	a.input.end()
	a.paste("alpha\nbeta\ngamma")
	if len(a.pastes) != 1 || !strings.Contains(a.input.String(), pasteTokenHead) {
		t.Fatalf("unknown slash prose kept no paste identity: %q %+v", a.input.String(), a.pastes)
	}
	spoken, _ := a.composed(a.input.String())
	if !strings.Contains(spoken, "/api/v1 is broken") || !strings.Contains(spoken, "alpha\nbeta\ngamma") {
		t.Fatalf("unknown slash prose composed as %q", spoken)
	}
}

func TestMarginTaskAndImageEditsKeepAPasteWhole(t *testing.T) {
	pasted := "alpha\nbeta\ngamma"

	door := &taskCommandFake{Agent: &fakeAgent{model: "m"}}
	a := newTestApp(door)
	a.paste(pasted)
	a.marginType(marginTaskType)
	drive(t, a, key("enter"))
	if !strings.Contains(door.brief, pasted) || strings.Count(door.brief, "paste 1:\n") != 1 {
		t.Fatalf("margin task received %q", door.brief)
	}

	a, _, dir := attachLab(t, map[string]int{"shot.png": 12})
	a.paste(pasted)
	a.input.cursor = 0
	pasteText(t, a, filepath.Join(dir, "shot.png"))
	if len(a.chips) != 1 || len(a.pastes) != 1 {
		t.Fatalf("mixed tray has %d images and %d pastes", len(a.chips), len(a.pastes))
	}
	a.removeChip(0)
	spoken, _ := a.composed(a.input.String())
	if !strings.Contains(spoken, pasted) || strings.Count(spoken, "paste 1:\n") != 1 {
		t.Fatalf("image insertion/removal moved the paste out of %q", spoken)
	}
}

func TestMarginStandingPasteStaysCompactWhileParkedAndSendsWhole(t *testing.T) {
	a, agent := marginApp(t)
	a.stands.Items = func(string) []standing.Item { return nil }
	pasted := "alpha\nbeta\ngamma"

	a.paste(pasted)
	a.marginType(marginStandType)
	a.state = stateWorking
	drive(t, a, key("enter"))

	if len(a.parks) != 1 || !a.parks[0].standing || len(a.parks[0].pastes) != 1 {
		t.Fatalf("margin standing did not park its paste identity: %+v", a.parks)
	}
	if strings.Contains(a.parks[0].text, "beta") || !strings.Contains(a.parks[0].text, pasteTokenHead) {
		t.Fatalf("waiting row did not keep the compact text: %q", a.parks[0].text)
	}

	a.state = stateIdle
	drive(t, a, runCmd(a.sendParked())...)
	if len(agent.marked) != 1 || !strings.Contains(agent.marked[0], pasted) || strings.Count(agent.marked[0], "paste 1:\n") != 1 {
		t.Fatalf("marked door received %q", agent.marked)
	}
	if got := a.entries[len(a.entries)-1].text; strings.Contains(got, "beta") || !strings.Contains(got, pasteTokenHead) {
		t.Fatalf("transcript did not keep the compact text: %q", got)
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
	a.input.setText(" \u2003")
	a.input.end()
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
