package client

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// A WORKER'S REPORT NEVER SENDS A STEP'S MODEL PICK in its reshaped pipeline:
// the pick is the person's, which the factory keeps itself, and a factory older
// than picks decodes the report strictly and would refuse the whole of it. The
// caller's report is not changed.
func TestAWorkerReportLeavesTheModelPickOutOfItsPipeline(t *testing.T) {
	report := api.WorkerReport{Pipeline: []api.Step{{StepDef: api.StepDef{Name: "review", UseModel: "vendor/picked"}, State: "pending", InPipeline: true}}}
	prepared, err := PrepareWorkerReport(report, "key")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(prepared.Body), "use_model") {
		t.Fatalf("the report sends the pick: %s", prepared.Body)
	}
	if report.Pipeline[0].UseModel != "vendor/picked" {
		t.Fatal("preparation changed the caller's report")
	}
}
