package serve_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/client"
)

func TestWorkerRuntimeHeaderCarriesChecksAndClearsThem(t *testing.T) {
	f := workerSetup(t)
	job := f.add(t, "sam")
	attached := f.attach(t, "sam")
	pulled := f.pull(t, attached)
	cli := client.Open(f.server.URL, "").Worker(attached.Credential)
	verification := &factory.StepVerification{State: "running", Checks: []factory.RunningCheck{{ID: "run-1", Purpose: "session suite", StartedAt: time.Date(2026, 10, 23, 8, 0, 0, 0, time.UTC)}}}
	for _, current := range []*factory.StepVerification{verification, nil} {
		report := api.WorkerReport{Steps: []api.StepReport{{Name: "proof", State: string(factory.PhaseRunning), Round: 1, RoundLimit: 6, Verification: current}}}
		request, err := client.PrepareWorkerReport(report, "")
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(request.Body), `"verification"`) || strings.Contains(string(request.Body), `"round_limit"`) {
			t.Fatal("new fields leaked into strict legacy JSON")
		}
		if current == nil && !strings.Contains(request.Headers.Get(api.StepRuntimeHeader), `"verification":null`) {
			t.Fatal("clear was not explicit")
		}
		if err := cli.ReportPrepared(context.Background(), pulled.Lease.ID, request); err != nil {
			t.Fatal(err)
		}
		saved, err := f.st.Get(job.ID)
		if err != nil {
			t.Fatal(err)
		}
		ph := saved.Stream.Phases[0]
		if ph.RoundLimit != 6 || (ph.Verification == nil) != (current == nil) {
			t.Fatalf("runtime lost: %+v", ph)
		}
		if current != nil && (len(ph.Verification.Checks) != 1 || ph.Verification.Checks[0].Purpose != "session suite") {
			t.Fatal("running check lost")
		}
	}
}

func TestWorkerRuntimeHeaderRejectsInvalidMetadata(t *testing.T) {
	f := workerSetup(t)
	f.add(t, "sam")
	attached := f.attach(t, "sam")
	pulled := f.pull(t, attached)
	for _, raw := range []string{`[]`, `{"missing":{"round_limit":2}}`, `{"proof":{"round_limit":-1}}`, strings.Repeat("x", (64<<10)+1)} {
		r := httptest.NewRequest("POST", fmt.Sprintf("/v1/workers/leases/%s/report", pulled.Lease.ID), strings.NewReader(`{"steps":[{"name":"proof"}]}`))
		r.Header.Set("Authorization", "Bearer "+attached.Credential)
		r.Header.Set(api.StepRuntimeHeader, raw)
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, r)
		if w.Code != 400 {
			var body any
			_ = json.Unmarshal(w.Body.Bytes(), &body)
			t.Fatalf("invalid metadata status=%d body=%v", w.Code, body)
		}
	}
}
