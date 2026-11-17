package api

import (
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// THE INTAKE STATUS IS THE JOB WIRE'S OWN FACTS: every field is what
// FromItem says of the same job, never worked out again.
func TestIntakeStatusIsTheJobWiresFacts(t *testing.T) {
	changed := time.Date(2026, 11, 6, 12, 30, 0, 0, time.UTC)
	it := factory.Item{
		ID: 9, Origin: "inbox:ops", SourceRef: "inbox:ops:A1", Repo: "acme/web", Kind: factory.KindIssue, Changed: changed,
		Route:       "ops to web",
		Stages:      []factory.Stage{{Name: "plan", On: true}, {Name: "write", On: true}},
		Stream:      &factory.Stream{Cur: 1, Phases: []factory.Phase{{Name: "plan", State: factory.PhaseDone}, {Name: "write", State: factory.PhaseRunning}}},
		PullRequest: &factory.PullRequest{URL: "https://github.com/acme/web/pull/4", State: factory.PROpen},
	}
	j := FromItem(it)
	s := IntakeStatusOf(it)
	if s.Job != 9 || s.State != j.State || s.StepsDone != 1 || s.StepsTotal != 2 || s.Route != "ops to web" ||
		s.PRURL != "https://github.com/acme/web/pull/4" || !s.Updated.Equal(changed) {
		t.Fatalf("status %+v, job state %q", s, j.State)
	}
	waiting := it
	waiting.PullRequest = nil
	waiting.State, waiting.Question = factory.StateNeedsYou, "Which database should it use?"
	s, j = IntakeStatusOf(waiting), FromItem(waiting)
	want := ""
	if n := j.NeedsYou; n != nil && n.Question != nil {
		want = n.Question.Text
	}
	if want == "" || s.State != j.State || s.Question != want || s.PRURL != "" {
		t.Fatalf("waiting status %+v, wire state %q question %q", s, j.State, want)
	}
}
