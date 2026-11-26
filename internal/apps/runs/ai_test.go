package runs_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/runs"
)

func TestAIActionChecksOutputBeforeMakingItAvailable(t *testing.T) {
	const definition = `
run: example
budget: {per_run: 0.01}
params:
  mention: {type: text, default: "Can I import records?"}
steps:
  - id: draft
    ai:
      prompt: "Draft a reply to {{params.mention}}"
      output:
        type: object
        additionalProperties: false
        required: [reply]
        properties:
          reply: {type: string}
`
	for _, tc := range []struct {
		name, output, state string
	}{
		{"valid", `{"reply":"Yes, you can import CSV."}`, runs.Done},
		{"invalid", `{"reply":42}`, runs.Incomplete},
		{"extra", `{"reply":"Yes."} Thanks!`, runs.Incomplete},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newEngine(t, definition, runs.Config{Agent: func(_ context.Context, call runs.AgentCall) (runs.Reply, error) {
				if call.Prompt != "Draft a reply to Can I import records?" || call.Remaining != 0.01 {
					t.Fatalf("call = %+v", call)
				}
				return runs.Reply{Output: tc.output, Cost: 0.002}, nil
			}})
			r := start(t, e)
			tick(t, e)
			if got := state(t, e, r.ID); got.State != tc.state || got.Cost != 0.002 {
				t.Fatalf("run = %+v", got)
			}
		})
	}
}

func TestUnavailableAILeavesDeterministicRunsWorking(t *testing.T) {
	const definition = "run: example\nsteps:\n  - id: pause\n    wait: {for: 1h}"
	e := newEngine(t, definition, runs.Config{})
	r := start(t, e)
	tick(t, e)
	if got := state(t, e, r.ID); got.State != runs.Running || got.WakeAt.IsZero() || got.Cost != 0 {
		t.Fatalf("deterministic run = %+v", got)
	}

	const aiDefinition = `
run: example
budget: {per_run: 0.01}
steps:
  - id: draft
    ai: {prompt: Draft a reply, output: {type: string}}
`
	ai := newEngine(t, aiDefinition, runs.Config{})
	if _, err := ai.Start("app", "example", nil); !errors.Is(err, runs.ErrAIUnavailable) {
		t.Fatalf("start without a harness = %v", err)
	}
	if list, err := ai.List("app", ""); err != nil || len(list) != 0 {
		t.Fatalf("runs without a harness = %v, %v", list, err)
	}
}
