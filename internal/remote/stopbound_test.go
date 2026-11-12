package remote

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

// A STOP'S BOUND CROSSES THE WIRE, AND ONLY A PERSON'S STOP CARRIES ONE.
//
// The desktop app has no second key to press when a stopped turn will not let
// go, so its Stop sends the bound with the stop and the engine keeps it
// ([session.Agent.InterruptWithin]). These pin both ends of that: what the
// surface sends, and which door the engine answers it with.

// THE REAL AGENT IS ONE THE SERVER CAN BOUND. The server asks for the second
// stage by an optional interface, so a rename on the session's side would
// leave every hosted stop unbounded again with these tests still passing.
var _ boundedStopper = (*session.Agent)(nil)

// boundedFakeAgent is a [fakeAgent] whose stop can carry its second stage.
type boundedFakeAgent struct {
	*fakeAgent

	mu     sync.Mutex
	graces []time.Duration
}

func (a *boundedFakeAgent) InterruptWithin(grace time.Duration) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.graces = append(a.graces, grace)
}

func (a *boundedFakeAgent) bounds() []time.Duration {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]time.Duration(nil), a.graces...)
}

func TestAPersonsBoundedStopReachesTheAgentsSecondStage(t *testing.T) {
	plain := &fakeAgent{}
	bounded := &boundedFakeAgent{fakeAgent: plain}
	engine := engineOn(plain)
	engine.Agent = bounded
	sess := NewSession(engine, true)
	t.Cleanup(func() { _ = sess.Close() })
	l := dialSession(t, sess)
	l.hello(Hello{Version: Version})

	l.ok(1, MethodInterrupt, InterruptArgs{Door: string(session.StopByPerson), Within: 2500})
	if got := bounded.bounds(); len(got) != 1 || got[0] != 2500*time.Millisecond {
		t.Fatalf("the bounded stop armed %v, want one bound of 2.5s", got)
	}
	plain.mu.Lock()
	interrupts := plain.interrupts
	plain.mu.Unlock()
	if interrupts != 0 {
		t.Fatalf("a bounded stop also sent %d plain interrupts", interrupts)
	}

	// A STOP WITH NO BOUND IS THE STOP IT ALWAYS WAS, from the terminal and
	// from every surface older than the field.
	l.ok(2, MethodInterrupt, InterruptArgs{Door: string(session.StopByPerson)})
	l.ok(3, MethodInterrupt, nil)
	// AND MACHINERY IS NEVER BOUNDED: a window leaving is not a person asking
	// for the work to end, and a bound there would abandon a turn nobody stopped.
	l.ok(4, MethodInterrupt, InterruptArgs{Door: string(session.StopByLeaving), Within: 2500})
	plain.mu.Lock()
	interrupts, door := plain.interrupts, plain.stopDoor
	plain.mu.Unlock()
	if interrupts != 3 || door != session.StopByLeaving {
		t.Fatalf("the unbounded stops reached the agent %d times, last through %q", interrupts, door)
	}
	if got := bounded.bounds(); len(got) != 1 {
		t.Fatalf("an unbounded or machinery stop armed a bound: %v", got)
	}
}

// AN AGENT THAT CANNOT BE ABANDONED IS STOPPED AS IT ALWAYS WAS: the bound is
// asked of the agent, never required of it.
func TestABoundedStopToAnAgentWithoutASecondStageIsAPlainStop(t *testing.T) {
	agent := &fakeAgent{}
	sess := NewSession(engineOn(agent), true)
	t.Cleanup(func() { _ = sess.Close() })
	l := dialSession(t, sess)
	l.hello(Hello{Version: Version})

	l.ok(1, MethodInterrupt, InterruptArgs{Door: string(session.StopByPerson), Within: 2500})
	agent.mu.Lock()
	defer agent.mu.Unlock()
	if agent.interrupts != 1 || agent.stopDoor != session.StopByPerson {
		t.Fatalf("the stop reached the agent %d times through %q", agent.interrupts, agent.stopDoor)
	}
}

// AND THE SURFACE SENDS THE BOUND IN MILLISECONDS ON THE PERSON'S DOOR.
func TestTheClientsBoundedStopSendsItsBound(t *testing.T) {
	client, e := newEngine(t)
	client.Agent().InterruptWithin(10 * time.Second)

	calls := e.calls(MethodInterrupt)
	if len(calls) != 1 {
		t.Fatalf("the bounded stop travelled %d times", len(calls))
	}
	var args InterruptArgs
	if err := json.Unmarshal(calls[0].Payload, &args); err != nil {
		t.Fatalf("stop payload: %v", err)
	}
	if args.Door != string(session.StopByPerson) || args.Within != 10_000 {
		t.Fatalf("stop args = %+v", args)
	}
}
