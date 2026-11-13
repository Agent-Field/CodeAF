package run

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/crewroute"
	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/session"
)

type scopedSpendCompleter struct {
	cost  float64
	calls int
}

func (c *scopedSpendCompleter) CompleteWithMessages(context.Context, []ai.Message, ...ai.Option) (*ai.Response, error) {
	c.calls++
	cost := c.cost
	return &ai.Response{Usage: &ai.Usage{Cost: &cost}}, nil
}

// C1, C2: the real factory separates two checks on a shared model and keeps
// a check's ceiling when that same task is seated again.
func TestCrewFactoryGivesEachCheckItsOwnCeiling(t *testing.T) {
	store := boundedRunStore(t, "Root work")
	if _, err := store.AddMany([]plandb.TaskSpec{
		{ID: "check-a", Title: "First check", Role: plandb.RoleCheck},
		{ID: "check-b", Title: "Second check", Role: plandb.RoleCheck},
	}); err != nil {
		t.Fatal(err)
	}
	guard := &session.SpendGuard{
		Price:         func(string) (float64, float64, float64, bool) { return 0, 1e-6, 0, true },
		SeatCeilings:  map[crewroute.Seat]float64{crewroute.Checker: .125},
		CeilingAction: config.CrewCheckCeilingAction,
	}
	provider := &scopedSpendCompleter{cost: .0625}
	factory := CrewFactory(store, t.TempDir(), t.TempDir(), Seats{Check: "vendor/shared"}, "", false, func(model string) session.Completer {
		return guard.Wrap(model, provider)
	})
	for _, id := range []string{"check-a", "check-b"} {
		for i := 0; i < 2; i++ {
			worker := factory(*store.Task(id)).(*BashWorker)
			if _, err := worker.completer.CompleteWithMessages(t.Context(), nil); err != nil {
				t.Fatalf("%s call %d: %v", id, i, err)
			}
		}
		worker := factory(*store.Task(id)).(*BashWorker)
		_, err := worker.completer.CompleteWithMessages(t.Context(), nil)
		var stopped session.ErrSpendStopped
		if !errors.As(err, &stopped) || stopped.TaskLimit || stopped.Action != "the check stopped at its spend ceiling of $0.12, three times its estimate, before it finished" {
			t.Fatalf("%s lost its ceiling: %v", id, err)
		}
	}
	if provider.calls != 4 {
		t.Fatalf("provider calls=%d, want 4", provider.calls)
	}
}

// C3: factory-marked work and separate checks reach one task cap. A real
// guard refusal reaches the supervisor as TaskLimit and ends on LimitCost.
func TestScopedCheckSpendEndsTheWholeRunAtTheTaskCap(t *testing.T) {
	store := boundedRunStore(t, "Root work")
	if _, err := store.AddMany([]plandb.TaskSpec{
		{ID: "work", Title: "Work", Priority: 3},
		{ID: "check-a", Title: "First check", Role: plandb.RoleCheck, Priority: 2},
		{ID: "check-b", Title: "Second check", Role: plandb.RoleCheck, Priority: 1},
	}); err != nil {
		t.Fatal(err)
	}
	guard := &session.SpendGuard{
		Price: func(string) (float64, float64, float64, bool) { return 0, 1e-6, 0, true },
		Day:   session.NewSpendDay(0), Cap: .75, CapAction: "day limit",
		TaskCap: .5, TaskAction: "task limit",
		SeatCeilings:  map[crewroute.Seat]float64{crewroute.Checker: .25},
		CeilingAction: config.CrewCheckCeilingAction,
	}
	provider := &scopedSpendCompleter{}
	crew := CrewFactory(store, t.TempDir(), t.TempDir(), Seats{Work: "vendor/work", Plan: "vendor/plan", Check: "vendor/check"}, "", false, func(model string) session.Completer {
		return guard.Wrap(model, provider)
	})
	taskLimit := false
	factory := func(task plandb.Task) Worker {
		worker := crew(task).(*BashWorker)
		return workerFunc(func(ctx context.Context, task plandb.Task) (Report, error) {
			if task.ID == store.RootID() {
				return Report{Result: "coordinated"}, nil
			}
			provider.cost = .125
			if task.ID == "work" {
				provider.cost = .25
			}
			_, err := worker.completer.CompleteWithMessages(ctx, nil)
			if err != nil {
				return Report{}, err
			}
			report := Report{Result: "finished", Steps: 1, USD: provider.cost}
			if task.ID == "check-b" {
				_, err = worker.completer.CompleteWithMessages(ctx, nil)
				var stopped session.ErrSpendStopped
				taskLimit = errors.As(err, &stopped) && stopped.TaskLimit
			}
			return report, err
		})
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	outcome, summary := Start(ctx, Spec{Store: store, Workspace: t.TempDir(), Slots: 1, Factory: factory, Limits: Limits{CostUSD: .5}})
	if outcome != OutcomeLimit || summary.Limit != LimitCost || summary.USD != .5 || !taskLimit || provider.calls != 3 || guard.Day.Total() != .5 || ctx.Err() != nil {
		t.Fatalf("shared task cap: outcome=%s summary=%+v taskLimit=%v calls=%d day=%v context=%v", outcome, summary, taskLimit, provider.calls, guard.Day.Total(), ctx.Err())
	}
}
