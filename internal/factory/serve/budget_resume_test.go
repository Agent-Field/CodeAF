package serve_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
)

func TestBudgetRaiseSignalsTheCapBeforeTheContinue(t *testing.T) {
	f := workerSetupAs(t, "sam")
	worker := f.attach(t, "sam")
	job := f.add(t, "sam")
	if err := f.st.Update(job.ID, func(it *factory.Item) error { it.Cap = 5; it.Autonomy = factory.AutonomyAuto; return nil }); err != nil {
		t.Fatal(err)
	}
	pulled := f.pull(t, worker)
	endpoint := "/v1/workers/leases/" + pulled.Lease.ID
	f.request(t, "POST", endpoint+"/report", worker.Credential, api.WorkerReport{Cost: 5, Question: &api.Question{Kind: "cap", Text: "Budget reached"}}, 200, nil)
	f.request(t, "POST", fmt.Sprintf("/v1/jobs/%d/raise_budget", job.ID), "", map[string]float64{"budget": 10}, 200, nil)
	var hb api.WorkerHeartbeat
	f.request(t, "POST", endpoint+"/heartbeat", worker.Credential, nil, 200, &hb)
	if len(hb.Signals) != 2 || hb.Signals[0].Verb != lease.SignalBudget || hb.Signals[0].Budget != 10 || hb.Signals[0].Yes || hb.Signals[1].Verb != lease.SignalAnswer || !hb.Signals[1].Yes || hb.Signals[1].Words != factory.ChoiceContinue {
		t.Fatalf("want cap-only budget then answered continue; signals=%+v", hb.Signals)
	}
}

func TestBudgetRaiseDoesNotClaimContinueWhenTheAnswerFails(t *testing.T) {
	f := newV2Jobs(t)
	job := f.create(t, factory.Item{State: factory.StateNeedsYou, Cap: 5, Autonomy: factory.AutonomyAuto, Assignee: "sam", QKind: "cap", Question: "Budget reached", Stream: &factory.Stream{Spent: 5}})
	f.seam.Answer = func(int, bool, string) error { return fmt.Errorf("answer refused") }
	f.h.Close()
	f.h = serve.New(f.seam, f.cfg)
	f.post(t, job, "raise_budget", `{"budget":10}`, 500)
	got, err := f.st.Get(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range got.Stream.Log {
		if strings.Contains(line.Text, "carrying on") {
			t.Fatalf("failed answer claimed recovery: %q", line.Text)
		}
	}
	if got.State != factory.StateNeedsYou || got.QKind != "cap" || got.Cap != 10 {
		t.Fatalf("failed answer changed the wait: %+v", got)
	}
}
