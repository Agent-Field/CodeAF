package intake_test

import (
	"context"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/route"
)

// A JOB THAT RAN IS NEVER STARTED AGAIN BY A RULE. A run that ended in a
// step's failure leaves the job new with the failure on it; a routing rule's
// start: auto matches it exactly as it matched the job the first time, and
// without this law it started the whole run again two seconds later and once
// more at the next engine start. Only a person's Run again restarts it.
func TestAFailedRunIsNeverStartedAgainByARule(t *testing.T) {
	cases := map[string]func(*factory.Item){
		"failed": func(it *factory.Item) {
			it.Failure = &factory.Failure{Step: "write", Code: "step_failed", Detail: "the step said it failed"}
			it.Stream = &factory.Stream{Phases: []factory.Phase{{Name: "write", State: factory.PhaseFailed}}, Ended: time.Unix(200, 0)}
		},
		"ended without a failure": func(it *factory.Item) {
			it.Stream = &factory.Stream{Phases: []factory.Phase{{Name: "write", State: factory.PhaseDone}}, Ended: time.Unix(200, 0)}
		},
	}
	for name, ran := range cases {
		t.Run(name, func(t *testing.T) {
			r, job := routedFloor(t, []route.Rule{jiraRule(route.StartAuto)})
			if err := r.st.Update(job.ID, func(cur *factory.Item) error { ran(cur); return nil }); err != nil {
				t.Fatal(err)
			}
			for pass := 0; pass < 2; pass++ {
				if err := r.in.Once(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
			if len(r.started) != 0 {
				t.Fatalf("a job that already ran was started again: %v", r.started)
			}
			got, err := r.st.Get(job.ID)
			if err != nil {
				t.Fatal(err)
			}
			if name == "failed" && factory.JobState(got) != "needs_you" {
				t.Fatalf("a failed run reads %s, want needs_you", factory.JobState(got))
			}
		})
	}
}
