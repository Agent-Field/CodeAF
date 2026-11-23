package main

import (
	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"strings"
	"testing"
)

func TestAppFactoryLabelEditHello(t *testing.T) {
	wire, _ := attachStep(t, startStepFactory(t))
	for _, word := range []string{"factory.job_step_edit_label", "factory.pipeline_edit_label"} {
		if !strings.Contains(wire.hello, `"`+word+`"`) {
			t.Errorf("hello lacks %s", word)
		}
	}
}

func TestAppFactoryPipelineLabelEdit(t *testing.T) {
	stub := newEditStub(t, api.Me{Mode: api.FactoryTeam})
	wire := newEditWire(t, stub, false, api.FactoryTeam, prToken)
	var out appwire.FactoryPipelinePreview
	wire.must("factory.pipeline_preview", map[string]any{
		"factory": wire.id, "repo": prRepo, "base_sha": prBaseSHA, "base_text": editTestFile,
		"edits": []any{map[string]any{"op": "set_step", "name": "write", "label": " Security review "}},
	}, &out)
	if !out.Changed || !strings.Contains(out.Text, "label: Security review") || len(out.Steps) != 1 || strings.Join(out.Steps[0].Fields, ",") != "label" {
		t.Fatalf("label preview lost change: %+v", out)
	}
}

func TestAppFactoryJobLabelEdit(t *testing.T) {
	f := startStepFactory(t)
	f.reply = `{"factory":{"name":"acme"},"steps":[{"name":"write","label":"Security review"}],"version":5,"lines":[]}`
	wire, id := attachStep(t, f)
	var out appwire.FactoryJobStepEdited
	wire.must("factory.job_step_edit", map[string]any{
		"factory": id, "job": 7, "expect": map[string]any{"version": 4},
		"edit": map[string]any{"label": map[string]string{"write": " Security review "}},
	}, &out)
	if len(f.bodies) != 1 || !strings.Contains(f.bodies[0], `"label":{"write":"Security review"}`) {
		t.Fatalf("job label lost: %v", f.bodies)
	}
}
