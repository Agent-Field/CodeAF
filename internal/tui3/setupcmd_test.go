package tui3

import (
	tea "charm.land/bubbletea/v2"
	"context"
	"strings"
	"testing"

	machine "github.com/Agent-Field/codeaf/internal/preflight"
	"github.com/Agent-Field/codeaf/internal/session"
)

// setupAgent is a fake conversation with a machine to prepare.
type setupAgent struct {
	*fakeAgent
	report machine.Report
	runs   int
}

func (s *setupAgent) SetupPlan() (machine.Report, bool) { return s.report, true }

func (s *setupAgent) SubmitSetup(context.Context) (<-chan session.Event, error) {
	s.runs++
	events := make(chan session.Event)
	close(events)
	return events, nil
}

func machineApp(t *testing.T, items ...machine.Item) (*app, *setupAgent) {
	t.Helper()
	a, _ := sheetApp(t)
	agent := &setupAgent{fakeAgent: &fakeAgent{model: "openai/gpt-4.1-mini"}, report: machine.Report{Items: items}}
	a.agent = agent
	return a, agent
}

func TestSetupShowsTheLinesAndActsOnlyOnNow(t *testing.T) {
	a, agent := machineApp(t,
		machine.Item{Bucket: machine.Installable, Name: "jq"},
		machine.Item{Bucket: machine.Impossible, Name: "nvcc", Reason: "needs CUDA"})

	a.slash("/setup")
	shown := lastNote(t, a)
	if !strings.Contains(shown, "/setup now") || agent.runs != 0 {
		t.Fatalf("bare /setup said %q and ran %d turns; it must only show", shown, agent.runs)
	}
	var all []string
	for _, e := range a.entries {
		all = append(all, e.text)
	}
	text := strings.Join(all, "\n")
	for _, want := range []string{"needs jq", "needs CUDA"} {
		if !strings.Contains(text, want) {
			t.Errorf("the lines are missing %q:\n%s", want, text)
		}
	}

	runFirst(a.slash("/setup now"))
	if agent.runs != 1 {
		t.Fatalf("/setup now started %d setup turns, want 1", agent.runs)
	}
}

func TestSetupWithNothingInstallableSaysSo(t *testing.T) {
	a, agent := machineApp(t, machine.Item{Bucket: machine.Present, Name: "git"})
	a.slash("/setup")
	if got := lastNote(t, a); got != setupAllHere {
		t.Fatalf("said %q, want %q", got, setupAllHere)
	}
	runFirst(a.slash("/setup now"))
	if agent.runs != 0 {
		t.Fatalf("started %d turns", agent.runs)
	}
}

// runFirst runs the turn-starting command of a sent message: the first of the
// batch the send returns (the second is the paint clock).
func runFirst(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	if batch, ok := cmd().(tea.BatchMsg); ok && len(batch) > 0 {
		batch[0]()
	}
}
