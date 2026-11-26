package runs_test

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/runs"
	"github.com/Agent-Field/codeaf/internal/apps/schema"
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

func TestFanoutSharesOneBudgetAndStagesDistinctChanges(t *testing.T) {
	const definition = `
run: example
title: Reply
budget: {per_run: 0.09}
params: {mention: {type: ref, to: mention}}
steps:
  - id: draft
    ai:
      prompt: "Draft a reply to {{params.mention.text}}"
      propose: $params.mention
      output:
        type: object
        additionalProperties: false
        required: [reply]
        properties:
          reply: {type: string}
`
	adapter := runs.Harnesses{
		runs.HarnessCodeaf: {
			Check: func(*schema.AIStep) error { return nil },
			Run: func(_ context.Context, call runs.AgentCall) (runs.Reply, error) {
				if math.Abs(call.Remaining-0.03) > 1e-9 {
					t.Fatalf("per-record budget = %f", call.Remaining)
				}
				raw, err := json.Marshal(map[string]any{"reply": call.Prompt})
				return runs.Reply{Output: string(raw), Cost: 0.004}, err
			},
		},
	}
	e := newEngine(t, definition, runs.Config{
		CheckAI: adapter.Check, Agent: adapter.Run,
		ResolveParams: func(_ string, _ map[string]runs.Parameter, params map[string]any) (map[string]any, error) {
			id := params["mention"]
			params["mention"] = map[string]any{"id": id, "type": "mention", "text": id, "_rev": int64(7)}
			return params, nil
		},
	})
	queue, err := e.StartFanout("app", "example", "mentions", nil, []string{"first", "second", "third"}, "client-1")
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := e.StartFanout("app", "example", "mentions", nil, []string{"first", "second", "third"}, "client-1")
	if err != nil || replayed != queue {
		t.Fatalf("replayed queue = %q, %v", replayed, err)
	}
	tick(t, e)
	list, err := e.List("app", "example")
	if err != nil || len(list) != 3 {
		t.Fatalf("runs = %v, %v", list, err)
	}
	for _, run := range list {
		if run.State != runs.Done || run.Queue != queue || run.Subject == nil || run.Proposal == nil ||
			run.Proposal.Revision != 7 || run.Proposal.Set["reply"] != "Draft a reply to "+run.Subject.ID {
			t.Fatalf("draft = %+v", run)
		}
	}
}
