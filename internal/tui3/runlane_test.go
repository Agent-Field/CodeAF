package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// THE STANDING RUN LANE, which is what makes an adaptive run visible at all.
//
// A run is not a node: no row, no roster, nothing on screen that knows it
// exists. Its three event kinds are the whole of the surface's knowledge of one,
// and they arrive on a subscription of the session's own — never on a turn's
// stream, because the run outlives the turn that started it. Until this lane was
// opened the events fired into nothing and the run's page could not be reached.

// runLaneAgent is a session driving adaptive runs: the standing lane, and
// nothing else. It deliberately does NOT implement the run's doors, because the
// lane is the half under test and a surface with the lane alone must still fold
// what arrives on it.
type runLaneAgent struct {
	*fakeAgent
	lane chan session.Event
}

func (r *runLaneAgent) Orchestrations() <-chan session.Event { return r.lane }

// runLane opens the lane and pumps one event through it, the way the program
// loop does.
func runLane(t *testing.T, events ...session.Event) (*runLaneAgent, *app) {
	t.Helper()
	agent := &runLaneAgent{
		fakeAgent: &fakeAgent{model: "m"},
		lane:      make(chan session.Event, len(events)+1),
	}
	a := newTestApp(agent)
	// Wide and tall on purpose: these assertions are about what reached the
	// transcript, and the default sixty-column frame would cut a run's own
	// sentence in half and make them assertions about wrapping.
	a.width, a.height = 100, 44
	cmd := a.watchRuns()
	if cmd == nil {
		t.Fatal("the surface did not open the run lane")
	}
	for _, event := range events {
		agent.lane <- event
	}
	for range events {
		drive(t, a, runCmd(cmd)...)
	}
	return agent, a
}

// A NOTE OFF THE LANE REACHES THE CONVERSATION, and the run it names becomes
// the one this surface knows about — which is what → opens and what a later
// gate is raised on.
func TestARunNoteOffTheStandingLaneLandsInTheConversation(t *testing.T) {
	_, a := runLane(t, session.Event{
		Kind: session.EventOrchestrateNote, ID: 7,
		Text: "adaptive run started on $5.00: audit the pricing code",
	})
	if a.orchLive != "7" {
		t.Fatalf("the surface knows about run %q, want 7", a.orchLive)
	}
	body := plain(frame(a))
	if !strings.Contains(body, "audit the pricing code") {
		t.Fatalf("the note never reached the transcript:\n%s", body)
	}
	if !strings.Contains(body, orchNotePrefix) {
		t.Fatalf("the note does not say which lane it came off:\n%s", body)
	}
}

// THE GAUGE TOO, on the same lane and with the same effect: a person who has
// walked away from the page still hears that the tank is getting low.
func TestTheFuelWarningOffTheStandingLaneLandsInTheConversation(t *testing.T) {
	_, a := runLane(t, session.Event{
		Kind: session.EventOrchestrateFuel, ID: 7, Text: "$4.00 of $5.00", Hint: "$5.00",
	})
	if a.orchLive != "7" {
		t.Fatalf("the surface knows about run %q, want 7", a.orchLive)
	}
	if body := plain(frame(a)); !strings.Contains(body, "$4.00 of $5.00") {
		t.Fatalf("the gauge never reached the transcript:\n%s", body)
	}
}

// THE LANE KEEPS PUMPING. One event must re-arm the read, or a run would be
// heard from exactly once and then go quiet for the rest of its life.
func TestTheRunLaneRearmsItself(t *testing.T) {
	agent := &runLaneAgent{
		fakeAgent: &fakeAgent{model: "m"},
		lane:      make(chan session.Event, 2),
	}
	a := newTestApp(agent)
	a.width, a.height = 100, 44
	// BOTH EVENTS ARE WAITING BEFORE THE FIRST READ, and the pump is turned
	// exactly once: everything after that is the surface re-arming itself, which
	// is the whole of what this test is about.
	agent.lane <- session.Event{Kind: session.EventOrchestrateNote, ID: 7, Text: "one node to start"}
	agent.lane <- session.Event{Kind: session.EventOrchestrateNote, ID: 7, Text: "the second thing it said"}
	drive(t, a, runCmd(a.watchRuns())...)

	body := plain(frame(a))
	for _, said := range []string{"one node to start", "the second thing it said"} {
		if !strings.Contains(body, said) {
			t.Fatalf("the lane stopped before %q:\n%s", said, body)
		}
	}
}

// A LANE FROM A CONVERSATION THAT WAS REPLACED PAINTS INTO NOTHING, which is the
// generation's whole job: /new hands over a new agent and a late event from the
// old one must not land in the new conversation.
func TestARunEventFromAReplacedConversationIsDropped(t *testing.T) {
	_, a := runLane(t)
	stale := orchEventMsg{gen: a.orchGen - 1, ev: session.Event{
		Kind: session.EventOrchestrateNote, ID: 9, Text: "a run from the conversation before",
	}}
	drive(t, a, stale)
	if a.orchLive != "" {
		t.Fatalf("a stale lane named run %q as this session's", a.orchLive)
	}
	if body := plain(frame(a)); strings.Contains(body, "a run from the conversation before") {
		t.Fatalf("a stale event painted:\n%s", body)
	}
}

// A SURFACE OVER A SESSION THAT KNOWS NOTHING OF RUNS OPENS NO LANE and pays
// nothing for the feature — the same posture the design lane keeps.
func TestASessionWithNoRunsOpensNoLane(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	if cmd := a.watchRuns(); cmd != nil {
		t.Fatal("a session with no adaptive runs opened a lane anyway")
	}
}
