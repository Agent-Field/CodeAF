package tui3

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// THE DEFECT: "option left right and cmd and clicking does not seem to work".
//
// The terminal was never the problem. iTerm2's Natural Text Editing mappings —
// which the reporter's only profile carries — send `esc b` for ⌥←, `esc f` for
// ⌥→, the byte 0x01 for ⌘← and 0x05 for ⌘→, and the decoder hands those to this
// surface as `alt+b`, `alt+f`, `ctrl+a` and `ctrl+e`. The message box has bound
// all four for as long as it has existed. What had none of them was every OTHER
// box on the program — home's, the errand pane's, the settings and task and
// rewind filters, and the twelve overlays behind [listNavigate] — so the jumps
// worked in the conversation and were dead everywhere a person met the surface
// first. `ctrl+e` was worse than dead on home: it archived the row under the
// cursor, so a hand reaching for the end of a sentence put a conversation away.
//
// The tests below feed the chords in the SHAPE THE TERMINAL DELIVERS THEM —
// a Key with a code and a modifier and no text — rather than the byte strings,
// because that is what every one of these routers actually reads.

// macKey is one of the four chords a Mac keyboard sends for the word and line
// jumps, spelled the way the decoder spells it: `option+←` reaches this program
// as `alt+b` with the modifier set and NO text, and `cmd+←` as `ctrl+a`.
func macKey(code rune, mod tea.KeyMod) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: code, Mod: mod}
}

var (
	optionLeft  = macKey('b', tea.ModAlt)
	optionRight = macKey('f', tea.ModAlt)
	cmdLeft     = macKey('a', tea.ModCtrl)
	cmdRight    = macKey('e', tea.ModCtrl)
)

// THE ONE VOCABULARY ANSWERS EVERY NAME A TERMINAL SENDS THE JUMP BY. A person
// who learns the gesture on one machine keeps it on the next one, and a name
// that fell out of this table would be a keyboard that quietly stopped working.
func TestTheWordAndLineJumpsAnswerToEveryNameATerminalSendsThemBy(t *testing.T) {
	const sentence = "fix the flaky test"
	for _, tc := range []struct {
		key  string
		want string // what is left BEHIND the caret
	}{
		{"alt+left", "fix the flaky "},
		{"alt+b", "fix the flaky "},
		{"ctrl+left", "fix the flaky "},
		{"super+left", ""},
		{"meta+left", ""},
		{"ctrl+a", ""},
	} {
		e := &editor{}
		e.setText(sentence)
		e.cursor = len(e.value)
		if !editorMotion(e, tc.key) {
			t.Fatalf("%s was not taken by the shared caret vocabulary", tc.key)
		}
		if got := string(e.value[:e.cursor]); got != tc.want {
			t.Fatalf("%s left %q behind the caret, want %q", tc.key, got, tc.want)
		}
	}
	for _, tc := range []struct {
		key  string
		want string // what is left IN FRONT OF the caret
	}{
		{"alt+right", " flaky test"},
		{"alt+f", " flaky test"},
		{"ctrl+right", " flaky test"},
		{"super+right", ""},
		{"meta+right", ""},
	} {
		e := &editor{}
		e.setText(sentence)
		e.cursor = len("fix ")
		if !editorMotion(e, tc.key) {
			t.Fatalf("%s was not taken by the shared caret vocabulary", tc.key)
		}
		if got := string(e.value[e.cursor:]); got != tc.want {
			t.Fatalf("%s left %q in front of the caret, want %q", tc.key, got, tc.want)
		}
	}
	// AND IT CLAIMS NOTHING THAT IS SPENT ELSEWHERE. `home` and `end` walk the
	// LIST on the settings, task and rewind sheets, the plain arrows are
	// navigation over an empty box, and `ctrl+e` sets a row aside on home. A
	// shared map that took any of them would quietly take a working key off a
	// screen.
	for _, key := range []string{"home", "end", "left", "right", "ctrl+e", "ctrl+b", "ctrl+f", "a", "backspace"} {
		e := &editor{}
		e.setText(sentence)
		if editorMotion(e, key) {
			t.Fatalf("%s was claimed by the shared caret vocabulary and belongs to the box that owns it", key)
		}
	}
}

// THE WORD KILL ANSWERS TO THE TWO NAMES A HAND ACTUALLY PRESSES. `ctrl+w` stays
// with each box because each has its own rebuild to do after it; what was
// missing everywhere but the message box was ⌥⌫ and ctrl+⌫.
func TestTheWordKillAnswersToTheNamesAHandPresses(t *testing.T) {
	for _, key := range []string{"alt+backspace", "ctrl+backspace"} {
		e := &editor{}
		e.setText("fix the flaky test")
		e.cursor = len(e.value)
		if !editorWordKill(e, key) {
			t.Fatalf("%s did not kill a word", key)
		}
		if got := e.String(); got != "fix the flaky " {
			t.Fatalf("%s left %q", key, got)
		}
	}
	e := &editor{}
	e.setText("fix the flaky test")
	if editorWordKill(e, "ctrl+w") {
		t.Fatal("ctrl+w was claimed by the shared kill and belongs to the box that owns it")
	}
}

// THE FOUR CHORDS A MAC SENDS MOVE THE CARET IN THE MESSAGE BOX, in the exact
// shape the terminal delivers them. The byte sequences are iTerm2's Natural Text
// Editing preset; the shapes below are what the decoder makes of them.
func TestTheMacJumpsMoveTheCaretInTheMessageBox(t *testing.T) {
	_, a := wired(nil)
	for _, r := range "fix the flaky test" {
		drive(t, a, key(string(r)))
	}
	drive(t, a, optionLeft)
	if got := a.input.String()[:a.input.cursor]; got != "fix the flaky " {
		t.Fatalf("option+← left %q behind the caret", got)
	}
	drive(t, a, cmdLeft)
	if a.input.cursor != 0 {
		t.Fatalf("cmd+← left the caret at %d, want the start of the line", a.input.cursor)
	}
	drive(t, a, optionRight)
	if got := a.input.String()[:a.input.cursor]; got != "fix" {
		t.Fatalf("option+→ left %q behind the caret", got)
	}
	drive(t, a, cmdRight)
	if a.input.cursor != len(a.input.value) {
		t.Fatalf("cmd+→ left the caret at %d, want the end of the line", a.input.cursor)
	}
	// AND NOTHING WAS TYPED BY ANY OF THEM. A chord carries no text, and a router
	// that fell through to its bare-letter arm would have written `b` and `f` into
	// the sentence.
	if got := a.input.String(); got != "fix the flaky test" {
		t.Fatalf("the four jumps changed the draft to %q", got)
	}
}

// AND IN HOME'S BOX, which is where a person meets this surface and where not
// one of them worked. The box at the foot of a place is a box a whole sentence
// leaves as a new conversation, so it moves the way every other box does.
func TestTheMacJumpsMoveTheCaretInHomesBox(t *testing.T) {
	a := placeApp(t)
	typeHome(a, "fix the flaky test")

	a.homeKey(optionLeft)
	if got := a.home.box.String()[:a.home.box.cursor]; got != "fix the flaky " {
		t.Fatalf("option+← on home left %q behind the caret", got)
	}
	a.homeKey(cmdLeft)
	if a.home.box.cursor != 0 {
		t.Fatalf("cmd+← on home left the caret at %d, want the start of the line", a.home.box.cursor)
	}
	a.homeKey(optionRight)
	if got := a.home.box.String()[:a.home.box.cursor]; got != "fix" {
		t.Fatalf("option+→ on home left %q behind the caret", got)
	}
	a.homeKey(macKey('\x7f', tea.ModAlt)) // ⌥⌫, which iTerm2 sends as esc-del
	if got := a.home.box.String(); got != " the flaky test" {
		t.Fatalf("option+backspace on home left %q", got)
	}
	if got := a.home.box.String(); strings.ContainsAny(got, "bf") && got != " the flaky test" {
		t.Fatalf("a chord typed a letter into home's box: %q", got)
	}
}

// CMD+→ MAY NOT PUT A CONVERSATION AWAY. It arrives as `ctrl+e`, which is home's
// own key for setting the row under the cursor aside — so with a sentence in the
// box the caret wins, exactly as it does in the message box. A destructive key
// may not be reachable by a gesture that means "move the caret".
func TestCmdRightMovesTheCaretOnHomeRatherThanArchivingTheRow(t *testing.T) {
	a := placeApp(t)
	before := homeArchivedWord(a)
	typeHome(a, "fix the flaky test")
	a.home.box.cursor = 0

	a.homeKey(cmdRight)
	if a.home.box.cursor != len(a.home.box.value) {
		t.Fatalf("cmd+→ with a sentence typed left the caret at %d, want the end of the line", a.home.box.cursor)
	}
	if got := homeArchivedWord(a); got != before {
		t.Fatalf("cmd+→ put a row away while somebody was typing: %q became %q", before, got)
	}
	// AND THE KEY IS NOT LOST, only guarded: over an empty box it is the
	// put-away key the card's legend names, and from the END of a typed line it
	// is that key too — which is the way back out of the archive
	// (home_test.go's own row-away test walks it: type the name of a row you put
	// away, the list finds it, `ctrl+e` from there brings it back).
	a.home.box.reset()
	a.home.build()
	a.home.say("", "")
	a.homeKey(cmdRight)
	if a.home.msg == "" {
		t.Fatal("ctrl+e over an empty box said nothing — the card's legend names the key")
	}
}

// homeArchivedWord is what home is holding put away, as one comparable string.
func homeArchivedWord(a *app) string {
	var out []string
	for _, row := range a.home.world.Sessions() {
		if row.Archived {
			out = append(out, row.Dir)
		}
	}
	return strings.Join(out, ",")
}

// A CLICK ON A PLACE'S BOX PUTS THE CARET UNDER THE POINTER, which is the
// ordinary text-field gesture the message box already answered and no place did.
// The row and the column are the frame's own — the press is resolved against the
// rows that were actually drawn.
func TestAClickOnAPlacesBoxPutsTheCaretUnderThePointer(t *testing.T) {
	a := placeApp(t)
	typeHome(a, "fix the flaky test")
	// The frame has to have been drawn for the pointer to have anything to
	// resolve against, which is the bargain every hit map here strikes.
	a.frame()
	if a.boxRows < 1 {
		t.Fatal("the frame recorded no box rows for a place with a sentence typed into it")
	}
	end := a.home.box.cursor
	// One cell for the margin, then the prompt, then four letters in — measured
	// in CELLS, which is what a press arrives in.
	a.placeBoxPress(1+ansi.StringWidth(prompt)+4, a.boxRow)
	if a.home.box.cursor == end {
		t.Fatal("a click on the box moved nothing")
	}
	if got := a.home.box.String()[:a.home.box.cursor]; got != "fix " {
		t.Fatalf("a click four letters in left %q behind the caret", got)
	}
	// AND A PRESS OFF THE BOX IS NOT THE BOX'S. A row above it belongs to the
	// place, and a caret that jumped on a click somewhere else would be worse
	// than a caret that never moved.
	at := a.home.box.cursor
	if a.placeBoxPress(4, a.boxRow-2) {
		t.Fatal("a press two rows above the box was taken by the box")
	}
	if a.home.box.cursor != at {
		t.Fatal("a press off the box moved the caret")
	}
}

// AND THE FILTERABLE OVERLAYS GOT THE SAME VOCABULARY IN ONE PLACE. Twelve boxes
// share [listNavigate]; a jump added to one of them and not the rest is how the
// surface came to have two answers to one gesture in the first place.
func TestTheSharedListKeyMapCarriesTheWordAndLineJumps(t *testing.T) {
	filter := &editor{}
	filter.setText("fix the flaky test")
	filter.cursor = len(filter.value)
	ranked := 0
	nav := func(msg tea.KeyPressMsg) {
		listNavigate(msg, filter, func(int) {}, func() { ranked++ }, 8)
	}
	nav(optionLeft)
	if got := string(filter.value[:filter.cursor]); got != "fix the flaky " {
		t.Fatalf("option+← in a filter left %q behind the caret", got)
	}
	nav(cmdLeft)
	if filter.cursor != 0 {
		t.Fatalf("cmd+← in a filter left the caret at %d", filter.cursor)
	}
	nav(optionRight)
	if got := string(filter.value[:filter.cursor]); got != "fix" {
		t.Fatalf("option+→ in a filter left %q behind the caret", got)
	}
	// A JUMP CHANGES NO TEXT, so the list behind the box is never re-ranked for
	// one — which is the whole reason the motions are held apart from the kills.
	if ranked != 0 {
		t.Fatalf("the caret jumps re-ranked the list %d times", ranked)
	}
	if got := filter.String(); got != "fix the flaky test" {
		t.Fatalf("the jumps changed the filter to %q", got)
	}
}

// A WORD JUMP IS NOT PROOF THAT THE OPTION KEY IS META. iTerm2's Natural Text
// Editing preset sends `esc b` and `esc f` for ⌥←/⌥→ on a profile where Option
// is still composing accents, so those two chords arrive while ⌥1 goes on typing
// `¡`. Letting them retire the note settled the question wrongly on the
// commonest Mac profile there is: the first word jump of the session silenced
// the one line that would have explained why the places do not answer.
func TestAWordJumpDoesNotSettleWhetherTheOptionKeyIsMeta(t *testing.T) {
	a := placeApp(t)
	a.chords = chordSpelling{meta: chordMetaWord, terminal: "iTerm2",
		setting: "Profiles › Keys › Left Option: Esc+"}

	a.chordWatch(optionLeft)
	a.chordWatch(optionRight)
	if a.chordReal {
		t.Fatal("a word jump settled the option-as-meta question, which it cannot answer")
	}
	// And the note still arms behind them, on the character a place's own chord
	// composed.
	a.chordWatch(tea.KeyPressMsg{Code: '£', Text: "£"})
	if !a.chordLost {
		t.Fatal("⌥3 typing £ on a place drew no note")
	}
	if a.chordNote(60) == "" {
		t.Fatal("the note armed and drew nothing")
	}
	// ANY OTHER alt CHORD STILL SETTLES IT FOREVER. A person whose option key is
	// already meta must not be told to turn on a setting they have on.
	a.chordWatch(tea.KeyPressMsg{Code: '1', Mod: tea.ModAlt})
	if !a.chordReal || a.chordLost {
		t.Fatal("a real alt+1 did not retire the note")
	}
	a.chordWatch(tea.KeyPressMsg{Code: '£', Text: "£"})
	if a.chordLost {
		t.Fatal("the note came back after a real chord had settled the question")
	}
}
