package session

import (
	"context"
	"errors"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

func TestTaskJudgeParsesYesAndCarriesMastermindEffort(t *testing.T) {
	client := &scriptedCompleter{steps: []step{func(context.Context, []ai.Message) (*ai.Response, error) {
		return textResponse(`{"parallelizable":true,"parts":["scan api","scan ui"],"why":"two independent surfaces"}`), nil
	}}}
	agent, _ := newTestAgent(t, client, func(c *Config) {
		c.RolesSource = func(key string) (string, bool) {
			if key == roles.TierKey(roles.TierMastermind) {
				return "judge/model:high", true
			}
			return "", false
		}
	})
	yes, parts, why := agent.JudgeDecomposable(t.Context(), "inspect both surfaces")
	if !yes || len(parts) != 2 || why != "two independent surfaces" {
		t.Fatalf("judge = %v %v %q", yes, parts, why)
	}
	if got := client.efforts[0]; got != provider.EffortHigh {
		t.Fatalf("effort = %q, want high", got)
	}
}

func TestTaskJudgeRepairsOnceAndFailuresAreNo(t *testing.T) {
	client := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("not json"), nil },
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse(`{"parallelizable":false}`), nil
		},
	}}
	agent, _ := newTestAgent(t, client, nil)
	yes, _, _ := agent.JudgeDecomposable(t.Context(), "one linear job")
	if yes || client.requests() != 2 {
		t.Fatalf("yes=%v requests=%d", yes, client.requests())
	}

	broken, _ := newTestAgent(t, &scriptedCompleter{steps: []step{func(context.Context, []ai.Message) (*ai.Response, error) {
		return nil, errors.New("offline")
	}}}, nil)
	if yes, _, _ := broken.JudgeDecomposable(t.Context(), "anything"); yes {
		t.Fatal("an error became a yes")
	}
}
