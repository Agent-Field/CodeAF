package tui3

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// ── the chip ────────────────────────────────────────────────────────────────
//
// A chip is a BACKGROUND behind the cells a command already occupies
// (slashchip.go), so what these tests read is the background sequence itself:
// on the ANSI256 profile every test app pins, [palette.background] opens with
// "\x1b[48;5;<n>m" and closes with "\x1b[49m". Nothing else in a draft row or a
// person's message paints one, so a run found between those two IS a chip.

const (
	chipOpen  = "\x1b[48;5;"
	chipClose = "\x1b[49m"
)

// chipRuns is what wears a chip in these painted rows, in order, with every
// other escape sequence stripped off.
func chipRuns(painted ...string) []string {
	var out []string
	for _, line := range painted {
		rest := line
		for {
			at := strings.Index(rest, chipOpen)
			if at < 0 {
				break
			}
			rest = rest[at+len(chipOpen):]
			mark := strings.IndexByte(rest, 'm')
			if mark < 0 {
				break
			}
			rest = rest[mark+1:]
			end := strings.Index(rest, chipClose)
			if end < 0 {
				break
			}
			out = append(out, ansi.Strip(rest[:end]))
			rest = rest[end+len(chipClose):]
		}
	}
	return out
}

// boxRuns is what wears a chip in the message box as it stands.
func boxRuns(a *app) []string {
	block, _, _ := a.inputBlock(a.width)
	return chipRuns(block...)
}

func sameRuns(t *testing.T, got, want []string, what string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: chipped %q, want %q", what, got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("%s: chipped %q, want %q", what, got, want)
		}
	}
}

func TestAKnownCommandIsChippedInTheBoxAndAnUnknownWordIsNot(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})

	typeInto(t, a, "/task fix the wrap")
	sameRuns(t, boxRuns(a), []string{"/task"}, "a command at the head of the draft")

	// The chip is a repaint and NOTHING ELSE: the row still reads exactly as it
	// was typed, which is the law the caret arithmetic rests on.
	block, _, _ := a.inputBlock(a.width)
	if got := ansi.Strip(strings.Join(block, "\n")); !strings.Contains(got, "/task fix the wrap") {
		t.Fatalf("the chip changed the text: %q", got)
	}

	// A word this surface would answer with "unknown command" gets nothing.
	a.input.reset()
	typeInto(t, a, "/tsak fix the wrap")
	sameRuns(t, boxRuns(a), nil, "a word that is nobody's command")

	// And an alias is a command, because the dispatch runs it as one.
	a.input.reset()
	typeInto(t, a, "/clear")
	sameRuns(t, boxRuns(a), []string{"/clear"}, "an alias")
}

func TestAPathInTheBoxIsNeverChipped(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})

	for _, line := range []string{
		"/Users/santosh/notes.md",
		"read /tmp/task and say what it does",
		"look at cmd/aforge/main.go",
		"https://example.com/help",
	} {
		a.input.reset()
		typeInto(t, a, line)
		sameRuns(t, boxRuns(a), nil, line)
	}
}

func TestACommandInsideASentenceIsChippedInTheBoxAndInTheMessage(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})

	typeInto(t, a, "later I will run /compact on this")
	sameRuns(t, boxRuns(a), []string{"/compact"}, "a mention mid-sentence")

	// AND IT KEEPS THE CHIP AFTER IT IS SENT. The transcript is the only record
	// of what was asked for, and a mark that survived only until enter would be
	// taken back at the moment it is worth having.
	a.entries = []entry{
		{kind: entryUser, text: "later I will run /compact on this, not /nope"},
	}
	sameRuns(t, chipRuns(a.renderEntry(0, &a.entries[0], a.width)...),
		[]string{"/compact"}, "the sent message")
}

func TestTheModelsOwnProseIsNeverChipped(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.entries = []entry{{kind: entryAssistant, text: "run /compact when it gets long", settled: true}}
	sameRuns(t, chipRuns(a.renderEntry(0, &a.entries[0], a.width)...), nil, "an answer")
}

// ── the mid-text list ───────────────────────────────────────────────────────

func TestTheCommandListOpensAtAWordBoundaryAndNotInsideAWord(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})

	// At the head of the draft, as it always did.
	typeInto(t, a, "/comp")
	if !a.menu.open || a.menu.at != 0 {
		t.Fatalf("a leading slash left open=%v at=%d", a.menu.open, a.menu.at)
	}

	// And after a space, which is the whole of this wave: a person half a
	// sentence in can still ask what there is.
	a.input.reset()
	typeInto(t, a, "remind me to /comp")
	if !a.menu.open {
		t.Fatal("a slash after a space did not open the list")
	}
	if a.menu.at != len("remind me to ") {
		t.Fatalf("the list is filtering under rune %d", a.menu.at)
	}
	if chosen, _ := a.menu.choice(); chosen.name != "compact" {
		t.Fatalf("the row under the cursor is %q", chosen.name)
	}

	// A slash with a letter in front of it opens nothing at all.
	a.input.reset()
	typeInto(t, a, "cmd/")
	if a.menu.open {
		t.Fatal("a slash inside a word opened the list")
	}
}

func TestAnAbsolutePathDoesNotHoldTheCommandListOpen(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})

	typeInto(t, a, "/Users")
	if a.menu.open {
		t.Fatalf("typing a path left the list up over %q", a.input.String())
	}
	typeInto(t, a, "/santosh/notes.md")
	if a.menu.open {
		t.Fatalf("the rest of a path reopened the list over %q", a.input.String())
	}

	// The second slash of a path is not a boundary either, so nothing about the
	// depth of a path can bring it back.
	a.input.reset()
	typeInto(t, a, "open /tmp/aforge/scratch")
	if a.menu.open {
		t.Fatalf("a path mid-sentence left the list up over %q", a.input.String())
	}
}

func TestTheListClosesOnNoMatchOnSpaceAndOnEsc(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})

	// Nothing matched is nothing to offer, and a backspace back into a word that
	// does match brings it straight back.
	typeInto(t, a, "/zzz")
	if a.menu.open {
		t.Fatal("a filter that matched nothing held the list open")
	}
	drive(t, a, key("backspace"))
	drive(t, a, key("backspace"))
	drive(t, a, key("backspace"))
	typeInto(t, a, "task")
	if !a.menu.open {
		t.Fatal("backspacing back to a real command did not reopen the list")
	}

	// A space is an argument being typed.
	typeInto(t, a, " ")
	if a.menu.open {
		t.Fatal("a space left the list up")
	}

	// esc SEALS the word: the list may not be back on the next letter of it.
	a.input.reset()
	typeInto(t, a, "note the /task")
	if !a.menu.open {
		t.Fatal("the list did not open")
	}
	drive(t, a, key("esc"))
	if a.menu.open {
		t.Fatal("esc left the list open")
	}
	typeInto(t, a, "s")
	if a.menu.open {
		t.Fatalf("the list came back inside a word esc dismissed: %q", a.input.String())
	}
	// A new word is a new question, and the seal does not follow it there.
	typeInto(t, a, " /he")
	if !a.menu.open {
		t.Fatal("the seal outlived the word it was set on")
	}
}

func TestChoosingARowMidSentenceWritesTheWordAndRunsNothing(t *testing.T) {
	agent := &fakeAgent{model: "m"}
	a := newTestApp(agent)

	typeInto(t, a, "before you answer, /comp")
	drive(t, a, key("enter"))

	if got := a.input.String(); got != "before you answer, /compact" {
		t.Fatalf("choosing a row mid-sentence wrote %q", got)
	}
	if agent.packs != 0 {
		t.Fatalf("a row chosen inside a sentence ran the command (%d)", agent.packs)
	}
	if a.menu.open {
		t.Fatal("the list reopened on top of its own answer")
	}
	// The caret is after the word it just wrote, and the word wears its chip.
	if a.input.cursor != len([]rune("before you answer, /compact")) {
		t.Fatalf("the caret parked at %d", a.input.cursor)
	}
	sameRuns(t, boxRuns(a), []string{"/compact"}, "the word the list wrote")

	// And enter now SENDS the sentence: only a leading slash is a command, so a
	// mention travels to the model as the words a person typed.
	drive(t, a, key("enter"))
	if agent.packs != 0 {
		t.Fatalf("the sentence ran a command on its way out (%d)", agent.packs)
	}
	said := ""
	for _, e := range a.entries {
		if e.kind == entryUser {
			said = e.text
		}
	}
	if !strings.Contains(said, "/compact") {
		t.Fatalf("the mention did not travel as text: %q", said)
	}
}

func TestARowChosenOverAnAlreadyTypedArgumentKeepsTheArgument(t *testing.T) {
	agent := &fakeAgent{model: "m"}
	a := newTestApp(agent)

	// A command with its argument already typed, and the caret standing back
	// inside the command word. The row replaces the WORD; the argument is
	// untouched and nothing runs, because the line is no longer a bare command
	// and a list may not decide to send one.
	a.input.setText("/mod some-slug")
	a.input.cursor = len("/mod")
	a.menu.sync(&a.input)
	if !a.menu.open {
		t.Fatal("the caret inside the command word did not open the list")
	}
	drive(t, a, key("enter"))
	if got := a.input.String(); got != "/model some-slug" {
		t.Fatalf("the row rewrote the line as %q", got)
	}
	if agent.packs != 0 {
		t.Fatalf("it ran something (%d)", agent.packs)
	}
}
