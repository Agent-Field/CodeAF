package session

import (
	"context"
	"errors"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/crewroute"
)

// C1: a shared day's last actual cost from an overrun cannot reserve a fresh
// check's ceiling, while the task and day keep their conservative estimates.
func TestEachCheckHasItsOwnCeilingWithASharedDay(t *testing.T) {
	guard := &SpendGuard{
		Price:         func(string) (float64, float64, float64, bool) { return 0, 1e-6, 0, true },
		Day:           NewSpendDay(0),
		SeatCeilings:  map[crewroute.Seat]float64{crewroute.Checker: .125},
		CeilingAction: config.CrewCheckCeilingAction,
	}
	first := &spendingCompleter{usd: .1875}
	checkA := SeatCompleterFor(crewroute.Checker, "check-a", guard.Wrap("vendor/shared", first))
	if _, err := checkA.CompleteWithMessages(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	var stopped ErrSpendStopped
	_, err := checkA.CompleteWithMessages(t.Context(), nil)
	if !errors.As(err, &stopped) || stopped.TaskLimit || stopped.Action != "the check stopped at its spend ceiling of $0.12, three times its estimate, before it finished" || first.calls != 1 {
		t.Fatalf("first check past its ceiling: %v, calls=%d", err, first.calls)
	}
	second := &spendingCompleter{usd: .0625}
	probe := &scriptedCompleter{steps: []step{
		func(ctx context.Context, messages []ai.Message) (*ai.Response, error) {
			if guard.tally().held != .1875 || guard.Day.held != .1875 || guard.ceilingTally(ctx, crewroute.Checker).held != .002 {
				t.Fatalf("first call holds: task=%v day=%v check=%v", guard.tally().held, guard.Day.held, guard.ceilingTally(ctx, crewroute.Checker).held)
			}
			return second.CompleteWithMessages(ctx, messages)
		},
		func(ctx context.Context, messages []ai.Message) (*ai.Response, error) {
			if guard.ceilingTally(ctx, crewroute.Checker).held != .0625 {
				t.Fatalf("check forgot its own last actual cost: held=%v", guard.ceilingTally(ctx, crewroute.Checker).held)
			}
			return second.CompleteWithMessages(ctx, messages)
		},
	}}
	checkB := SeatCompleterFor(crewroute.Checker, "check-b", guard.Wrap("vendor/shared", probe))
	for i := 0; i < 2; i++ {
		if _, err := checkB.CompleteWithMessages(t.Context(), nil); err != nil {
			t.Fatalf("fresh check call %d: %v", i, err)
		}
	}
	_, err = checkB.CompleteWithMessages(t.Context(), nil)
	if !errors.As(err, &stopped) || stopped.TaskLimit || stopped.Action != "the check stopped at its spend ceiling of $0.12, three times its estimate, before it finished" || second.calls != 2 {
		t.Fatalf("second check at its own ceiling: %v, calls=%d", err, second.calls)
	}
	if guard.tally().Total() != .3125 || guard.Day.Total() != .3125 || guard.seatTally(crewroute.Checker).Total() != .3125 || guard.tally().held != 0 || guard.Day.held != 0 {
		t.Fatalf("shared accounting: task=%+v day=%+v checker=%+v", guard.tally(), guard.Day, guard.seatTally(crewroute.Checker))
	}
}

// C1, C2, C3: rollback, zero-cost answers, failed calls and discarded usage
// release only their own reservations even when the shared estimates are larger.
func TestScopedCheckReleasesOnlyWhatEachTallyHeld(t *testing.T) {
	for _, ending := range []string{"answer", "error", "discarded", "task", "day"} {
		t.Run(ending, func(t *testing.T) {
			guard := &SpendGuard{
				Price:         func(string) (float64, float64, float64, bool) { return 0, .03125 / 2000, 0, true },
				Day:           NewSpendDay(0),
				TaskAction:    "task limit",
				CapAction:     "day limit",
				SeatCeilings:  map[crewroute.Seat]float64{crewroute.Checker: .125},
				CeilingAction: config.CrewCheckCeilingAction,
			}
			seed := SeatCompleterFor(crewroute.Checker, "check-a", guard.Wrap("vendor/shared", &spendingCompleter{usd: .1875}))
			if _, err := seed.CompleteWithMessages(t.Context(), nil); err != nil {
				t.Fatal(err)
			}
			failure := errors.New("scripted call failed")
			inner := &scriptedCompleter{steps: []step{func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
				if ending == "discarded" {
					guard.after(ctx, "vendor/shared", usage(usd(.0625)), 0)
					if guard.ceilingTally(ctx, crewroute.Checker).held != .0625 || guard.tally().held != .375 || guard.Day.held != .375 {
						t.Fatal("discarded usage released an in-flight reservation")
					}
				}
				if ending == "error" {
					return nil, failure
				}
				return usage(usd(0)), nil
			}}}
			check := SeatCompleterFor(crewroute.Checker, "check-b", guard.Wrap("vendor/shared", inner))
			outer := &scriptedCompleter{steps: []step{func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
				if ending == "task" {
					guard.TaskCap = .5
				} else if ending == "day" {
					guard.Cap = .5
				}
				_, err := check.CompleteWithMessages(ctx, nil)
				if ending == "task" || ending == "day" {
					var stopped ErrSpendStopped
					if !errors.As(err, &stopped) || stopped.TaskLimit != (ending == "task") || stopped.Action != ending+" limit" || inner.requests() != 0 {
						t.Fatalf("shared limit refusal: %v, calls=%d", err, inner.requests())
					}
				} else if ending == "error" {
					if !errors.Is(err, failure) {
						t.Fatalf("provider failure changed: %v", err)
					}
				} else if err != nil {
					t.Fatal(err)
				}
				if guard.ceilingTally(ctx, crewroute.Checker).held != .03125 || guard.tally().held != .1875 || guard.Day.held != .1875 {
					t.Fatalf("inner call released the outer hold: check=%v task=%v day=%v", guard.ceilingTally(ctx, crewroute.Checker).held, guard.tally().held, guard.Day.held)
				}
				return usage(usd(0)), nil
			}}}
			marked := SeatCompleterFor(crewroute.Checker, "check-b", guard.Wrap("vendor/shared", outer))
			if _, err := marked.CompleteWithMessages(t.Context(), nil); err != nil {
				t.Fatal(err)
			}
			if guard.tally().held != 0 || guard.Day.held != 0 || guard.scopedSpent[seatScope{seat: crewroute.Checker, scope: "check-b"}].held != 0 {
				t.Fatal("call left an outstanding reservation")
			}
		})
	}
}
