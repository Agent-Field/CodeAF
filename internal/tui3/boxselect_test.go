package tui3

// SELECTING TEXT IN A BOX, AND TAKING BACK WHAT WAS TYPED. The two halves of
// what the owner asked for: "i am unable to select text with highlight from
// input bar in all places … it acts like normal text", and "maybe even ctrl+z
// as well like undo or redo like shift z".

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// boxApp is a conversation with a sentence already in the message box.
func boxApp(t *testing.T, text string) *app {
	t.Helper()
	a := newTestApp(&fakeAgent{model: "m"})
	a.pal = newPalette(tokens.TrueColor, false)
	a.width, a.height = 80, 30
	a.input.setText(text)
	a.input.forgetUndo()
	a.touch()
	frame(a)
	return a
}

// draftRowY is the screen row the message box's row `at` is drawn on.
func draftRowY(t *testing.T, a *app, at int) int {
	t.Helper()
	y := markedRowY(a, chromeDraft, at)
	if y < 0 {
		t.Fatalf("no frame row carries draft row %d", at)
	}
	return y
}

// A SWEEP ACROSS THE MESSAGE BOX HIGHLIGHTS WHAT IT CROSSED AND COPIES IT ON
// RELEASE — the transcript's own bargain, kept in the one place a person is
// actually writing.
func TestASweepOverTheMessageBoxSelectsAndCopiesIt(t *testing.T) {
	a := boxApp(t, "alpha beta gamma")
	y := draftRowY(t, a, 0)
	head := len(inputPad) + ansi.StringWidth(prompt)

	drive(t, a, tea.MouseClickMsg{X: head, Y: y, Button: tea.MouseLeft})
	drive(t, a, tea.MouseMotionMsg{X: head + 10, Y: y, Button: tea.MouseLeft})
	if got := a.input.selectedText(); got != "alpha beta" {
		t.Fatalf("the sweep selected %q, want %q", got, "alpha beta")
	}
	model, cmd := a.Update(tea.MouseReleaseMsg{X: head + 10, Y: y, Button: tea.MouseLeft})
	a = model.(*app)
	if got := rawPayload(t, runCmd(cmd)); got != "alpha beta" {
		t.Fatalf("the release put %q on the clipboard, want %q", got, "alpha beta")
	}
	if got := a.dragWord(); got != "copied · 10 chars" {
		t.Fatalf("the status line says %q", got)
	}
	// AND THE SELECTION STAYS, which is what makes it a text field's selection
	// rather than the transcript's three-second flash.
	if got := a.input.selectedText(); got != "alpha beta" {
		t.Fatalf("the release dropped the selection: %q", got)
	}
	// AND IT LIGHTS NOTHING OVER THE TRANSCRIPT. dragLit is a span of body rows
	// and a box's copy has none.
	if _, on := a.dragSel(); on {
		t.Fatal("a copy made in the box lit rows of the transcript")
	}
}

// The highlight is DRAWN, in the same step the transcript's own selection wears.
func TestTheSelectedRunOfTheBoxIsPainted(t *testing.T) {
	a := boxApp(t, "alpha beta gamma")
	y := draftRowY(t, a, 0)
	head := len(inputPad) + ansi.StringWidth(prompt)

	drive(t, a, tea.MouseClickMsg{X: head, Y: y, Button: tea.MouseLeft})
	drive(t, a, tea.MouseMotionMsg{X: head + 5, Y: y, Button: tea.MouseLeft})

	painted := strings.Split(frame(a), "\n")[y]
	// The mark's own opening step, taken from the palette rather than spelled
	// out, so a change to the selection colour does not fail this test.
	lit := strings.SplitN(a.pal.mark("|", 1), "|", 2)[0]
	at := strings.Index(painted, lit)
	if at < 0 {
		t.Fatalf("the box's row wears no selection at all:\n%q", painted)
	}
	if got := plain(painted[at+len(lit):]); !strings.HasPrefix(got, "alpha") {
		t.Fatalf("the mark opens over %q, want it over %q", got, "alpha")
	}
	if got := plain(painted); !strings.HasSuffix(got, "alpha beta gamma") {
		t.Fatalf("the marked row reads %q", got)
	}
}

// A PRESS THAT NEVER MOVED IS STILL A CLICK. The caret lands where the pointer
// did and no selection is made, which is what draftclick.go promised before
// there was a sweep at all.
func TestAPressInTheBoxThatDoesNotMoveIsStillTheCaret(t *testing.T) {
	a := boxApp(t, "alpha beta gamma")
	y := draftRowY(t, a, 0)
	head := len(inputPad) + ansi.StringWidth(prompt)

	drive(t, a, tea.MouseClickMsg{X: head + 6, Y: y, Button: tea.MouseLeft})
	drive(t, a, tea.MouseReleaseMsg{X: head + 6, Y: y, Button: tea.MouseLeft})
	if a.input.cursor != 6 {
		t.Fatalf("the press left the caret at %d, want 6", a.input.cursor)
	}
	if _, _, ok := a.input.selection(); ok {
		t.Fatal("a press that did not move made a selection")
	}
}

// TWO CLICKS TAKE THE WORD AND THREE TAKE THE LINE, in the box exactly as they
// do in the transcript above it.
func TestADoubleClickTakesTheWordAndATripleTakesTheLine(t *testing.T) {
	a := boxApp(t, "alpha beta gamma")
	y := draftRowY(t, a, 0)
	head := len(inputPad) + ansi.StringWidth(prompt)
	at := head + 7 // inside "beta"

	drive(t, a, tea.MouseClickMsg{X: at, Y: y, Button: tea.MouseLeft})
	drive(t, a, tea.MouseClickMsg{X: at, Y: y, Button: tea.MouseLeft})
	if got := a.input.selectedText(); got != "beta" {
		t.Fatalf("the double-click took %q, want %q", got, "beta")
	}
	model, cmd := a.Update(tea.MouseReleaseMsg{X: at, Y: y, Button: tea.MouseLeft})
	a = model.(*app)
	if got := rawPayload(t, runCmd(cmd)); got != "beta" {
		t.Fatalf("the release copied %q", got)
	}
	if got := a.dragWord(); got != "copied · 1 word" {
		t.Fatalf("the status line says %q for a word", got)
	}

	drive(t, a, tea.MouseClickMsg{X: at, Y: y, Button: tea.MouseLeft})
	if got := a.input.selectedText(); got != "alpha beta gamma" {
		t.Fatalf("the triple-click took %q, want the whole line", got)
	}
}

// A SWEEP THAT LEAVES THE BOX RUNS TO THE END OF THE TEXT rather than stopping
// at the boundary, which is what a sweep does on every screen there is.
func TestASweepOutOfTheBoxRunsToTheEndOfTheDraft(t *testing.T) {
	a := boxApp(t, "alpha beta gamma")
	y := draftRowY(t, a, 0)
	head := len(inputPad) + ansi.StringWidth(prompt)

	drive(t, a, tea.MouseClickMsg{X: head, Y: y, Button: tea.MouseLeft})
	drive(t, a, tea.MouseMotionMsg{X: 79, Y: y + 6, Button: tea.MouseLeft})
	if got := a.input.selectedText(); got != "alpha beta gamma" {
		t.Fatalf("a sweep past the foot of the box took %q", got)
	}
}

// TYPING OVER A SELECTION REPLACES IT, and backspace deletes it whole. That is
// the whole of "acts like normal text".
func TestTypingAndBackspaceAnswerTheSelection(t *testing.T) {
	a := boxApp(t, "alpha beta gamma")
	a.input.pickSpan(0, 5)
	a.key(tea.KeyPressMsg{Code: 'x', Text: "x"})
	if got := a.input.String(); got != "x beta gamma" {
		t.Fatalf("typing over the selection left %q", got)
	}

	a.input.pickSpan(1, 6)
	a.key(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if got := a.input.String(); got != "x gamma" {
		t.Fatalf("backspace over the selection left %q", got)
	}
	if _, _, ok := a.input.selection(); ok {
		t.Fatal("the selection outlived the edit that consumed it")
	}
}

// SHIFT WITH A MOTION KEY SELECTS, and a plain arrow puts the selection down.
func TestShiftArrowsSelectAndAPlainArrowDropsIt(t *testing.T) {
	a := boxApp(t, "alpha beta")
	a.input.cursor = 0

	for range 5 {
		a.key(tea.KeyPressMsg{Code: tea.KeyRight, Mod: tea.ModShift})
	}
	if got := a.input.selectedText(); got != "alpha" {
		t.Fatalf("five shift+rights selected %q", got)
	}
	a.key(tea.KeyPressMsg{Code: tea.KeyRight, Mod: tea.ModShift | tea.ModAlt})
	if got := a.input.selectedText(); got != "alpha beta" {
		t.Fatalf("alt+shift+right did not extend by a word: %q", got)
	}
	a.key(tea.KeyPressMsg{Code: tea.KeyLeft})
	if _, _, ok := a.input.selection(); ok {
		t.Fatal("a plain arrow left the selection standing")
	}
}

// ── the undo ────────────────────────────────────────────────────────────────

// A STEP IS A WORD, NOT A KEYSTROKE. Typing a sentence and pressing ctrl+z
// takes back the last word rather than the last letter.
func TestUndoCoalescesTypingIntoWords(t *testing.T) {
	e := &editor{}
	for _, r := range "fix the flaky test" {
		e.insert(string(r))
	}
	if !e.undo() {
		t.Fatal("nothing to undo after a typed sentence")
	}
	if got := e.String(); got != "fix the flaky " {
		t.Fatalf("one undo left %q, want %q", got, "fix the flaky ")
	}
	if !e.undo() || e.String() != "fix the " {
		t.Fatalf("two undos left %q, want %q", e.String(), "fix the ")
	}
	// And back forward again, exactly.
	if !e.redo() || e.String() != "fix the flaky " {
		t.Fatalf("one redo left %q", e.String())
	}
	if !e.redo() || e.String() != "fix the flaky test" {
		t.Fatalf("two redos left %q", e.String())
	}
	if e.redo() {
		t.Fatal("a third redo found somewhere to go")
	}
}

// A PASTE IS A STEP OF ITS OWN, and so is each of the kills — none of them
// folds into the sentence somebody was halfway through typing.
func TestAPasteAndTheKillsAreEachOneStep(t *testing.T) {
	e := &editor{}
	e.insert("keep ")
	e.insert("a whole pasted paragraph")
	if !e.undo() || e.String() != "keep " {
		t.Fatalf("undoing a paste left %q, want %q", e.String(), "keep ")
	}

	e = &editor{}
	e.setText("alpha beta")
	e.cursor = len(e.value)
	e.deleteWord()
	if e.String() != "alpha " {
		t.Fatalf("the word kill left %q", e.String())
	}
	if !e.undo() || e.String() != "alpha beta" {
		t.Fatalf("undoing the word kill left %q", e.String())
	}
}

// SWITCHING FROM TYPING TO DELETING STARTS A NEW STEP, because the hand plainly
// changed what it was doing — and a run of deletes folds into one.
func TestDeletesFoldIntoOneStepAndDoNotJoinTheTyping(t *testing.T) {
	e := &editor{}
	e.insert("h")
	e.insert("e")
	e.insert("y")
	e.deleteBackward()
	e.deleteBackward()
	if e.String() != "h" {
		t.Fatalf("two backspaces left %q", e.String())
	}
	if !e.undo() || e.String() != "hey" {
		t.Fatalf("one undo of a run of deletes left %q, want %q", e.String(), "hey")
	}
	if !e.undo() || e.String() != "" {
		t.Fatalf("undoing the typing left %q", e.String())
	}
}

// A NEW EDIT RETIRES THE FUTURE: typing after an undo is the branch a person
// chose, and a redo may not step back onto the one they abandoned.
func TestAnEditAfterAnUndoThrowsTheRedoAway(t *testing.T) {
	e := &editor{}
	e.insert("one ")
	e.insert("two")
	e.undo()
	e.insert("!")
	if e.redo() {
		t.Fatal("a redo survived an edit made after the undo")
	}
}

// THE STACK IS BOUNDED. Sixty-four steps back and no further, so a long session
// in one box cannot grow without end.
func TestTheUndoStackIsBounded(t *testing.T) {
	e := &editor{}
	for i := range undoDepth + 40 {
		_ = i
		// Each word is its own step: a letter, then the space that ends it.
		e.insert("a")
		e.insert(" ")
	}
	if len(e.past) > undoDepth {
		t.Fatalf("the stack held %d steps, want at most %d", len(e.past), undoDepth)
	}
}

// A SENT MESSAGE IS NOT UNDOABLE. The words have left the box and `↑` is where
// they live now, so the reset takes the history with it.
func TestSendingTheDraftForgetsItsHistory(t *testing.T) {
	e := &editor{}
	e.insert("hello")
	e.reset()
	if e.undo() {
		t.Fatalf("ctrl+z after a send put %q back in the box", e.String())
	}
}

// AND THE CHORDS REACH THE MESSAGE BOX through the key router, which is the
// half a unit test of the editor cannot see.
func TestCtrlZReachesTheMessageBox(t *testing.T) {
	a := boxApp(t, "")
	for _, r := range "fix the flaky test" {
		a.key(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	a.key(tea.KeyPressMsg{Code: 'z', Mod: tea.ModCtrl})
	if got := a.input.String(); got != "fix the flaky " {
		t.Fatalf("ctrl+z in the message box left %q, want %q", got, "fix the flaky ")
	}
	a.key(tea.KeyPressMsg{Code: 'z', Mod: tea.ModCtrl | tea.ModShift})
	if got := a.input.String(); got != "fix the flaky test" {
		t.Fatalf("ctrl+shift+z left %q", got)
	}
}

// ── and on the places, which is what "in all places" asked for ──────────────

// THE SWEEP IS THE ROUTER'S GESTURE AND NOT THE CONVERSATION'S. The composer at
// the foot of every place answers it on exactly the message box's terms — the
// press puts the caret down, the sweep highlights, the release copies.
func TestASweepOverAPlacesComposerSelectsAndCopiesIt(t *testing.T) {
	lab := newHomeLab(t)
	a, box := driveToPlace(t, lab, pageSearch)
	if box == nil {
		t.Fatal("the search place has no box to type into")
	}
	box.setText("alpha beta gamma")
	box.forgetUndo()
	frame(a)
	if a.boxRows < 1 {
		t.Fatal("no frame recorded where the place's composer was drawn")
	}
	// The place's box is drawn one cell in, with the prompt in front of it
	// (placemouse.go's [app.placeBoxOffsetIn]).
	head := 1 + ansi.StringWidth(prompt)
	y := a.boxRow

	drive(t, a, tea.MouseClickMsg{X: head, Y: y, Button: tea.MouseLeft})
	drive(t, a, tea.MouseMotionMsg{X: head + 10, Y: y, Button: tea.MouseLeft})
	if got := box.selectedText(); got != "alpha beta" {
		t.Fatalf("the sweep over the place's composer selected %q", got)
	}
	model, cmd := a.Update(tea.MouseReleaseMsg{X: head + 10, Y: y, Button: tea.MouseLeft})
	a = model.(*app)
	if got := rawPayload(t, runCmd(cmd)); got != "alpha beta" {
		t.Fatalf("the release copied %q", got)
	}
}

// AND ctrl+z REACHES EVERY BOX ON THE SURFACE, not only the message one — the
// defect editkeys.go was written for, kept from happening again.
func TestCtrlZReachesAPlacesComposer(t *testing.T) {
	lab := newHomeLab(t)
	a, box := driveToPlace(t, lab, pageSearch)
	if box == nil {
		t.Fatal("the search place has no box to type into")
	}
	for _, r := range "fix the flaky test" {
		a.key(key(string(r)))
	}
	if got := box.String(); got != "fix the flaky test" {
		t.Fatalf("the place's box holds %q", got)
	}
	a.key(tea.KeyPressMsg{Code: 'z', Mod: tea.ModCtrl})
	if got := box.String(); got != "fix the flaky " {
		t.Fatalf("ctrl+z on the place left %q, want %q", got, "fix the flaky ")
	}
	a.key(tea.KeyPressMsg{Code: 'z', Mod: tea.ModCtrl | tea.ModShift})
	if got := box.String(); got != "fix the flaky test" {
		t.Fatalf("ctrl+shift+z on the place left %q", got)
	}
}
