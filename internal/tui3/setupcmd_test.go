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

	settleDoor(t, a, a.slash("/setup"))
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

	runFirst(settledCmd(t, a, a.slash("/setup now")))
	if agent.runs != 1 {
		t.Fatalf("/setup now started %d setup turns, want 1", agent.runs)
	}
}

func TestSetupWithNothingInstallableSaysSo(t *testing.T) {
	a, agent := machineApp(t, machine.Item{Bucket: machine.Present, Name: "git"})
	settleDoor(t, a, a.slash("/setup"))
	if got := lastNote(t, a); got != setupAllHere {
		t.Fatalf("said %q, want %q", got, setupAllHere)
	}
	runFirst(settledCmd(t, a, a.slash("/setup now")))
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

// settledCmd runs the plan door of /setup to its answer and hands back what the
// fold started, the way the update loop would on the doorMsg.
func settledCmd(t *testing.T, a *app, cmd tea.Cmd) tea.Cmd {
	t.Helper()
	msg, ok := cmd().(doorMsg)
	if !ok {
		t.Fatalf("the plan was not asked on the door line: %T", cmd())
	}
	return msg.fold(true)
}
