package session

import (
	"context"
	"errors"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/crewroute"
	"github.com/Agent-Field/codeaf/internal/provider"
)

// C1: each check has its own ceiling on both priced and unpriced routes;
// an actual cost above one check's ceiling cannot refuse the next check.
func TestEachCheckHasItsOwnSeatCeiling(t *testing.T) {
	for _, priced := range []bool{true, false} {
		name := "unpriced"
		if priced {
			name = "priced"
		}
		t.Run(name, func(t *testing.T) {
			guard := &SpendGuard{
				Price:         func(string) (float64, float64, float64, bool) { return 0, 1e-6, 0, priced },
				SeatCeilings:  map[crewroute.Seat]float64{crewroute.Checker: .125},
				CeilingAction: config.CrewCheckCeilingAction,
			}
			first := &spendingCompleter{usd: .1875}
			checkA := SeatCompleterFor(crewroute.Checker, "check-a", guard.Wrap("vendor/shared", first))
			if _, err := checkA.CompleteWithMessages(t.Context(), nil); err != nil {
				t.Fatal(err)
			}
			_, err := checkA.CompleteWithMessages(t.Context(), nil)
			var stopped ErrSpendStopped
			if !errors.As(err, &stopped) || stopped.TaskLimit || stopped.Action != "the check stopped at its spend ceiling of $0.12, three times its estimate, before it finished" || first.calls != 1 {
				t.Fatalf("first check past its ceiling: %v, calls=%d", err, first.calls)
			}

			second := &spendingCompleter{usd: .0625}
			checkB := SeatCompleterFor(crewroute.Checker, "check-b", guard.Wrap("vendor/shared", second))
			for i := 0; i < 2; i++ {
				if _, err := checkB.CompleteWithMessages(t.Context(), nil); err != nil {
					t.Fatalf("second check call %d: %v", i, err)
				}
			}
			_, err = checkB.CompleteWithMessages(t.Context(), nil)
			if !errors.As(err, &stopped) || stopped.TaskLimit || stopped.Action != "the check stopped at its spend ceiling of $0.12, three times its estimate, before it finished" || second.calls != 2 {
				t.Fatalf("second check at its own ceiling: %v, calls=%d", err, second.calls)
			}
			if guard.tally().Total() != .3125 || guard.seatTally(crewroute.Checker).Total() != .3125 {
				t.Fatalf("aggregate spend changed: task=%v checker=%v", guard.tally().Total(), guard.seatTally(crewroute.Checker).Total())
			}
		})
	}
}

// C2: a real turn's model fallback retains the check's scope and all its
// earlier calls still count when that check asks again.
func TestCheckCeilingSurvivesTheTurnsModelFallback(t *testing.T) {
	paidCut := func(ctx context.Context, messages []ai.Message) (*ai.Response, error) {
		_, err := blindCutStep(provider.CutSilent, "")(ctx, messages)
		return usage(usd(.0625)), err
	}
	next := chained([]string{"vendor/fallback"}, paidCut, paidCut,
		func(context.Context, []ai.Message) (*ai.Response, error) {
			response := textResponse("checked on the fallback")
			response.Usage = &ai.Usage{Cost: usd(.0625)}
			return response, nil
		},
	)
	guard := &SpendGuard{
		Price:         func(string) (float64, float64, float64, bool) { return 0, 1e-9, 0, true },
		SeatCeilings:  map[crewroute.Seat]float64{crewroute.Checker: .1875},
		CeilingAction: config.CrewCheckCeilingAction,
	}
	marked := SeatCompleterFor(crewroute.Checker, "check-a", guard.Wrap("test/model", next))
	agent, _ := newTestAgent(t, marked, nil)
	events, err := agent.Submit(t.Context(), "check the result")
	if err != nil {
		t.Fatal(err)
	}
	collected := collect(t, events)
	if failure, failed := firstOfKind(collected, EventError); failed {
		t.Fatalf("fallback failed: %v", failure.Err)
	}
	if next.requests() != 3 || next.model(2) != "vendor/fallback" {
		t.Fatalf("fallback was not called: requests=%d", next.requests())
	}
	events, err = agent.Submit(t.Context(), "check once more")
	if err != nil {
		t.Fatal(err)
	}
	failure, failed := firstOfKind(collect(t, events), EventError)
	var stopped ErrSpendStopped
	if !failed || !errors.As(failure.Err, &stopped) || stopped.TaskLimit || stopped.Action != "the check stopped at its spend ceiling of $0.19, three times its estimate, before it finished" || next.requests() != 3 {
		t.Fatalf("check forgot its fallback spend: %v, requests=%d", failure.Err, next.requests())
	}
}

// C3: work and separate checks share the task and day tallies; the task-wide
// refusal is typed even while every check remains below its own ceiling.
func TestScopedChecksStillShareTheTaskAndDayLimits(t *testing.T) {
	for _, priced := range []bool{true, false} {
		guard := &SpendGuard{
			Price: func(string) (float64, float64, float64, bool) { return 0, 1e-6, 0, priced },
			Day:   NewSpendDay(0), Cap: .75, CapAction: "day limit",
			TaskCap: .5, TaskAction: "task limit",
			SeatCeilings:  map[crewroute.Seat]float64{crewroute.Checker: .25},
			CeilingAction: config.CrewCheckCeilingAction,
		}
		worker := &spendingCompleter{usd: .25}
		work := SeatCompleterFor(crewroute.Worker, "work", guard.Wrap("vendor/work", worker))
		if _, err := work.CompleteWithMessages(t.Context(), nil); err != nil {
			t.Fatal(err)
		}
		checks := &spendingCompleter{usd: .125}
		for _, scope := range []string{"check-a", "check-b"} {
			check := SeatCompleterFor(crewroute.Checker, scope, guard.Wrap("vendor/check", checks))
			if _, err := check.CompleteWithMessages(t.Context(), nil); err != nil {
				t.Fatalf("priced=%v scope=%s: %v", priced, scope, err)
			}
		}
		check := SeatCompleterFor(crewroute.Checker, "check-c", guard.Wrap("vendor/check", checks))
		_, err := check.CompleteWithMessages(t.Context(), nil)
		var stopped ErrSpendStopped
		if !errors.As(err, &stopped) || !stopped.TaskLimit || stopped.Action != "task limit" || checks.calls != 2 {
			t.Fatalf("priced=%v: task limit lost: %v, check calls=%d", priced, err, checks.calls)
		}
		if guard.tally().Total() != .5 || guard.Day.Total() != .5 || guard.seatTally(crewroute.Checker).Total() != .25 {
			t.Fatalf("priced=%v: task=%v day=%v checker=%v", priced, guard.tally().Total(), guard.Day.Total(), guard.seatTally(crewroute.Checker).Total())
		}
	}
}

// C4: an unscoped crew retains its per-seat ceiling, while seats without
// ceilings can keep working on the same model, with or without task scopes.
func TestUnscopedCrewKeepsItsSeatCeiling(t *testing.T) {
	guard := &SpendGuard{
		Price:         func(string) (float64, float64, float64, bool) { return 0, 1e-6, 0, true },
		SeatCeilings:  map[crewroute.Seat]float64{crewroute.Checker: .125},
		CeilingAction: config.CrewCheckCeilingAction,
	}
	checks := &spendingCompleter{usd: .0625}
	check := SeatCompleter(crewroute.Checker, guard.Wrap("vendor/shared", checks))
	for i := 0; i < 2; i++ {
		if _, err := check.CompleteWithMessages(t.Context(), nil); err != nil {
			t.Fatal(err)
		}
	}
	// A fresh wrapper without a scope must keep the crew's earlier checker spend.
	check = SeatCompleter(crewroute.Checker, guard.Wrap("vendor/fallback", checks))
	_, err := check.CompleteWithMessages(t.Context(), nil)
	var stopped ErrSpendStopped
	if !errors.As(err, &stopped) || stopped.TaskLimit || checks.calls != 2 {
		t.Fatalf("unscoped ceiling changed: %v, calls=%d", err, checks.calls)
	}
	for _, seat := range []crewroute.Seat{crewroute.Worker, crewroute.Planner} {
		calls := &spendingCompleter{usd: .25}
		for _, scope := range []string{"", "work"} {
			marked := SeatCompleterFor(seat, scope, guard.Wrap("vendor/shared", calls))
			for i := 0; i < 3; i++ {
				if _, err := marked.CompleteWithMessages(t.Context(), nil); err != nil {
					t.Fatalf("%s scope=%q: %v", seat, scope, err)
				}
			}
		}
		if calls.calls != 6 {
			t.Fatalf("%s calls=%d", seat, calls.calls)
		}
	}
}

// C1, C2, C4: in-flight calls hold the ceiling for their own scope. Finishing
// another scope cannot release an unscoped crew's outstanding reservation.
func TestCheckCeilingReservationsStayWithTheirScope(t *testing.T) {
	guard := &SpendGuard{
		Price:         func(string) (float64, float64, float64, bool) { return 0, .125 / 2000, 0, true },
		SeatCeilings:  map[crewroute.Seat]float64{crewroute.Checker: .125},
		CeilingAction: config.CrewCheckCeilingAction,
	}
	calls := &spendingCompleter{}
	unscoped := SeatCompleter(crewroute.Checker, guard.Wrap("vendor/shared", calls))
	checkA := SeatCompleterFor(crewroute.Checker, "check-a", guard.Wrap("vendor/shared", calls))
	checkB := SeatCompleterFor(crewroute.Checker, "check-b", guard.Wrap("vendor/shared", calls))
	inside := &scriptedCompleter{steps: []step{func(context.Context, []ai.Message) (*ai.Response, error) {
		_, err := checkA.CompleteWithMessages(t.Context(), nil)
		var stopped ErrSpendStopped
		if !errors.As(err, &stopped) || stopped.TaskLimit || calls.calls != 0 {
			t.Fatalf("same check ignored its outstanding hold: %v, calls=%d", err, calls.calls)
		}
		if _, err := checkB.CompleteWithMessages(t.Context(), nil); err != nil {
			t.Fatalf("second check shared the first hold: %v", err)
		}
		_, err = unscoped.CompleteWithMessages(t.Context(), nil)
		if !errors.As(err, &stopped) || calls.calls != 1 {
			t.Fatalf("second check released the unscoped hold: %v, calls=%d", err, calls.calls)
		}
		return usage(usd(.125)), nil
	}}}
	outer := &scriptedCompleter{steps: []step{func(context.Context, []ai.Message) (*ai.Response, error) {
		marked := SeatCompleterFor(crewroute.Checker, "check-a", guard.Wrap("vendor/shared", inside))
		if _, err := marked.CompleteWithMessages(t.Context(), nil); err != nil {
			t.Fatal(err)
		}
		return usage(usd(.125)), nil
	}}}
	marked := SeatCompleter(crewroute.Checker, guard.Wrap("vendor/shared", outer))
	if _, err := marked.CompleteWithMessages(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	if guard.tally().Total() != .25 || guard.seatTally(crewroute.Checker).Total() != .25 || guard.tally().held != 0 || guard.seatTally(crewroute.Checker).held != 0 {
		t.Fatalf("reservations or aggregates changed: task=%+v checker=%+v", guard.tally(), guard.seatTally(crewroute.Checker))
	}
}

// C2, C3: a task/day refusal releases the check's estimate, so raising that
// shared limit admits the same check without losing its own ceiling.
func TestSharedLimitRefusalReleasesTheCheckReservation(t *testing.T) {
	for _, limit := range []string{"task", "day"} {
		t.Run(limit, func(t *testing.T) {
			guard := &SpendGuard{
				Price: func(string) (float64, float64, float64, bool) { return 0, .125 / 2000, 0, true },
				Day:   NewSpendDay(0), CapAction: "day limit", TaskAction: "task limit",
				SeatCeilings:  map[crewroute.Seat]float64{crewroute.Checker: .125},
				CeilingAction: config.CrewCheckCeilingAction,
			}
			if limit == "task" {
				guard.TaskCap = .0625
			} else {
				guard.Cap = .0625
			}
			calls := &spendingCompleter{usd: .125}
			check := SeatCompleterFor(crewroute.Checker, "check-a", guard.Wrap("vendor/shared", calls))
			_, err := check.CompleteWithMessages(t.Context(), nil)
			var stopped ErrSpendStopped
			if !errors.As(err, &stopped) || stopped.TaskLimit != (limit == "task") || stopped.Action != limit+" limit" || calls.calls != 0 {
				t.Fatalf("shared limit did not refuse the call: %v, calls=%d", err, calls.calls)
			}
			guard.TaskCap, guard.Cap = 1, 1
			if _, err := check.CompleteWithMessages(t.Context(), nil); err != nil || calls.calls != 1 {
				t.Fatalf("refusal leaked the check reservation: %v, calls=%d", err, calls.calls)
			}
			_, err = check.CompleteWithMessages(t.Context(), nil)
			if !errors.As(err, &stopped) || stopped.TaskLimit || stopped.Action != "the check stopped at its spend ceiling of $0.12, three times its estimate, before it finished" || calls.calls != 1 {
				t.Fatalf("raised shared limit bypassed the check ceiling: %v, calls=%d", err, calls.calls)
			}
			if guard.Day.held != 0 || guard.tally().held != 0 {
				t.Fatal("shared reservations were not released")
			}
		})
	}
}
