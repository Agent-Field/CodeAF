package tui3

import (
	"strings"
	"testing"
)

// ── ctrl+t IS A NEW TAB ─────────────────────────────────────────────────────
//
// The strip is drawn as tabs, so the key every browser opens a tab with is the
// key people press at it. These hold the whole of that claim: it is the SAME
// door the `+` presses (chatstart.go's [app.openChatStart]) and not a second
// way of making a conversation; it costs the conversation behind it nothing;
// leaning on it does not throw away what has been typed on the page; and the
// two surfaces that already spend this chord — the model picker's effort ladder
// and home's new-chat-in-that-folder — keep it, because both of them are read
// above this rung.
//
// AND THE ROSTER MOVED RATHER THAN DIED. It answers [railHoldChord] now, one
// modifier away, which is the key the tests below press for it.

// newChatDoor is [startDoor] with the machine emptied under it: a window needs
// somewhere to send a first message before [app.canStart] will draw the page at
// all, and a window reading the developer's own history is a window whose
// switcher rows depend on the laptop the suite is run on.
func newChatDoor(a *app, made *int) {
	startDoor(a, made)
	emptyMachine(a)
}

// THE CHORD IS THE `+`, AND IT MAKES NOTHING. Everything the plus promises is
// promised here: no create, no close, no interrupt, and the conversation behind
// the page keeps the sentence it was holding.
func TestCtrlTOpensTheSameStartPageThePlusDoes(t *testing.T) {
	lab := newStartLab(t)
	a := lab.app()
	a.input.setText("half a sentence about the parser")

	drive(t, a, key(newChatChord))
	if !a.startingChat() {
		t.Fatal("ctrl+t opened no start page")
	}
	if lab.made != 0 {
		t.Fatalf("ctrl+t asked the door for %d conversations, and it may ask for none", lab.made)
	}
	if lab.agent.closes != 0 || lab.agent.stops != 0 {
		t.Fatalf("ctrl+t closed %d and interrupted %d — it may do neither", lab.agent.closes, lab.agent.stops)
	}
	if got := a.input.String(); got != "" {
		t.Fatalf("the page opened holding %q, and it must open blank", got)
	}
	kept := a.composers[mainRecipient].box
	if got := kept.String(); got != "half a sentence about the parser" {
		t.Fatalf("the conversation behind the page holds %q, want its own sentence back", got)
	}
}

// AND LEANING ON IT KEEPS WHAT IS TYPED. A person who presses the key they just
// pressed is not asking for a second page — they are asking whether the first
// one is still there.
func TestCtrlTOnTheStartPageKeepsWhatIsTyped(t *testing.T) {
	lab := newStartLab(t)
	a := lab.app()

	drive(t, a, key(newChatChord))
	typeInto(t, a, "port the parser")
	drive(t, a, key(newChatChord), key(newChatChord))

	if !a.startingChat() {
		t.Fatal("a second ctrl+t put the start page away")
	}
	if got := a.input.String(); got != "port the parser" {
		t.Fatalf("the page's own sentence reads %q after two more presses, want it untouched", got)
	}
	if lab.made != 0 {
		t.Fatalf("three presses made %d conversations", lab.made)
	}
}

// AND esc IS EXACT. The page came up over a conversation holding a sentence and
// a caret inside it, and both come back where they were — the cancel road the
// `+` already had, reached by the chord ([app.cancelChatStart]).
func TestEscapeAfterCtrlTGivesTheConversationBack(t *testing.T) {
	lab := newStartLab(t)
	a := lab.app()
	a.input.setText("half a sentence about the parser")
	a.input.cursor = 4

	drive(t, a, key(newChatChord))
	typeInto(t, a, "a different thing entirely")
	drive(t, a, key("esc"))

	if a.startingChat() {
		t.Fatal("esc left the start page up")
	}
	if got := a.input.String(); got != "half a sentence about the parser" {
		t.Fatalf("the conversation came back holding %q", got)
	}
	if a.input.cursor != 4 {
		t.Fatalf("the caret came back at %d, want 4", a.input.cursor)
	}
	if lab.made != 0 {
		t.Fatalf("a press and an escape made %d conversations", lab.made)
	}
}

// ── THE ROSTER, AND THE KEY IT NO LONGER TAKES ──────────────────────────────

// A PERSON STANDING ON A RUNNING TASK GETS A NEW CHAT. The roster is read before
// this router (app.go's [app.route]) and hands ctrl+t on, and the hold goes back
// to the box on the way — a keyboard pointed at a list nobody can see is the bug
// chordfocus.go states.
func TestCtrlTFromAHeldRosterStartsANewChatAndGivesTheKeyboardBack(t *testing.T) {
	a, _, _ := taskApp(t)
	made := 0
	newChatDoor(a, &made)
	railRun(a)

	drive(t, a, altT())
	if !a.railHold {
		t.Fatal("alt+t did not hand the roster the keyboard")
	}

	drive(t, a, key(newChatChord))
	if !a.startingChat() {
		t.Fatal("ctrl+t on a held roster did not start a new chat")
	}
	if a.railHold {
		t.Fatal("the roster kept the keyboard under the start page")
	}
	if made != 0 {
		t.Fatalf("the chord asked the door for %d conversations", made)
	}
}

// AND THE HAND-OFF IS alt+t AND NOTHING ELSE. This is the whole of the move: the
// key that used to fold the column open now opens a page, and the column answers
// the same letter under the other modifier.
func TestTheRosterTakesTheKeyboardOnAltTAndNotOnCtrlT(t *testing.T) {
	a, _, _ := taskApp(t)
	made := 0
	newChatDoor(a, &made)
	railRun(a)

	drive(t, a, key(newChatChord))
	if a.railHold {
		t.Fatal("ctrl+t still hands the roster the keyboard")
	}
	drive(t, a, key("esc"))
	if a.startingChat() {
		t.Fatal("esc left the start page up")
	}

	drive(t, a, altT())
	if !a.railHold {
		t.Fatal("alt+t did not hand the roster the keyboard")
	}
	// AND IT IS STILL THE TOGGLE IT ALWAYS WAS: pressed again, the roster gives
	// the keyboard back.
	drive(t, a, altT())
	if a.railHold {
		t.Fatal("a second alt+t did not give the keyboard back")
	}
}

// ── WHAT KEEPS THE CHORD ────────────────────────────────────────────────────

// THE MODEL PICKER'S EFFORT LADDER IS UNTOUCHED. It is modal above this rung
// (input.go), so a person walking a row's thinking level never opens a page by
// doing it.
func TestTheModelPickerKeepsCtrlTForTheEffortLadder(t *testing.T) {
	agent := &fakeAgent{model: "moonshotai/kimi-k3"}
	a := pickerApp(t, agent, richCatalog)
	made := 0
	newChatDoor(a, &made)
	typeLine(t, a, "/model")
	typeInto(t, a, "sonnet")

	drive(t, a, ctrlT())
	if got := agent.ReasoningFor("anthropic/claude-sonnet-4.5"); got != "low" {
		t.Fatalf("the picker's ctrl+t moved the level to %q, want %q", got, "low")
	}
	if a.startingChat() {
		t.Fatal("ctrl+t in the model picker opened the start page")
	}
	if made != 0 {
		t.Fatalf("the picker's chord asked the door for %d conversations", made)
	}
}

// AND THE KEY SHEET TEACHES BOTH, because a chord that moved and is not written
// down anywhere is a feature a person concludes was removed (commands.go).
func TestTheKeySheetNamesTheNewTabAndTheRostersOwnChord(t *testing.T) {
	sheet := helpText("", chordSpelling{meta: chordAltWord})
	newTab, roster := "", ""
	for _, line := range strings.Split(sheet, "\n") {
		switch {
		case strings.HasPrefix(line, newChatChord+" "):
			newTab = line
		case strings.HasPrefix(line, railHoldChord+" "):
			roster = line
		}
	}
	if newTab == "" {
		t.Fatalf("the key sheet does not name %s at all:\n%s", newChatChord, sheet)
	}
	if !strings.Contains(newTab, "new chat") {
		t.Fatalf("the new-tab row does not say what the key does:\n  %s", newTab)
	}
	if roster == "" {
		t.Fatalf("the key sheet does not name %s at all:\n%s", railHoldChord, sheet)
	}
	if !strings.Contains(roster, "task roster") {
		t.Fatalf("the roster row does not say what the key reaches:\n  %s", roster)
	}
}
