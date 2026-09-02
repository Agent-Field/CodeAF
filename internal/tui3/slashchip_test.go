package tui3

import (
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/standing"
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

func TestOnlyASendDoorInsideASentenceIsChipped(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})

	typeInto(t, a, "later I will run /compact on this")
	sameRuns(t, boxRuns(a), nil, "an inert command mid-sentence")

	a.input.reset()
	typeInto(t, a, "keep this true /standing")
	sameRuns(t, boxRuns(a), []string{"/standing"}, "a send-door tag")

	// AND IT KEEPS THE CHIP AFTER IT IS SENT. The transcript is the only record
	// of what was asked for, and a mark that survived only until enter would be
	// taken back at the moment it is worth having.
	a.entries = []entry{{kind: entryUser, text: "later I will run /compact on this, not /nope"}}
	sameRuns(t, chipRuns(a.renderEntry(0, &a.entries[0], a.width)...),
		nil, "plain slash prose in the sent message")
}

func tagTestApp() (*app, *fakeAgent) {
	agent := &fakeAgent{model: "m"}
	a := newTestApp(agent)
	a.stands.Items = func(string) []standing.Item { return nil }
	return a, agent
}

func TestATrailingAndMidSentenceTagRouteAndStrip(t *testing.T) {
	for _, line := range []string{"keep the tests green /standing", "keep /standing the tests green"} {
		a, agent := tagTestApp()
		typeInto(t, a, line)
		drive(t, a, key("enter"))
		if len(agent.marked) != 1 || agent.marked[0] != "keep the tests green" {
			t.Fatalf("%q routed marked words %q", line, agent.marked)
		}
		if got := chipRuns(a.renderEntry(0, &a.entries[0], a.width)...); len(got) != 1 || got[0] != "/standing" {
			t.Fatalf("the routed transcript chipped %q", got)
		}
	}
}

func TestTwoTagsRefuseAndKeepTheDraft(t *testing.T) {
	a, agent := tagTestApp()
	line := "keep /standing this /task"
	typeInto(t, a, line)
	drive(t, a, key("enter"))
	if a.input.String() != line || len(agent.sent) != 0 {
		t.Fatalf("refusal left draft %q and sent %q", a.input.String(), agent.sent)
	}
	if len(a.entries) == 0 || a.entries[len(a.entries)-1].text != slashTagRefusal {
		t.Fatalf("refusal note is %#v", a.entries)
	}
}

func TestBackspaceDemotesATagThenEditsAndSendsItAsProse(t *testing.T) {
	a, agent := tagTestApp()
	typeInto(t, a, "say /standing")
	drive(t, a, key("backspace"))
	if a.input.String() != "say /standing" || len(boxRuns(a)) != 0 {
		t.Fatal("first backspace did not demote without editing")
	}
	drive(t, a, key("enter"))
	if len(agent.sent) != 1 || agent.sent[0] != "say /standing" || len(agent.marked) != 0 {
		t.Fatalf("demoted send: sent=%q marked=%q", agent.sent, agent.marked)
	}
	if got := chipRuns(a.renderEntry(0, &a.entries[0], a.width)...); len(got) != 0 {
		t.Fatalf("demoted transcript chipped %q", got)
	}

	a, _ = tagTestApp()
	typeInto(t, a, "say /standing")
	drive(t, a, key("backspace"), key("backspace"))
	if a.input.String() != "say /standin" {
		t.Fatalf("second backspace left %q", a.input.String())
	}
}

func TestEditingADemotedTagRecognizesItAfreshAndAliasesWork(t *testing.T) {
	a, agent := tagTestApp()
	typeInto(t, a, "say /orders")
	drive(t, a, key("backspace"))
	drive(t, a, key("left"), key("x"), key("backspace"), key("right"))
	if got := boxRuns(a); len(got) != 1 || got[0] != "/orders" {
		t.Fatalf("edited alias chipped %q", got)
	}
	drive(t, a, key("enter"))
	if len(agent.marked) != 1 || agent.marked[0] != "say" {
		t.Fatalf("alias routed %q", agent.marked)
	}
}

func TestAnEditBeforeADemotedTagMovesItsPlainRange(t *testing.T) {
	a, agent := tagTestApp()
	typeInto(t, a, "say /standing")
	drive(t, a, key("backspace"))
	a.input.cursor = 0
	drive(t, a, key("x"), key("enter"))
	if len(agent.sent) != 1 || agent.sent[0] != "xsay /standing" || len(agent.marked) != 0 {
		t.Fatalf("shifted demotion sent=%q marked=%q", agent.sent, agent.marked)
	}
}

func TestNonDoorCommandsStayInertAndLeadingCommandsAreUnchanged(t *testing.T) {
	a, agent := tagTestApp()
	typeInto(t, a, "please /compact later")
	drive(t, a, key("enter"))
	if len(agent.sent) != 1 || agent.sent[0] != "please /compact later" {
		t.Fatalf("inert command sent %q", agent.sent)
	}

	a, agent = tagTestApp()
	typeInto(t, a, "/standing keep this")
	drive(t, a, key("enter"))
	if len(agent.marked) != 1 || agent.marked[0] != "keep this" {
		t.Fatalf("leading command routed %q", agent.marked)
	}
}

func TestTaskTagUsesTheTaskCommandRoad(t *testing.T) {
	base := &fakeAgent{model: "m"}
	door := &taskCommandFake{Agent: base}
	a := newTestApp(door)
	typeInto(t, a, "investigate the wrap /task")
	drive(t, a, key("enter"))
	if door.judgeCalls != 1 {
		t.Fatalf("task tag made %d sizing calls", door.judgeCalls)
	}
	if a.input.String() != "" {
		t.Fatalf("task tag left %q in the draft", a.input.String())
	}
}

func TestCommandLinesUseUnicodeSeparatorsAndPreserveTheirArgument(t *testing.T) {
	want := "\"/tmp/a b\"  \\\\server\\share\n```go\nx := 1\n```"
	word, rest, ok := splitCommandLine("/export\u2003\t" + want + "\u00a0")
	if !ok || word != "export" || rest != want {
		t.Fatalf("split (%q, %q, %v), want (%q, %q, true)", word, rest, ok, "export", want)
	}
	for _, line := range []string{
		"/api/v1 returns 500",
		"/Users/alice/repo fix this",
		"/user",
		"/definitely-not-an-aforge-path/file.go:12 inspect `x`",
	} {
		if isCommandLine(line) {
			t.Fatalf("slash prose %q was classified as a command", line)
		}
	}
}

func TestSlashPathsQuotesBackslashesAndFencesReachChatUnchanged(t *testing.T) {
	for _, line := range []string{
		"/api/v1 returns 500",
		"/Users/alice/repo fix this",
		"/not/a/local/file \"quoted\" \\\\server\\share\n```text\nkeep /slashes\n```",
	} {
		agent := &fakeAgent{model: "m"}
		a := newTestApp(agent)
		typeLine(t, a, line)
		if len(agent.sent) != 1 || agent.sent[0] != line {
			t.Fatalf("%q was submitted as %q", line, agent.sent)
		}
	}
}

func TestAnUnknownSlashPathProbeKeepsAnInlineTagDemoted(t *testing.T) {
	line := "/api/v1 fix /task"
	agent := &fakeAgent{model: "m"}
	a := newTestApp(agent)
	a.input.setText(line)
	tags := a.liveTags()
	if len(tags) != 1 {
		t.Fatalf("draft has %d live tags, want one", len(tags))
	}
	a.input.demotedTags = append(a.input.demotedTags, tags[0])

	drive(t, a, key("enter"))
	if len(agent.sent) != 1 || agent.sent[0] != line {
		t.Fatalf("demoted slash prose was routed as sent=%q", agent.sent)
	}
}

func TestAnExistingRootFolderStillUsesTheDropDoor(t *testing.T) {
	if _, err := os.Stat("/tmp"); err != nil {
		t.Skip("this machine has no /tmp")
	}
	agent := &fakeAgent{model: "m"}
	a := newTestApp(agent)
	typeLine(t, a, "/tmp")
	if len(agent.sent) != 0 {
		t.Fatalf("existing root folder was sent as %q", agent.sent)
	}
}

func TestInlineSendDoorsUnfoldAPasteExactlyOnce(t *testing.T) {
	pasted := "alpha\nbeta\ngamma"

	a, agent := tagTestApp()
	a.paste(pasted)
	typeInto(t, a, "keep this /standing")
	drive(t, a, key("enter"))
	if len(agent.marked) != 1 || !strings.Contains(agent.marked[0], pasted) ||
		strings.Count(agent.marked[0], "paste 1:\n") != 1 {
		t.Fatalf("standing received %q", agent.marked)
	}

	door := &taskCommandFake{Agent: &fakeAgent{model: "m"}}
	a = newTestApp(door)
	a.paste(pasted)
	typeInto(t, a, "inspect this /task")
	drive(t, a, key("enter"))
	if !strings.Contains(door.brief, pasted) || strings.Count(door.brief, "paste 1:\n") != 1 {
		t.Fatalf("task sizing received %q", door.brief)
	}
}

func TestLeadingWhitespaceDoesNotMoveAPasteOnARefusedSend(t *testing.T) {
	a, agent := tagTestApp()
	a.input.setText(" \u2003")
	a.input.end()
	a.paste("alpha\nbeta\ngamma")
	typeInto(t, a, "keep /standing /standing")
	drive(t, a, key("enter"))
	if len(a.pastes) != 1 || len(a.pasteSpans()) != 1 {
		t.Fatalf("the refusal corrupted the held paste: %+v", a.pastes)
	}
	tags := a.liveTags()
	a.input.demotedTags = append(a.input.demotedTags, tags[len(tags)-1])
	drive(t, a, key("enter"))
	if len(agent.marked) != 1 || !strings.Contains(agent.marked[0], "alpha\nbeta\ngamma") {
		t.Fatalf("the later send received %q", agent.marked)
	}
}

func TestInlineStandingPasteWaitsFoldedAndUnfoldsOnce(t *testing.T) {
	a, _ := steerableTurn(t, "working. ")
	a.paste("alpha\nbeta\ngamma")
	typeInto(t, a, "keep this /standing")
	drive(t, a, key("enter"))
	if len(a.parks) != 1 || len(a.parks[0].pastes) != 1 || !strings.Contains(a.parks[0].text, "[paste 1") {
		t.Fatalf("the tagged message did not wait folded: %+v", a.parks)
	}
	spoken := a.parks[0].spoken()
	if strings.Contains(spoken, "/standing") || !strings.Contains(spoken, "alpha\nbeta\ngamma") ||
		strings.Count(spoken, "paste 1:\n") != 1 {
		t.Fatalf("the waiting message speaks as %q", spoken)
	}
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
	sameRuns(t, boxRuns(a), nil, "the inert word the list wrote")

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
