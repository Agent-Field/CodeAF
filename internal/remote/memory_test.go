package remote

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

// brainAgent is an engine whose conversation has a brain: the four doors
// *session.Agent has, over a list of its own.
type brainAgent struct {
	*fakeAgent
	on   bool
	kept []session.MemoryLine
}

func (a *brainAgent) Remembers() bool { return a.on }

func (a *brainAgent) Remember(text string) (string, error) {
	a.kept = append(a.kept, session.MemoryLine{ID: "m1", Title: "kept: " + text, Text: text})
	return "kept: " + text, nil
}

func (a *brainAgent) Forget(query string) (string, error) {
	if len(a.kept) == 0 {
		return "", nil
	}
	title := a.kept[0].Title
	a.kept = nil
	return title, nil
}

func (a *brainAgent) Memories(string) ([]session.MemoryLine, error) { return a.kept, nil }

// memoryClient is the surface's slice of the agent, spelled as internal/tui3's
// memoryAgent spells it, so a door dropped from *remote.Agent fails here.
type memoryClient interface {
	Remembers() bool
	Remember(string) (string, error)
	Forget(string) (string, error)
	Memories(string) ([]session.MemoryLine, error)
}

func memoryLoop(t *testing.T, far *brainAgent) *Loop {
	t.Helper()
	loop, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) {
		return &Engine{Agent: far, SessionFile: "/srv/session.jsonl"}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { loop.Close() })
	return loop
}

// THE ORDINARY LAUNCH'S AGENT IS A *remote.Agent, and /remember asks it
// whether the session remembers at all. It once had no such door, so every
// launch answered "memory is off for this session" while the session behind the
// socket was writing memory notes.
func TestMemoryCommandsCrossTheHostConnection(t *testing.T) {
	far := &brainAgent{fakeAgent: &fakeAgent{}, on: true}
	var agent memoryClient = memoryLoop(t, far).Client.Agent()
	if !agent.Remembers() {
		t.Fatal("the host hid the brain of a conversation that has one")
	}
	if title, err := agent.Remember("Ada likes tea"); err != nil || title != "kept: Ada likes tea" {
		t.Fatalf("Remember answered %q, %v", title, err)
	}
	lines, err := agent.Memories("")
	if err != nil || len(lines) != 1 || lines[0].Text != "Ada likes tea" {
		t.Fatalf("Memories answered %+v, %v", lines, err)
	}
	if title, err := agent.Forget("tea"); err != nil || title != "kept: Ada likes tea" || len(far.kept) != 0 {
		t.Fatalf("Forget answered %q, %v", title, err)
	}
}

// A CONVERSATION WITH NO BRAIN SAYS SO AT THE DOOR, whether its agent has the
// methods and memory is off, or has never heard of them.
func TestAnEngineWithoutABrainAdvertisesNone(t *testing.T) {
	for _, far := range []*brainAgent{{fakeAgent: &fakeAgent{}, on: false}} {
		agent := memoryLoop(t, far).Client.Agent()
		if agent.Remembers() {
			t.Fatal("an engine with memory off advertised a brain")
		}
		if _, err := agent.Remember("x"); err == nil {
			t.Fatal("Remember succeeded with no brain")
		}
	}
	loop, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) {
		return &Engine{Agent: &fakeAgent{}, SessionFile: "/srv/session.jsonl"}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer loop.Close()
	if loop.Client.Agent().Remembers() {
		t.Fatal("an engine that has no memory doors advertised a brain")
	}
}
