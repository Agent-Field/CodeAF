package tui3

import (
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
