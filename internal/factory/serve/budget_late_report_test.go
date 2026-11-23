package serve_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// The old question was frozen at $5, before the person raised the cap to $10.
func TestLateCapReportUsesPostReportLifetimeCost(t *testing.T) {
	for _, tc := range []struct {
		name        string
		cost, total float64
		opens       bool
	}{
		{"equal", 4.5, 10, true},
		{"own_cost_crosses", 5, 10.5, true},
		{"own_cost_still_below", .25, 5.75, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := workerSetupAs(t, "sam")
			worker := f.attach(t, "sam")
			job := f.add(t, "sam")
			if err := f.st.Update(job.ID, func(it *factory.Item) error {
				it.Cap, it.SpentBefore, it.Autonomy = 5, .5, factory.AutonomyAuto
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			endpoint := "/v1/workers/leases/" + f.pull(t, worker).Lease.ID
			old := &api.Question{Kind: "cap", Text: "budget of $5 reached", FromStep: "proof"}
			f.request(t, "POST", endpoint+"/report", worker.Credential, api.WorkerReport{Cost: 5, Question: old}, 200, nil)
			path := fmt.Sprintf("/v1/jobs/%d", job.ID)
			f.request(t, "POST", path+"/raise_budget", "", map[string]float64{"budget": 10}, 200, nil)
			f.request(t, "POST", endpoint+"/report", worker.Credential, api.WorkerReport{Cost: tc.cost, Question: old}, 200, nil)
			var got api.Job
			f.request(t, "GET", path, "", nil, 200, &got)
			if got.Budget != 10 || got.LifetimeCost != tc.total {
				t.Fatalf("report accounting: budget=%g lifetime=%g, want 10 and %g", got.Budget, got.LifetimeCost, tc.total)
			}
			var open []api.Question
			for _, q := range got.Questions {
				if q.State == "open" {
					open = append(open, q)
				}
			}
			if !tc.opens {
				if got.State != api.StateRunning || got.SpendingLimit != nil || len(open) != 0 {
					t.Fatalf("dropped cap left job stuck: state=%s limit=%+v open=%+v", got.State, got.SpendingLimit, open)
				}
				return
			}
			if got.State != api.StateNeedsYou || got.SpendingLimit == nil || got.SpendingLimit.Limit != 10 || got.SpendingLimit.Spent != tc.total || len(open) != 1 {
				t.Fatalf("report crossing raised cap did not ask: state=%s limit=%+v open=%+v", got.State, got.SpendingLimit, open)
			}
			if open[0].FromStep != "proof" || open[0].Kind != "cap" || !strings.Contains(open[0].Text, "budget of $10 reached") || strings.Contains(open[0].Text, "budget of $5 reached") {
				t.Fatalf("question does not name current cap: %+v", open[0])
			}
		})
	}
}
