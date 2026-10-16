package session

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/crewroute"
	"github.com/Agent-Field/codeaf/internal/provider"
)

// The three sends of the mixed-role regression. heldSend is the original
// every role was routed on. The planner and the checker leave it for
// different models; a held worker must not follow either.
const (
	heldSend  = "role/held"
	planSend  = "role/planner-fallback"
	checkSend = "role/checker-fallback"
)

func clearLiveKeys(t *testing.T) {
	t.Helper()
	for _, key := range []string{"OPENROUTER_API_KEY", "OPENAI_API_KEY", "ANTHROPIC_API_KEY", "CODEAF_API_KEY"} {
		t.Setenv(key, "")
	}
}

// mixedCrew is three roles on one original send. pin shapes the worker's hold.
// The planner and the checker stay unpinned and keep different ladders.
func mixedCrew(pin func(*taskCrew)) *taskCrew {
	pick := func(seat crewroute.Seat, send string) crewroute.Pick {
		return crewroute.Pick{Seat: seat, Model: send, Send: send, Provider: "openrouter", Kind: crewroute.Metered, EstUSD: 0.02}
	}
	d := crewroute.Decision{Class: crewroute.Bugfix, Mode: crewroute.ModeAuto, Crew: []crewroute.Pick{
		pick(crewroute.Worker, heldSend),
		pick(crewroute.Planner, heldSend),
		pick(crewroute.Checker, heldSend),
	}}
	crew := &taskCrew{
		roleFacts: true, title: "hold the worker", decision: d, original: originals(d),
		ladders: map[crewroute.Seat][]crewroute.Pick{
			crewroute.Planner: {pick(crewroute.Planner, planSend)},
			crewroute.Checker: {pick(crewroute.Checker, checkSend)},
		},
		guard: &SpendGuard{},
	}
	if pin != nil {
		pin(crew)
	}
	return crew
}

func dispatchRun(crew *taskCrew) *beltRun {
	return &beltRun{row: 1, crew: crew, recoveredCrew: &TaskCrewRecord{}}
}

func callOriginal(t *testing.T, agent *Agent, run *beltRun, seat crewroute.Seat) error {
	t.Helper()
	ctx := context.WithValue(t.Context(), crewSeatContextKey{}, seat)
	_, err := (crewSeatCompleter{agent: agent, run: run}).CompleteWithMessages(ctx,
		[]ai.Message{textMessage("user", "go")}, ai.WithModel(heldSend))
	return err
}

func holdStub(t *testing.T) (*crewStub, *Agent) {
	t.Helper()
	clearLiveKeys(t)
	stub := newCrewStub(t, http.StatusOK, nil)
	stub.mu.Lock()
	stub.only = map[string]int{heldSend: http.StatusTooManyRequests}
	stub.cost = 0.02
	stub.mu.Unlock()
	agent, _ := crewStubAgent(t, stub)
	return stub, agent
}

func roleBySeat(view TaskRoutingView) map[crewroute.Seat]TaskRoutingRole {
	out := map[crewroute.Seat]TaskRoutingRole{}
	for _, role := range view.Roles {
		out[role.Seat] = role
	}
	return out
}

// THE MIXED-ROLE PIN: only the worker is pinned. The planner and the checker
// share its original send and are not pinned. The held route answers 429; each
// unpinned role has its own rung that answers. The worker's call is what moves
// them. The worker stays on the held send — recorded and dispatched — and is
// never sent to either fallback.
func TestAPinnedWorkerIsNotDraggedOntoAnotherRolesFallback(t *testing.T) {
	stub, agent := holdStub(t)
	crew := mixedCrew(func(c *taskCrew) {
		c.decision.Crew[0].Pinned = true
		// A rung the worker must not take, even though it would answer.
		c.ladders[crewroute.Worker] = []crewroute.Pick{{Seat: crewroute.Worker, Model: "role/worker-ladder", Send: "role/worker-ladder", Provider: "openrouter", Kind: crewroute.Metered}}
	})
	run := dispatchRun(crew)

	err := callOriginal(t, agent, run, crewroute.Worker)
	var stopped crewStopped
	if !errors.As(err, &stopped) {
		t.Fatalf("pinned worker ended %v, asked %v", err, stub.models())
	}
	if got := stub.models(); len(got) != 1 || got[0] != heldSend {
		t.Fatalf("pinned worker asked %v, want only %s", got, heldSend)
	}
	if crew.current().Seat(crewroute.Worker).Send != heldSend || crew.sendForSeat(crewroute.Worker, heldSend) != heldSend {
		t.Fatalf("worker recorded %s dispatched %s", crew.current().Seat(crewroute.Worker).Send, crew.sendForSeat(crewroute.Worker, heldSend))
	}
	if crew.current().Seat(crewroute.Planner).Send != planSend || crew.sendForSeat(crewroute.Planner, heldSend) != planSend {
		t.Fatalf("planner recorded %s dispatched %s", crew.current().Seat(crewroute.Planner).Send, crew.sendForSeat(crewroute.Planner, heldSend))
	}
	if crew.current().Seat(crewroute.Checker).Send != checkSend || crew.sendForSeat(crewroute.Checker, heldSend) != checkSend {
		t.Fatalf("checker recorded %s dispatched %s", crew.current().Seat(crewroute.Checker).Send, crew.sendForSeat(crewroute.Checker, heldSend))
	}
	// A call that names no role still follows the one shared swap.
	if crew.sendFor(heldSend) != planSend || crew.sendForSeat("", heldSend) != planSend {
		t.Fatalf("seatless sendFor %s sendForSeat %s, want %s", crew.sendFor(heldSend), crew.sendForSeat("", heldSend), planSend)
	}
	kinds := map[crewroute.Seat]string{}
	for _, change := range crew.changes {
		kinds[change.Seat] = change.Kind
	}
	if kinds[crewroute.Worker] != TaskCrewChangeHeld || kinds[crewroute.Planner] != TaskCrewChangeFallback || kinds[crewroute.Checker] != TaskCrewChangeFallback {
		t.Fatalf("changes %+v", crew.changes)
	}
	mid := roleBySeat(taskRoutingOf("1", crew.record(), false, 20))
	if mid[crewroute.Worker].Status != TaskRoleUsed || mid[crewroute.Planner].Status != TaskRoleNotRunYet || mid[crewroute.Checker].Status != TaskRoleNotRunYet {
		t.Fatalf("after the worker's call: worker %s planner %s checker %s", mid[crewroute.Worker].Status, mid[crewroute.Planner].Status, mid[crewroute.Checker].Status)
	}
	if mid[crewroute.Worker].Source != TaskRolePinned || mid[crewroute.Worker].Model != heldSend {
		t.Fatalf("worker role %+v", mid[crewroute.Worker])
	}

	if err := callOriginal(t, agent, run, crewroute.Planner); err != nil {
		t.Fatalf("planner: %v (asked %v)", err, stub.models())
	}
	if err := callOriginal(t, agent, run, crewroute.Checker); err != nil {
		t.Fatalf("checker: %v (asked %v)", err, stub.models())
	}
	if got := stub.models(); len(got) != 3 || got[0] != heldSend || got[1] != planSend || got[2] != checkSend {
		t.Fatalf("asked %v, want %s then %s then %s", got, heldSend, planSend, checkSend)
	}
	done := roleBySeat(taskRoutingOf("1", crew.record(), false, 20))
	if done[crewroute.Worker].SpentUSD != 0 || done[crewroute.Planner].SpentUSD != 0.02 || done[crewroute.Checker].SpentUSD != 0.02 {
		t.Fatalf("spend worker %v planner %v checker %v", done[crewroute.Worker].SpentUSD, done[crewroute.Planner].SpentUSD, done[crewroute.Checker].SpentUSD)
	}
	if done[crewroute.Planner].Status != TaskRoleUsed || done[crewroute.Checker].Status != TaskRoleUsed || done[crewroute.Worker].Model != heldSend {
		t.Fatalf("roles worker %+v planner %+v checker %+v", done[crewroute.Worker], done[crewroute.Planner], done[crewroute.Checker])
	}
}

// A pin for this task, with the pick's own pin flag left unset and no ladder
// of its own. The unpinned roles still fall back. The worker's recorded send
// stays the original, so the shared swap must not become its next call.
func TestAPerTaskPinHoldsWhenAnUnpinnedRoleFallsBack(t *testing.T) {
	stub, agent := holdStub(t)
	crew := mixedCrew(func(c *taskCrew) {
		c.taskPins = map[crewroute.Seat]bool{crewroute.Worker: true}
	})
	if crew.decision.Seat(crewroute.Worker).Pinned {
		t.Fatal("this case pins through the task pin, not the pick flag")
	}
	run := dispatchRun(crew)
	err := callOriginal(t, agent, run, crewroute.Worker)
	var stopped crewStopped
	if !errors.As(err, &stopped) {
		t.Fatalf("task-pinned worker ended %v, asked %v", err, stub.models())
	}
	if got := stub.models(); len(got) != 1 || got[0] != heldSend {
		t.Fatalf("task-pinned worker asked %v, want only %s", got, heldSend)
	}
	if crew.sendForSeat(crewroute.Worker, heldSend) != heldSend || crew.current().Seat(crewroute.Worker).Send != heldSend {
		t.Fatalf("worker recorded %s dispatched %s", crew.current().Seat(crewroute.Worker).Send, crew.sendForSeat(crewroute.Worker, heldSend))
	}
	view := roleBySeat(taskRoutingOf("1", crew.record(), false, 20))
	if view[crewroute.Worker].Source != TaskRoleTaskPin || view[crewroute.Planner].Model != planSend || view[crewroute.Checker].Model != checkSend {
		t.Fatalf("worker %+v planner %+v checker %+v", view[crewroute.Worker], view[crewroute.Planner], view[crewroute.Checker])
	}
	if err := callOriginal(t, agent, run, crewroute.Planner); err != nil {
		t.Fatalf("planner: %v (asked %v)", err, stub.models())
	}
	if err := callOriginal(t, agent, run, crewroute.Checker); err != nil {
		t.Fatalf("checker: %v (asked %v)", err, stub.models())
	}
	if got := stub.models(); len(got) != 3 || got[1] != planSend || got[2] != checkSend {
		t.Fatalf("asked %v, want the held send then each role's own fallback", got)
	}
}

// One model for every role: the route fails, nobody moves, and the call is
// not retried on a rung that would have answered.
func TestOneModelHoldsEveryRoleWhenTheRouteFails(t *testing.T) {
	stub, agent := holdStub(t)
	crew := mixedCrew(func(c *taskCrew) {
		c.decision.Mode = crewroute.ModeOne
		c.ladders[crewroute.Worker] = []crewroute.Pick{{Seat: crewroute.Worker, Model: planSend, Send: planSend, Provider: "openrouter", Kind: crewroute.Metered}}
	})
	run := dispatchRun(crew)
	err := callOriginal(t, agent, run, crewroute.Worker)
	var stopped crewStopped
	if !errors.As(err, &stopped) {
		t.Fatalf("one model ended %v, asked %v", err, stub.models())
	}
	if got := stub.models(); len(got) != 1 || got[0] != heldSend {
		t.Fatalf("one model asked %v, want only %s", got, heldSend)
	}
	for _, seat := range crewroute.Seats {
		if crew.current().Seat(seat).Send != heldSend || crew.sendForSeat(seat, heldSend) != heldSend {
			t.Fatalf("%s recorded %s dispatched %s", seat, crew.current().Seat(seat).Send, crew.sendForSeat(seat, heldSend))
		}
	}
	view := roleBySeat(taskRoutingOf("1", crew.record(), false, 20))
	if view[crewroute.Worker].Source != TaskRoleOneModel || view[crewroute.Planner].Model != heldSend {
		t.Fatalf("one model view worker %+v planner %+v", view[crewroute.Worker], view[crewroute.Planner])
	}
}

// answeredCrew is the router behind a live pick, Back to Auto, and Stronger.
// A worker named for Auto this once becomes role/stronger. An explicit other
// send is a pin. Anything else keeps the send the ask already resolved.
func answeredCrew(ask config.CrewAsk) crewroute.Decision {
	worker, pinned := heldSend, false
	switch {
	case ask.AutoSeats[crewroute.Worker]:
		worker = "role/stronger"
	case ask.Sends[crewroute.Worker] != "" && ask.Sends[crewroute.Worker] != heldSend:
		worker, pinned = ask.Sends[crewroute.Worker], true
	case ask.Sends[crewroute.Worker] != "":
		worker = ask.Sends[crewroute.Worker]
	}
	send := func(seat crewroute.Seat, fallback string) string {
		if got := ask.Sends[seat]; got != "" {
			return got
		}
		return fallback
	}
	pick := func(seat crewroute.Seat, model string, pin bool) crewroute.Pick {
		return crewroute.Pick{Seat: seat, Model: model, Send: model, Provider: "openrouter", Kind: crewroute.Metered, EstUSD: 0.02, Pinned: pin}
	}
	return crewroute.Decision{Mode: crewroute.ModeAuto, Crew: []crewroute.Pick{
		pick(crewroute.Worker, worker, pinned),
		pick(crewroute.Planner, send(crewroute.Planner, planSend), false),
		pick(crewroute.Checker, send(crewroute.Checker, checkSend), false),
	}}
}

// After the unpinned roles have fallen back, a person's pick, Back to Auto,
// and Stronger (the worker named for Auto this once) still decide the worker.
// None of them is the planner's fallback.
func TestExplicitPickAndBackToAutoStillNameTheWorker(t *testing.T) {
	var asks []config.CrewAsk
	agent := routerAgent(t, func(ask config.CrewAsk) (crewroute.Decision, error) {
		asks = append(asks, ask)
		return answeredCrew(ask), nil
	})
	crew := mixedCrew(func(c *taskCrew) { c.decision.Crew[0].Pinned = true })
	run := dispatchRun(crew)
	if _, moved := agent.moveCrewSeat(run, heldSend, heldSend, provider.RouteUnavailable, time.Time{}); !moved {
		t.Fatal("unpinned roles did not fall back")
	}
	if crew.sendForSeat(crewroute.Worker, heldSend) != heldSend || crew.sendFor(heldSend) != planSend {
		t.Fatalf("after the move worker %s seatless %s", crew.sendForSeat(crewroute.Worker, heldSend), crew.sendFor(heldSend))
	}
	if _, err := agent.actOnLiveCrew(1, crew, TaskRoutingAction{Kind: TaskRoutingPick, Picks: map[crewroute.Seat]string{crewroute.Worker: "role/person"}}); err != nil {
		t.Fatal(err)
	}
	if crew.sendForSeat(crewroute.Worker, heldSend) != "role/person" || crew.current().Seat(crewroute.Worker).Send != "role/person" {
		t.Fatalf("pick recorded %s dispatched %s", crew.current().Seat(crewroute.Worker).Send, crew.sendForSeat(crewroute.Worker, heldSend))
	}
	if _, err := agent.actOnLiveCrew(1, crew, TaskRoutingAction{Kind: TaskRoutingAuto}); err != nil {
		t.Fatal(err)
	}
	if crew.sendForSeat(crewroute.Worker, heldSend) != heldSend || crew.current().Seat(crewroute.Worker).Pinned || crew.taskPins[crewroute.Worker] {
		t.Fatalf("back to auto dispatched %s pinned %v task pin %v", crew.sendForSeat(crewroute.Worker, heldSend), crew.current().Seat(crewroute.Worker).Pinned, crew.taskPins[crewroute.Worker])
	}
	if _, err := agent.actOnLiveCrew(1, crew, TaskRoutingAction{Kind: TaskRoutingStronger}); err != nil {
		t.Fatal(err)
	}
	if len(asks) == 0 || !asks[len(asks)-1].AutoSeats[crewroute.Worker] {
		t.Fatalf("stronger asked %+v", asks)
	}
	if crew.sendForSeat(crewroute.Worker, heldSend) != "role/stronger" {
		t.Fatalf("stronger dispatched %s", crew.sendForSeat(crewroute.Worker, heldSend))
	}
	if crew.sendForSeat(crewroute.Planner, heldSend) != planSend || crew.sendForSeat(crewroute.Checker, heldSend) != checkSend {
		t.Fatalf("planner %s checker %s", crew.sendForSeat(crewroute.Planner, heldSend), crew.sendForSeat(crewroute.Checker, heldSend))
	}
}
