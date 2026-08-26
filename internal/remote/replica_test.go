package remote

// replica_test.go pins the half of "intent up, facts down" that a person feels:
// the facts a frame draws are RIGHT without ever having been asked for.
//
// Every test here drives the real client against the real server over
// loopback.go's in-memory pipe, so the frames are the frames and the ordering is
// the ordering. What it does not reproduce is latency — see loopback.go — which
// is exactly why the laws below count round trips instead of timing them.

import (
	"context"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// stateOn is a loop over one scripted agent.
func stateOn(t *testing.T, agent *fakeAgent) *Loop {
	t.Helper()
	loop, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) {
		return &Engine{Agent: agent, Workspace: "/srv/app", SessionFile: "/srv/j.jsonl"}, nil
	}})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = loop.Close() })
	return loop
}

// name changes the conversation's name on the engine, the way the naming errand
// does before it says so on the turn's stream.
func (f *fakeAgent) name(title string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.title = title
}

// spend moves the figures a status line draws, the way a finished turn does.
func (f *fakeAgent) spend(usage session.Usage, tokens int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.usage, f.tokens = usage, tokens
}

// THE WELCOME IS THE FIRST STATEMENT, so the first frame a surface draws is
// drawn from memory.
func TestTheFirstFrameIsDrawnFromTheWelcomesOwnFacts(t *testing.T) {
	agent := &fakeAgent{model: "openai/gpt-5", title: "the roof leaks", tokens: 4212}
	agent.SetReasoningFor("openai/gpt-5", "high")
	loop := stateOn(t, agent)

	handle := loop.Client.Agent()
	if got := handle.Model(); got != "openai/gpt-5" {
		t.Fatalf("Model = %q", got)
	}
	if got := handle.Title(); got != "the roof leaks" {
		t.Fatalf("Title = %q", got)
	}
	if got := handle.ContextTokens(); got != 4212 {
		t.Fatalf("ContextTokens = %d", got)
	}
	if got := handle.ReasoningFor("openai/gpt-5"); got != "high" {
		t.Fatalf("ReasoningFor = %q", got)
	}
	if spent := loop.CallsMade(); spent != 0 {
		t.Fatalf("the welcome's own facts cost %d round trips, want 0", spent)
	}
}

// A NAME THE SESSION EARNS REACHES THE SURFACE WITHOUT BEING ASKED FOR. This is
// the whole freshness claim in one event: nothing on the surface polls, nothing
// on the surface asks, and the row still says what the conversation is called.
func TestATitleSettledOnTheEngineReachesTheReplicaWithoutARequest(t *testing.T) {
	agent := &fakeAgent{model: "a/b"}
	loop := stateOn(t, agent)
	handle := loop.Client.Agent()

	events, err := handle.Submit(context.Background(), "fix the roof")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if got := handle.Title(); got != "" {
		t.Fatalf("a conversation with no name answered %q", got)
	}
	asked := loop.CallsMade()

	stream := waitForStream(t, agent)
	agent.name("the roof leaks")
	stream <- session.Event{Kind: session.EventTitleChanged, Text: "the roof leaks"}
	// THE PUSH IS AHEAD OF THE EVENT ON THE WIRE (server.go's emit), so reading
	// the name the moment the event lands is reading the settled one.
	if ev := <-events; ev.Kind != session.EventTitleChanged {
		t.Fatalf("first event was %v", ev.Kind)
	}
	if got := handle.Title(); got != "the roof leaks" {
		t.Fatalf("the replica answered %q after the engine named itself", got)
	}
	if spent := loop.CallsMade() - asked; spent != 0 {
		t.Fatalf("learning the name cost %d round trips, want 0", spent)
	}
	agent.finish(stream)
}

// AND THE SETTLE READS THIS TURN'S FIGURES AND NOT THE TURN BEFORE IT. The
// surface reads the spending and the weight the instant EventTurnDone lands, so
// a push that arrived after that event would be one turn late for ever.
func TestATurnsSpendingIsOnTheReplicaBeforeItsOwnEndingLands(t *testing.T) {
	agent := &fakeAgent{model: "a/b"}
	loop := stateOn(t, agent)
	handle := loop.Client.Agent()

	events, err := handle.Submit(context.Background(), "go")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	stream := waitForStream(t, agent)
	agent.spend(session.Usage{Turns: 1, CostUSD: 0.25, Input: 900, Output: 100}, 8000)
	stream <- session.Event{Kind: session.EventTurnDone}
	if ev := <-events; ev.Kind != session.EventTurnDone {
		t.Fatalf("the turn ended with %v", ev.Kind)
	}
	if got := handle.Usage(); got.CostUSD != 0.25 || got.Turns != 1 {
		t.Fatalf("the settle read %+v", got)
	}
	if got := handle.ContextTokens(); got != 8000 {
		t.Fatalf("the settle read %d tokens", got)
	}
	agent.finish(stream)
}

// A LATE PUSH NEVER MOVES A LIVE ROW BACKWARDS. Two facts can move in the same
// instant on the engine and the two pushes race to the writer; the revision is
// what makes the newest one the truth whatever order they arrive in.
func TestAPushWithAnOlderRevisionIsDropped(t *testing.T) {
	var held replica
	held.fill(&FactsPush{Rev: 4, Facts: session.Facts{Model: "b/c", ContextTokens: 900}})
	held.take(mustClientJSON(FactsPush{Rev: 3, Facts: session.Facts{Model: "a/b"}}))
	if got := held.read().Model; got != "b/c" {
		t.Fatalf("an older push landed: model = %q", got)
	}
	held.take(mustClientJSON(FactsPush{Rev: 5, Facts: session.Facts{Model: "c/d"}}))
	if got := held.read().Model; got != "c/d" {
		t.Fatalf("a newer push did not land: model = %q", got)
	}
	// AND A PUSH THAT WILL NOT DECODE IS ONE LOST STATEMENT AND NOT A LOST
	// REPLICA: the next one is complete, because a push is never a delta.
	held.take([]byte("{not json"))
	if got := held.read().Model; got != "c/d" {
		t.Fatalf("an unreadable push emptied the replica: model = %q", got)
	}
}

// THE KNOB MOVES BEFORE THE ENGINE ANSWERS, and the engine's own statement lands
// over the top of it. The person pressed a key and is looking at the row it
// changed; a segment that waited for a round trip would read as a key that did
// not work.
func TestAModelSwitchShowsAtOnceAndTheEnginesOwnWordSettlesIt(t *testing.T) {
	agent := &fakeAgent{model: "a/b"}
	loop := stateOn(t, agent)
	handle := loop.Client.Agent()

	handle.SetModel("openai/gpt-5")
	if got := handle.Model(); got != "openai/gpt-5" {
		t.Fatalf("the row still said %q after the key", got)
	}
	// The engine announces the change to every surface on the conversation, so
	// the assumption is replaced by the fact within one round trip.
	waitFor(t, "the engine's own statement of the model", func() bool {
		return loop.Client.Facts().Model == "openai/gpt-5" && loop.Client.facts.rev > 1
	})

	handle.SetReasoningFor("OpenAI/GPT-5", "high")
	if got := handle.ReasoningFor("openai/gpt-5"); got != "high" {
		t.Fatalf("the level under the folded id = %q", got)
	}
	// AND OFF IS STORED AS ABSENCE, on both sides of the seam.
	handle.SetReasoningFor("openai/gpt-5", "")
	if got := handle.ReasoningFor("openai/gpt-5"); got != "" {
		t.Fatalf("a level cycled back to off answered %q", got)
	}
}

// ── the far-call laws (PERF.md) ─────────────────────────────────────────────

// A FRAME'S OWN READS ISSUE ZERO FAR CALLS. This is the law stated against the
// exact five methods internal/tui3 asks from its update loop, asked as often as
// a busy repaint asks them.
func TestTheFactsAFrameDrawsIssueZeroFarCalls(t *testing.T) {
	agent := &fakeAgent{model: "a/b", title: "roof", tokens: 4212}
	loop := stateOn(t, agent)
	handle := loop.Client.Agent()

	before := loop.CallsMade()
	for range 200 {
		_ = handle.Model()
		_ = handle.Title()
		_ = handle.Usage()
		_ = handle.ContextTokens()
		_ = handle.ReasoningFor("a/b")
	}
	if spent := loop.CallsMade() - before; spent != 0 {
		t.Fatalf("two hundred frames' worth of reads cost %d round trips, want 0", spent)
	}
}

// AND A SUBMIT ISSUES EXACTLY ONE. What goes up is the intent — the sentence —
// and nothing else: a submit that also asked what the model was, or what the
// turn had cost, would be three round trips on the keystroke a person is
// waiting on.
func TestSubmitOverAConnectionIssuesExactlyOneFarCall(t *testing.T) {
	agent := &fakeAgent{model: "a/b"}
	loop := stateOn(t, agent)
	handle := loop.Client.Agent()

	before := loop.CallsMade()
	events, err := handle.Submit(context.Background(), "fix the roof")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if spent := loop.CallsMade() - before; spent != 1 {
		t.Fatalf("a submit cost %d round trips, want exactly 1", spent)
	}
	stream := waitForStream(t, agent)
	agent.finish(stream)
	for range events {
	}
}

// waitForStream is the turn the engine opened, once it has opened one.
func waitForStream(t *testing.T, agent *fakeAgent) chan session.Event {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if stream := agent.stream(0); stream != nil {
			return stream
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("the engine never opened a turn")
	return nil
}
