package serve_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// P5, P6: the actual worker report and step-begin HTTP routes withdraw the
// lease, retain spending and expose the policy reason on the job read.
func TestWorkerPolicyGate(t *testing.T) {
	for _, ended := range []bool{false, true} {
		t.Run(fmt.Sprint("ended=", ended), func(t *testing.T) {
			f := workerSetupAs(t, "owner")
			t.Cleanup(f.handler.Close)
			worker := f.attach(t, "owner")
			job := f.add(t, "owner")
			if err := f.st.Update(job.ID, func(it *factory.Item) error { it.Group, it.PolicyRun, it.Cap = "g", "m2", 20; return nil }); err != nil {
				t.Fatal(err)
			}
			instant := time.Unix(0, f.clock.Load()).UTC()
			if _, err := f.st.ChangePolicy("g", func(p *factory.GroupPolicy) error {
				p.Allowances = []factory.Allowance{{ID: "m2", Jobs: []int{job.ID}, CapUSD: 5, EndsAt: instant.Add(10 * time.Second).Format(time.RFC3339)}}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			pulled := f.pull(t, worker)
			if ended {
				f.clock.Add(int64(10 * time.Second))
			}
			cost := 5.0
			if ended {
				cost = 0
			}
			endpoint := fmt.Sprintf("/v1/workers/leases/%s", pulled.Lease.ID)
			f.request(t, "POST", endpoint+"/report", worker.Credential, api.WorkerReport{Cost: cost, Steps: []api.StepReport{{Name: "proof", State: api.StepRunning, Round: 1}}}, 409, nil)
			f.request(t, "POST", endpoint+"/heartbeat", worker.Credential, struct{}{}, 409, nil)
			f.request(t, "POST", endpoint+"/done", worker.Credential, api.WorkerDone{State: api.StateBacklog}, 409, nil)
			var j api.Job
			f.request(t, "GET", fmt.Sprintf("/v1/jobs/%d", job.ID), "", nil, 200, &j)
			if j.State != "needs_you" || j.Blocked == nil || j.Blocked.By != "policy" || j.LifetimeCost != cost || j.Held {
				t.Fatalf("read = %+v", j)
			}
			it, err := f.st.Get(job.ID)
			if err != nil || it.PolicyRun != "" || it.Asking == nil || it.QKind != "cap" {
				t.Fatalf("hold = %+v, %v", it, err)
			}
		})
	}
}
