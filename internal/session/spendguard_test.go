package session

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

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

// A CALL THAT COSTS NOTHING IS NEVER STOPPED BY A DOLLAR LINE: a free pool, a
// local model and a subscription plan are priced at nothing — a price that is
// KNOWN — so a day past its cap, a task at its limit and a checker at its
// ceiling all let it through. Only a call whose price nobody knows is held at
// a line it cannot be estimated against.
func TestACallThatCostsNothingPassesEveryDollarLine(t *testing.T) {
	free := func(string) (float64, float64, float64, bool) { return 0, 0, 0, true }
	task := &SpendTask{}
	task.settle(0, 6)
	guard := &SpendGuard{Price: free, Day: NewSpendDay(0.6), Cap: 0.5, CapAction: "daily cap",
		TaskCap: 5, TaskAction: "task limit", Task: task,
		SeatCeilings: map[crewroute.Seat]float64{crewroute.Checker: 0.05}, CeilingAction: "ceiling $%.2f"}
	guard.seatTally(crewroute.Checker).settle(0, 0.06)
	calls := &spendingCompleter{}
	seat := SeatCompleter(crewroute.Checker, guard.Wrap("ollama/gemma4:12b", calls))
	if _, err := seat.CompleteWithMessages(t.Context(), []ai.Message{textMessage("user", "hi")}, ai.WithMaxTokens(2000)); err != nil || calls.calls != 1 {
		t.Fatalf("a call that costs nothing past every line: %v after %d calls", err, calls.calls)
	}
}

func TestTaskCrewDelegationEnforcesDailyBudget(t *testing.T) {
	dir := t.TempDir()
	if err := config.WriteDailyBudgetUSD(dir, 1.00); err != nil {
		t.Fatal(err)
	}
	a := &Agent{
		config: Config{
			ProfileDir: dir,
			RouteCrew: func(config.CrewAsk) (crewroute.Decision, error) {
				return crewroute.Decision{
					Crew: []crewroute.Pick{
						{Seat: crewroute.Worker, Send: "test/worker"},
					},
				}, nil
			},
		},
	}
	crew, err := a.routeTaskCrew(t.Context(), 1, "task", "brief")
	if err != nil {
		t.Fatalf("routeTaskCrew: %v", err)
	}
	if crew.guard == nil {
		t.Fatal("expected crew.guard to be non-nil")
	}
	// The daily limit is read at each call, not frozen into Cap (which is the
	// crew's own cap): the guard carries the reader, and the reader says $1.00.
	if crew.guard.Cap != 0 || crew.guard.DayLimit == nil || crew.guard.Day == nil {
		t.Fatalf("crew.guard = cap %v, reader %v, day %v", crew.guard.Cap, crew.guard.DayLimit != nil, crew.guard.Day != nil)
	}
	limit, action, err := crew.guard.DayLimit()
	if err != nil || limit != 1.00 {
		t.Fatalf("daily limit = %v, %v, want 1.00", limit, err)
	}
	if !strings.Contains(action, "today's spending limit") {
		t.Fatalf("daily limit action = %q, want it to mention daily spending limit", action)
	}
}

func TestHelperGuardEnforcesDailyBudget(t *testing.T) {
	dir := t.TempDir()
	if err := config.WriteDailyBudgetUSD(dir, 0.75); err != nil {
		t.Fatal(err)
	}
	a := &Agent{
		config: Config{
			ProfileDir: dir,
			RouteCrew: func(config.CrewAsk) (crewroute.Decision, error) {
				return crewroute.Decision{}, nil
			},
		},
	}
	helper := a.helperGuard(nil)
	if helper == nil {
		t.Fatal("expected helper to be non-nil")
	}
	limit, action, err := helper.DayLimit()
	if err != nil || limit != 0.75 {
		t.Fatalf("helper daily limit = %v, %v, want 0.75", limit, err)
	}
	if !strings.Contains(action, "today's spending limit") {
		t.Fatalf("helper daily limit action = %q, want it to mention daily spending limit", action)
	}
}

func TestDailyGuardSeesOtherConversationsAndRollsOver(t *testing.T) {
	now := time.Date(2026, 9, 27, 23, 59, 0, 0, time.Local)
	ledger := 1.0
	day := newLedgerSpendDay(func() float64 { return ledger }, func() time.Time { return now })
	if got := day.Total(); got != 1 {
		t.Fatal(got)
	}
	held, ok := day.hold("model", 0.25, 2)
	if !ok {
		t.Fatal("first call refused")
	}
	day.settle("model", held, 0.25)
	ledger = 1.25
	if got := day.Total(); got != 1.25 {
		t.Fatalf("double counted receipt: %v", got)
	}
	ledger = 2.0
	if _, ok := day.hold("model", 0.1, 2); ok {
		t.Fatal("ignored another conversation's receipt")
	}
	now = now.Add(2 * time.Minute)
	ledger = 0
	if got := day.Total(); got != 0 {
		t.Fatalf("yesterday's spend survived midnight: %v", got)
	}
	if _, ok := day.hold("model", 0.1, 2); !ok {
		t.Fatal("new day remained blocked")
	}
}

// A LIMIT CHOSEN IN SETTINGS HOLDS THE GUARD ALREADY BUILT: the row is changed
// after the guards exist, and the very next paid call meets the new line —
// lowered, raised or removed — on the helper's guard and on a crew's, with the
// per-task limit unmoved.
func TestASettingsRowChangeReachesGuardsAlreadyBuilt(t *testing.T) {
	t.Setenv("CODEAF_DAILY_BUDGET", "")
	dir := t.TempDir()
	if err := config.WriteDailyBudgetUSD(dir, 500); err != nil {
		t.Fatal(err)
	}
	row, ok := config.NewSettings(config.SettingsOptions{ProfileDir: dir}).Row(config.KeyDailyBudget)
	if !ok {
		t.Fatal("no daily budget row")
	}
	a := &Agent{config: Config{ProfileDir: dir, RouteCrew: func(config.CrewAsk) (crewroute.Decision, error) {
		return crewroute.Decision{Crew: []crewroute.Pick{{Seat: crewroute.Worker, Send: "test/worker"}}}, nil
	}}}
	a.crewDayOnce.Do(func() { a.crewDayHeld = NewSpendDay(2) })
	crew, err := a.routeTaskCrew(t.Context(), 1, "task", "brief")
	if err != nil {
		t.Fatal(err)
	}
	crew.guard.Price = kimiPrice
	helper := a.helperGuard(nil)
	helper.Price = kimiPrice
	guards := map[string]*SpendGuard{"crew": crew.guard, "helper": helper}
	messages := []ai.Message{textMessage("user", "hello")}
	for name, guard := range guards {
		calls := &spendingCompleter{usd: 0.001}
		wrapped := guard.Wrap("moonshotai/kimi-k3", calls)
		if _, err := wrapped.CompleteWithMessages(t.Context(), messages, ai.WithMaxTokens(100)); err != nil || calls.calls != 1 {
			t.Fatalf("%s under $500: %v after %d calls", name, err, calls.calls)
		}
		if err := row.Apply("1"); err != nil {
			t.Fatal(err)
		}
		_, err := wrapped.CompleteWithMessages(t.Context(), messages, ai.WithMaxTokens(100))
		var stopped ErrSpendStopped
		if !errors.As(err, &stopped) || !strings.Contains(stopped.Action, "$1.00") || !strings.Contains(stopped.Action, "/budget") || calls.calls != 1 {
			t.Fatalf("%s after the row said $1: %v after %d calls", name, err, calls.calls)
		}
		if err := row.Apply("10"); err != nil {
			t.Fatal(err)
		}
		if _, err := wrapped.CompleteWithMessages(t.Context(), messages, ai.WithMaxTokens(100)); err != nil || calls.calls != 2 {
			t.Fatalf("%s after the row said $10: %v after %d calls", name, err, calls.calls)
		}
		if err := row.Apply("none"); err != nil {
			t.Fatal(err)
		}
		if _, err := wrapped.CompleteWithMessages(t.Context(), messages, ai.WithMaxTokens(100)); err != nil || calls.calls != 3 {
			t.Fatalf("%s after the row said none: %v after %d calls", name, err, calls.calls)
		}
		if err := row.Apply("500"); err != nil {
			t.Fatal(err)
		}
	}
	if crew.guard.TaskCap != config.CrewTaskCapDefault {
		t.Fatalf("the per-task limit moved to %v", crew.guard.TaskCap)
	}
}

// A DAILY LIMIT NOBODY CAN READ REFUSES WHAT MIGHT BE PAID — a priced call and
// one whose price nobody knows — and never reads as the default or as no
// limit; a call known to cost nothing is untouched.
func TestAnUnreadableDailyLimitRefusesPaidCallsOnly(t *testing.T) {
	t.Setenv("CODEAF_DAILY_BUDGET", "")
	dir := t.TempDir()
	if err := os.WriteFile(config.BudgetConfigPath(dir), []byte(`{"daily_budget_usd":null}`), 0o600); err != nil {
		t.Fatal(err)
	}
	guard := &SpendGuard{Price: kimiPrice, DayLimit: config.CrewDailyLimit(dir)}
	messages := []ai.Message{textMessage("user", "hello")}
	for name, price := range map[string]SpendPrice{
		"priced":   kimiPrice,
		"unpriced": func(string) (float64, float64, float64, bool) { return 0, 0, 0, false },
	} {
		guard.Price = price
		calls := &spendingCompleter{usd: 0.001}
		_, err := guard.Wrap("some/model", calls).CompleteWithMessages(t.Context(), messages, ai.WithMaxTokens(100))
		var stopped ErrSpendStopped
		if !errors.As(err, &stopped) || stopped.Action != spendLimitUnread || calls.calls != 0 {
			t.Fatalf("%s: %v after %d calls", name, err, calls.calls)
		}
	}
	guard.Price = func(string) (float64, float64, float64, bool) { return 0, 0, 0, true }
	calls := &spendingCompleter{}
	if _, err := guard.Wrap("ollama/gemma", calls).CompleteWithMessages(t.Context(), messages); err != nil || calls.calls != 1 {
		t.Fatalf("a known-free call: %v after %d calls", err, calls.calls)
	}
}

// THE DAY EXISTS BEFORE THE LIMIT DOES: a crew admitted with no limit today,
// and a guard handed one later, both measure the next one on a real day.
func TestAGuardAlwaysHasADayWhereTheDailyLimitCouldTurnOn(t *testing.T) {
	t.Setenv("CODEAF_DAILY_BUDGET", "")
	dir := t.TempDir()
	if err := config.WriteDailyBudgetUSD(dir, 0); err != nil {
		t.Fatal(err)
	}
	if guard := crewSpendGuard(dir, crewroute.Decision{}, true); guard.Day == nil || guard.DayLimit == nil {
		t.Fatalf("a crew guard under no limit has day %v, reader %v", guard.Day != nil, guard.DayLimit != nil)
	}
	if guard := crewSpendGuard(dir, crewroute.Decision{}, false); guard.DayLimit != nil {
		t.Fatal("a run lifted past the daily limit still reads it")
	}
	guard := &SpendGuard{Price: kimiPrice, DayLimit: config.CrewDailyLimit(dir)}
	calls := &spendingCompleter{usd: 0.001}
	if _, err := guard.Wrap("m", calls).CompleteWithMessages(t.Context(), []ai.Message{textMessage("user", "hi")}, ai.WithMaxTokens(10)); err != nil {
		t.Fatal(err)
	}
	if err := config.WriteDailyBudgetUSD(dir, 0.0000001); err != nil {
		t.Fatal(err)
	}
	if guard.Day != nil {
		t.Fatal("no limit should not have made a day")
	}
	if _, err := guard.Wrap("m", calls).CompleteWithMessages(t.Context(), []ai.Message{textMessage("user", "hi")}, ai.WithMaxTokens(10)); err == nil || guard.Day == nil {
		t.Fatalf("a limit turned on later did not hold: %v (day %v)", err, guard.Day != nil)
	}
}
