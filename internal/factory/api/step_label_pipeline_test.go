package api_test

import (
	"encoding/json"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
)

func TestStepLabelReachesPipelineEditor(t *testing.T) {
	src, _, ok := pipeline.Inspect("pipeline:\n  issue: [securityrevi]\nsteps:\n  securityrevi:\n    label: Security review\n    prompt: review security\n")
	if !ok { t.Fatal("pipeline is not editable") }
	var out api.PipelineEditSource
	out.SetParsed(src)
	raw, err := json.Marshal(out)
	if err != nil { t.Fatal(err) }
	var payload struct { Steps []struct { Name, Label string } }
	if err := json.Unmarshal(raw, &payload); err != nil { t.Fatal(err) }
	for _, step := range payload.Steps {
		if step.Name == "securityrevi" {
			if step.Label != "Security review" { t.Fatalf("pipeline editor lost label: %+v", step) }
			return
		}
	}
	t.Fatal("pipeline editor lost step")
}
