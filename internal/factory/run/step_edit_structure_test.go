package run

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// A REMOVED OR MOVED STEP IS WHAT THE RUN READS NEXT. The run's phases after
// the last one begun are laid again from the edited steps, so a step taken out
// of a running job never runs, and a moved one runs in its new place; the
// phases already begun are kept as they were.
func TestEditStepsItemLaysThePhasesAgainAfterARemovalAndAMove(t *testing.T) {
	it := factory.Item{ID: 1, Kind: factory.KindIssue, State: factory.StateRunning, Stages: []factory.Stage{
		{Name: "plan", Kind: factory.StageChat, Ask: "plan it", On: true},
		{Name: "write", Kind: factory.StageChat, Ask: "write it", On: true},
		{Name: "pretest", Kind: factory.StageChat, Ask: "run the old tests", On: true},
		{Name: "review", Kind: factory.StageChat, Ask: "read it", On: true},
		{Name: "docs", Kind: factory.StageChat, Ask: "say it", On: true},
		{Name: "proof", Kind: factory.StageChat, Ask: "the sheet", Until: factory.UntilProven, On: true},
	}}
	it.Stream = &factory.Stream{Cur: 1, Phases: []factory.Phase{
		phase("plan", factory.PhaseDone), phase("write", factory.PhaseRunning), phase("pretest", factory.PhasePending),
		phase("review", factory.PhasePending), phase("docs", factory.PhasePending), phase("proof", factory.PhasePending),
	}}
	e := factory.RunEdit{By: factory.ByYou, Remove: []string{"pretest"}, Move: []factory.Moved{{Stage: "docs", After: "write"}}}
	lines, err := EditStepsItem(&it, e, factory.DefaultRecipe())
	if err != nil || strings.Join(lines, "\n") != "you removed pretest\nyou moved docs after write" {
		t.Fatalf("lines = %q, %v", lines, err)
	}
	var names []string
	for _, p := range it.Stream.Phases {
		names = append(names, p.Name+":"+string(p.State))
	}
	if got := strings.Join(names, " "); got != "plan:done write:running docs:pending review:pending proof:pending" {
		t.Fatalf("phases = %s", got)
	}
}
