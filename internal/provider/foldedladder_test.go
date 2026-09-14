package provider

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	lanes "github.com/Agent-Field/codeaf/internal/lane"
	"github.com/Agent-Field/codeaf/internal/lane/control"
)

// ── THE RUNG IS A MOVE ──────────────────────────────────────────────────────
//
// docs/design/recovery/DESIGN.md §4's last un-folded loop. The ladder walked
// `relaxationPlan` itself until 2026-09-11 — a private length, under a deadline
// that could not see it, beside a move generator that already knew the rungs —
// and what it does now is ask [control.Next] for its rung like every other move.
// These scenarios are what that has to mean: the same rungs in the same order on
// the wire, ONE history under them, and the plan's deadline over them.

// watchedPlan installs a controller that keeps the plan every arm is built with,
// which is the only way outside this race to read the log an arm is writing —
// and the log is the claim under test.
func watchedPlan(t *testing.T) *control.Plan {
	t.Helper()
	kept := &control.Plan{}
	shipped := lanes.SetController(func(plan control.Plan) control.Controller {
		*kept = plan
		return control.New(plan)
	})
	t.Cleanup(func() { lanes.SetController(shipped) })
	return kept
}

// TestTheLadderTakesItsRungFromTheMoveGenerator stages a router that refuses
// every body still carrying tools, so the whole ladder is climbed.
//
// WHAT IT ASSERTS BEYOND THE OLD LADDER TEST is the plan: each rung reached the
// wire because [control.Next] answered [control.MoveShape], and every one of
// them is written on the SAME move log the machine walk writes into — in rung
// order, starting at one. A ladder with a counter of its own passed the old test
// and would leave this log empty.
func TestTheLadderTakesItsRungFromTheMoveGenerator(t *testing.T) {
	kept := watchedPlan(t)
	client, recorded := refusingClient(t, func(body map[string]any) bool {
		_, hasTools := body["tools"]
		return !hasTools
	}, Config{})

	var notices []string
	ctx := noticeContext(context.Background(), &notices)
	if _, err := client.CompleteWithMessages(ctx, userMessages("hi"), toolRequest()...); err != nil {
		t.Fatalf("the ladder should have landed the call once the tools came off: %v", err)
	}

	// THE RUNGS THE PERSON READ, in the order the design climbs them. The words
	// are the manual's and internal/e2e's, so they are spelled out here rather
	// than derived from the table they came from.
	want := []string{
		"Retry 1/3: relaxed the endpoint filter",
		"Retry 2/3: removed max_tokens",
		"Retry 3/3: removed tools",
	}
	if len(notices) != len(want) {
		t.Fatalf("the ladder said %#v, want %#v", notices, want)
	}
	for index, line := range want {
		if notices[index] != line {
			t.Fatalf("rung %d read %q, want %q", index+1, notices[index], line)
		}
	}
	// AND EVERY RUNG IS A MOVE ON THE PLAN. One shape move per rung, in order,
	// numbered from one — which is what makes a rung and a machine one history
	// rather than two budgets.
	var shapes []control.Move
	for _, move := range kept.Moves.List() {
		if move.Kind == control.MoveShape {
			shapes = append(shapes, move)
		}
	}
	if len(shapes) != len(want) {
		t.Fatalf("the plan recorded %d shape moves, want one per rung: %#v", len(shapes), kept.Moves.List())
	}
	for index, move := range shapes {
		if int(move.Shape) != index+1 {
			t.Fatalf("shape move %d is rung %d, want %d", index, move.Shape, index+1)
		}
	}
	// And the wire agrees: the first body, then one per rung.
	if got := len(recorded.bodies); got != len(want)+1 {
		t.Fatalf("the call made %d requests, want the original and %d rungs", got, len(want))
	}
}

// TestASpentDeadlineStopsTheLadderWhereACountUsedTo is the other half of the
// fold: the ladder is bounded by the plan and by nothing of its own.
//
// A request with rungs left and no deadline left climbs NONE of them — the
// refusal goes back whole — because a rung is a whole request, and a ladder that
// climbed past the moment this build has already decided to stop at is the
// multiplication the wave deleted. It asks the ladder directly rather than
// through a race, because a race stamps its own plan (hedge.go's startArm) and
// what is under test is the bound, not who built it.
func TestASpentDeadlineStopsTheLadderWhereACountUsedTo(t *testing.T) {
	client, recorded := refusingClient(t, func(map[string]any) bool { return false }, Config{})
	plan := lanes.PlanFor(lanes.Choice{}, lanes.Pace{}, lanes.RoleTalk, time.Now())
	plan.Deadline = time.Now().Add(-time.Second)
	ctx := withCallPlan(context.Background(), plan)

	request := &ai.Request{Model: "sim/model", Messages: userMessages("hi")}
	cap := 4096
	request.MaxTokens = &cap
	request.Tools = []ai.ToolDefinition{{Type: "function", Function: ai.ToolFunction{Name: "read"}}}
	knobs := knobsFrom(ctx)
	if _, err := client.recoverFromRefusal(ctx, request, knobs, false, []byte(refusalBody)); err == nil {
		t.Fatal("a refused request with no deadline left should still fail the call")
	}
	if got := len(recorded.bodies); got != 0 {
		t.Fatalf("a spent plan sent %d rungs, want none at all", got)
	}
	for _, move := range plan.Moves.List() {
		if move.Kind == control.MoveShape {
			t.Fatalf("a spent plan climbed a rung: %#v", move)
		}
	}
}

// TestASecondArmCannotDemandAMachineTheFirstOneTried is the race's half of the
// same fold. The claim used to be answered from the race's own `tried` map — a
// second copy of the question the dispatcher under every arm answers from
// [control.Plan.Moves] — and this is that copy's absence, stated as behaviour.
func TestASecondArmCannotDemandAMachineTheFirstOneTried(t *testing.T) {
	plan := lanes.PlanFor(lanes.Choice{}, lanes.Pace{}, lanes.RoleTalk, time.Now())
	plan.Lane = "DeepInfra"
	plan.Alts = []control.Alternative{{Lane: "Fireworks"}, {Lane: "GMICloud"}}
	race := &hedgeRace{model: "openrouter/pool", winner: -1, plan: plan}

	// The first arm is on Fireworks — written down by the dispatcher under it,
	// which is the only writer the race can see and the point of a shared log.
	plan.Moves.Add(control.Move{Kind: control.MoveMachine, Model: race.model, Lane: "Fireworks"})

	lane, why := race.claim("Fireworks", false)
	if equalLane(lane, "Fireworks") {
		t.Fatalf("the race handed out a machine an arm is already on (%q)", why)
	}
	// AND IT STILL ANSWERS WITH THE MACHINE NOBODY HAS. A log that refused
	// everything would pass the line above and be useless.
	if !equalLane(lane, "GMICloud") {
		t.Fatalf("the race claimed %q, want the one machine left", lane)
	}
	// AND A CLAIM THE PURSE THEN REFUSES IS GIVEN BACK, so a machine nothing was
	// ever sent to does not read as tried to the walk under it.
	race.releaseMove(lane)
	if plan.Moves.Tried("GMICloud") {
		t.Fatal("a released claim is still on the move log")
	}
	// And a question with nowhere left says so rather than repeating itself.
	race.claim("", false)
	if lane, why := race.claim("", false); lane != "" || why != "no alt" {
		t.Fatalf("the race claimed %q (%q) with every machine taken", lane, why)
	}
}

// TestTheHeadOfTheChoiceIsAMoveBeforeAnyArmAsksForOne holds the seam that made
// the two lists one: the machine the primary is about to ask is written down as
// a move when the race is built, so a rescue deciding in the same millisecond
// cannot be handed it.
func TestTheHeadOfTheChoiceIsAMoveBeforeAnyArmAsksForOne(t *testing.T) {
	client := poolClient(t, "head", http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusOK)
	}))
	choice := lanes.Choice{Order: []string{"DeepInfra", "Fireworks"}}
	ctx := WithLaneChoice(context.Background(), choice)
	race, made := client.raceFor(ctx, nil, func(plan control.Plan) control.Controller { return control.New(plan) },
		"openrouter/head")
	if !made {
		t.Fatal("a build with a controller factory should race")
	}
	if !race.plan.Moves.Tried("DeepInfra") {
		t.Fatalf("the head of the choice is not on the move log: %#v", race.plan.Moves.List())
	}
	if lane, _ := race.claim("DeepInfra", false); equalLane(lane, "DeepInfra") {
		t.Fatal("a rescue was handed the machine the primary is on")
	}
}

// TestTheMoveLogNamesTheMachineThatAnsweredAndNotTheOneWeAsked is the other
// half of that seam, and without it the head's claim is a lie the walk believes.
//
// `provider.order` IS ADVISORY once `allow_fallbacks` is on, and R1 measured
// this router fanning straight past it (#850): a call that asks for DeepInfra
// and is refused by Parasail has SENT NOTHING to DeepInfra. Leaving the claim on
// the log tells [control.Next] the one machine we actually wanted has been tried
// — on the evidence of a machine we never demanded — and the call ends after one
// attempt with somewhere perfectly good left to go, which is what the rebase
// onto #859 caught.
func TestTheMoveLogNamesTheMachineThatAnsweredAndNotTheOneWeAsked(t *testing.T) {
	kept := watchedPlan(t)
	handler := &poolHandler{
		advisory: true,
		lanes:    []string{"Parasail", "DeepInfra"},
		refusing: map[string]bool{"Parasail": true},
	}
	client := poolClient(t, "served-not-asked", handler)
	ctx := WithLaneChoice(context.Background(), lanes.Choice{Order: []string{"DeepInfra"}})
	if _, err := client.CompleteWithMessages(ctx, userMessages("hello")); err != nil {
		t.Fatalf("the machine we asked for was healthy and the call still failed: %v", err)
	}

	log := kept.Moves.List()
	if len(log) == 0 {
		t.Fatal("a call that walked two machines wrote no moves at all")
	}
	// The pool that REFUSED is on the log, because bytes reached it.
	if !kept.Moves.Tried("Parasail") {
		t.Fatalf("the machine that answered is not on the move log: %#v", log)
	}
	// And the demand the router fanned past was claimed, given back when the
	// refusal named somebody else, and claimed again by the move that really
	// went there — so it is on the log ONCE, as the second move and not the
	// first.
	if !kept.Moves.Tried("DeepInfra") {
		t.Fatalf("the machine the second body really went to is not on the log: %#v", log)
	}
	if last := log[len(log)-1]; !equalLane(last.Lane, "DeepInfra") {
		t.Fatalf("the last move was %q, want the machine the answer came from", last.Lane)
	}
	seen := 0
	for _, move := range log {
		if equalLane(move.Lane, "DeepInfra") {
			seen++
		}
	}
	if seen != 1 {
		t.Fatalf("the demanded machine is on the log %d times, want exactly one: %#v", seen, log)
	}
}
