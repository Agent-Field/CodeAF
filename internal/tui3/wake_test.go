package tui3

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// A TURN NOBODY ASKED FOR REACHES THE SCREEN.
//
// Work handed to a task lands minutes later, usually with nothing running: the
// session takes the news and starts a turn of its own to say what it makes of it
// ([session.Agent.Wakes]). Nobody is holding that turn's channel, so what these
// tests own is that the surface holds it — and that it draws a turn the person
// did not type without inventing a line saying they did.

// THE LANE THIS SURFACE READS IS THE ONE THE REAL SESSION HANDS OUT. The
// assertion is optional at runtime (followup.go's [wakeAgent] is asserted, not
// required), so this is where a signature drift would otherwise be found by
// nobody: a surface that silently stopped matching would simply never wake.
var _ wakeAgent = (*session.Agent)(nil)

// wakeFake is a session that can wake: it hands the standing lane over and lets
// a test push one woken turn's stream onto it.
type wakeFake struct {
	*fakeAgent
	wakes chan (<-chan session.Event)
}

func (w *wakeFake) Wakes() <-chan (<-chan session.Event) { return w.wakes }

func wakeApp(t *testing.T) (*app, *wakeFake) {
	t.Helper()
	agent := &wakeFake{
		fakeAgent: &fakeAgent{model: "m"},
		wakes:     make(chan (<-chan session.Event), 4),
	}
	return newTestApp(agent), agent
}

// woken is one woken turn: the stream, already carrying what it said and closed
// the way a turn that has ended closes it.
func woken(said ...session.Event) <-chan session.Event {
	ch := make(chan session.Event, len(said)+1)
	for _, ev := range said {
		ch <- ev
	}
	close(ch)
	return ch
}

// wake pushes one turn onto the lane and runs the surface until it settles.
func wake(t *testing.T, a *app, agent *wakeFake, cmd tea.Cmd, said ...session.Event) {
	t.Helper()
	agent.wakes <- woken(said...)
	drive(t, a, runCmd(cmd)...)
	drive(t, a, frameMsg{})
}

func TestAWokenTurnSpeaksIntoTheLiveFeed(t *testing.T) {
	a, agent := wakeApp(t)
	cmd := a.watchWakes()
	if cmd == nil {
		t.Fatal("a session that wakes was not subscribed to")
	}
	wake(t, a, agent, cmd, text(session.EventTextDelta, "task 7 came home and its tests pass."))

	if got := plain(frame(a)); !strings.Contains(got, "task 7 came home and its tests pass.") {
		t.Fatalf("the woken turn's reply never reached the feed:\n%s", got)
	}
	// NOBODY TYPED IT, so no user line is written: a "›" row above a turn the
	// session started would be this surface putting words in a person's mouth.
	for _, e := range a.entries {
		if e.kind == entryUser {
			t.Fatalf("a woken turn drew a user line: %q", e.text)
		}
	}
	// The stream is closed at the turn's end, and the surface goes idle on it
	// rather than sitting in stateWorking forever.
	if a.stream != nil || a.state == stateWorking {
		t.Fatalf("the surface is still pumping a turn that ended: stream=%v state=%v", a.stream != nil, a.state)
	}
	// And the count above the box is untouched: a woken stream is not a message
	// somebody queued (followup.go).
	if a.followRow(60) != "" {
		t.Fatalf("a woken turn was counted as a queued message: %q", plain(a.followRow(60)))
	}
}

// TWO TURNS NEVER SPEAK AT ONCE. A wake that arrives while the surface is still
// pumping a stream waits for it, exactly as a queued message does — and starts
// when that stream closes.
func TestAWokenTurnWaitsForTheStreamBeingPumped(t *testing.T) {
	a, agent := wakeApp(t)
	cmd := a.watchWakes()
	live := make(chan session.Event, 4)
	a.stream = live
	a.state = stateWorking

	wake(t, a, agent, cmd, text(session.EventTextDelta, "and the second task landed too."))
	if got := plain(frame(a)); strings.Contains(got, "and the second task landed too.") {
		t.Fatalf("a woken turn painted over the turn being pumped:\n%s", got)
	}
	if a.stream != live {
		t.Fatal("the woken turn took the stream from the turn in flight")
	}
	if a.followRow(60) != "" {
		t.Fatalf("the waiting wake was counted above the box: %q", plain(a.followRow(60)))
	}

	// The turn ends, and the woken one is adopted on the way out — the same door
	// a queued message goes through ([app.startFollow]).
	close(live)
	drive(t, a, streamClosedMsg{gen: a.gen}, frameMsg{})
	if got := plain(frame(a)); !strings.Contains(got, "and the second task landed too.") {
		t.Fatalf("the woken turn never started after the stream closed:\n%s", got)
	}
}

// A LANE FROM AN AGENT THAT IS GONE STARTS NOTHING. /new replaces the session,
// and a wake from the old one must not open a turn in the conversation that
// replaced it — which is what the generation is for.
func TestAWokenTurnFromAReplacedAgentIsDropped(t *testing.T) {
	a, _ := wakeApp(t)
	a.wakeGen = 3
	stale := woken(text(session.EventTextDelta, "from the session before this one."))
	drive(t, a, wokenMsg{gen: 2, ch: stale})

	if len(a.follows) != 0 || a.stream != nil {
		t.Fatal("a wake from a replaced agent was adopted")
	}
	if got := plain(frame(a)); strings.Contains(got, "from the session before this one.") {
		t.Fatalf("a dead session spoke into a live one:\n%s", got)
	}
}
