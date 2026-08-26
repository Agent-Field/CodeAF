package tui3

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// hostedApp is a surface sitting in front of an engine on another machine.
func hostedApp(agent Agent) *app {
	a := newTestApp(agent)
	a.host = "devbox"
	return a
}

// userBlocks is every one of the person's own lines on the page.
func userBlocks(a *app) []entry {
	var said []entry
	for _, e := range a.entries {
		if e.kind == entryUser {
			said = append(said, e)
		}
	}
	return said
}

// THE LINE IS ON THE PAGE BEFORE THE ENGINE HAS BEEN ASKED. Everything about
// the block is already final — its place, its glyph, its wrap — and the one
// thing the gap changes is that its words are marked.
func TestOverAConnectionTheMessageAppearsBeforeTheEngineAnswers(t *testing.T) {
	agent := &fakeAgent{model: "a/b"}
	a := hostedApp(agent)

	cmd := a.submit("fix the roof")
	said := userBlocks(a)
	if len(said) != 1 || said[0].text != "fix the roof" {
		t.Fatalf("the message was not on the page before the call: %+v", said)
	}
	if !said[0].pending {
		t.Fatal("the echoed line carries no mark")
	}
	if len(agent.sent) != 0 {
		t.Fatalf("the engine was asked inside the update loop: %v", agent.sent)
	}

	// THE MARK COMES OFF THE SAME BLOCK, and no second block is appended: one
	// sentence is one line from the moment it is typed.
	a.adopt(runSubmit(t, cmd))
	said = userBlocks(a)
	if len(said) != 1 {
		t.Fatalf("confirming the message left %d lines, want exactly 1", len(said))
	}
	if said[0].pending {
		t.Fatal("the mark stayed on after the engine took the message")
	}
	if said[0].text != "fix the roof" {
		t.Fatalf("the confirmed line reads %q", said[0].text)
	}
}

// AND THE SAME MESSAGE AT HOME IS NEVER MARKED. There is no gap to mark — the
// answer is back inside the same update — and a mark that appeared and vanished
// on every message would be a flicker charged to everybody.
func TestAtThisMachineTheMessageIsNeverMarked(t *testing.T) {
	agent := &fakeAgent{model: "a/b"}
	a := newTestApp(agent)
	a.submit("fix the roof")
	said := userBlocks(a)
	if len(said) != 1 || said[0].pending {
		t.Fatalf("a local message was marked: %+v", said)
	}
	if a.echoAt != -1 {
		t.Fatalf("a local submit left an echo at %d", a.echoAt)
	}
}

// A MESSAGE THE ENGINE REFUSED NEVER HAPPENED. The line comes off the page and
// the engine's own sentence is put where it was — a transcript that kept the
// line would be the only record of the conversation showing a sentence the
// model was never given.
func TestARefusedMessageIsWithdrawnAndTheRefusalTakesItsPlace(t *testing.T) {
	agent := &fakeAgent{model: "a/b", failing: errors.New("another window is driving this conversation")}
	a := hostedApp(agent)

	before := a.turn
	cmd := a.submit("fix the roof")
	if len(userBlocks(a)) != 1 {
		t.Fatal("the message was not echoed at all")
	}
	a.adopt(runSubmit(t, cmd))

	if said := userBlocks(a); len(said) != 0 {
		t.Fatalf("the refused line stayed on the page: %+v", said)
	}
	last := a.entries[len(a.entries)-1]
	if last.kind != entryNote || !strings.Contains(last.text, "another window is driving") {
		t.Fatalf("the refusal is not where the line was: %+v", last)
	}
	// AND THE WORDING IS THE ENGINE'S OWN, not a second one invented here.
	if strings.Contains(last.text, "submit failed") {
		t.Fatalf("the surface wrote its own wording over the engine's: %q", last.text)
	}
	// AND THE TURN NUMBER GOES BACK WITH THE LINE: a turn was opened for a
	// message that never reached the engine.
	if a.turn != before {
		t.Fatalf("the turn counter is at %d, want %d", a.turn, before)
	}
}

// A LOCAL REFUSAL KEEPS THE BEHAVIOUR IT ALWAYS HAD: nothing was echoed, so
// nothing is withdrawn and the note lands under the person's line.
func TestARefusalAtThisMachineStillKeepsTheLine(t *testing.T) {
	agent := &fakeAgent{model: "a/b", failing: errors.New("no")}
	a := newTestApp(agent)
	cmd := a.submit("fix the roof")
	a.adopt(runSubmit(t, cmd))
	if said := userBlocks(a); len(said) != 1 {
		t.Fatalf("a local refusal removed the line: %+v", said)
	}
}

// THE ECHO RECONCILES ONCE AND ONLY ONCE. A confirmation that arrived twice —
// which is what a redial's replay can look like from up here — must not find a
// second line to mark, and must not un-mark a message typed after it.
func TestTheEchoReconcilesOnceAndNeverTwice(t *testing.T) {
	agent := &fakeAgent{model: "a/b"}
	a := hostedApp(agent)

	cmd := a.submit("fix the roof")
	answer := runSubmit(t, cmd)
	a.adopt(answer)
	// The same answer again, as a replayed one would arrive.
	a.adopt(answer)
	if said := userBlocks(a); len(said) != 1 {
		t.Fatalf("a repeated confirmation left %d lines, want 1", len(said))
	}

	// A SECOND MESSAGE MARKS ITSELF AND IS NOT UN-MARKED BY THE FIRST'S ANSWER.
	a.stream = nil
	next := a.submit("and the gutter")
	a.adopt(answer)
	said := userBlocks(a)
	if len(said) != 2 {
		t.Fatalf("the second message is not on the page: %+v", said)
	}
	if !said[1].pending {
		t.Fatal("an old confirmation took the mark off a newer message")
	}
	a.adopt(runSubmit(t, next))
	if said = userBlocks(a); said[1].pending {
		t.Fatal("the second message never settled")
	}
}

// AND THE MARK IS THE narr TIER AND NOT A COLOUR OF ITS OWN. The frame is read
// rather than the field, because what a person sees is the paint.
func TestTheMarkIsOneReadingStepDownAndNothingElse(t *testing.T) {
	agent := &fakeAgent{model: "a/b"}
	a := hostedApp(agent)
	cmd := a.submit("fix the roof")

	marked := frame(a)
	if !strings.Contains(marked, a.pal.narr("fix the roof")) {
		t.Fatalf("the marked line is not painted in the narr tier:\n%s", marked)
	}
	a.adopt(runSubmit(t, cmd))
	settled := frame(a)
	if !strings.Contains(settled, a.pal.muted("fix the roof")) {
		t.Fatalf("the settled line is not painted in the person's own tier:\n%s", settled)
	}
	if strings.Contains(settled, a.pal.narr("fix the roof")) {
		t.Fatalf("the mark survived the confirmation:\n%s", settled)
	}
}

// runSubmit runs the command a submit returns and hands back its answer.
func runSubmit(t *testing.T, cmd tea.Cmd) submittedMsg {
	t.Helper()
	if cmd == nil {
		t.Fatal("the submit produced no command")
	}
	for _, msg := range runCmd(cmd) {
		if answer, ok := msg.(submittedMsg); ok {
			return answer
		}
	}
	t.Fatal("the submit never answered")
	return submittedMsg{}
}
