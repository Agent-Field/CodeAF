package session

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

func TestInteractiveLaunchLimitsRefuseBeforeRecordingOrCalling(t *testing.T) {
	for _, tc := range []struct {
		name    string
		budget  Budget
		spent   float64
		elapsed time.Duration
	}{
		{"money", Budget{USD: 1}, 1, 0},
		{"time", Budget{Wall: time.Minute}, 0, 2 * time.Minute},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := &scriptedCompleter{}
			a, _ := newTestAgent(t, model, func(c *Config) {
				c.Interactive, c.Unattended, c.Budget = true, true, tc.budget
			})
			a.startedAt = time.Now().Add(-tc.elapsed)
			spend(a, tc.spent)
			events := collect(t, mustSubmit(t, a, "another idea"))
			if len(events) != 1 || !errors.Is(events[0].Err, ErrLaunchBudget) {
				t.Fatalf("want launch refusal, got %v", events)
			}
			if model.requests() != 0 || len(transcriptRoles(a)) != 1 {
				t.Fatal("refused input was recorded or reached the model")
			}
		})
	}
}

func TestInteractiveLimitsDoNotFreezeAnUnrelatedQuestion(t *testing.T) {
	model := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("first answer"), nil },
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("second answer"), nil },
	}}
	a, _ := newTestAgent(t, model, func(c *Config) {
		c.Interactive, c.Unattended, c.Budget = true, true, Budget{USD: 1, Wall: time.Hour}
	})
	collect(t, mustSubmit(t, a, "first question"))
	collect(t, mustSubmit(t, a, "unrelated second question"))
	if a.who().Ask() != "unrelated second question" || a.who().Acceptance() != "" || model.requests() != 2 {
		t.Fatal("interactive conversation acquired a frozen goal or extra work")
	}
}

func TestInteractiveLaunchLimitScopeAndRunCap(t *testing.T) {
	a := &Agent{config: Config{Interactive: true, Budget: Budget{USD: 1}, SpendRailUSD: 2}}
	if got := a.railCap(10); got != 1 {
		t.Fatalf("run cap = %v", got)
	}
	a.config.SpendRailUSD = 0.5
	if got := a.railCap(10); got != 0.5 {
		t.Fatalf("stricter conversation limit lost: %v", got)
	}
	a.config.SpendRailUSD = 0
	a.usage.CostUSD = 2
	for _, field := range []*bool{&a.config.InTask, &a.config.Errand, &a.config.inHand} {
		*field = true
		if err := a.railBlockLocked(); err != nil {
			t.Fatal("copied parent launch limit applied to worker", err)
		}
		*field = false
	}
	a.config.Interactive = false
	if err := a.railBlockLocked(); err != nil {
		t.Fatal("legacy headless budget moved to new admission policy", err)
	}
}
