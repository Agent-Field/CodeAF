package session

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/crewroute"
	"github.com/Agent-Field/codeaf/internal/provider"
)

func seatCtx(seat crewroute.Seat) context.Context {
	return context.WithValue(context.Background(), crewSeatContextKey{}, seat)
}

func usage(cost *float64) *ai.Response {
	return &ai.Response{Usage: &ai.Usage{PromptTokens: 1000, CompletionTokens: 100, Cost: cost}}
}

func usd(v float64) *float64 { return &v }

// A ROLE'S SPEND IS BOOKED WITH NO CEILING SET, two roles on one model stay
// apart, and an unknown cost is counted as unknown — never as $0 spent —
// while a provider's own cost of 0 and a priced free route are known nothing.
func TestRoleSpendIsBookedWithoutCeilingsAndUnknownIsNotZero(t *testing.T) {
	priced := map[string]bool{"priced/model": true, "free/model": true}
	g := &SpendGuard{Price: func(model string) (float64, float64, float64, bool) {
		if !priced[model] {
			return 0, 0, 0, false
		}
		if model == "free/model" {
			return 0, 0, 0, true
		}
		return 1e-6, 2e-6, 0, true
	}}
	g.after(seatCtx(crewroute.Worker), "shared/model", usage(usd(0.25)), 0)
	g.after(seatCtx(crewroute.Planner), "shared/model", usage(usd(0.05)), 0)
	g.after(seatCtx(crewroute.Checker), "unpriced/model", usage(nil), 0)
	g.after(seatCtx(crewroute.Checker), "unpriced/model", usage(usd(0)), 0)
	g.after(seatCtx(crewroute.Planner), "free/model", usage(nil), 0)
	g.after(seatCtx(crewroute.Worker), "priced/model", usage(nil), 0)
	g.after(seatCtx(crewroute.Worker), "priced/model", &ai.Response{}, 0)

	want := map[crewroute.Seat]struct {
		spent          float64
		calls, unknown int
	}{
		crewroute.Worker:  {0.25 + 1000*1e-6 + 100*2e-6, 3, 1},
		crewroute.Planner: {0.05, 2, 0},
		crewroute.Checker: {0, 2, 1},
	}
	for seat, w := range want {
		tally := g.seatTally(seat)
		calls, unknown := tally.Counts()
		if got := tally.Total(); got < w.spent-1e-12 || got > w.spent+1e-12 || calls != w.calls || unknown != w.unknown {
			t.Errorf("%s: spent %v calls %d unknown %d, want %+v", seat, got, calls, unknown, w)
		}
	}
	calls, unknown := g.Task.Counts()
	if calls != 7 || unknown != 2 {
		t.Fatalf("task calls %d unknown %d, want 7 and 2", calls, unknown)
	}

	// The checkpoint keeps the role figures and the counts across a restart.
	crew := &taskCrew{decision: liveDecision(), original: originals(liveDecision()), guard: g}
	record := crew.record()
	if record.Guard.SeatSpent[crewroute.Planner] != 0.05 || record.Guard.SeatCalls[crewroute.Checker] != 2 || record.Guard.SeatUnknown[crewroute.Checker] != 1 || record.Guard.TaskUnknown != 2 {
		t.Fatalf("checkpoint lost role accounting: %+v", record.Guard)
	}
	record.Routed = true
	a := &Agent{config: Config{Workspace: t.TempDir(), ProfileDir: t.TempDir()}}
	back, err := a.restoreTaskCrew(9, record)
	if err != nil {
		t.Fatal(err)
	}
	calls, unknown = back.guard.seatTally(crewroute.Checker).Counts()
	if back.guard.seatTally(crewroute.Worker).Total() != g.seatTally(crewroute.Worker).Total() || calls != 2 || unknown != 1 {
		t.Fatalf("reload lost role accounting: calls %d unknown %d", calls, unknown)
	}
	view := taskRoutingOf("9", back.record(), false, 20)
	if view.UnknownCalls != 2 || view.Roles[2].UnknownCalls != 1 || view.Roles[1].SpentUSD != 0.05 {
		t.Fatalf("view: %+v", view)
	}
	if len(view.Restored) != 1 || view.Changes[len(view.Changes)-1].Kind != TaskCrewChangeRestored {
		t.Fatalf("restart was not marked: %+v", view.Changes)
	}
}

// The cap and ceiling reservations are unchanged: a seat with a ceiling holds
// its estimate and releases it, one with none holds nothing.
func TestCeilingReservationsAreUnchanged(t *testing.T) {
	g := &SpendGuard{Price: func(string) (float64, float64, float64, bool) { return 1e-3, 1e-3, 0, true },
		SeatCeilings: map[crewroute.Seat]float64{crewroute.Checker: 100}, TaskCap: 100}
	held, err := g.before(seatCtx(crewroute.Checker), "m", nil, nil)
	if err != nil || held <= 0 {
		t.Fatalf("held %v err %v", held, err)
	}
	if g.seatTally(crewroute.Checker).held != held {
		t.Fatalf("checker ceiling did not hold the estimate")
	}
	g.after(seatCtx(crewroute.Checker), "m", usage(usd(0.5)), held)
	if g.seatTally(crewroute.Checker).held != 0 || g.Task.held != 0 {
		t.Fatalf("holds not released")
	}
	held, _ = g.before(seatCtx(crewroute.Worker), "m", nil, nil)
	if g.seatTally(crewroute.Worker).held != 0 {
		t.Fatalf("a seat with no ceiling held an estimate")
	}
	g.after(seatCtx(crewroute.Worker), "m", usage(usd(0.5)), held)
	if g.seatTally(crewroute.Worker).Total() != 0.5 || g.Task.Total() != 1.0 {
		t.Fatalf("spend not booked: %v %v", g.seatTally(crewroute.Worker).Total(), g.Task.Total())
	}
}

func liveDecision() crewroute.Decision {
	return crewroute.Decision{Class: crewroute.Bugfix, Mode: crewroute.ModeAuto, Crew: []crewroute.Pick{
		{Seat: crewroute.Worker, Model: "glm-flash", Send: "glm-flash", Provider: "openrouter", Kind: crewroute.Metered, EstUSD: 0.06},
		{Seat: crewroute.Planner, Model: "ds-flash", Send: "ds-flash", Provider: "openrouter", Kind: crewroute.Metered, EstUSD: 0.01},
		{Seat: crewroute.Checker, Model: "ds-flash", Send: "ds-flash", Provider: "openrouter", Kind: crewroute.Metered, EstUSD: 0.01, Pinned: true},
	}}
}

func originals(d crewroute.Decision) map[crewroute.Seat]string {
	out := map[crewroute.Seat]string{}
	for _, p := range d.Crew {
		out[p.Seat] = p.Send
	}
	return out
}

func liveCrew(capUSD float64) *taskCrew {
	d := liveDecision()
	return &taskCrew{title: "Update fixtures", brief: "update every fixture", decision: d, original: originals(d),
		ladders: map[crewroute.Seat][]crewroute.Pick{crewroute.Worker: {{Seat: crewroute.Worker, Model: "glm-5.3", Send: "glm-5.3", Provider: "openrouter", Kind: crewroute.Metered, EstUSD: 0.4}}},
		guard:   &SpendGuard{TaskCap: capUSD}}
}

func routerAgent(t *testing.T, route func(config.CrewAsk) (crewroute.Decision, error)) *Agent {
	return &Agent{config: Config{Workspace: t.TempDir(), ProfileDir: t.TempDir(), RouteCrew: route}, model: "chat/model"}
}

// Use a stronger worker moves the worker alone, at its next call, recorded as
// the person's — even though planner and checker share one model — and the
// planner keeps asking its own model.
func TestStrongerWorkerMovesOnlyTheWorkerAtItsNextCall(t *testing.T) {
	var asked config.CrewAsk
	a := routerAgent(t, func(ask config.CrewAsk) (crewroute.Decision, error) {
		asked = ask
		d := liveDecision()
		d.Crew[0] = crewroute.Pick{Seat: crewroute.Worker, Model: "sonnet", Send: "sonnet", Provider: "openrouter", Kind: crewroute.Metered, EstUSD: 0.5}
		return d, nil
	})
	crew := liveCrew(0)
	changed, err := a.actOnLiveCrew(1, crew, TaskRoutingAction{Kind: TaskRoutingStronger})
	if err != nil || len(changed) != 1 || changed[0] != crewroute.Worker {
		t.Fatalf("changed %v err %v", changed, err)
	}
	if asked.Stronger == nil || asked.Sends[crewroute.Planner] != "ds-flash" || asked.Sends[crewroute.Checker] != "ds-flash" || asked.Sends[crewroute.Worker] != "" {
		t.Fatalf("router was asked %+v", asked)
	}
	if got := crew.sendForSeat(crewroute.Worker, "glm-flash"); got != "sonnet" {
		t.Fatalf("worker asks %s", got)
	}
	if got := crew.sendForSeat(crewroute.Planner, "ds-flash"); got != "ds-flash" {
		t.Fatalf("planner moved to %s", got)
	}
	view := taskRoutingOf("1", crew.record(), false, 20)
	if view.Roles[0].Status != TaskRolePending || view.Roles[0].Model != "sonnet" || view.Roles[0].Source != TaskRoleAuto {
		t.Fatalf("worker role %+v", view.Roles[0])
	}
	if view.Roles[2].Status != TaskRoleNotRunYet || view.Roles[2].Source != TaskRolePinned {
		t.Fatalf("checker role %+v", view.Roles[2])
	}
	last := view.Changes[len(view.Changes)-1]
	if last.By != TaskCrewByPerson || last.Kind != TaskCrewChangeStronger || last.From != "glm-flash" || last.To != "sonnet" || last.At.IsZero() {
		t.Fatalf("change %+v", last)
	}
	if view.EstimateUSD == nil || *view.EstimateUSD < 0.519 || *view.EstimateUSD > 0.521 {
		t.Fatalf("estimate %v", view.EstimateUSD)
	}
	// The worker's first answer clears pending.
	a.crewAnswered(&beltRun{row: 1, crew: crew}, "sonnet")
	if view := taskRoutingOf("1", crew.record(), false, 20); view.Roles[0].Status != TaskRoleUsed {
		t.Fatalf("worker after answer %+v", view.Roles[0])
	}
}

// A per-role pick parts two roles that share one model: the checker moves,
// the planner stays, both asking through the same send id.
func TestPickPartsRolesThatShareAModel(t *testing.T) {
	a := routerAgent(t, func(ask config.CrewAsk) (crewroute.Decision, error) {
		d := liveDecision()
		for i := range d.Crew {
			if send := ask.Sends[d.Crew[i].Seat]; send != "" && send != d.Crew[i].Send {
				d.Crew[i] = crewroute.Pick{Seat: d.Crew[i].Seat, Model: send, Send: send, Provider: "openrouter", Kind: crewroute.Metered, EstUSD: 0.2, Pinned: true}
			}
		}
		return d, nil
	})
	crew := liveCrew(0)
	if _, err := a.actOnLiveCrew(1, crew, TaskRoutingAction{Kind: TaskRoutingPick, Picks: map[crewroute.Seat]string{crewroute.Checker: "sonnet"}}); err != nil {
		t.Fatal(err)
	}
	if crew.sendForSeat(crewroute.Checker, "ds-flash") != "sonnet" || crew.sendForSeat(crewroute.Planner, "ds-flash") != "ds-flash" {
		t.Fatalf("checker %s planner %s", crew.sendForSeat(crewroute.Checker, "ds-flash"), crew.sendForSeat(crewroute.Planner, "ds-flash"))
	}
	view := taskRoutingOf("1", crew.record(), false, 20)
	if view.Roles[2].Source != TaskRoleTaskPin {
		t.Fatalf("checker source %s", view.Roles[2].Source)
	}
	// Back to Auto drops the task's pin through the router.
	if _, err := a.actOnLiveCrew(1, crew, TaskRoutingAction{Kind: TaskRoutingAuto}); err != nil {
		t.Fatal(err)
	}
	view = taskRoutingOf("1", crew.record(), false, 20)
	if view.Roles[2].Source == TaskRoleTaskPin || view.Roles[2].Model != "ds-flash" || view.ChangeCount != 2 {
		t.Fatalf("after auto %+v (changes %d)", view.Roles[2], view.ChangeCount)
	}
}

// A choice over the task cap is refused with the cap it needs, and goes
// ahead only with an explicit raise at least that high.
func TestBudgetRefusalNamesTheCapAndNeedsAnExplicitRaise(t *testing.T) {
	a := routerAgent(t, func(ask config.CrewAsk) (crewroute.Decision, error) {
		if ask.Effort != crewroute.EffortBest {
			t.Fatalf("best asked %q", ask.Effort)
		}
		d := liveDecision()
		for i := range d.Crew {
			d.Crew[i].Model, d.Crew[i].Send, d.Crew[i].EstUSD = "opus-"+string(d.Crew[i].Seat), "opus-"+string(d.Crew[i].Seat), 1.0
		}
		return d, nil
	})
	crew := liveCrew(2)
	_, err := a.actOnLiveCrew(1, crew, TaskRoutingAction{Kind: TaskRoutingBest})
	var refusal *TaskRoutingRefusal
	if !errors.As(err, &refusal) || refusal.Code != TaskRoutingOverCap || refusal.RequiredUSD != 3 || refusal.CapUSD != 2 {
		t.Fatalf("refusal %+v", err)
	}
	if crew.current().Seat(crewroute.Worker).Send != "glm-flash" || crew.changeCount != 0 {
		t.Fatal("a refused change moved the crew")
	}
	if _, err = a.actOnLiveCrew(1, crew, TaskRoutingAction{Kind: TaskRoutingBest, RaiseCapUSD: 2.5}); !errors.As(err, &refusal) || refusal.Code != TaskRoutingOverCap {
		t.Fatalf("a raise below the need was taken: %v", err)
	}
	if _, err = a.actOnLiveCrew(1, crew, TaskRoutingAction{Kind: TaskRoutingBest, RaiseCapUSD: 3}); err != nil {
		t.Fatal(err)
	}
	if crew.guard.TaskCap != 3 {
		t.Fatalf("task cap %v", crew.guard.TaskCap)
	}
	view := taskRoutingOf("1", crew.record(), false, 20)
	if last := view.Changes[len(view.Changes)-1]; last.Kind != TaskCrewChangeCap || last.CapUSD != 3 {
		t.Fatalf("cap raise not recorded: %+v", last)
	}
}

// No eligible model, a refused pin, a route already failed here and a stale
// card are each refused typed, and nothing moves.
func TestActionRefusalsAreTypedAndChangeNothing(t *testing.T) {
	none := &crewroute.NoEligibleError{Seat: crewroute.Checker, Mode: crewroute.ModeAuto}
	none.Reject("haiku-4.5", "anthropic", crewroute.RejectNotAllowed)
	a := routerAgent(t, func(config.CrewAsk) (crewroute.Decision, error) { return crewroute.Decision{}, none })
	crew := liveCrew(0)
	_, err := a.actOnLiveCrew(1, crew, TaskRoutingAction{Kind: TaskRoutingBest})
	var refusal *TaskRoutingRefusal
	if !errors.As(err, &refusal) || refusal.Code != TaskRoutingNoEligible || refusal.Blocked == nil || refusal.Blocked.Rejected[0].Reason != crewroute.RejectNotAllowed {
		t.Fatalf("no eligible: %+v", err)
	}
	a = routerAgent(t, func(config.CrewAsk) (crewroute.Decision, error) {
		d := liveDecision()
		d.Warnings = []crewroute.SeatWarning{{Seat: crewroute.Worker, Kind: crewroute.PinNotAllowed, Model: "gpt-5.2"}}
		return d, nil
	})
	if _, err = a.actOnLiveCrew(1, crew, TaskRoutingAction{Kind: TaskRoutingPick, Picks: map[crewroute.Seat]string{crewroute.Worker: "gpt-5.2"}}); !errors.As(err, &refusal) || refusal.Code != TaskRoutingNotAllowed {
		t.Fatalf("refused pin: %v", err)
	}
	a = routerAgent(t, func(config.CrewAsk) (crewroute.Decision, error) {
		d := liveDecision()
		d.Crew[0].Model, d.Crew[0].Send = "glm-5.3", "glm-5.3"
		return d, nil
	})
	crew.noteFailed("glm-5.3", crewFailure{kind: provider.RouteUnavailable, err: errors.New("gone")})
	if _, err = a.actOnLiveCrew(1, crew, TaskRoutingAction{Kind: TaskRoutingStronger}); !errors.As(err, &refusal) || refusal.Code != TaskRoutingRouteFailed {
		t.Fatalf("failed route: %v", err)
	}
	stale := 4
	if _, err = a.actOnLiveCrew(1, crew, TaskRoutingAction{Kind: TaskRoutingStronger, ChangeCount: &stale}); !errors.As(err, &refusal) || refusal.Code != TaskRoutingStale {
		t.Fatalf("stale: %v", err)
	}
	if crew.changeCount != 0 || crew.current().Seat(crewroute.Worker).Send != "glm-flash" {
		t.Fatal("a refusal moved the crew")
	}
}

// A provider failure moves an Auto role down its ladder with the engine's
// reason in the history; in one-model mode no role moves on its own.
func TestFallbackIsRecordedAndOneModelNeverMoves(t *testing.T) {
	a := &Agent{config: Config{Workspace: t.TempDir(), ProfileDir: t.TempDir()}, model: "chat/model"}
	crew := liveCrew(0)
	run := &beltRun{row: 1, crew: crew, recoveredCrew: &TaskCrewRecord{}}
	if _, moved := a.moveCrewSeat(run, "glm-flash", "glm-flash", provider.RouteUnavailable, time.Time{}); !moved {
		t.Fatal("worker did not fall back")
	}
	view := taskRoutingOf("1", crew.record(), false, 20)
	last := view.Changes[0]
	if view.Roles[0].Model != "glm-5.3" || last.By != TaskCrewByEngine || last.Kind != TaskCrewChangeFallback || last.Why != string(provider.RouteUnavailable) {
		t.Fatalf("fallback %+v / %+v", view.Roles[0], last)
	}

	one := liveCrew(0)
	one.decision.Mode = crewroute.ModeOne
	run = &beltRun{row: 2, crew: one, recoveredCrew: &TaskCrewRecord{}}
	if _, moved := a.moveCrewSeat(run, "glm-flash", "glm-flash", provider.RouteUnavailable, time.Time{}); moved {
		t.Fatal("one-model mode moved a role")
	}
	if one.changeCount != 1 || one.changes[0].Kind != TaskCrewChangeHeld || one.changes[0].To != "" {
		t.Fatalf("one-model mode recorded %+v, want the role held", one.changes)
	}
	if view := taskRoutingOf("2", one.record(), false, 20); view.Mode != crewroute.ModeOne || view.Roles[0].Source != TaskRoleOneModel {
		t.Fatalf("one-model view %+v", view)
	}
}

// A finished task freezes: its roles read not used where they never ran, an
// unpriced pin leaves the estimate unknown, and an older task with no
// routing record reads its one model and nothing about the other roles.
func TestFinishedUnpricedAndOlderHistoryReadTruthfully(t *testing.T) {
	crew := liveCrew(0)
	crew.decision.Crew[2] = crewroute.Pick{Seat: crewroute.Checker, Model: "unpriced", Send: "unpriced", Kind: crewroute.Metered, Pinned: true}
	crew.guard.after(seatCtx(crewroute.Worker), "glm-flash", usage(usd(0.07)), 0)
	view := taskRoutingOf("1", crew.record(), true, 20)
	if view.EstimateUSD != nil || view.Roles[2].EstimateUSD != nil {
		t.Fatalf("an unpriced pin produced an estimate: %v", view.EstimateUSD)
	}
	if view.Roles[0].Status != TaskRoleUsed || view.Roles[1].Status != TaskRoleNotUsed || view.Roles[2].Status != TaskRoleNotUsed || !view.Settled {
		t.Fatalf("roles %+v", view.Roles)
	}
	if view.SpentUSD != 0.07 || view.Roles[0].SpentUSD != 0.07 {
		t.Fatalf("spent %v", view.SpentUSD)
	}

	a, _ := newTestAgent(t, &recoveryModelRecorder{}, nil)
	g := a.graph()
	a.publishRunRow(g, TaskNotice{ID: 7, Title: "old", State: TaskDone, Model: "glm-5.3"})
	got, err := a.TaskRouting("7", 0)
	if err != nil || got.State != TaskRoutingSingle || got.Model != "glm-5.3" || len(got.Roles) != 0 {
		t.Fatalf("older history %+v %v", got, err)
	}
	if _, err := a.TaskRoutingAct(context.Background(), "7", TaskRoutingAction{Kind: TaskRoutingBest}); err == nil {
		t.Fatal("a finished task changed without a rerun")
	}
}

// A role with no model parks the hand-off as a waiting ask with the typed
// reason; the engine starts nothing, and stop ends it as not started.
func TestNoEligibleParksTheTaskAsAWaitingAsk(t *testing.T) {
	a, _ := newTestAgent(t, &recoveryModelRecorder{}, nil)
	none := &crewroute.NoEligibleError{Seat: crewroute.Checker, Mode: crewroute.ModeAuto, Action: ""}
	none.Reject("haiku-4.5", "anthropic", crewroute.RejectNotAllowed)
	none.Reject("gpt-5.2", "openai", crewroute.RejectProviderUnavailable)
	p := &stagedProposal{id: 12}
	receipt, parked := a.parkUnroutedHandoff(context.Background(), p, taskSpec{title: "Update fixtures", brief: "b"}, "", nil, errors.Join(errors.New("route"), none))
	if !parked || receipt == "" {
		t.Fatal("no-eligible was not parked")
	}
	view, err := a.TaskRouting("12", 0)
	if err != nil || view.State != TaskRoutingBlocked || view.Blocked.Seat != crewroute.Checker || len(view.Blocked.Rejected) != 2 {
		t.Fatalf("blocked view %+v %v", view, err)
	}
	row, ok := a.appRunRow(12)
	if !ok || row.State != TaskQueued || row.Waiting != TaskWaitingNoModel {
		t.Fatalf("row %+v", row)
	}
	if _, parked = a.parkUnroutedHandoff(context.Background(), &stagedProposal{id: 13}, taskSpec{}, "", nil, errors.New("other")); parked {
		t.Fatal("another failure was parked")
	}
	if !a.DropParkedTask(12) {
		t.Fatal("stop did not end the waiting task")
	}
	row, _ = a.appRunRow(12)
	if row.State != TaskFailed {
		t.Fatalf("stopped waiting task is %s", row.State)
	}
}

// A choice made before a task starts reaches the router as one-task pins.
func TestAChoiceBeforeStartReachesTheRouter(t *testing.T) {
	var asked config.CrewAsk
	a := routerAgent(t, func(ask config.CrewAsk) (crewroute.Decision, error) { asked = ask; return liveDecision(), nil })
	if err := a.actOnProposal(3, TaskRoutingAction{Kind: TaskRoutingPick, Picks: map[crewroute.Seat]string{crewroute.Checker: "sonnet"}}); err != nil {
		t.Fatal(err)
	}
	ctx := withCrewWish(context.Background(), a.proposalCrewWish(3, crewWish{worker: "named"}))
	crew, err := a.routeTaskCrew(ctx, 3, "t", "b")
	if err != nil {
		t.Fatal(err)
	}
	if asked.Sends[crewroute.Checker] != "sonnet" || asked.Sends[crewroute.Worker] != "named" || !crew.taskPins[crewroute.Checker] {
		t.Fatalf("asked %+v pins %v", asked.Sends, crew.taskPins)
	}
}

// THE PIN LAW AT RUN TIME: a pinned role (a saved pin, a pin for this task,
// one model for every role) whose route fails holds its exact model — it is
// never moved down a ladder or onto a rescue — and the history says it
// failed, when and why, once per failure.
func TestAPinnedRoleHoldsItsModelWhenItsRouteFails(t *testing.T) {
	a := &Agent{config: Config{Workspace: t.TempDir(), ProfileDir: t.TempDir()}, model: "chat/model"}
	crew := liveCrew(0)
	crew.decision.Crew[0].Pinned = true
	crew.ladders[crewroute.Worker] = []crewroute.Pick{{Seat: crewroute.Worker, Model: "rescue", Send: "rescue", Kind: crewroute.Metered}}
	run := &beltRun{row: 1, crew: crew, recoveredCrew: &TaskCrewRecord{}}
	for range 3 {
		if _, moved := a.moveCrewSeat(run, "glm-flash", "glm-flash", provider.RoutePayment, time.Time{}); moved {
			t.Fatal("a pinned worker was moved")
		}
	}
	if got := crew.sendForSeat(crewroute.Worker, "glm-flash"); got != "glm-flash" {
		t.Fatalf("pinned worker asks %s", got)
	}
	view := taskRoutingOf("1", crew.record(), false, 20)
	if view.ChangeCount != 1 || view.Changes[0].Kind != TaskCrewChangeHeld || view.Changes[0].Why != string(provider.RoutePayment) || view.Changes[0].At.IsZero() {
		t.Fatalf("held history %+v", view.Changes)
	}
	// A person's explicit pick still moves it, recorded as theirs.
	b := routerAgent(t, func(ask config.CrewAsk) (crewroute.Decision, error) {
		d := liveDecision()
		d.Crew[0] = crewroute.Pick{Seat: crewroute.Worker, Model: ask.Sends[crewroute.Worker], Send: ask.Sends[crewroute.Worker], Kind: crewroute.Metered, EstUSD: 0.1, Pinned: true}
		return d, nil
	})
	if _, err := b.actOnLiveCrew(1, crew, TaskRoutingAction{Kind: TaskRoutingPick, Picks: map[crewroute.Seat]string{crewroute.Worker: "glm-5.3"}}); err != nil {
		t.Fatal(err)
	}
	if last := taskRoutingOf("1", crew.record(), false, 20).Changes; last[len(last)-1].By != TaskCrewByPerson || crew.sendForSeat(crewroute.Worker, "glm-flash") != "glm-5.3" {
		t.Fatalf("person's pick %+v", last)
	}
}

// A person's per-task pin the router would not seat as asked — a model the
// allowed list leaves out, a provider that is not reachable — is refused
// typed; it never runs as Auto in its place.
func TestAPinTheRouterWouldNotSeatIsRefusedNotRunAsAuto(t *testing.T) {
	for _, kind := range []crewroute.WarningKind{crewroute.PinNotAllowed, crewroute.PinProviderUnavailable} {
		a := routerAgent(t, func(config.CrewAsk) (crewroute.Decision, error) {
			d := liveDecision()
			d.Warnings = []crewroute.SeatWarning{{Seat: crewroute.Checker, Kind: kind, Model: "claude-sonnet"}}
			return d, nil
		})
		crew := liveCrew(0)
		_, err := a.actOnLiveCrew(1, crew, TaskRoutingAction{Kind: TaskRoutingPick, Picks: map[crewroute.Seat]string{crewroute.Checker: "claude-sonnet"}})
		var refusal *TaskRoutingRefusal
		if !errors.As(err, &refusal) || refusal.Code != TaskRoutingNotAllowed || refusal.Seat != crewroute.Checker {
			t.Fatalf("%s: %v", kind, err)
		}
		if crew.changeCount != 0 || crew.sendForSeat(crewroute.Checker, "ds-flash") != "ds-flash" {
			t.Fatalf("%s: a refused pin moved the checker", kind)
		}
	}
}

// What a call cost is known only when somebody said it: an answer with no
// usage on a route priced at nothing is known free; a failed call and a
// usage-less answer on a priced route are unknown, never a proven $0.
func TestCallCertaintyEdges(t *testing.T) {
	g := &SpendGuard{Price: func(model string) (float64, float64, float64, bool) {
		switch model {
		case "free/pool":
			return 0, 0, 0, true
		case "priced":
			return 1e-6, 1e-6, 0, true
		}
		return 0, 0, 0, false
	}}
	g.after(seatCtx(crewroute.Worker), "free/pool", &ai.Response{}, 0)
	g.after(seatCtx(crewroute.Worker), "free/pool", nil, 0)
	g.after(seatCtx(crewroute.Planner), "priced", &ai.Response{}, 0)
	g.after(seatCtx(crewroute.Planner), "priced", nil, 0)
	g.after(seatCtx(crewroute.Checker), "unpriced", usage(usd(0)), 0)
	if _, calls, unknown := g.seatTally(crewroute.Worker).snapshot(); calls != 2 || unknown != 0 {
		t.Fatalf("free pool: calls %d unknown %d", calls, unknown)
	}
	if _, calls, unknown := g.seatTally(crewroute.Planner).snapshot(); calls != 2 || unknown != 2 {
		t.Fatalf("priced route without usage: calls %d unknown %d", calls, unknown)
	}
	if spent, calls, unknown := g.seatTally(crewroute.Checker).snapshot(); spent != 0 || calls != 1 || unknown != 0 {
		t.Fatalf("explicit provider cost 0: %v %d %d", spent, calls, unknown)
	}
}

// A checkpoint taken while calls are booked always sees a task's figures and
// its roles' figures from the same bookings.
func TestCheckpointSeesCoherentBookings(t *testing.T) {
	g := &SpendGuard{}
	crew := &taskCrew{decision: liveDecision(), original: originals(liveDecision()), guard: g}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 2000 {
			g.after(seatCtx(crewroute.Worker), "m", usage(usd(0.001)), 0)
		}
	}()
	for {
		select {
		case <-done:
			return
		default:
		}
		r := crew.record()
		if r == nil || r.Guard == nil {
			continue
		}
		if r.Guard.TaskCalls != r.Guard.SeatCalls[crewroute.Worker] {
			t.Fatalf("task saw %d calls, worker %d", r.Guard.TaskCalls, r.Guard.SeatCalls[crewroute.Worker])
		}
	}
}
