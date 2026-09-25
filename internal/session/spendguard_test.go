package session

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/crewroute"
)

// spendingCompleter answers every call at a fixed cost, as the provider's own
// usage figure, and counts what it was asked.
type spendingCompleter struct {
	usd   float64
	calls int
}

func (c *spendingCompleter) CompleteWithMessages(context.Context, []ai.Message, ...ai.Option) (*ai.Response, error) {
	c.calls++
	cost := c.usd
	return &ai.Response{Choices: []ai.Choice{{Message: textMessage("assistant", "ok")}},
		Usage: &ai.Usage{PromptTokens: 1000, CompletionTokens: 100, Cost: &cost}}, nil
}

// kimiPrice is a checker's prices per token.
func kimiPrice(string) (float64, float64, float64, bool) { return 3e-6, 1.5e-5, 3e-7, true }

// A DEAR CHECKER UNDER A SMALL DAY: an open-ended task under a $0.25 day whose
// checker would run far past its estimate. Every call is priced before it is
// made: the checker stops at its own ceiling, three times its estimate, and
// the day never passes its cap by more than one call's misjudgement.
func TestTheCheckerStopsAtItsCeilingAndTheDayAtItsCap(t *testing.T) {
	messages := []ai.Message{textMessage("user", strings.Repeat("the diff and the tests ", 800))}
	checker := &spendingCompleter{usd: 0.06}
	guard := &SpendGuard{Price: kimiPrice, Day: NewSpendDay(0), Cap: 1, CapAction: "today's spending limit of $1.00 is reached · raise it with /budget",
		SeatCeilings: map[crewroute.Seat]float64{crewroute.Checker: 0.0882 * 3}, CeilingAction: "the check stopped at its spend ceiling of $%.2f"}
	seat := SeatCompleter(crewroute.Checker, guard.Wrap("moonshotai/kimi-k3", checker))
	var stopped ErrSpendStopped
	for i := 0; i < 20; i++ {
		if _, err := seat.CompleteWithMessages(t.Context(), messages, ai.WithMaxTokens(2000)); err != nil {
			if !errors.As(err, &stopped) {
				t.Fatalf("call %d: %v", i, err)
			}
			break
		}
	}
	if !strings.HasPrefix(stopped.Action, "the check stopped at its spend ceiling of $0.26") {
		t.Fatalf("the checker ended on %q after %d calls", stopped.Action, checker.calls)
	}
	if spent := guard.Day.Total(); spent > 0.0882*3 {
		t.Errorf("the checker spent $%.3f", spent)
	}

	// The day's cap binds a seat with no ceiling of its own, before its call.
	worker := &spendingCompleter{usd: 0.05}
	day := &SpendGuard{Price: kimiPrice, Day: NewSpendDay(0.23), Cap: 0.25, CapAction: "cap reached"}
	seat = day.Wrap("z-ai/glm-5.3", worker)
	_, err := seat.CompleteWithMessages(t.Context(), messages, ai.WithMaxTokens(2000))
	if !errors.As(err, &stopped) || stopped.Action != "cap reached" || worker.calls != 0 {
		t.Errorf("a call that would cross the cap: %v after %d calls", err, worker.calls)
	}
}

// A TASK STOPS AT ITS LIMIT: seats on two models and a helper share one
// tally. The call that would take the task past the limit is not made, and
// the task ends on the limit's sentence.
func TestATaskStopsAtItsLimitAcrossEveryModel(t *testing.T) {
	messages := []ai.Message{textMessage("user", "the brief")}
	worker := &spendingCompleter{usd: 1.5}
	checker := &spendingCompleter{usd: 1.5}
	guard := &SpendGuard{Price: kimiPrice, Day: NewSpendDay(0), TaskCap: 5, TaskAction: config.CrewTaskCapAction(5)}
	seats := []Completer{guard.Wrap("z-ai/glm-5.3", worker), guard.Wrap("moonshotai/kimi-k3", checker)}
	var stopped ErrSpendStopped
	made := 0
	for i := 0; i < 10; i++ {
		_, err := seats[i%2].CompleteWithMessages(t.Context(), messages, ai.WithMaxTokens(2000))
		if err != nil {
			if !errors.As(err, &stopped) {
				t.Fatalf("call %d: %v", i, err)
			}
			break
		}
		made++
	}
	if stopped.Action != "this task reached its $5 limit · raise it in /crew" {
		t.Fatalf("the task ended on %q", stopped.Action)
	}
	if made != 3 || worker.calls+checker.calls != 3 {
		t.Fatalf("%d calls were made (%d worker, %d checker), want 3", made, worker.calls, checker.calls)
	}
	if spent := guard.Task.Total(); spent > 5 {
		t.Fatalf("the task spent $%.2f past its $5 limit", spent)
	}

	// A helper made for the same task is held to the same tally.
	helper := &SpendGuard{Price: kimiPrice, Day: guard.Day, TaskCap: guard.TaskCap, TaskAction: guard.TaskAction, Task: guard.tally()}
	aux := &spendingCompleter{usd: 1.5}
	if _, err := helper.Wrap("z-ai/glm-5.3", aux).CompleteWithMessages(t.Context(), messages); !errors.As(err, &stopped) || aux.calls != 0 {
		t.Fatalf("a helper past the task's limit: %v after %d calls", err, aux.calls)
	}

	// The checker's own ceiling still binds inside the task's limit.
	ceilinged := &SpendGuard{Price: kimiPrice, Day: NewSpendDay(0), TaskCap: 5, TaskAction: config.CrewTaskCapAction(5),
		SeatCeilings: map[crewroute.Seat]float64{crewroute.Checker: 0.5}, CeilingAction: "the check stopped at its spend ceiling of $%.2f"}
	check := &spendingCompleter{usd: 0.4}
	seat := SeatCompleter(crewroute.Checker, ceilinged.Wrap("moonshotai/kimi-k3", check))
	var err error
	for i := 0; i < 5 && err == nil; i++ {
		_, err = seat.CompleteWithMessages(t.Context(), messages, ai.WithMaxTokens(2000))
	}
	if !errors.As(err, &stopped) || !strings.HasPrefix(stopped.Action, "the check stopped at its spend ceiling") {
		t.Fatalf("the ceiling inside the task's limit: %v", err)
	}
}

// THE PER-TASK LIMIT IS ON EVERY CREW GUARD, whether or not the daily limit
// is (withDaily false is how --yes-spend builds it), and on a helper's guard
// for a task, which shares the task's tally.
func TestTheTaskLimitIsOnEveryGuard(t *testing.T) {
	dir := t.TempDir()
	for _, withDaily := range []bool{true, false} {
		guard := CrewSpendGuard(dir, crewroute.Decision{}, withDaily)
		if guard.TaskCap != 5 || guard.TaskAction != "this task reached its $5 limit · raise it in /crew" {
			t.Fatalf("withDaily=%v: task cap %v, %q", withDaily, guard.TaskCap, guard.TaskAction)
		}
	}
	if err := config.SetCrewTaskCap(dir, "2.5"); err != nil {
		t.Fatal(err)
	}
	if guard := TaskSpendGuard(dir); guard.TaskCap != 2.5 || guard.TaskAction != "this task reached its $2.50 limit · raise it in /crew" {
		t.Fatalf("a set limit reads %v, %q", guard.TaskCap, guard.TaskAction)
	}
	crew := &taskCrew{guard: crewSpendGuard(dir, crewroute.Decision{}, false)}
	a := &Agent{config: Config{ProfileDir: dir, RouteCrew: func(config.CrewAsk) (crewroute.Decision, error) { return crewroute.Decision{}, nil }}}
	helper := a.helperGuard(crew)
	if helper.TaskCap != 2.5 || helper.Task != crew.guard.Task {
		t.Fatalf("a helper for the task is held to %v on its own tally", helper.TaskCap)
	}
	if loose := a.helperGuard(nil); loose.TaskCap != 0 {
		t.Fatalf("a helper for no task is held to a task limit of %v", loose.TaskCap)
	}
}

// A SHARED MODEL DOES NOT SHARE A SEAT'S CEILING. The checker also keeps its
// tally when its next call goes through another model on the fallback ladder.
func TestSharedModelCheckerCeilingFollowsTheSeat(t *testing.T) {
	d := crewroute.Decision{Crew: []crewroute.Pick{
		{Seat: crewroute.Worker, Model: "vendor/cheap", Send: "vendor/cheap"},
		{Seat: crewroute.Planner, Model: "vendor/cheap", Send: "vendor/cheap"},
		{Seat: crewroute.Checker, Model: "vendor/cheap", Send: "vendor/cheap", EstUSD: 0.01},
	}}
	guard := CrewSpendGuard(t.TempDir(), d, false)
	guard.Price = func(string) (float64, float64, float64, bool) { return 0, 1e-6, 0, true }
	worker := &spendingCompleter{usd: 0.02}
	check := &spendingCompleter{usd: 0.02}
	workCall := SeatCompleter(crewroute.Worker, guard.Wrap("vendor/cheap", worker))
	checkCall := SeatCompleter(crewroute.Checker, guard.Wrap("vendor/cheap", check))
	messages := []ai.Message{textMessage("user", "check")}
	for i := 0; i < 4; i++ {
		if _, err := workCall.CompleteWithMessages(t.Context(), messages, ai.WithMaxTokens(20000)); err != nil {
			t.Fatalf("worker call %d: %v", i, err)
		}
	}
	for i := 0; i < 2; i++ {
		if _, err := checkCall.CompleteWithMessages(t.Context(), messages, ai.WithMaxTokens(20000)); err != nil {
			t.Fatalf("checker call %d: %v", i, err)
		}
	}
	fallback := SeatCompleter(crewroute.Checker, guard.Wrap("vendor/fallback", check))
	_, err := fallback.CompleteWithMessages(t.Context(), messages, ai.WithMaxTokens(20000))
	var stopped ErrSpendStopped
	if !errors.As(err, &stopped) || stopped.Action != "the check stopped at its spend ceiling of $0.05, three times its estimate, before it finished" || check.calls != 2 || worker.calls != 4 {
		t.Fatalf("fallback after a shared model: %v, %d checker and %d worker calls", err, check.calls, worker.calls)
	}
	// A call without a seat still has the task limit, but not this ceiling.
	if _, err := guard.Wrap("vendor/cheap", worker).CompleteWithMessages(t.Context(), messages, ai.WithMaxTokens(20000)); err != nil {
		t.Fatalf("a call without a crew seat: %v", err)
	}
}

// THE SEAT MARK KEEPS THE MODEL CHAIN: a wrapper that dropped it would end
// model fallback for every run task it wraps.
func TestSeatCompleterKeepsTheModelFallbackChain(t *testing.T) {
	next := chained([]string{"vendor/fallback"})
	marked := SeatCompleter(crewroute.Checker, (&SpendGuard{}).Wrap("vendor/first", next))
	chain, ok := marked.(modelChain)
	if !ok || len(chain.FallbackModels("vendor/first")) != 1 || chain.FallbackModels("vendor/first")[0] != "vendor/fallback" {
		t.Fatalf("the seat wrapper dropped the fallback chain: %T", marked)
	}
}

// AT THE DAILY CAP A CALL NOBODY PRICES IS NOT SENT EITHER, on the same
// sentence a priced one ends on; below the cap it goes as it always did, and
// with no cap nothing stops it.
func TestUnpricedCallAtDailyCapIsNotSent(t *testing.T) {
	for _, tc := range []struct {
		day, cap float64
		sent     bool
	}{
		{0.5, 0.5, false}, {0.6, 0.5, false}, {0.4, 0.5, true}, {0.5, 0, true},
	} {
		guard := &SpendGuard{Price: func(string) (float64, float64, float64, bool) { return 0, 0, 0, false },
			Day: NewSpendDay(tc.day), Cap: tc.cap, CapAction: "daily cap"}
		calls := &spendingCompleter{usd: 0.01}
		_, err := guard.Wrap("local/unpriced", calls).CompleteWithMessages(t.Context(), nil)
		var stopped ErrSpendStopped
		if tc.sent && (err != nil || calls.calls != 1) || !tc.sent && (!errors.As(err, &stopped) || stopped.Action != "daily cap" || calls.calls != 0) {
			t.Errorf("day=%v cap=%v: %v after %d calls", tc.day, tc.cap, err, calls.calls)
		}
	}
}

// A HELPER IS HELD THE SAME WAY: the guard a conversation's auxiliary calls
// go through refuses an unpriced call once the day is at the crew's cap.
func TestHelperGuardRefusesUnpricedCallAtDailyCap(t *testing.T) {
	dir := t.TempDir()
	if err := config.SetCrewCap(dir, "0.5"); err != nil {
		t.Fatal(err)
	}
	a := &Agent{config: Config{ProfileDir: dir, RouteCrew: func(config.CrewAsk) (crewroute.Decision, error) { return crewroute.Decision{}, nil }}}
	a.crewDayOnce.Do(func() { a.crewDayHeld = NewSpendDay(0.5) })
	guard := a.helperGuard(nil)
	guard.Price = func(string) (float64, float64, float64, bool) { return 0, 0, 0, false }
	calls := &spendingCompleter{usd: 0.01}
	_, err := guard.Wrap("local/unpriced", calls).CompleteWithMessages(t.Context(), nil)
	var stopped ErrSpendStopped
	if !errors.As(err, &stopped) || stopped.Action != guard.CapAction || calls.calls != 0 {
		t.Fatalf("helper at cap: %v after %d calls", err, calls.calls)
	}
}
