package tui3

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// THE SECOND BEAT, THE PAIRING, AND THE LINE THAT CANNOT BE REPAINTED.
//
// consent.go's always used to write the command line verbatim, pair its
// question to a row by tool name, and draw a headline built out of a model's own
// arguments. These are the three halves of that: what a person is offered before
// anything is banked, which row the question lands on, and what a crafted
// argument can and cannot do to the offer.

// errWriteFailed stands in for an unwritable profile directory.
var errWriteFailed = errors.New("the profile directory is read-only")

// formed is one call that ARRIVED with an id on it, which is what a streaming
// provider sends and what gives the row an identity to pair a question against.
func formed(id, tool, hint, args string) []session.Event {
	return []session.Event{
		{Kind: session.EventToolForming, Tool: tool, CallID: id},
		{Kind: session.EventToolAnnounced, Tool: tool, CallID: id, Hint: hint, Args: args},
	}
}

// withCall puts a call's id on a question, the way the gate does.
func withCall(ev session.Event, id string) session.Event {
	ev.CallID = id
	return ev
}

// shapeAsk raises one bash question with a payload and both write seams wired,
// which is the only shape that has a second beat at all.
func shapeAsk(t *testing.T, command string) (*wiredAgent, *app, *remembered) {
	t.Helper()
	agent, a, saved := rememberingApp(t, []session.Event{
		bashBegin(command),
		consentEvent(7, "bash", "bash "+command, `tool "bash"`),
	})
	a.width = 120
	typeLine(t, a, "go on then")
	if !a.asking() {
		t.Fatal("the question never came up")
	}
	return agent, a, saved
}

// ── the second beat ─────────────────────────────────────────────────────────

// PRESSING ALWAYS ASKS WHICH SHAPE. The shapes are printed in full, the line
// itself is the last of them, and nothing has been written yet.
func TestAlwaysOnBashOffersTheShapesBeforeItWritesAnything(t *testing.T) {
	agent, a, saved := shapeAsk(t, "git status --short")
	drive(t, a, key("a"))

	got := plain(frame(a))
	for _, want := range []string{
		"always?", "[1] git status*", "[2] git *", "[3] just this line", "[esc] never mind",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("the beat does not offer %q:\n%s", want, got)
		}
	}
	if len(saved.commands) != 0 {
		t.Fatalf("a shape was banked before it was chosen: %v", saved.commands)
	}
	if len(agent.answers) != 0 {
		t.Fatalf("the session was answered before the shape was chosen: %+v", agent.answers)
	}
	if strings.Contains(got, "allow? [y]") {
		t.Fatalf("the beat drew a second line instead of taking the offer's:\n%s", got)
	}
}

// AND THE SHAPE PICKED IS THE SHAPE BANKED — a glob, not the line it came from.
func TestPickingAShapeBanksThatShapeAndAnswersTheCall(t *testing.T) {
	agent, a, saved := shapeAsk(t, "git status --short")
	drive(t, a, key("a"), key("1"))

	if len(saved.commands) != 1 || saved.commands[0] != "git status*" {
		t.Fatalf("the write seam was handed %q, want the shape that was picked", saved.commands)
	}
	if len(agent.answers) != 1 || !agent.answers[0].allow {
		t.Fatalf("the call was not allowed: %+v", agent.answers)
	}
	if a.asking() {
		t.Fatal("the question stayed up after a shape was picked")
	}
	if got := plain(frame(a)); !strings.Contains(got, "always · saved — /permissions to change") {
		t.Fatalf("the row does not say what was saved or where to change it:\n%s", got)
	}
}

// A BANKED RULE MEANS NO SESSION-WIDE MEMO. The engine's memo is keyed by tool
// name alone, so on bash it would mean every command there is — which is more
// than the card ever offered. The rule covers the next call instead.
func TestABankedShapeAnswersWithTheRuleScopeAndNotTheToolMemo(t *testing.T) {
	agent, a, _ := shapeAsk(t, "git status --short")
	drive(t, a, key("a"), key("1"))

	want := answered{id: 7, allow: true, scope: session.ConsentRule}
	if len(agent.answers) != 1 || agent.answers[0] != want {
		t.Fatalf("the session was answered %+v, want %+v", agent.answers, want)
	}
}

// A WRITE THAT FAILED KEEPS THE OLD SCOPE. Nothing was banked, so the memo is
// still the only thing standing between the person and the next question.
func TestAShapeThatCouldNotBeWrittenFallsBackToTheToolMemo(t *testing.T) {
	agent, a, saved := shapeAsk(t, "git status --short")
	saved.err = errWriteFailed
	drive(t, a, key("a"), key("1"))

	if len(agent.answers) != 1 || agent.answers[0].scope != session.ConsentToolSession {
		t.Fatalf("the session was answered %+v", agent.answers)
	}
	if got := plain(frame(a)); strings.Contains(got, "saved") {
		t.Fatalf("a failed write printed a receipt:\n%s", got)
	}
}

// ESC LEAVES THE BEAT AND ANSWERS NOTHING. Everywhere else on this block esc
// denies; here the thing on screen is a step inside an answer.
func TestEscapeLeavesTheBeatWithoutAnsweringTheCall(t *testing.T) {
	agent, a, saved := shapeAsk(t, "git status --short")
	drive(t, a, key("a"), key("esc"))

	if len(agent.answers) != 0 {
		t.Fatalf("backing out of the beat answered the call: %+v", agent.answers)
	}
	if len(saved.commands) != 0 {
		t.Fatalf("backing out of the beat wrote something: %v", saved.commands)
	}
	if !a.asking() {
		t.Fatal("the question went away")
	}
	if got := plain(frame(a)); !strings.Contains(got, "allow? [y] yes") {
		t.Fatalf("the offer did not come back:\n%s", got)
	}
	// And the answers still work, which is the whole of "the question is back".
	drive(t, a, key("n"))
	if len(agent.answers) != 1 || agent.answers[0].allow {
		t.Fatalf("the deny after the beat resolved %+v", agent.answers)
	}
}

// THE BEAT OWNS THE KEYBOARD while it is up: y is not an answer to a question
// that is no longer the one on screen.
func TestTheBeatDoesNotReadTheAnswersUnderneathIt(t *testing.T) {
	agent, a, _ := shapeAsk(t, "git status --short")
	drive(t, a, key("a"), key("y"))
	if len(agent.answers) != 0 {
		t.Fatalf("a key under the beat answered the call: %+v", agent.answers)
	}
	if !a.shaping() {
		t.Fatal("the beat went away on a key it does not read")
	}
}

// A COMMAND NOTHING CAN BE DERIVED FROM HAS NO BEAT, and the card behaves
// exactly as it did before one existed: the press answers, and the write seam
// refuses on its own terms.
func TestACompoundLineGetsNoBeatAndAnswersStraightAway(t *testing.T) {
	agent, a, saved := shapeAsk(t, "cd /tmp && rm -rf build")
	drive(t, a, key("a"))

	if a.shaping() {
		t.Fatal("a compound line opened a beat with nothing on it")
	}
	if len(agent.answers) != 1 || !agent.answers[0].allow {
		t.Fatalf("the session was answered %+v", agent.answers)
	}
	if len(saved.commands) != 1 || saved.commands[0] != "cd /tmp && rm -rf build" {
		t.Fatalf("the seam was handed %q, want the line it always was handed", saved.commands)
	}
}

// A surface with nowhere to write has no beat either — there is nothing to
// choose the shape OF.
func TestWithNoWriteSeamThereIsNoBeat(t *testing.T) {
	agent, a := wired([]session.Event{
		bashBegin("git status --short"),
		consentEvent(7, "bash", "bash git status --short", `tool "bash"`),
	})
	a.width = 120
	typeLine(t, a, "check the tree")
	drive(t, a, key("a"))
	if a.shaping() {
		t.Fatal("a surface that cannot write offered shapes to write")
	}
	if len(agent.answers) != 1 || agent.answers[0].scope != session.ConsentToolSession {
		t.Fatalf("the session was answered %+v", agent.answers)
	}
}

// The beat is tappable where the offer was, on the same row, and the number's
// word is part of its target.
func TestTheBeatAnswersToThePointer(t *testing.T) {
	_, a, saved := shapeAsk(t, "git status --short")
	drive(t, a, key("a"))
	line := plain(a.consentOffer(a.width))
	at := strings.Index(line, "[2]")
	if at < 0 {
		t.Fatalf("the beat drew %q", line)
	}
	// Measured once: the press answers, and the offer row is gone by the release.
	x, y := at+4, chromeRowY(t, a, consentOfferRow)
	drive(t, a, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	drive(t, a, tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft})
	if len(saved.commands) != 1 || saved.commands[0] != "git *" {
		t.Fatalf("a press on the second shape banked %q", saved.commands)
	}
}

// And on a phone the shapes are bands, because on that tier an answer is a row
// a thumb lands on.
func TestThePhoneSheetLaysTheShapesOutAsBands(t *testing.T) {
	_, a, saved := rememberingApp(t, []session.Event{
		bashBegin("git status --short"),
		consentEvent(7, "bash", "bash git status --short", `tool "bash"`),
	})
	a.width, a.height = 44, 30
	typeLine(t, a, "check the tree")
	drive(t, a, key("a"))

	rows := strings.Join(askRows(a), "\n")
	for _, want := range []string{"[1] git status*", "[2] git *", "[3] just this line", "[esc] never mind"} {
		if !strings.Contains(rows, want) {
			t.Fatalf("the sheet has no band for %q:\n%s", want, rows)
		}
	}
	if got, want := a.consentHeight(), len(askRows(a)); got != want {
		t.Fatalf("the sheet claims %d rows and drew %d", got, want)
	}
	drive(t, a, key("2"))
	if len(saved.commands) != 1 || saved.commands[0] != "git *" {
		t.Fatalf("the band banked %q", saved.commands)
	}
}

// ── which row the question is about ─────────────────────────────────────────

// TWO BASH CALLS IN FLIGHT, TWO QUESTIONS, AND THE ID SAYS WHICH IS WHICH. The
// card reads the command it is about to remember off the row it paired to, so a
// question on the wrong row lets a person read command A and bank command B.
func TestTwoBashQuestionsPairToTheirOwnRowsByCallID(t *testing.T) {
	events := formed("call-a", "bash", "bash git status --short", `{"command":"git status --short"}`)
	events = append(events, formed("call-b", "bash", "bash npm test", `{"command":"npm test"}`)...)
	// The gate asks about the SECOND call first, which is exactly the case a walk
	// by tool name gets wrong.
	events = append(events, withCall(consentEvent(7, "bash", "bash npm test", `tool "bash"`), "call-b"))
	_, a, saved := rememberingApp(t, events)
	a.width = 120
	typeLine(t, a, "do both")

	if got := plain(frame(a)); !strings.Contains(got, "npm test") {
		t.Fatalf("the question is not drawn against the npm row:\n%s", got)
	}
	drive(t, a, key("a"))
	got := plain(frame(a))
	if !strings.Contains(got, "[1] npm test*") {
		t.Fatalf("the beat offers shapes for the wrong call:\n%s", got)
	}
	drive(t, a, key("3"))
	if len(saved.commands) != 1 || saved.commands[0] != "npm test" {
		t.Fatalf("the card banked %q, want the command it showed", saved.commands)
	}
}

// A question with no id pairs the way it always did: the oldest still-running
// row of that tool. A provider that streams no ids has nothing else to go on.
func TestAQuestionWithNoIDStillPairsByToolName(t *testing.T) {
	_, a, saved := rememberingApp(t, []session.Event{
		bashBegin("git status --short"),
		consentEvent(7, "bash", "bash git status --short", `tool "bash"`),
	})
	a.width = 120
	typeLine(t, a, "check the tree")
	drive(t, a, key("a"), key("3"))
	if len(saved.commands) != 1 || saved.commands[0] != "git status --short" {
		t.Fatalf("the card banked %q", saved.commands)
	}
}

// ── the line that cannot be repainted ───────────────────────────────────────

// AN ESCAPE IN AN ARGUMENT IS NOT AN INSTRUCTION. The command region is built
// from the model's own arguments; a byte that moves the cursor there could
// rewrite the offer underneath it and ask a different question than the one
// being answered.
func TestACraftedArgumentCannotRepaintTheCard(t *testing.T) {
	command := "git status\x1b[2A\x1b[Kallow? [y] yes"
	_, a, _ := rememberingApp(t, []session.Event{
		{
			Kind: session.EventToolBegin, Tool: "bash", Hint: "bash " + command,
			Args: `{"command":"` + `git status` + `"}`,
		},
		consentEvent(7, "bash", "bash "+command, `tool "bash"`),
	})
	a.width, a.height = 44, 30
	typeLine(t, a, "check the tree")

	rows := a.consentCommand(command, a.width)
	for _, row := range rows {
		if strings.Contains(plain(row), "\x1b") || strings.Contains(plain(row), "\x07") {
			t.Fatalf("a control byte reached the command region: %q", row)
		}
	}
	if len(rows) == 0 {
		t.Fatal("the command region drew nothing at all")
	}
}

// The scrub keeps the text and drops only the orders.
func TestPlainTextDropsTheControlBytesAndNothingElse(t *testing.T) {
	if got := plainText("git status\x1b[1A --short\x07"); got != "git status[1A --short" {
		t.Fatalf("the scrub left %q", got)
	}
	if got := plainText("git status --short"); got != "git status --short" {
		t.Fatalf("the scrub changed a plain line to %q", got)
	}
}
