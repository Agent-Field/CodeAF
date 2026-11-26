package runs_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/runs"
)

func TestRunRepairsSimulatedCallsBeforePreparingNote(t *testing.T) {
	const definition = `run: example
budget: {per_run: 0.5}
params:
  deal: {type: ref, to: deal}
steps:
  - id: draft
    agent: CRM assistant
    prompt: Write a short follow-up message.
  - id: save
    gate: {approval: proof}
    records.create:
      type: note
      set: {body: $steps.draft.output}
`
	// The I0b report records both simulated calls followed by status prose.
	const bad = "search_deals({\"company\":\"Acme\"})\nstart_run({\"run\":\"follow-up\"})\nI found the deal and started the follow-up run."
	const clean = "Hi Ada,\n\nAre you free next week to discuss Acme's proposal?"
	for _, repaired := range []bool{true, false} {
		name := "bad output stops before approval"
		if repaired {
			name = "one repair reaches approval"
		}
		t.Run(name, func(t *testing.T) {
			calls := 0
			e := newEngine(t, definition, runs.Config{
				ResolveParams: func(_ string, _ map[string]runs.Parameter, _ map[string]any) (map[string]any, error) {
					return map[string]any{"deal": map[string]any{"name": "Acme", "contact": map[string]any{"name": "Ada"}, "owner": "Morgan"}}, nil
				},
				Agent: func(_ context.Context, call runs.AgentCall) (runs.Reply, error) {
					calls++
					if calls == 2 && repaired {
						if call.Remaining != 0.375 || !strings.Contains(call.Prompt, "previous answer") {
							return runs.Reply{}, errors.New("the repair needs feedback and the remaining run budget")
						}
						return runs.Reply{Output: clean, Cost: 0.125}, nil
					}
					for _, word := range []string{"Acme", "Ada", "Morgan"} {
						if !strings.Contains(call.ModelPrompt(), word) {
							return runs.Reply{}, errors.New("the prompt is missing the bound record context")
						}
					}
					return runs.Reply{Output: bad, Cost: 0.125}, nil
				},
			})
			run, err := e.Start("app", "example", map[string]any{"deal": "acme"})
			if err != nil {
				t.Fatal(err)
			}
			tick(t, e)
			got := state(t, e, run.ID)
			if calls != 2 || got.Cost != 0.25 {
				t.Fatalf("calls = %d, cost = %v, run error = %s", calls, got.Cost, got.Error)
			}
			if repaired {
				if got.Approval == nil || got.Approval.Args["body"] != clean {
					t.Fatalf("approval does not hold the clean artifact: %+v", got)
				}
			} else if got.State != runs.Incomplete || got.Approval != nil {
				t.Fatalf("unsafe draft reached the save step: %+v", got)
			}
		})
	}
}

func TestRunKeepsTheSummaryOfAnExplicitRenderStep(t *testing.T) {
	const definition = `run: example
steps:
  - id: document
    agent: CRM assistant
    prompt: Generate the declared document.
    tools: [render]
`
	calls := 0
	e := newEngine(t, definition, runs.Config{Agent: func(_ context.Context, call runs.AgentCall) (runs.Reply, error) {
		calls++
		if len(call.Tools) != 1 || call.Tools[0] != "render" {
			t.Fatalf("render tool declaration is missing: %+v", call)
		}
		return runs.Reply{Output: "Here’s the draft document I saved."}, nil
	}})
	run := start(t, e)
	tick(t, e)
	if got := state(t, e, run.ID); calls != 1 || got.State != runs.Done {
		t.Fatalf("render step reached %+v after %d calls", got, calls)
	}
}
