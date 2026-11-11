package session

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/crewroute"
)

// THE PROJECTION OF ONE ROLE. How it was chosen and whether it ran are facts
// about that seat, read off the record, with no router and no settings.
func TestRoleProjectionNamesSourceAndWhetherThatSeatRan(t *testing.T) {
	if roleSource(crewroute.ModeOne, true, true) != TaskRoleOneModel {
		t.Fatal("one model for every role outranks a pin")
	}
	if roleSource(crewroute.ModeAuto, true, true) != TaskRoleTaskPin {
		t.Fatal("a pin for this task outranks a saved pin")
	}
	if roleSource(crewroute.ModeAuto, false, true) != TaskRolePinned {
		t.Fatal("a saved pin")
	}
	if roleSource(crewroute.ModeAuto, false, false) != TaskRoleAuto {
		t.Fatal("auto")
	}
	if !roleHasRun(true, 0, 0) || !roleHasRun(false, 1, 0) || !roleHasRun(false, 0, 0.01) || roleHasRun(false, 0, 0) {
		t.Fatal("a role ran when its own call started, answered, or spent")
	}
	if roleStatus(true, true, true, true) != TaskRolePending {
		t.Fatal("pending outranks used")
	}
	if roleStatus(false, true, false, true) != TaskRoleUsed {
		t.Fatal("used")
	}
	if roleStatus(false, false, false, true) != TaskRoleUnknown {
		t.Fatal("an older task with no per-role facts is unknown")
	}
	if roleStatus(false, false, true, true) != TaskRoleNotUsed {
		t.Fatal("settled and unused")
	}
	if roleStatus(false, false, true, false) != TaskRoleNotRunYet {
		t.Fatal("not run yet")
	}
}

// THE CREW ESTIMATE. Unknown stays unknown. A factored router figure replaces
// the sum of the roles; without one the sum is the estimate.
func TestRoutedEstimateKeepsTheRoutersFigureAndUnknownApart(t *testing.T) {
	if routedEstimate(crewroute.Decision{}, 1, false) != nil {
		t.Fatal("an unpriced role makes the crew estimate unknown")
	}
	factored := routedEstimate(crewroute.Decision{EstUSD: 0.4, CostFactor: 2}, 0.1, true)
	if factored == nil || *factored != 0.4 {
		t.Fatalf("factored estimate %v", factored)
	}
	summed := routedEstimate(crewroute.Decision{EstUSD: 0.4}, 0.1, true)
	if summed == nil || *summed != 0.1 {
		t.Fatalf("summed estimate %v", summed)
	}
}

// A CHANGE OVER THE CAP needs a cap the person named at least that high.
// Under the cap, nothing is raised.
func TestRaisedTaskCapRefusesOnlyAnEstimateOverTheCap(t *testing.T) {
	raise, err := raisedTaskCap(true, 0.2, 1, 0)
	if err != nil || raise != 0 {
		t.Fatalf("under the cap: %v %v", raise, err)
	}
	if _, err := raisedTaskCap(true, 2, 1, 0); err == nil {
		t.Fatal("over the cap with no raise was allowed")
	}
	raise, err = raisedTaskCap(true, 2, 1, 2)
	if err != nil || raise != 2 {
		t.Fatalf("a named cap at the estimate: %v %v", raise, err)
	}
	plan := routingPlan{known: true, estimate: 2, capUSD: 1}
	if !plan.overCap() {
		t.Fatal("overCap and raisedTaskCap disagree")
	}
}

// SAME FREEZES THE RECORDED SENDS. Stronger freezes the planner and checker
// and asks for a new worker. Retry is not a rerun.
func TestRerunWishFreezesSameAndAsksFreshForBest(t *testing.T) {
	prior := crewroute.Decision{Crew: []crewroute.Pick{
		{Seat: crewroute.Worker, Send: "worker-send"},
		{Seat: crewroute.Planner, Send: "planner-send"},
		{Seat: crewroute.Checker, Send: "checker-send"},
	}}
	same, err := rerunWish(TaskRoutingAction{Kind: TaskRoutingSame}, &prior)
	if err != nil || same.sends[crewroute.Worker] != "worker-send" || same.sends[crewroute.Checker] != "checker-send" {
		t.Fatalf("same: %+v %v", same, err)
	}
	stronger, err := rerunWish(TaskRoutingAction{Kind: TaskRoutingStronger}, &prior)
	if err != nil || stronger.sends[crewroute.Worker] != "" || stronger.sends[crewroute.Planner] != "planner-send" || !stronger.autoSeats[crewroute.Worker] || stronger.stronger == nil {
		t.Fatalf("stronger: %+v %v", stronger, err)
	}
	best, err := rerunWish(TaskRoutingAction{Kind: TaskRoutingBest}, nil)
	if err != nil || best.effort != crewroute.EffortBest || !best.autoSeats[crewroute.Checker] {
		t.Fatalf("best: %+v %v", best, err)
	}
	if _, err := rerunWish(TaskRoutingAction{Kind: TaskRoutingRetry}, &prior); err == nil {
		t.Fatal("retry was accepted as a rerun")
	}
}

// A LIVE CHANGE NAMES WHO MOVES. Stronger holds the planner and checker.
// Auto with nothing pinned is not a change. An empty pick is Auto for that role.
func TestLiveChangeIntentNamesWhoMoves(t *testing.T) {
	cur := crewroute.Decision{Crew: []crewroute.Pick{
		{Seat: crewroute.Worker, Send: "worker-send"},
		{Seat: crewroute.Planner, Send: "planner-send"},
		{Seat: crewroute.Checker, Send: "checker-send"},
	}}
	stronger, targets, err := liveChangeIntent(&cur, nil, TaskRoutingAction{Kind: TaskRoutingStronger})
	if err != nil || stronger.Sends[crewroute.Planner] != "planner-send" || stronger.Sends[crewroute.Worker] != "" || stronger.Stronger == nil || len(targets) != 1 || targets[0] != crewroute.Worker {
		t.Fatalf("stronger intent: %+v %v %v", stronger, targets, err)
	}
	if _, _, err := liveChangeIntent(&cur, nil, TaskRoutingAction{Kind: TaskRoutingAuto}); err == nil {
		t.Fatal("auto with nothing pinned was a change")
	}
	pinned := map[crewroute.Seat]bool{crewroute.Checker: true}
	auto, targets, err := liveChangeIntent(&cur, pinned, TaskRoutingAction{Kind: TaskRoutingAuto})
	if err != nil || len(targets) != 1 || targets[0] != crewroute.Checker || auto.Sends[crewroute.Worker] != "worker-send" {
		t.Fatalf("auto intent: %+v %v %v", auto, targets, err)
	}
	pick, targets, err := liveChangeIntent(&cur, nil, TaskRoutingAction{Kind: TaskRoutingPick, Picks: map[crewroute.Seat]string{crewroute.Worker: ""}})
	if err != nil || pick.Sends[crewroute.Worker] != "" || pick.Sends[crewroute.Planner] != "planner-send" || len(targets) != 1 {
		t.Fatalf("empty pick: %+v %v %v", pick, targets, err)
	}
}

// A PIN AND ONE MODEL FOR EVERY ROLE hold. Auto and an explicit pick record a
// role even when its model stays; stronger does not.
func TestAHeldSeatAndAnUnchangedStrongerStayPut(t *testing.T) {
	if !seatHoldsItsModel(crewroute.Pick{Pinned: true}, crewroute.ModeAuto) || !seatHoldsItsModel(crewroute.Pick{}, crewroute.ModeOne) {
		t.Fatal("a pin and one-model mode hold")
	}
	if seatHoldsItsModel(crewroute.Pick{}, crewroute.ModeAuto) {
		t.Fatal("an unpinned auto role holds")
	}
	if !keepsUnchangedSeat(TaskRoutingStronger) || keepsUnchangedSeat(TaskRoutingAuto) || keepsUnchangedSeat(TaskRoutingPick) {
		t.Fatal("only stronger and best drop a role that did not move")
	}
}
