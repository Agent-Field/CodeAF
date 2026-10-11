package serve_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
)

// A job nobody holds is shaped by this factory, and the line comes back.
func TestShapeRunsHereWhenNobodyHoldsTheJob(t *testing.T) {
	s := factory.FixtureSeam(now)
	calls := 0
	s.Shape = func(ctx context.Context, id int) (string, error) {
		if ctx.Err() != nil {
			t.Fatal(ctx.Err())
		}
		calls++
		if id != 8 {
			t.Fatalf("shaped %d", id)
		}
		return "manager set write: the ledger", nil
	}
	w := verbRequest(serve.New(s, config), "POST", "/v1/jobs/8/shape", "")
	checkJSON(t, w, 200)
	var body struct {
		Line string `json:"line"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || body.Line != "manager set write: the ledger" {
		t.Fatalf("calls %d line %q", calls, body.Line)
	}
}

// A job a worker holds is not shaped on this copy. The ask is left on the
// lease, and a factory with no Shape door still answers 200.
func TestShapeSignalsTheWorkerHoldingTheJob(t *testing.T) {
	f := workerSetup(t)
	job := f.add(t, "sam")
	worker := f.attach(t, "sam")
	pulled := f.pull(t, worker)
	var body struct {
		Line  string `json:"line"`
		Error string `json:"error"`
	}
	f.request(t, "POST", fmt.Sprintf("/v1/jobs/%d/shape", job.ID), "", nil, 200, &body)
	if body.Line != "" || body.Error != "" {
		t.Fatalf("held shape answered %+v", body)
	}
	var hb struct {
		Signals []api.WorkerSignal `json:"signals"`
	}
	f.request(t, "POST", "/v1/workers/leases/"+pulled.Lease.ID+"/heartbeat", worker.Credential, nil, 200, &hb)
	if len(hb.Signals) != 1 || hb.Signals[0].Verb != lease.SignalShape {
		t.Fatalf("signals %+v", hb.Signals)
	}
}
