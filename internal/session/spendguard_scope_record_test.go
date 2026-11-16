package session

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/crewroute"
)

// C5: scoped calls persist the same aggregate totals, seat figures and call
// certainty, and the unchanged record format restores without scope data.
func TestScopedCrewRecordRestoresAggregateSpend(t *testing.T) {
	guard := &SpendGuard{
		Day: NewSpendDay(0), TaskCap: 1, TaskAction: "task limit",
		SeatCeilings:  map[crewroute.Seat]float64{crewroute.Checker: .25},
		CeilingAction: config.CrewCheckCeilingAction,
	}
	for _, call := range []struct {
		seat  crewroute.Seat
		scope string
		cost  float64
	}{
		{crewroute.Worker, "work", .25},
		{crewroute.Planner, "plan", .125},
		{crewroute.Checker, "check-a", .0625},
		{crewroute.Checker, "check-b", .0625},
	} {
		marked := SeatCompleterFor(call.seat, call.scope, guard.Wrap("vendor/shared", &spendingCompleter{usd: call.cost}))
		if _, err := marked.CompleteWithMessages(t.Context(), nil); err != nil {
			t.Fatal(err)
		}
	}
	unknown := &scriptedCompleter{steps: []step{func(context.Context, []ai.Message) (*ai.Response, error) {
		return &ai.Response{}, nil
	}}}
	marked := SeatCompleterFor(crewroute.Checker, "check-b", guard.Wrap("vendor/unknown", unknown))
	if _, err := marked.CompleteWithMessages(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	decision := liveDecision()
	crew := &taskCrew{decision: decision, original: originals(decision), guard: guard, roleFacts: true}
	record := crew.record()
	saved := record.Guard
	if saved.TaskSpent != .5 || saved.TaskCalls != 5 || saved.TaskUnknown != 1 ||
		saved.SeatSpent[crewroute.Worker] != .25 || saved.SeatSpent[crewroute.Planner] != .125 || saved.SeatSpent[crewroute.Checker] != .125 ||
		saved.SeatCalls[crewroute.Checker] != 3 || saved.SeatUnknown[crewroute.Checker] != 1 || saved.DaySpent != .5 {
		t.Fatalf("record lost aggregate accounting: %+v", saved)
	}
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	var decoded TaskCrewRecord
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	a := &Agent{config: Config{Workspace: t.TempDir(), ProfileDir: t.TempDir()}}
	a.crewDayOnce.Do(func() { a.crewDayHeld = NewSpendDay(0) })
	restored, err := a.restoreTaskCrew(1, &decoded)
	if err != nil {
		t.Fatal(err)
	}
	back := restored.record().Guard
	if back.TaskSpent != saved.TaskSpent || back.TaskCalls != saved.TaskCalls || back.TaskUnknown != saved.TaskUnknown || back.DaySpent != saved.DaySpent ||
		!reflect.DeepEqual(back.SeatSpent, saved.SeatSpent) || !reflect.DeepEqual(back.SeatCalls, saved.SeatCalls) ||
		!reflect.DeepEqual(back.SeatUnknown, saved.SeatUnknown) || !reflect.DeepEqual(back.ModelSpent, saved.ModelSpent) {
		t.Fatalf("record changed on restore: before=%+v after=%+v", saved, back)
	}
}

// C5, C4: an older record without scopes or call counts still restores its
// task/seat totals and enforces the original unscoped checker ceiling.
func TestOlderCrewRecordWithoutScopesStillHoldsItsSeatCeiling(t *testing.T) {
	decision := liveDecision()
	record := &TaskCrewRecord{
		Version: 1, Routed: true, Decision: decision, Original: originals(decision),
		Guard: &crewGuardRecord{
			TaskCap: 1, TaskSpent: .375, TaskAction: "task limit",
			SeatCeilings:  map[crewroute.Seat]float64{crewroute.Checker: .125},
			CeilingAction: config.CrewCheckCeilingAction,
			SeatSpent:     map[crewroute.Seat]float64{crewroute.Worker: .25, crewroute.Checker: .125},
			ModelSpent:    map[string]float64{"vendor/shared": .375},
		},
	}
	a := &Agent{config: Config{Workspace: t.TempDir(), ProfileDir: t.TempDir()}}
	a.crewDayOnce.Do(func() { a.crewDayHeld = NewSpendDay(0) })
	restored, err := a.restoreTaskCrew(1, record)
	if err != nil {
		t.Fatal(err)
	}
	guard := restored.guard
	guard.DayLimit = nil
	guard.Price = func(string) (float64, float64, float64, bool) { return 0, 0, 0, false }
	if guard.tally().Total() != .375 || guard.seatTally(crewroute.Worker).Total() != .25 || guard.seatTally(crewroute.Checker).Total() != .125 {
		t.Fatalf("older totals changed: %+v", restored.record().Guard)
	}
	calls := &spendingCompleter{usd: .0625}
	check := SeatCompleter(crewroute.Checker, guard.Wrap("vendor/shared", calls))
	_, err = check.CompleteWithMessages(t.Context(), nil)
	var stopped ErrSpendStopped
	if !errors.As(err, &stopped) || stopped.TaskLimit || stopped.Action != "the check stopped at its spend ceiling of $0.12, three times its estimate, before it finished" || calls.calls != 0 {
		t.Fatalf("older checker ceiling reset: %v, calls=%d", err, calls.calls)
	}
	work := SeatCompleter(crewroute.Worker, guard.Wrap("vendor/shared", calls))
	if _, err := work.CompleteWithMessages(t.Context(), nil); err != nil || calls.calls != 1 || guard.tally().Total() != .4375 {
		t.Fatalf("older worker accounting: %v, calls=%d total=%v", err, calls.calls, guard.tally().Total())
	}
}
