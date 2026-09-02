package tui3

// THE STOP HAS A BOUND, THE BOUND IS ON THE SCREEN, AND PAST IT THE SURFACE
// LETS GO.
//
// stopping_test.go pins what the window looks like. These pin that it ENDS.
// Before this wave it did not: `stopping` was unbounded by design, and a turn
// parked on a wait nothing could cancel left a person with no key that would end
// it — the measured run in issue #265 stood for four minutes and then had to
// take the whole process, every other conversation with it.

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// abandoningAgent is a [fakeAgent] that also has the second stage's door. The
// door is an optional assertion on this surface ([abandonAgent]), so an agent
// WITHOUT it is the other half of the law and is tested with the plain fake.
type abandoningAgent struct {
	*fakeAgent
	spend    session.Usage
	letGo    bool
	abandons int
	reasons  []session.AbandonReason
}

func (a *abandoningAgent) Abandon(reason session.AbandonReason) (session.Usage, bool) {
	a.abandons++
	a.reasons = append(a.reasons, reason)
	if !a.letGo {
		return session.Usage{}, false
	}
	return a.spend, true
}

// boundedStopApp is a turn stopped by hand, on an agent that can actually be
// made to let go, with the clock in the test's hand.
func boundedStopApp(t *testing.T) (*app, *abandoningAgent, func(time.Duration)) {
	t.Helper()
	agent := &abandoningAgent{fakeAgent: &fakeAgent{model: "m"}, letGo: true}
	a := newTestApp(agent)
	now := time.Now()
	a.clock = func() time.Time { return now }
	drive(t, a, submittedMsg{ch: make(chan session.Event)})
	a.state = stateWorking
	a.turnBegan = a.now()
	drive(t, a, key("esc"))
	return a, agent, func(d time.Duration) { now = now.Add(d) }
}

// ── the bound is stated before it fires ─────────────────────────────────────

// THE STATUS LINE SAYS WHEN IT WILL DETACH, and it says it from the keypress.
// A countdown a person cannot see is a bound they have to take on trust, which
// is the same thing as no bound at all.
func TestTheStoppingLineSaysWhenItWillDetach(t *testing.T) {
	a, _, advance := boundedStopApp(t)

	word, _ := a.stateWord()
	if !strings.HasPrefix(word, stoppingWord) {
		t.Fatalf("the status line says %q, want it to start with %q", word, stoppingWord)
	}
	if !strings.Contains(word, stopDetachWord) {
		t.Fatalf("the status line says %q and never says when it detaches", word)
	}
	if !strings.Contains(word, "10s") {
		t.Fatalf("the first frame after esc says %q, want the whole bound", word)
	}
	if got := plain(frame(a)); !strings.Contains(got, stopDetachWord) {
		t.Fatalf("the frame does not carry the bound:\n%s", got)
	}

	// AND IT COUNTS DOWN. The figure is what makes it a promise rather than a
	// label, so a countdown that stood still would be the label again.
	advance(4 * time.Second)
	if word, _ := a.stateWord(); !strings.Contains(word, "6s") {
		t.Fatalf("four seconds in, the line says %q, want 6s left", word)
	}
}

// AND IT SAYS NOTHING IT CANNOT DO. An agent with no door behind the deadline
// gets the word alone: a capability that cannot work is absent, not broken, and
// a countdown drawn over nothing would be this surface promising an act it has
// no way to perform.
func TestAStopWithNoDoorBehindItAnnouncesNoBound(t *testing.T) {
	a, _ := stoppingApp(t)
	drive(t, a, key("esc"))

	if a.stopBounded() {
		t.Fatal("a surface with no abandon door claims a bound it cannot keep")
	}
	if word, _ := a.stateWord(); word != stoppingWord {
		t.Fatalf("the status line says %q, want the bare %q", word, stoppingWord)
	}
}

// ── the bound fires, and not before ─────────────────────────────────────────

// NOTHING IS DETACHED INSIDE THE WINDOW. The whole value of a ten-second bound
// is that it sits above the longest ordinary letting-go — three seconds for a
// bash holding a leaked pipe, four for a jobs kill — so a turn that was going to
// end tidily always does.
func TestTheTurnIsNotDetachedInsideTheWindow(t *testing.T) {
	a, agent, advance := boundedStopApp(t)

	advance(stopGrace - time.Second)
	drive(t, a, frameMsg{})
	if agent.abandons != 0 {
		t.Fatalf("the turn was detached %d second(s) early", 1)
	}
	if !a.windingDown() {
		t.Fatal("the surface stopped winding down before the bound")
	}

	// And a turn the engine lets go of properly never reaches the door at all.
	drive(t, a, streamClosedMsg{gen: a.gen})
	advance(2 * stopGrace)
	drive(t, a, frameMsg{})
	if agent.abandons != 0 {
		t.Fatal("a turn the engine let go of was detached anyway")
	}
	if !a.stopBy.IsZero() {
		t.Fatal("the deadline outlived the turn it bounded")
	}
}

// AND AT THE BOUND IT FIRES, ONCE, AND THE SURFACE IS FREE.
//
// Three things have to be true together, which is why they are one test: the
// engine's door was opened, this surface stopped holding the turn's stream, and
// the person was told — in the words the countdown they had been reading
// promised them.
func TestAtTheBoundTheTurnIsDetachedAndTheSurfaceIsFree(t *testing.T) {
	a, agent, advance := boundedStopApp(t)
	agent.spend = session.Usage{CostUSD: 0.04}

	advance(stopGrace)
	drive(t, a, frameMsg{})

	if agent.abandons != 1 {
		t.Fatalf("the door was opened %d times at the bound, want exactly one", agent.abandons)
	}
	if agent.reasons[0] != session.AbandonStopTimeout {
		t.Fatalf("the turn was abandoned for %q, want %q", agent.reasons[0], session.AbandonStopTimeout)
	}
	if a.stream != nil {
		t.Fatal("the surface is still holding the detached turn's stream")
	}
	if a.windingDown() {
		t.Fatal("the surface is still winding down after it detached")
	}
	// The note is read off the entries rather than off the frame: the frame
	// wraps it, and a test that matched the wrapped shape would be a test about
	// this terminal's width.
	said := lastNote(t, a)
	if !strings.HasPrefix(said, stopDetachedWord) {
		t.Fatalf("the conversation was told %q, want the detached note", said)
	}
	// THE MONEY IS NAMED, because what a person wants to know about work nobody
	// waited for is what it cost them.
	if !strings.Contains(said, "0.04") {
		t.Fatalf("the detached note does not name what the turn spent: %q", said)
	}

	// AND IT FIRES ONCE. Every later frame finds no deadline and no turn.
	advance(stopGrace)
	drive(t, a, frameMsg{})
	if agent.abandons != 1 {
		t.Fatalf("the bound fired again after it had already detached (%d)", agent.abandons)
	}
}

// A DETACHED TURN SPENT NOTHING SAYS NOTHING ABOUT MONEY, which is the emptiness
// law: a `$0.00` here would be a measurement where there is only an absence.
func TestADetachedTurnThatSpentNothingNamesNoMoney(t *testing.T) {
	a, agent, advance := boundedStopApp(t)

	advance(stopGrace)
	drive(t, a, frameMsg{})
	if agent.abandons != 1 {
		t.Fatalf("the door was opened %d times", agent.abandons)
	}
	if got := lastNote(t, a); got != stopDetachedWord {
		t.Fatalf("a turn that spent nothing was told %q, want the note with no money on it", got)
	}
}

// AND THE NEXT PROMPT IS USABLE. This is the whole of what the person wanted
// when they pressed the key, and the acceptance the issue asks for: after the
// bound the box takes a sentence and the surface starts an ordinary turn with
// it.
func TestAfterTheBoundTheNextPromptRuns(t *testing.T) {
	a, agent, advance := boundedStopApp(t)

	advance(stopGrace)
	drive(t, a, frameMsg{})
	drive(t, a, key("h"), key("i"), key("enter"))

	if len(agent.sent) != 1 || agent.sent[0] != "hi" {
		t.Fatalf("the prompt after a detached turn did not reach the session: %q", agent.sent)
	}
}

// THE CLOCK KEEPS TURNING FOR EXACTLY AS LONG AS THE WINDOW LASTS. A stopped
// turn draws nothing new by design, so without this the countdown would be a
// still photograph and the deadline would never be reached at all — which is
// how a bound driven by a frame clock fails.
func TestTheFrameClockRunsForTheWholeStoppingWindow(t *testing.T) {
	a, _, advance := boundedStopApp(t)

	if !a.stopBounded() {
		t.Fatal("the stop is not bounded, so the clock has no reason to turn")
	}
	advance(stopGrace)
	drive(t, a, frameMsg{})
	if a.stopBounded() {
		t.Fatal("the window outlived the detach that ended it")
	}
}

