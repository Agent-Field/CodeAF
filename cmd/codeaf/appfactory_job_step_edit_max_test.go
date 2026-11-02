package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
)

// A STEP'S TRIES TRAVEL AS max. The hello says factory.job_step_edit_max; the
// body carries exactly the tries the edit named; a factory older than the
// field says so as not supported, not as a broken request.
func TestAppFactoryJobStepEditSendsAStepsTries(t *testing.T) {
	f := startStepFactory(t)
	f.reply = `{"factory":{"name":"acme"},"steps":[{"name":"review","max":3}],"version":5,"lines":["you set review tries 3"]}`
	wire, id := attachStep(t, f)
	if !strings.Contains(wire.hello, appwire.FeatureFactoryJobStepEditMax) {
		t.Fatalf("hello does not announce %s: %s", appwire.FeatureFactoryJobStepEditMax, wire.hello)
	}
	edit := map[string]any{"factory": id, "job": 7, "expect": map[string]any{"version": 4}, "edit": map[string]any{"max": map[string]int{"review": 3}}}
	var out appwire.FactoryJobStepEdited
	wire.must("factory.job_step_edit", edit, &out)
	var body map[string]any
	if err := json.Unmarshal([]byte(f.bodies[0]), &body); err != nil {
		t.Fatal(err)
	}
	if got := body["max"].(map[string]any); len(body) != 2 || len(got) != 1 || got["review"] != float64(3) {
		t.Fatalf("body = %s", f.bodies[0])
	}
	old := startStepFactory(t)
	old.status, old.reply = 422, `{"error":"invalid JSON body: json: unknown field \"max\""}`
	oldWire, oldID := attachStep(t, old)
	edit["factory"] = oldID
	if msg := oldWire.mustFail("factory.job_step_edit", edit, appwire.CodeConflict); !strings.Contains(msg, "doesn't support that yet") {
		t.Fatalf("an older factory said %q", msg)
	}
}
