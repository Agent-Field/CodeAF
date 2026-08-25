package tui3

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// THE TAB SAYS WHICH ONE THIS IS AND WHETHER IT NEEDS YOU, AND NOTHING ELSE.
//
// The title is read from outside the window — a tab bar, cmd-tab, a tmux
// status line — so these tests hold it to the two facts worth a glance from
// there: the name (project, then the conversation's own name) and one glyph
// from the rail's vocabulary. Everything else the surface knows stays inside
// the frame.

func TestTheTabSaysTheProjectAndTheConversationsName(t *testing.T) {
	_, a := wired()

	// newTestApp opens on /tmp/lab, so the project is the whole of the name.
	if got := a.windowTitle(); got != "lab" {
		t.Fatalf("an untitled conversation's tab = %q, want the project alone", got)
	}

	a.title = "porting the parser"
	if got := a.windowTitle(); got != "lab · porting the parser" {
		t.Fatalf("a named conversation's tab = %q", got)
	}

	// The project leads because tabs truncate from the right: across projects
	// the project distinguishes, and within one the name still shows where a
	// bar is wide enough to draw it.
	a.place = ""
	if got := a.windowTitle(); got != "porting the parser" {
		t.Fatalf("with no workspace the tab = %q, want the name alone", got)
	}

	// Knowing nothing still claims the tab: an empty title would make the
	// renderer keep whatever the shell left there.
	a.title = ""
	if got := a.windowTitle(); got != product {
		t.Fatalf("knowing nothing the tab = %q, want %q", got, product)
	}
}

func TestAQuestionPutsTheAskGlyphOnTheTab(t *testing.T) {
	_, a := wired([]session.Event{
		toolBegin("bash", "bash go test ./..."),
		consentEvent(7, "bash", "bash go test ./...", `bash pattern "go test *"`),
	})
	a.title = "porting the parser"
	typeLine(t, a, "run the tests")
	if !a.asking() {
		t.Fatal("the question never came up")
	}

	if got := a.windowTitle(); got != titleAskGlyph+" lab · porting the parser" {
		t.Fatalf("a waiting question's tab = %q", got)
	}

	// The question outranks the tick: answering it is the next thing anyone
	// does here, and what landed keeps until then.
	a.landedAway = true
	if got := a.windowTitle(); !strings.HasPrefix(got, titleAskGlyph+" ") {
		t.Fatalf("with a question up the tab = %q, want the ask glyph first", got)
	}
}

func TestATurnThatLandsOnABlurredWindowTicksTheTab(t *testing.T) {
	_, a := wired([]session.Event{{Kind: session.EventTurnDone}})

	drive(t, a, tea.BlurMsg{})
	typeLine(t, a, "carry on without me")

	if got := a.windowTitle(); got != titleDoneGlyph+" lab" {
		t.Fatalf("a turn landing on a blurred window left the tab = %q", got)
	}

	// Coming back is the whole of seeing it: the reply on screen says the rest.
	drive(t, a, tea.FocusMsg{})
	if got := a.windowTitle(); got != "lab" {
		t.Fatalf("after refocus the tab = %q, want the bare name", got)
	}
}

func TestATurnThatLandsWhileWatchedStaysOffTheTab(t *testing.T) {
	_, a := wired([]session.Event{{Kind: session.EventTurnDone}})
	typeLine(t, a, "quick one")

	// The ordinary state is the quiet one: a person watching the turn end does
	// not need the tab to tell them about it, then or later.
	if got := a.windowTitle(); got != "lab" {
		t.Fatalf("a watched turn marked the tab = %q", got)
	}
}

func TestANameCannotCarryAnEscapeIntoTheTitle(t *testing.T) {
	_, a := wired()

	// A conversation is free to be NAMED after an escape sequence; the name
	// still may not carry one into an OSC payload, where an ESC would end the
	// string early and spill the rest into the frame as printable garbage.
	a.title = "evil\x1b]2;pwned\aname"
	if got := a.windowTitle(); strings.ContainsAny(got, "\x1b\a") {
		t.Fatalf("an escape survived into the title: %q", got)
	}

	a.title = strings.Repeat("long ", 60)
	if got := []rune(a.windowTitle()); len(got) > windowTitleMax {
		t.Fatalf("the title ran to %d runes, cap is %d", len(got), windowTitleMax)
	}
}

func TestTheViewDeclaresTheTitle(t *testing.T) {
	_, a := wired()
	if got := a.View().WindowTitle; got != a.windowTitle() {
		t.Fatalf("the view declares %q, the tab says %q", got, a.windowTitle())
	}
	if a.View().WindowTitle == "" {
		t.Fatal("the view declared no title at all")
	}
}
