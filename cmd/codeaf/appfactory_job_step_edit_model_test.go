package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
)

// A STEP'S MODEL IS ONE THIS ENGINE OFFERS. The hello says
// factory.job_step_edit_model; a model models.list does not offer is refused
// before anything is sent; an offered one is sent in the list's own spelling;
// "" (the job's model) needs no list; and a factory older than the field says
// so as not supported, not as a broken request.
func TestAppFactoryJobStepEditSendsOnlyAModelThisEngineOffers(t *testing.T) {
	f := startStepFactory(t)
	f.reply = `{"factory":{"name":"acme"},"steps":[{"name":"review","use_model":"anthropic/claude-opus-5.5"}],"version":5,"lines":["you set review model anthropic/claude-opus-5.5"]}`
	wire, id := attachStep(t, f)
	if !strings.Contains(wire.hello, appwire.FeatureFactoryJobStepEditModel) {
		t.Fatalf("hello does not announce %s: %s", appwire.FeatureFactoryJobStepEditModel, wire.hello)
	}
	wire.backend.listModels = func() ([]appwire.Model, error) {
		return []appwire.Model{{ID: "anthropic/claude-opus-5.5", Provider: "openrouter"}}, nil
	}
	edit := func(model string) map[string]any {
		return map[string]any{"factory": id, "job": 7, "expect": map[string]any{"version": 4}, "edit": map[string]any{"model": map[string]string{"review": model}}}
	}
	msg := wire.mustFail("factory.job_step_edit", edit("vendor/nowhere"), appwire.CodeBadParams)
	if !strings.Contains(msg, "vendor/nowhere") || !strings.Contains(msg, "models.list") {
		t.Fatalf("said %q", msg)
	}
	if _, posts := f.counts(); posts != 0 {
		t.Fatal("a model the engine does not offer was sent")
	}
	var out appwire.FactoryJobStepEdited
	wire.must("factory.job_step_edit", edit("Anthropic/Claude-Opus-5.5"), &out)
	var body map[string]any
	if err := json.Unmarshal([]byte(f.bodies[0]), &body); err != nil {
		t.Fatal(err)
	}
	if got := body["model"].(map[string]any); len(got) != 1 || got["review"] != "anthropic/claude-opus-5.5" {
		t.Fatalf("body = %s", f.bodies[0])
	}
	if len(out.Steps) != 1 || out.Steps[0].UseModel != "anthropic/claude-opus-5.5" {
		t.Fatalf("out = %+v", out.Steps)
	}
	wire.backend.listModels = func() ([]appwire.Model, error) { return nil, nil }
	wire.must("factory.job_step_edit", edit(""), &out)

	old := startStepFactory(t)
	old.status, old.reply = 422, `{"error":"invalid JSON body: json: unknown field \"model\""}`
	oldWire, oldID := attachStep(t, old)
	oldWire.backend.listModels = wire.backend.listModels
	msg = oldWire.mustFail("factory.job_step_edit", map[string]any{"factory": oldID, "job": 7, "expect": map[string]any{"version": 4}, "edit": map[string]any{"model": map[string]string{"review": ""}}}, appwire.CodeConflict)
	if !strings.Contains(msg, "doesn't support that yet") {
		t.Fatalf("an older factory said %q", msg)
	}
}
