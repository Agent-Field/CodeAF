package remote

import (
	"context"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/preflight"
	"github.com/Agent-Field/codeaf/internal/session"
)

// setupFar is an engine whose conversation has a machine to prepare, and says
// what its setup turn did.
type setupFar struct {
	*fakeAgent
	report preflight.Report
	runs   int
}

func (a *setupFar) SetupPlan() (preflight.Report, bool) { return a.report, true }

func (a *setupFar) SubmitSetup(context.Context) (<-chan session.Event, error) {
	a.runs++
	events := make(chan session.Event, 1)
	events <- session.Event{Kind: session.EventTextDelta, Text: "installing jq"}
	close(events)
	return events, nil
}

func setupLoop(t *testing.T, far WrappedAgent) *Agent {
	t.Helper()
	loop, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) {
		return &Engine{Agent: far, SessionFile: "/srv/session.jsonl"}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { loop.Close() })
	return loop.Client.Agent()
}

// THE MACHINE TO PREPARE IS THE ENGINE'S. Over a connection /setup reads the
// host's lines and /setup now runs the turn there, its output streaming back
// like any turn's.
func TestSetupCrossesTheHostConnection(t *testing.T) {
	far := &setupFar{fakeAgent: &fakeAgent{}, report: preflight.Report{Items: []preflight.Item{
		{Bucket: preflight.Installable, Name: "jq"},
		{Bucket: preflight.Impossible, Name: "nvcc", Reason: "needs CUDA"},
	}}}
	agent := setupLoop(t, far)
	if !agent.SetupSupported() {
		t.Fatal("the host hid the setup doors of a conversation that has them")
	}

	plan, ok := agent.SetupPlan()
	if !ok || len(plan.Pending()) != 1 || plan.Lines()[0] != far.report.Lines()[0] || plan.Lines()[1] != far.report.Lines()[1] {
		t.Fatalf("the host's plan came back as %v (ok=%v)", plan.Lines(), ok)
	}

	events, err := agent.SubmitSetup(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	select {
	case ev := <-events:
		if ev.Text != "installing jq" {
			t.Fatalf("the setup turn streamed %+v", ev)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the setup turn's output never arrived")
	}
	if far.runs != 1 {
		t.Fatalf("the host ran %d setup turns, want 1", far.runs)
	}
}

// An engine whose conversation has no setup doors is absent, not broken.
func TestSetupIsAbsentOnAnEngineWithoutIt(t *testing.T) {
	agent := setupLoop(t, &fakeAgent{})
	if agent.SetupSupported() {
		t.Fatal("an engine with no setup doors advertised them")
	}
	if _, ok := agent.SetupPlan(); ok {
		t.Fatal("a plan came back from an engine with no machine")
	}
	if _, err := agent.SubmitSetup(context.Background()); err == nil {
		t.Fatal("a setup turn started on an engine with no setup doors")
	}
}
