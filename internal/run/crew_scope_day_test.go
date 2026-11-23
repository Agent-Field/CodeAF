package run

import (
	"errors"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/crewroute"
	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/session"
)

// C1: factory-marked checks on one day-backed model each get their own
// ceiling, even when the preceding check's actual cost overran its ceiling.
func TestCrewFactorySeparatesCheckEstimatesWithASharedDay(t *testing.T) {
	store := boundedRunStore(t, "Root work")
	if _, err := store.AddMany([]plandb.TaskSpec{
		{ID: "check-a", Title: "First check", Role: plandb.RoleCheck},
		{ID: "check-b", Title: "Second check", Role: plandb.RoleCheck},
	}); err != nil {
		t.Fatal(err)
	}
	guard := &session.SpendGuard{
		Price:         func(string) (float64, float64, float64, bool) { return 0, 1e-6, 0, true },
		Day:           session.NewSpendDay(0),
		Task:          &session.SpendTask{},
		SeatCeilings:  map[crewroute.Seat]float64{crewroute.Checker: .125},
		CeilingAction: config.CrewCheckCeilingAction,
	}
	provider := &scopedSpendCompleter{cost: .1875}
	factory := CrewFactory(store, t.TempDir(), t.TempDir(), Seats{Check: "vendor/shared"}, "", false, func(model string) session.Completer {
		return guard.Wrap(model, provider)
	})
	for _, id := range []string{"check-a", "check-b"} {
		calls := 1
		if id == "check-b" {
			provider.cost, calls = .0625, 2
		}
		for i := 0; i < calls; i++ {
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
	if provider.calls != 3 || guard.Task.Total() != .3125 || guard.Day.Total() != .3125 {
		t.Fatalf("shared accounting: calls=%d task=%v day=%v", provider.calls, guard.Task.Total(), guard.Day.Total())
	}
}
