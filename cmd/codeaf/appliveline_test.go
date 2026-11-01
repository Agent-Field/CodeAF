package main

import (
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui3"
)

// linesSaid is every live line agent.changed has said for agent, in order.
func linesSaid(rec *appRecorder, agent string) []string {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	var out []string
	for _, data := range rec.data {
		if c, ok := data.(appwire.AgentChanged); ok && c.Agent.ID == agent && c.Agent.Line != "" {
			out = append(out, c.Agent.Line)
		}
	}
	return out
}

// A RUNNING TURN'S NEXT CALL IS SAID AS THE AGENT'S LIVE LINE: the window
// that watches the agent reads the call it is on, by the same title its tool
// row carries, without listing the agents again.
func TestAppLiveLineMovesWithTheTurnsCalls(t *testing.T) {
	hold := make(chan struct{})
	defer close(hold)
	args := `{"command":"canvas show cv_1"}`
	conv := &fakeConversation{script: []session.Event{
		{Kind: session.EventToolBegin, CallID: "c1", Tool: "bash", Args: args},
	}, hold: hold}
	backend, rec, agent := workBackend(t, conv)
	if _, err := backend.Send(agent.ID, "write the opening scene", ""); err != nil {
		t.Fatal(err)
	}
	want := tui3.ToolTitle("bash", args, "")
	rec.waitFor(t, 3*time.Second, func(event string, data any) bool {
		c, ok := data.(appwire.AgentChanged)
		return ok && c.Agent.ID == agent.ID && c.Agent.State == appwire.StateWorking && c.Agent.Line == want
	})
}

// THE LINE IS SAID AT MOST ONCE A SECOND, AND THE NEWEST ALWAYS: calls in
// quick succession say the first at once and the last when the second has
// passed, never the ones between; a flush due after the turn ended says
// nothing, because the end says the agent itself.
func TestAppLiveLineIsRateLimitedAndEndsOnTheNewest(t *testing.T) {
	backend, rec, agent := workBackend(t, &fakeConversation{})
	clock := &closeClock{at: time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC)}
	backend.now, backend.after = clock.now, clock.after
	a, _ := backend.find(agent.ID)
	if _, _, err := backend.heldLive(agent.ID); err != nil {
		t.Fatal(err)
	}
	live := backend.liveOf(a)
	backend.locked(func() { live.running = true })

	backend.stepMoved(a, live, "Read premise.md")
	if got := linesSaid(rec, agent.ID); len(got) != 1 || got[0] != "Read premise.md" {
		t.Fatalf("the first call said %q", got)
	}
	clock.advance(t, 100*time.Millisecond)
	backend.stepMoved(a, live, "Read spine.md")
	clock.advance(t, 100*time.Millisecond)
	backend.stepMoved(a, live, "bash canvas patch n_4")
	if got := linesSaid(rec, agent.ID); len(got) != 1 {
		t.Fatalf("calls inside the second said %q", got)
	}
	clock.advance(t, 800*time.Millisecond)
	if got := linesSaid(rec, agent.ID); len(got) != 2 || got[1] != "bash canvas patch n_4" {
		t.Fatalf("after the second the lines said were %q", got)
	}

	// A call after the interval is said at once.
	clock.advance(t, 2*time.Second)
	backend.stepMoved(a, live, "Read scene.md")
	if got := linesSaid(rec, agent.ID); len(got) != 3 || got[2] != "Read scene.md" {
		t.Fatalf("a call after the interval said %q", got)
	}

	// A line waiting when the turn ends is never said.
	backend.stepMoved(a, live, "Read notes.md")
	backend.locked(func() {
		live.running = false
		a.step = ""
		backend.lineSettledLocked(a)
	})
	clock.advance(t, 2*time.Second)
	if got := linesSaid(rec, agent.ID); len(got) != 3 {
		t.Fatalf("after the turn ended the lines said were %q", got)
	}
}
