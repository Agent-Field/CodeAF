//go:build !windows

package tool

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/seniordev/engine/steploop"
)

func submitCall(t *testing.T, input map[string]any) steploop.ToolCall {
	t.Helper()
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	return steploop.ToolCall{
		ID: "call_1", Name: "submit", Input: raw, SessionID: "ses_solo", Agent: "coder",
	}
}

func TestSubmitToolIsAbsentWithoutAFreezer(t *testing.T) {
	// An embedder that runs no submission protocol must not advertise a submit
	// action at all. Advertising one and then refusing every call teaches the
	// model that the tool is broken, which is worse than not having it.
	for _, id := range New(t.TempDir()).IDs() {
		if id == "submit" {
			t.Fatal("submit advertised with no SubmitFreeze installed")
		}
	}
}

func TestSubmitFreezesTheCandidateInsideTheCall(t *testing.T) {
	// The freeze must happen during the tool call, not after the turn returns.
	// That placement is what makes "no later stage may reopen the
	// implementation" structural: by the time the model's next step runs, the
	// artifact of record already exists.
	var captured Submission
	frozen := 0
	registry := NewWithOptions(t.TempDir(), RegistryOptions{
		SubmitFreeze: func(_ context.Context, submission Submission) (string, error) {
			frozen++
			captured = submission
			return "812 B across 2 files", nil
		},
	})

	advertised := false
	for _, id := range registry.IDs() {
		if id == "submit" {
			advertised = true
		}
	}
	if !advertised {
		t.Fatal("submit not advertised despite an installed freezer")
	}

	result, err := registry.Execute(context.Background(), submitCall(t, map[string]any{
		"reason":              "parser fix implemented and green",
		"evidence":            "make test: 12 passed, exit 0",
		"checklist_satisfied": true,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if frozen != 1 {
		t.Fatalf("freezer called %d times, want exactly 1", frozen)
	}
	if captured.Reason != "parser fix implemented and green" ||
		!captured.ChecklistSatisfied || captured.SessionID != "ses_solo" {
		t.Fatalf("captured submission = %#v", captured)
	}
	if !strings.Contains(result.Output, "812 B across 2 files") {
		t.Fatalf("freeze description not echoed to the model: %q", result.Output)
	}
	if !strings.Contains(result.Output, "Stop editing") {
		t.Fatalf("accepted submit does not tell the model to stop: %q", result.Output)
	}
}

func TestRefusedSubmitIsRecoverableRatherThanFatal(t *testing.T) {
	// A submit the freezer refuses -- empty patch, dirty tree -- must come back
	// as a tool result the model can act on, not an error that kills the turn.
	// Catching it here is the entire value: the same defect found after the run
	// has ended costs the whole run.
	registry := NewWithOptions(t.TempDir(), RegistryOptions{
		SubmitFreeze: func(context.Context, Submission) (string, error) {
			return "", errors.New("the working tree is identical to the base commit")
		},
	})
	result, err := registry.Execute(context.Background(), submitCall(t, map[string]any{
		"reason": "done", "evidence": "make test exit 0", "checklist_satisfied": true,
	}))
	if err != nil {
		t.Fatalf("a refused submit must not error the turn: %v", err)
	}
	if !strings.Contains(result.Output, "identical to the base commit") {
		t.Fatalf("refusal reason lost: %q", result.Output)
	}
	if !strings.Contains(result.Output, "NOT captured") {
		t.Fatalf("refusal must say the tree was not captured: %q", result.Output)
	}
}

func TestSubmitDemandsEvidenceBeforeItReachesTheFreezer(t *testing.T) {
	// "reason" alone is a claim. The evidence field is where the pinned command
	// and its exit status go, and a submit without it is refused at validation
	// so the freezer never sees an unsupported claim.
	for name, input := range map[string]map[string]any{
		"no evidence": {"reason": "done", "checklist_satisfied": true},
		"blank evidence": {
			"reason": "done", "evidence": "   ", "checklist_satisfied": true,
		},
		"no reason": {"evidence": "make test exit 0", "checklist_satisfied": true},
	} {
		raw, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		if err := validateSubmit(raw); err == nil {
			t.Fatalf("%s: validation accepted %v", name, input)
		}
	}
	raw, err := json.Marshal(map[string]any{
		"reason": "done", "evidence": "make test exit 0", "checklist_satisfied": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := validateSubmit(raw); err != nil {
		t.Fatalf("a complete submit was rejected: %v", err)
	}
}

func TestSubmitSurvivesTheFilterAndThePerLeafClone(t *testing.T) {
	// Two ways a tool silently disappears in this registry: the visibility
	// filter drops it for the agent, or forContext's shallow clone loses the
	// field it depends on. Both would turn every submit into the "no submission
	// handler" refusal at runtime rather than at wiring time.
	registry := NewWithOptions(t.TempDir(), RegistryOptions{
		SubmitFreeze: func(context.Context, Submission) (string, error) { return "ok", nil },
	})
	filtered := registry.DefinitionsFor(FilterInput{
		ProviderID: "openrouter", ModelID: "deepseek-v4-flash",
	})
	found := false
	for _, definition := range filtered {
		if definition.Provider.Name == "submit" {
			found = true
		}
	}
	if !found {
		t.Fatal("submit was filtered away for the coder agent")
	}
	if registry.forContext(context.Background()).submitFreeze == nil {
		t.Fatal("forContext dropped the freezer")
	}
}
