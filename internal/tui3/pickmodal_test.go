package tui3

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// THE CHAT OVERLAY IS A FRAMED SHEET. /model used to grow a bottom list under
// the draft; people expected to have opened something. The sheet names itself,
// keeps a cancel target, and holds the caret in its own filter box — the same
// laws the context chooser keeps (contextmodal_test.go).

func TestTheModelSheetNamesItselfAndBothWaysOut(t *testing.T) {
	a := pickerApp(t, &fakeAgent{model: "openai/gpt-4.1-mini"}, pickerCatalog)
	typeLine(t, a, "/model")
	drawn := plain(frame(a))
	for _, want := range []string{pickTitleWord, contextCancelWord} {
		if !strings.Contains(drawn, want) {
			t.Fatalf("%q is not on the sheet:\n%s", want, drawn)
		}
	}
}

func TestTheModelSheetCaretIsInItsBox(t *testing.T) {
	a := pickerApp(t, &fakeAgent{model: "openai/gpt-4.1-mini"}, pickerCatalog)
	typeLine(t, a, "/model")
	_, caretX, caretY := a.frameBody()
	win := a.pick.win
	if caretY != win.boxY {
		t.Fatalf("the caret is on row %d, want the box at %d", caretY, win.boxY)
	}
	if caretX < win.left || caretX >= win.left+win.width {
		t.Fatalf("the caret is at column %d, outside the sheet at %d..%d",
			caretX, win.left, win.left+win.width)
	}
}

func TestAPressOutsideTheModelSheetReachesNothing(t *testing.T) {
	a := pickerApp(t, &fakeAgent{model: "openai/gpt-4.1-mini"}, pickerCatalog)
	typeLine(t, a, "/model")
	_ = frame(a)
	model := a.model
	filter := a.pick.filter.String()

	for _, y := range []int{0, 1, a.pick.win.top - 2, a.height - 2, a.height - 1} {
		if y < 0 || y >= a.height {
			continue
		}
		drive(t, a, tea.MouseClickMsg{X: 2, Y: y, Button: tea.MouseLeft})
		drive(t, a, tea.MouseReleaseMsg{X: 2, Y: y, Button: tea.MouseLeft})
	}
	if !a.pick.open {
		t.Fatal("a press on the backdrop closed the sheet")
	}
	if a.model != model {
		t.Fatalf("a press on the backdrop switched the model to %q", a.model)
	}
	if a.pick.filter.String() != filter {
		t.Fatalf("a press on the backdrop changed the filter to %q", a.pick.filter.String())
	}
}

func TestTheModelSheetCancelTargetLeavesHavingChangedNothing(t *testing.T) {
	a := pickerApp(t, &fakeAgent{model: "openai/gpt-4.1-mini"}, pickerCatalog)
	typeInto(t, a, "half a thought")
	a.openPicker()
	_ = frame(a)
	win := a.pick.win
	if !win.cancel.pressable() {
		t.Fatal("the cancel target was not drawn")
	}
	drive(t, a, tea.MouseClickMsg{X: win.cancel.from, Y: win.cancelY, Button: tea.MouseLeft})
	if a.pick.open {
		t.Fatal("the cancel target left the sheet open")
	}
	if a.model != "openai/gpt-4.1-mini" {
		t.Fatalf("cancel switched the model to %q", a.model)
	}
	if got := a.input.String(); got != "half a thought" {
		t.Fatalf("cancel threw away the draft: %q", got)
	}
}

func TestATaskModelSheetNamesTheTaskSubject(t *testing.T) {
	a := pickerApp(t, &fakeAgent{model: "openai/gpt-4.1-mini"}, pickerCatalog)
	a.openTaskPicker(7)
	drawn := plain(frame(a))
	if !strings.Contains(drawn, pickTaskTitleWord) {
		t.Fatalf("%q is not on the sheet:\n%s", pickTaskTitleWord, drawn)
	}
	if strings.Contains(drawn, pickTitleWord) && !strings.Contains(drawn, pickTaskTitleWord) {
		t.Fatal("the conversation title showed on a task sheet")
	}
}

// THE ORDER CHIPS ARE ON THE SHEET so cheap / fast / used are doors by eye,
// not only words a person has to invent in the filter box.
func TestTheModelSheetOrderChipsAreDoors(t *testing.T) {
	a := pickerApp(t, &fakeAgent{model: "openai/gpt-4.1-mini"}, pickerCatalog)
	typeLine(t, a, "/model")
	drawn := plain(frame(a))
	for _, want := range pickOrderWords {
		if !strings.Contains(drawn, want) {
			t.Fatalf("order chip %q is not on the sheet:\n%s", want, drawn)
		}
	}
	win := a.pick.win
	if win.chipsY < 0 || len(win.chips) != len(pickOrderWords) {
		t.Fatalf("chip hit map is chipsY=%d spans=%d", win.chipsY, len(win.chips))
	}
	// Press "cheap".
	drive(t, a, tea.MouseClickMsg{X: win.chips[0].from, Y: win.chipsY, Button: tea.MouseLeft})
	drive(t, a, tea.MouseReleaseMsg{X: win.chips[0].from, Y: win.chipsY, Button: tea.MouseLeft})
	if got := a.pick.filter.String(); got != "cheap" {
		t.Fatalf("pressing cheap left the filter at %q", got)
	}
	// Press "fast" — exclusive with cheap.
	drive(t, a, tea.MouseClickMsg{X: win.chips[1].from, Y: win.chipsY, Button: tea.MouseLeft})
	drive(t, a, tea.MouseReleaseMsg{X: win.chips[1].from, Y: win.chipsY, Button: tea.MouseLeft})
	if got := a.pick.filter.String(); got != "fast" {
		t.Fatalf("pressing fast left the filter at %q, want fast alone", got)
	}
	// Press "used" — stacks with the order word.
	drive(t, a, tea.MouseClickMsg{X: win.chips[2].from, Y: win.chipsY, Button: tea.MouseLeft})
	drive(t, a, tea.MouseReleaseMsg{X: win.chips[2].from, Y: win.chipsY, Button: tea.MouseLeft})
	if got := a.pick.filter.String(); got != "fast used" {
		t.Fatalf("pressing used left the filter at %q", got)
	}
}
