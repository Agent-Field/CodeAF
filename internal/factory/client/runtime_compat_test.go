package client_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/client"
)

func TestWorkerRuntimeLeavesLegacyPipelineBodyUnchanged(t *testing.T) {
	type legacyStep struct {
		api.StepDef
		State      string `json:"state"`
		InPipeline bool   `json:"in_pipeline"`
		InRun      bool   `json:"in_run"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Pipeline []legacyStep `json:"pipeline"`
			Steps    []struct {
				Name string `json:"name"`
			} `json:"steps"`
			Cost float64 `json:"cost"`
		}
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&body); err != nil {
			t.Error("new runtime broke strict pipeline body:", err)
			w.WriteHeader(422)
			return
		}
		if len(body.Pipeline) != 1 || body.Pipeline[0].Name != "test" || body.Cost != .25 {
			t.Errorf("pipeline/cost lost: %+v", body)
		}
		var runtime map[string]api.StepRuntime
		if err := json.Unmarshal([]byte(r.Header.Get(api.StepRuntimeHeader)), &runtime); err != nil || runtime["test"].RoundLimit != 6 || runtime["test"].Verification == nil {
			t.Errorf("runtime header lost: %+v, %v", runtime, err)
		}
		w.WriteHeader(200)
	}))
	defer server.Close()
	verification := &factory.StepVerification{State: "running"}
	report := api.WorkerReport{
		Cost: .25,
		Pipeline: []api.Step{{StepDef: api.StepDef{Name: "test"}, State: "running", InPipeline: true, InRun: true,
			RoundLimit: 6, Verification: verification}},
		Steps: []api.StepReport{{Name: "test", RoundLimit: 6, Verification: verification}},
	}
	if err := client.Open(server.URL, "").Worker("credential").Report(context.Background(), "lease", report); err != nil {
		t.Fatal(err)
	}
	if report.Pipeline[0].RoundLimit != 6 || report.Pipeline[0].Verification != verification {
		t.Fatal("preparation mutated caller's pipeline")
	}
}
