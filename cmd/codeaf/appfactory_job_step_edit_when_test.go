package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

func TestAppFactoryJobStepEditAddedCondition(t *testing.T) {
	for _, kind := range []string{"chat", "check", "gate", ""} {
		t.Run("carry_"+kind, func(t *testing.T) {
			f := startStepFactory(t)
			f.reply = `{"factory":{"name":"acme"},"steps":[{"name":"inspect","when":"touches auth"}],"version":5,"lines":[]}`
			wire, id := attachStep(t, f)
			var out appwire.FactoryJobStepEdited
			wire.must("factory.job_step_edit", map[string]any{
				"factory": id, "job": 7, "expect": map[string]any{"version": 4},
				"edit": map[string]any{"add": []any{map[string]any{"step": map[string]any{"name": "inspect", "kind": kind, "ask": "inspect it", "when": " Touches Auth "}}}},
			}, &out)
			var body api.StepEditsRequest
			if err := json.Unmarshal([]byte(f.bodies[0]), &body); err != nil {
				t.Fatal(err)
			}
			if len(body.Add) != 1 || body.Add[0].Step.When != "touches auth" || len(out.Steps) != 1 || out.Steps[0].When != "touches auth" {
				t.Fatalf("condition not carried: body=%s out=%+v", f.bodies[0], out)
			}
		})
	}
	t.Run("hello", func(t *testing.T) {
		wire, _ := attachStep(t, startStepFactory(t))
		if !strings.Contains(wire.hello, `"factory.step_skipped"`) {
			t.Fatalf("hello lacks factory.step_skipped: %s", wire.hello)
		}
		if !strings.Contains(wire.hello, `"factory.job_step_edit_when"`) {
			t.Fatalf("hello lacks factory.job_step_edit_when: %s", wire.hello)
		}
	})
	t.Run("banana", func(t *testing.T) {
		f := startStepFactory(t)
		wire, id := attachStep(t, f)
		err := wire.mustFail("factory.job_step_edit", map[string]any{
			"factory": id, "job": 7, "expect": map[string]any{"version": 4},
			"edit": map[string]any{"add": []any{map[string]any{"step": map[string]any{"name": "inspect", "ask": "inspect it", "when": "banana"}}}},
		}, appwire.CodeBadParams)
		if !strings.Contains(err, "when is one of") {
			t.Fatalf("unclear condition refusal: %s", err)
		}
		if _, posts := f.counts(); posts != 0 {
			t.Fatal("invalid condition reached factory")
		}
	})
}
