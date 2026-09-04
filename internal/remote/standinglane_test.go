package remote

// THE BUG THESE PIN: a conversation held over a connection was built with its
// harness designer nilled, its intake cards off and its adaptive runner
// unwired, because all three raise their news on standing subscriptions and
// only a running turn's stream crossed this wire. Every test below is the same
// shape as tasklane_test.go's, which pinned the same fault in the task rail.

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/session"
)

// laneAgent is a [fakeAgent] carrying the two subscriptions a real session
// agent has, and the two doors their questions are answered through.
type laneAgent struct {
	*fakeAgent

	mu       sync.Mutex
	designs  []chan session.Event
	opened   map[laneName]int
	answered []string
}

func newLaneAgent() *laneAgent {
	return &laneAgent{fakeAgent: &fakeAgent{}, opened: map[laneName]int{}}
}

func (a *laneAgent) WatchHarnessDesigns() (<-chan session.Event, func()) {
	return a.watch(laneDesign)
}

func (a *laneAgent) watch(name laneName) (<-chan session.Event, func()) {
	lane := make(chan session.Event, 32)
	a.mu.Lock()
	a.opened[name]++
	a.designs = append(a.designs, lane)
	a.mu.Unlock()
	var once sync.Once
	return lane, func() { once.Do(func() { a.drop(name, lane) }) }
}

func (a *laneAgent) drop(name laneName, lane chan session.Event) {
	a.mu.Lock()
	held := &a.designs
	for at, one := range *held {
		if one == lane {
			*held = append((*held)[:at], (*held)[at+1:]...)
			break
		}
	}
	a.mu.Unlock()
	close(lane)
}

// raise is one card emitted with NO TURN RUNNING, which is the shape every one
// of these questions really has.
func (a *laneAgent) raise(name laneName, event session.Event) {
	a.mu.Lock()
	lanes := append([]chan session.Event(nil), a.designs...)
	a.mu.Unlock()
	for _, lane := range lanes {
		lane <- event
	}
}

func (a *laneAgent) ResolveSubharness(id uint64, run bool, input json.RawMessage) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.answered = append(a.answered, "subharness:"+itoa(id)+":"+boolWord(run)+":"+string(input))
}

func (a *laneAgent) said() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.answered...)
}

func (a *laneAgent) subscriptions(name laneName) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.opened[name]
}

func (a *laneAgent) holding(laneName) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.designs)
}

// nextLane takes one event off a surface's lane, or fails rather than hanging
// the suite on a lane that never spoke.
func nextLane(t *testing.T, lane <-chan session.Event) session.Event {
	t.Helper()
	select {
	case event, ok := <-lane:
		if !ok {
			t.Fatal("the lane closed with nothing on it")
		}
		return event
	case <-time.After(5 * time.Second):
		t.Fatal("nothing arrived on the lane")
	}
	return session.Event{}
}

func laneLoop(t *testing.T, far WrappedAgent) *Loop {
	t.Helper()
	loop, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) {
		return &Engine{Agent: far, Workspace: "/srv/app", SessionFile: "/srv/app/j.jsonl"}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = loop.Close() })
	return loop
}

// A DESIGN CARD RAISED WITH NO TURN RUNNING REACHES A HOSTED SURFACE. Before
// version 11 the card was emitted onto a subscription nothing carried, which is
// why the engine switched the designer off rather than raising pages nobody
// would see.
func TestAHarnessCardReachesAHostedSurface(t *testing.T) {
	far := newLaneAgent()
	loop := laneLoop(t, far)

	lane, stop := loop.Client.Agent().WatchHarnessDesigns()
	t.Cleanup(stop)
	waitFor(t, "the engine opened the design lane", func() bool { return far.subscriptions(laneDesign) == 1 })

	far.raise(laneDesign, session.Event{Kind: session.EventHarnessDesign, ID: 4, Text: "a page about migrations"})
	event := nextLane(t, lane)
	if event.Kind != session.EventHarnessDesign || event.ID != 4 || event.Text != "a page about migrations" {
		t.Fatalf("the design lane carried %v/%d/%q", event.Kind, event.ID, event.Text)
	}
}

// AND THE INTAKE CARD'S ANSWER REACHES THE ENGINE. The card and the design card
// ride one subscription; their answers do not, and a lane whose answer had no
// door would be a page whose keys pressed nothing.
func TestAnIntakeCardAnsweredOverTheWireReachesTheEngine(t *testing.T) {
	far := newLaneAgent()
	loop := laneLoop(t, far)

	lane, stop := loop.Client.Agent().WatchHarnessDesigns()
	t.Cleanup(stop)
	waitFor(t, "the engine opened the design lane", func() bool { return far.subscriptions(laneDesign) == 1 })

	far.raise(laneDesign, session.Event{Kind: session.EventSubharnessProposal, ID: 9, Text: "release-notes"})
	if event := nextLane(t, lane); event.Kind != session.EventSubharnessProposal || event.ID != 9 {
		t.Fatalf("the lane carried %v/%d, want the intake card", event.Kind, event.ID)
	}

	loop.Client.Agent().ResolveSubharness(9, true, json.RawMessage(`{"since":"v2"}`))
	waitFor(t, "the answer reached the engine", func() bool {
		said := far.said()
		return len(said) == 1 && said[0] == `subharness:9:yes:{"since":"v2"}`
	})
}

// AN ENGINE WHOSE AGENT HAS NO LANE IS ABSENT, NOT BROKEN. The subscription is
// refused quietly, the surface's lane simply never speaks, and answering a card
// it never raised is an error rather than a hang.
func TestAnEngineWithNoLanesRefusesQuietly(t *testing.T) {
	loop := laneLoop(t, &fakeAgent{})

	lane, stop := loop.Client.Agent().WatchHarnessDesigns()
	t.Cleanup(stop)
	select {
	case event, ok := <-lane:
		if ok {
			t.Fatalf("a lane nothing feeds delivered %v", event.Kind)
		}
	case <-time.After(200 * time.Millisecond):
		// Nothing arriving is the whole answer: the lane is open and empty.
	}
	loop.Client.Agent().ResolveSubharness(4, true, nil)
}

// TAKING UP A SECOND CONVERSATION REPLACES A LANE AND NEVER ADDS ONE. Two pumps
// on one channel each take half the events, which is a page that silently
// misses cards.
func TestWatchingALaneTwiceReplacesIt(t *testing.T) {
	far := newLaneAgent()
	loop := laneLoop(t, far)

	first, _ := loop.Client.Agent().WatchHarnessDesigns()
	waitFor(t, "the engine opened the design lane", func() bool { return far.subscriptions(laneDesign) == 1 })
	second, stop := loop.Client.Agent().WatchHarnessDesigns()
	t.Cleanup(stop)
	waitFor(t, "the engine reopened the design lane", func() bool { return far.subscriptions(laneDesign) == 2 })

	select {
	case _, ok := <-first:
		if ok {
			t.Fatal("the replaced lane delivered an event")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the replaced lane never closed")
	}
	waitFor(t, "the far end let go of the first subscription", func() bool { return far.holding(laneDesign) == 1 })

	far.raise(laneDesign, session.Event{Kind: session.EventHarnessDesign, ID: 5})
	if event := nextLane(t, second); event.ID != 5 {
		t.Fatalf("second lane = %v", event)
	}
}

// A LANE ENDS WITH THE CONNECTION, without an error event on it: nothing on it
// is mid-sentence, and the one sentence about a link that died belongs to the
// link.
func TestALaneEndsWhenTheConnectionDoes(t *testing.T) {
	far := newLaneAgent()
	loop := laneLoop(t, far)

	lane, _ := loop.Client.Agent().WatchHarnessDesigns()
	waitFor(t, "the engine opened the design lane", func() bool { return far.subscriptions(laneDesign) == 1 })

	if err := loop.Cut(); err != nil {
		t.Fatalf("cut the link: %v", err)
	}
	select {
	case event, ok := <-lane:
		if ok {
			t.Fatalf("a dead connection's lane delivered %v", event.Kind)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the lane never ended with the connection")
	}
	// And the far end let go of its half, so no goroutine is left pumping at a
	// pipe nobody is reading.
	waitFor(t, "the engine let go of the subscription", func() bool { return far.holding(laneDesign) == 0 })
}

// THE WIRE STATES WHAT IT CARRIES, and cmd/aforge builds its hosted
// conversations on that statement. The harness lane is complete; the adaptive
// run lane is NOT claimed, and lanes.go lists exactly what is left — a session
// lane with no replay and a run page whose journal door answers a path on the
// wrong machine. A build that flipped this without doing that work would put a
// fuel gate on a screen nobody could answer.
func TestThisWireSaysWhichStandingLanesItCarries(t *testing.T) {
	carried := StandingLanes()
	if !carried.Designs || !carried.Cards {
		t.Fatalf("StandingLanes() = %+v, want the harness lane and its answer door", carried)
	}
	if carried.Runs {
		t.Fatal("this build claims the adaptive run lane; lanes.go says what is still missing from it")
	}
}

// A SESSION SWAP RE-POINTS EVERY LANE IN THE ROOM. The subscriptions belonged
// to the agent /new just closed, and a lane left open across the swap would
// draw the previous conversation's cards into the one that replaced it.
func TestASwapRepointsTheStandingLanes(t *testing.T) {
	first, second := newLaneAgent(), newLaneAgent()
	loop, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) {
		return &Engine{
			Agent:       first,
			Workspace:   "/srv/app",
			SessionFile: "/srv/app/one.jsonl",
			Fresh: func() (WrappedAgent, string, error) {
				return second, "/srv/app/two.jsonl", nil
			},
		}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = loop.Close() })

	lane, stop := loop.Client.Agent().WatchHarnessDesigns()
	t.Cleanup(stop)
	waitFor(t, "the engine opened the first conversation's design lane", func() bool {
		return first.subscriptions(laneDesign) == 1
	})

	if _, err := loop.Client.NewSession(); err != nil {
		t.Fatalf("/new over the wire: %v", err)
	}
	waitFor(t, "the lane was reopened on the conversation that replaced it", func() bool {
		return second.subscriptions(laneDesign) == 1 && first.holding(laneDesign) == 0
	})

	// The surface's own channel is unbroken across the swap — it is the window's
	// lane, not the conversation's — and what arrives on it now comes from the
	// conversation that is actually open.
	second.raise(laneDesign, session.Event{Kind: session.EventHarnessDesign, ID: 11})
	if event := nextLane(t, lane); event.ID != 11 {
		t.Fatalf("after the swap the lane carried %v", event)
	}
	first.raise(laneDesign, session.Event{Kind: session.EventHarnessDesign, ID: 99})
	select {
	case event := <-lane:
		t.Fatalf("the closed conversation's card arrived: %v", event)
	case <-time.After(200 * time.Millisecond):
	}
}

// THE CARD ARRIVES WHOLE, which is what the page behind it needs to draw: the
// intake fields, what is still missing, and the sentence saying why it was
// raised. internal/tui3 builds the form out of this payload alone — nothing on
// the surface asks the far machine a second question about it — so a field lost
// in the encoding would be a form a person could not fill in.
func TestTheIntakeCardCrossesWholeEnoughToDraw(t *testing.T) {
	far := newLaneAgent()
	loop := laneLoop(t, far)

	lane, stop := loop.Client.Agent().WatchHarnessDesigns()
	t.Cleanup(stop)
	waitFor(t, "the engine opened the design lane", func() bool { return far.subscriptions(laneDesign) == 1 })

	far.raise(laneDesign, session.Event{
		Kind: session.EventSubharnessProposal,
		ID:   21,
		Text: "release-notes",
		Subharness: &session.SubharnessCard{
			Fields: []session.SubharnessField{{
				Field:  exec.Field{Name: "since", Required: true},
				Value:  json.RawMessage(`"v2"`),
				Filled: true,
			}},
			Missing: []string{"until"},
			Why:     "the brief names a release",
		},
	})
	event := nextLane(t, lane)
	if event.Subharness == nil {
		t.Fatal("the intake card crossed with no card on it — the page would have nothing to draw")
	}
	if got := event.Subharness.Why; got != "the brief names a release" {
		t.Fatalf("card why = %q", got)
	}
	field := event.Subharness.Fields[0]
	if len(event.Subharness.Fields) != 1 || field.Field.Name != "since" ||
		string(field.Value) != `"v2"` || !field.Filled || !field.Field.Required {
		t.Fatalf("card fields = %+v", event.Subharness.Fields)
	}
	if len(event.Subharness.Missing) != 1 || event.Subharness.Missing[0] != "until" {
		t.Fatalf("card missing = %v", event.Subharness.Missing)
	}
}

// ── WHAT THE SURFACE ASSERTS, ASSERTED HERE ─────────────────────────────────
//
// internal/tui3 takes each of these lanes as ONE optional interface on whatever
// agent it is holding, and a hosted surface is only not-a-lesser-surface if the
// remote agent satisfies the same shape a *session.Agent does. The shapes are
// restated here rather than imported because that package must not import this
// one; they are copied from internal/tui3's designAgent, leavableDesigner and
// subharnessOfferAgent, and a rename there fails here as an unsatisfied
// assertion rather than as a lane that silently never opens.

type surfaceDesigner interface {
	HarnessDesigns() <-chan session.Event
	WatchHarnessDesigns() (<-chan session.Event, func())
}

type surfaceOfferAnswer interface {
	ResolveSubharness(id uint64, run bool, input json.RawMessage)
}

var (
	_ surfaceDesigner    = (*Agent)(nil)
	_ surfaceOfferAnswer = (*Agent)(nil)
)

// surfaceRunRoom is internal/tui3's orchAgent, restated. A remote agent must NOT
// satisfy it while [StandingLanes] withholds the run lane: the page asserts all
// four doors at once, so a client carrying some of them would open a run page
// whose journal pane reads a path on the wrong machine.
type surfaceRunRoom interface {
	OrchestrateNodeJournal(runID, nodeID string) (string, bool)
	SteerOrchestrate(id, text string) error
	ResolveOrchestrate(id, answer string) (string, error)
}

func TestAHostedSurfaceDoesNotHalfOfferTheRunRoom(t *testing.T) {
	if _, ok := any((*Agent)(nil)).(surfaceRunRoom); ok != StandingLanes().Runs {
		t.Fatalf("the client offers %v of the run room while the wire claims Runs=%v — lanes.go states what a whole one needs",
			ok, StandingLanes().Runs)
	}
}
