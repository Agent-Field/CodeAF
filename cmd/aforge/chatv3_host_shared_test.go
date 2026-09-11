package main

import (
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/remote"
	"github.com/Agent-Field/aforge-v2/internal/session"
)

// ── ONE CONNECTION, ONE CONVERSATION ────────────────────────────────────────
//
// These drive the REAL server over the loopback pipe rather than a double,
// because the claim under test is a claim about the protocol: that this door's
// Resume SELECTS a conversation on a handle the surface is already holding,
// rather than building a second one beside it. A fake could be written either
// way; the engine cannot.

// swapEngine is a far machine with several conversations on its disk and one of
// them open. It is [remote.Engine]'s Open door with a record of what the engine
// did to each agent, which is the fact both tests below turn on.
type swapEngine struct {
	agents map[string]*recordingAgent
	// opened is the transcripts asked for, in order, so a test can say which
	// conversation a call was about.
	opened []string
}

func (e *swapEngine) open(file string) (remote.WrappedAgent, bool, error) {
	if e.agents == nil {
		e.agents = map[string]*recordingAgent{}
	}
	e.opened = append(e.opened, file)
	agent := &recordingAgent{}
	e.agents[file] = agent
	return agent, true, nil
}

// recordingAgent is [quietAgent] that remembers being interrupted and closed. It
// is one conversation on the far machine, so "this agent was closed" is "that
// conversation was ended".
type recordingAgent struct {
	quietAgent
	interrupts int
	closes     int
}

func (r *recordingAgent) Interrupt()                    { r.interrupts++ }
func (r *recordingAgent) InterruptFor(session.StopDoor) { r.interrupts++ }
func (r *recordingAgent) Close() error                  { r.closes++; return nil }

// swapClient is a real client against a real engine holding conversation A.
func swapClient(t *testing.T, engine *swapEngine) (*remote.Client, *recordingAgent) {
	t.Helper()
	first := &recordingAgent{}
	loop, err := remote.Loopback(
		remote.Hello{Version: remote.Version, Workspace: "/srv/app"},
		remote.Options{Boot: func(remote.Hello) (*remote.Engine, error) {
			return &remote.Engine{Agent: first, Workspace: "/srv/app",
				SessionFile: "/srv/app/a.jsonl", Open: engine.open}, nil
		}},
	)
	if err != nil {
		t.Fatalf("dial the loopback: %v", err)
	}
	t.Cleanup(func() { _ = loop.Close() })
	return loop.Client, first
}

// The door says what it is, and what it is is the reason the surface must not
// keep the handle it gets back.
func TestTheHostedDoorSaysItsResumeSelectsRatherThanAdds(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	client, _ := swapClient(t, &swapEngine{})
	options := hostOptions(onePipeFleet("devbox", client), client.Welcome(), false)
	if !options.SharedAgent {
		t.Fatal("the hosted door did not say its conversations share one handle, so the surface will keep the same agent twice")
	}
}

// THE WHOLE OF THE DEFECT, ON THE REAL WIRE: Resume hands back the SAME handle,
// that handle now names the conversation just opened, and the engine has already
// ended the one it replaced. So a surface that treated the answer as a second
// conversation would be holding one object under two names, and a surface that
// closed "the one it was leaving" would be closing the one it just opened.
func TestResumingOverAConnectionSelectsTheSameHandleAndEndsTheOneItLeft(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	engine := &swapEngine{}
	client, first := swapClient(t, engine)
	options := hostOptions(onePipeFleet("devbox", client), client.Welcome(), false)

	next, err := options.Resume("/srv/app/b.jsonl")
	if err != nil {
		t.Fatalf("resume over the connection: %v", err)
	}
	// ONE HANDLE. This is what the surface's keeper would have held a second
	// entry for, keyed by the conversation being left.
	if next != options.Agent {
		t.Fatal("the hosted resume handed back a second agent, which this protocol has no way to give")
	}
	if len(engine.opened) != 1 || engine.opened[0] != "/srv/app/b.jsonl" {
		t.Fatalf("the engine was asked for %v", engine.opened)
	}
	// THE ENGINE ENDED THE CONVERSATION IT REPLACED, which is why there is
	// nothing left for the surface to close (internal/remote's Session.swap).
	if first.closes != 1 || first.interrupts != 1 {
		t.Fatalf("the engine left the previous conversation running: %d interrupts, %d closes",
			first.interrupts, first.closes)
	}
	// AND A CLOSE MADE NOW LANDS ON THE CONVERSATION JUST OPENED. This is the
	// assertion the surface's guard is written from: [app.openSession] opens
	// before it closes, so on this door the thing it would close is B.
	second := engine.agents["/srv/app/b.jsonl"]
	if second == nil {
		t.Fatal("the engine never built the conversation that was asked for")
	}
	if err := next.Close(); err != nil {
		t.Fatalf("close over the connection: %v", err)
	}
	if second.closes != 1 {
		t.Fatalf("the close landed somewhere else: the opened conversation was closed %d times", second.closes)
	}
}
