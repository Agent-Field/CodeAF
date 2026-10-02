package tui3

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/standing"
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
		"look at cmd/codeaf/main.go",
		"https://example.com/help",
	} {
		a.input.reset()
		typeInto(t, a, line)
		sameRuns(t, boxRuns(a), nil, line)
	}
}

// TestAllRecognisedCommandsAreChippedInsideASentence is the acceptance
// sentence: a known command is chipped wherever it stands in the draft, and
// keeps that mark in the transcript. The mark is a recognition mark, not a
// promise enter will act — only the send doors still act (see the non-door
// test below).
func TestAllRecognisedCommandsAreChippedInsideASentence(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	// /senior-dev is a PROGRAM'S ROW, not a literal one: the engine's build
	// carries it as a delegate command (delegate.go) and the launch installs it
	// on the live table, which is how the shipped binary knows the word. Install
	// it the same way so this test reads the real path.
	t.Cleanup(func() { installDelegateCommands(nil) })
	installDelegateCommands([]session.DelegateRow{{Name: "senior-dev", Description: "a program"}})

	typeInto(t, a, "try running /senior-dev on this")
	sameRuns(t, boxRuns(a), []string{"/senior-dev"}, "a program named mid-sentence")

	// AND IT KEEPS THE CHIP AFTER IT IS SENT. The transcript is the only record
	// of what was asked for, and a mark that survived only until enter would be
	// taken back at the moment it is worth having.
	a.entries = []entry{{kind: entryUser, text: "try running /senior-dev on this"}}
	sameRuns(t, chipRuns(a.renderEntry(0, &a.entries[0], a.width)...),
		[]string{"/senior-dev"}, "the sent message keeps the chip")

	// A SEND-DOOR TAG IS UNCHANGED away from the head.
	a.entries = []entry{{kind: entryUser, text: "keep this true /standing"}}
	sameRuns(t, chipRuns(a.renderEntry(0, &a.entries[0], a.width)...),
		[]string{"/standing"}, "a send-door tag in the sent message")

	// AND SO IS A NON-DOOR COMMAND: it chips, and an unknown word beside it does
	// not — this is the recognition rule, not a second send rule.
	a.entries = []entry{{kind: entryUser, text: "later I will run /compact on this, not /nope"}}
	sameRuns(t, chipRuns(a.renderEntry(0, &a.entries[0], a.width)...),
		[]string{"/compact"}, "a non-door command in the sent message")
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
	// A DEMOTED WORD STAYS PLAIN IN THE TRANSCRIPT, exactly as it does in the
	// box: the word no longer routes — marked is empty and it travels as prose
	// — and the demotion now carries through the reset to the entry
	// (input.go's [app.enterLine] snapshots it), so no chip is painted for it.
	if got := chipRuns(a.renderEntry(0, &a.entries[0], a.width)...); len(got) != 0 {
		t.Fatalf("demoted transcript chipped %q, want none", got)
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
	// The edited word is still a demoted one, so the shifted range leaves the
	// transcript plain too — the plainness carried through the edit.
	if got := chipRuns(a.renderEntry(0, &a.entries[0], a.width)...); len(got) != 0 {
		t.Fatalf("shifted demotion chipped %q, want none", got)
	}
}

// TestADemotedTagStaysPlainWhenTheSentLineWraps is the coordinate half of the
// demotion promise. The demoted ranges are offsets into the pre-wrap text,
// while the transcript paints one wrapped row at a time and restarts each
// row's offsets at zero; a subtraction that ignored the row offset would
// either miss the tag or chip an unrelated word. Here the tag lands on a second
// row at the suite's sixty columns, so a whole-entry subtraction would be
// compared against a row-local span.
func TestADemotedTagStaysPlainWhenTheSentLineWraps(t *testing.T) {
	a, agent := tagTestApp()
	line := "the quick brown fox jumps over the lazy dog and keeps going /standing"
	typeInto(t, a, line)
	drive(t, a, key("backspace"))
	if got := boxRuns(a); len(got) != 0 {
		t.Fatalf("the demotion did not clear the box chip: %q", got)
	}
	drive(t, a, key("enter"))
	if len(agent.sent) != 1 || agent.sent[0] != line || len(agent.marked) != 0 {
		t.Fatalf("demoted wrapped send: sent=%q marked=%q", agent.sent, agent.marked)
	}
	rows := a.renderEntry(0, &a.entries[0], a.width)
	if len(rows) < 2 {
		t.Fatalf("the line did not wrap at this width: %#v", rows)
	}
	if got := chipRuns(rows...); len(got) != 0 {
		t.Fatalf("a demoted tag in a wrapped transcript chipped %q, want none", got)
	}
}

func TestNonDoorCommandsStayInertAndLeadingCommandsAreUnchanged(t *testing.T) {
	a, agent := tagTestApp()
	typeInto(t, a, "please /compact later")
	// A CHIP IS A RECOGNITION MARK, NOT A SEND PROMISE: /compact wears one
	// mid-sentence now, and enter still does not run it.
	if got := boxRuns(a); len(got) != 1 || got[0] != "/compact" {
		t.Fatalf("a non-door command mid-sentence chipped %q, want [/compact]", got)
	}
	drive(t, a, key("enter"))
	if len(agent.sent) != 1 || agent.sent[0] != "please /compact later" {
		t.Fatalf("inert command sent %q", agent.sent)
	}
	if len(agent.marked) != 0 {
		t.Fatalf("a non-door command routed %q", agent.marked)
	}
	if got := chipRuns(a.renderEntry(0, &a.entries[0], a.width)...); len(got) != 1 || got[0] != "/compact" {
		t.Fatalf("the sent non-door command chipped %q, want [/compact]", got)
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
	if door.singleCalls != 1 || door.brief != "investigate the wrap" {
		t.Fatalf("task tag started %d tasks with brief %q", door.singleCalls, door.brief)
	}
	if a.input.String() != "" {
		t.Fatalf("task tag left %q in the draft", a.input.String())
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

// A QUOTED COMMAND IS STILL THE COMMAND. Prose that names one — the setup's
// detail says '/budget 50' — gets its chip on the name alone, and a closing
// quote right after the name is not part of it. A quoted path stays a path.
func TestAQuoteIsAWordBoundaryForACommandChip(t *testing.T) {
	for _, c := range []struct {
		text string
		want []string
	}{
		{"type '/budget 50' to change it", []string{"/budget"}},
		{"\u201c/budget conversation 20\u201d sets one", []string{"/budget"}},
		{"see '/settings'", []string{"/settings"}},
		{"open '/Users/person/notes.md'", nil},
	} {
		var got []string
		for _, span := range recognizedCommandSpans([]rune(c.text), true) {
			got = append(got, string([]rune(c.text)[span.from:span.to]))
		}
		if strings.Join(got, " ") != strings.Join(c.want, " ") {
			t.Errorf("%q chipped %v, want %v", c.text, got, c.want)
		}
	}
}

func TestAnAbsolutePathDoesNotHoldTheCommandListOpen(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})

	typeInto(t, a, "/Users/person")
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
	typeInto(t, a, "open /tmp/codeaf/scratch")
	if a.menu.open {
		t.Fatalf("a path mid-sentence left the list up over %q", a.input.String())
	}
}

func TestTheListStaysInCommandModeUntilSpaceOrEsc(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})

	// Nothing matched is nothing to offer, and a backspace back into a word that
	// does match brings it straight back.
	typeInto(t, a, "/zzz")
	if !a.menu.open || len(a.menu.hits) != 0 {
		t.Fatal("an unmatched slash word left command mode")
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
	// The caret is after the word it just wrote, and the word wears its chip —
	// a recognition mark, not a send promise.
	if a.input.cursor != len([]rune("before you answer, /compact")) {
		t.Fatalf("the caret parked at %d", a.input.cursor)
	}
	sameRuns(t, boxRuns(a), []string{"/compact"}, "the inert word the list wrote")

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

// lastUserEntry is the newest line the person said, wherever the turn's other
// entries landed around it.
func lastUserEntry(t *testing.T, a *app) *entry {
	t.Helper()
	for i := len(a.entries) - 1; i >= 0; i-- {
		if a.entries[i].kind == entryUser {
			return &a.entries[i]
		}
	}
	t.Fatal("no user entry in the transcript")
	return nil
}

// A DEMOTION WAITS WITH THE WORDS. Enter over a running answer parks the
// message, and the box's reset has already happened by the time it goes, so the
// demoted range has to travel on the parked message or the transcript chips the
// word the person made plain.
func TestADemotedTagStaysPlainWhenTheMessageWaitsForTheAnswer(t *testing.T) {
	a, agent := tagTestApp()
	a.state = stateWorking
	typeInto(t, a, "say /standing")
	drive(t, a, key("backspace"), key("enter"))
	if len(a.parks) != 1 || len(a.parks[0].plain) != 1 {
		t.Fatalf("the demotion did not travel with the parked message: %+v", a.parks)
	}
	a.state = stateIdle
	drive(t, a, runCmd(a.sendParked())...)
	if len(agent.sent) != 1 || agent.sent[0] != "say /standing" || len(agent.marked) != 0 {
		t.Fatalf("parked demoted send: sent=%q marked=%q", agent.sent, agent.marked)
	}
	e := lastUserEntry(t, a)
	if got := chipRuns(a.renderEntry(0, e, a.width)...); len(got) != 0 {
		t.Fatalf("a parked demoted tag chipped %q in the transcript, want none", got)
	}
}

// And a parked message pulled back into the box keeps the tag plain there too:
// the words come back exactly as they were parked.
func TestARecalledParkedMessageKeepsItsTagPlain(t *testing.T) {
	a, _ := tagTestApp()
	a.state = stateWorking
	typeInto(t, a, "say /standing")
	drive(t, a, key("backspace"), key("enter"))
	if !a.recallParked() {
		t.Fatal("nothing was recalled")
	}
	if a.input.String() != "say /standing" || len(boxRuns(a)) != 0 {
		t.Fatalf("the recalled demotion came back chipped: %q runs=%q", a.input.String(), boxRuns(a))
	}
}

// The /standing tag's own road parks the words WITHOUT the tag, so a second,
// demoted tag in the same line has to be carried into those shorter words.
func TestADemotedTagBesideALiveStandingTagStaysPlainWhenParked(t *testing.T) {
	a, agent := tagTestApp()
	a.state = stateWorking
	typeInto(t, a, "keep this /task")
	drive(t, a, key("backspace"))
	typeInto(t, a, " /standing")
	drive(t, a, key("enter"))
	if len(a.parks) != 1 || a.parks[0].text != "keep this /task" || !a.parks[0].standing {
		t.Fatalf("the tagged line did not park as its marked words: %+v", a.parks)
	}
	a.state = stateIdle
	drive(t, a, runCmd(a.sendParked())...)
	if len(agent.marked) != 1 || agent.marked[0] != "keep this /task" {
		t.Fatalf("parked standing tag routed %q", agent.marked)
	}
	e := lastUserEntry(t, a)
	if got := chipRuns(a.renderEntry(0, e, a.width)...); len(got) != 0 {
		t.Fatalf("the demoted /task chipped %q once the parked words went, want none", got)
	}
}

func TestPlainWithoutTagFollowsTheWordsTheTagLeaves(t *testing.T) {
	for _, c := range []struct{ line, want string }{
		{"  say /task then /standing more", "say /task then more"},
		{"/standing  say /task", "say /task"},
		{"say /task /standing", "say /task"},
		{"say /standing then /task", "say then /task"},
	} {
		value := []rune(c.line)
		trimmed := []rune(strings.TrimSpace(c.line))
		at := len([]rune(strings.SplitN(string(trimmed), "/task", 2)[0]))
		plain := []segment{{from: at, to: at + len("/task")}}
		from := len([]rune(strings.SplitN(c.line, "/standing", 2)[0]))
		tag := segment{from: from, to: from + len("/standing")}
		if got := removeSlashTag(value, tag); got != c.want {
			t.Fatalf("%q: removeSlashTag gave %q, want %q", c.line, got, c.want)
		}
		got := plainWithoutTag(value, tag, plain)
		words := []rune(c.want)
		if len(got) != 1 || string(words[got[0].from:got[0].to]) != "/task" {
			t.Fatalf("%q: rebased %v onto %q, want the range of /task", c.line, got, c.want)
		}
	}
}
