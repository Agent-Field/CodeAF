package lane

import (
	"go/ast"
	"math"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/lane/control"
)

// The watch is pure, so every test here states a moment rather than waiting for
// one: the whole file runs in microseconds and none of it is about the machine
// it ran on.
//
// WHAT IS BEING TESTED HERE IS THE ADAPTER. The arithmetic itself lives in
// `internal/lane/control` and is tested against its own closed form there; this
// file is about the translation — a choice and a belief becoming a plan, a
// stream's totals becoming readings, and the older narrow answer the transport
// still reads.

func msIn(base time.Time, ms int) time.Time {
	return base.Add(time.Duration(ms) * time.Millisecond)
}

// beliefOf builds a belief whose median first token is ttft milliseconds and
// whose median rate is tokens a second, both believed reasonably firmly.
func beliefOf(ttft, rate float64) Belief {
	return Belief{
		TTFT: Posterior{X: math.Log(ttft), P: 0.04},
		Rate: Posterior{X: math.Log(rate), P: 0.04},
	}
}

// raced is what the chooser hands a request it has an opinion about: A first, B
// behind it, with the numbers each was scored on.
func raced() Choice {
	return Choice{
		Order: []string{"A", "B"},
		Frontier: []Scored{
			{ID: ID{Model: "m", Lane: "A"}, TTFT: 400, Rate: 50, Price: 0.002},
			{ID: ID{Model: "m", Lane: "B"}, TTFT: 500, Rate: 50, Price: 0.002},
		},
	}
}

// ── ONE CONTROLLER, INSTALLED ONCE ──────────────────────────────────────────

// TestTheControllerIsInstalledExactlyOnce is the seam's own law.
//
// A build with no controller sends every token-generating call into the bare
// stream loop, which is the defect this wave exists to end. A build with two
// would be a build where "when does this act" has two answers and the surface's
// countdown expires at a moment nothing happens at.
func TestTheControllerIsInstalledExactlyOnce(t *testing.T) {
	if Controller() == nil {
		t.Fatal("no controller is installed, so every call falls through to the bare stream loop")
	}
	fset, files := sources(t)
	installs := 0
	for name, file := range files {
		if isTest(name) {
			continue
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			if ident, ok := call.Fun.(*ast.Ident); ok && ident.Name == "SetController" {
				if name != "waiting.go" {
					t.Errorf("%s installs the controller; the seam is waiting.go's", fset.Position(call.Pos()))
				}
				installs++
			}
			return true
		})
	}
	if installs != 1 {
		t.Fatalf("the controller is installed %d times, want exactly once", installs)
	}
}

// ── THE PLAN ────────────────────────────────────────────────────────────────

// TestThePlanIsTheRolesAndTheFrontiers: the choice says which lane and the role
// says how long, and neither of them is allowed to say the other.
func TestThePlanIsTheRolesAndTheFrontiers(t *testing.T) {
	now := time.Now()
	plan := PlanFor(raced(), PaceOf(beliefOf(400, 50)), RoleStanding, now)
	if plan.Lane != "A" {
		t.Errorf("lane = %q, want the head of the order", plan.Lane)
	}
	if plan.Ceiling != RoleStanding.Ceiling() || plan.Floor != ActionFloor {
		t.Errorf("bounds = %s/%s, want the role's ceiling and the action floor", plan.Ceiling, plan.Floor)
	}
	if plan.Lambda != RoleStanding.Lambda() {
		t.Errorf("λ = %g, want the role's %g", plan.Lambda, RoleStanding.Lambda())
	}
	if len(plan.Alts) != 1 || plan.Alts[0].Lane != "B" {
		t.Fatalf("alternatives = %+v, want the frontier without the head", plan.Alts)
	}
	if got := plan.Alts[0]; got.Rate != 50 || got.Extra != 0.002 {
		t.Errorf("alternative = %+v, want the numbers the frontier scored it on", got)
	}
	// A median read as a mean would price every rescue as cheaper than it is.
	if got := plan.Alts[0].First.Mean(); math.Abs(got-0.5) > 1e-9 {
		t.Errorf("the alternative is expected to start in %gs, want the frontier's 0.5", got)
	}
}

// TestEveryPlanHasACeilingEvenWithNoChoiceAtAll is the root cause of the
// reported three-minute wait, said about the type that fixes it.
func TestEveryPlanHasACeilingEvenWithNoChoiceAtAll(t *testing.T) {
	for _, role := range Roles() {
		plan := PlanFor(Choice{}, Pace{}, role, time.Now())
		if plan.Ceiling != role.Ceiling() || plan.Ceiling <= 0 {
			t.Errorf("role %q: a request with no opinion about where to go got a ceiling of %s", role, plan.Ceiling)
		}
	}
}

// TestThePredictiveSpreadIsFloored: a posterior's variance is the variance of
// the ESTIMATE, and a controller handed it would believe a tail impossible.
func TestThePredictiveSpreadIsFloored(t *testing.T) {
	certain := Belief{TTFT: Posterior{X: math.Log(400), P: 1e-6}, Rate: Posterior{X: math.Log(50), P: 1e-6}}
	certainly := PaceOf(certain)
	if certainly.First.Sigma != SpreadFloor || certainly.Gap.Sigma != SpreadFloor {
		t.Fatalf("spreads = %g and %g, want the floor of %g", certainly.First.Sigma, certainly.Gap.Sigma, SpreadFloor)
	}
	wide := Belief{TTFT: Posterior{X: math.Log(400), P: 4}, Rate: Posterior{X: math.Log(50), P: 4}}
	if got := PaceOf(wide).First; got.Sigma != 2 {
		t.Fatalf("a genuinely wide belief was narrowed to %g", got.Sigma)
	}
	if blank := PaceOf(Belief{}); blank.First.Known() || blank.Gap.Known() {
		t.Fatal("an empty belief invented a distribution")
	}
}

// ── ACTING ──────────────────────────────────────────────────────────────────

// TestALaneNothingIsBelievedAboutIsStillBounded is the invariant from zero
// history: the ceiling exists whether or not a belief does.
func TestALaneNothingIsBelievedAboutIsStillBounded(t *testing.T) {
	start := time.Now()
	watch := NewWatch(raced(), Belief{}, start)
	for ms := 0; ms <= 20_000; ms += 50 {
		if act := watch.Quiet(msIn(start, ms)); act.Kind != control.None {
			if want := int(RoleTalk.Ceiling() / time.Millisecond); ms != want {
				t.Fatalf("acted at %dms, want the ceiling at %dms", ms, want)
			}
			return
		}
	}
	t.Fatal("a lane nobody has ever measured was never acted on")
}

// TestAFirstTokenPastTheCrossingIsHedged, and the crossing is the one the
// arithmetic names rather than a constant in this file.
func TestAFirstTokenPastTheCrossingIsHedged(t *testing.T) {
	start := time.Now()
	choice, belief := raced(), beliefOf(400, 50)
	plan := PlanFor(choice, PaceOf(belief), RoleTalk, start)
	cost := plan.Alts[0].First.Mean() + plan.Lambda*plan.Alts[0].Extra + plan.Margin

	crossing := 0
	for ms := int(ActionFloor / time.Millisecond); ms <= 10_000; ms++ {
		if plan.First.Remaining(float64(ms)/1000) > cost {
			crossing = ms
			break
		}
	}
	if crossing == 0 {
		t.Fatal("the scripted belief never crosses, so this test proves nothing")
	}

	watch := NewWatch(choice, belief, start)
	if verdict := watch.Silence(msIn(start, crossing-10)); verdict.Hedge {
		t.Fatalf("hedged at %dms, before the crossing at %dms", crossing-10, crossing)
	}
	verdict := watch.Silence(msIn(start, crossing))
	if !verdict.Hedge || verdict.Reason != "first token late" {
		t.Fatalf("verdict at the crossing = %+v, want a hedge for a late first token", verdict)
	}
	if watch.Last().Lane != "B" {
		t.Fatalf("the rescue went to %q, want the frontier's alternative", watch.Last().Lane)
	}
	if watch.PathFault() {
		t.Fatal("a late first token is a slow lane, not a dead path")
	}
	if !watch.Hedged() {
		t.Fatal("Hedged = false after a hedge went out")
	}
}

// TestARequestMayEarnMoreThanOneArm is what retired the once-only boolean: a
// request gets as many arms as the frontier has lanes and the purse will pay
// for, and not one hedge because a field said so.
func TestARequestMayEarnMoreThanOneArm(t *testing.T) {
	start := time.Now()
	choice := raced()
	choice.Order = append(choice.Order, "C")
	choice.Frontier = append(choice.Frontier, Scored{ID: ID{Model: "m", Lane: "C"}, TTFT: 600, Rate: 50, Price: 0.002})
	watch := NewWatch(choice, beliefOf(400, 50), start)
	var arms []string
	for ms := 0; ms <= 20_000; ms += 20 {
		if verdict := watch.Silence(msIn(start, ms)); verdict.Hedge {
			arms = append(arms, watch.Last().Lane)
		}
	}
	if len(arms) != 2 || arms[0] != "B" || arms[1] != "C" {
		t.Fatalf("arms went to %v, want each alternative once and in the frontier's order", arms)
	}
}

// TestNoHeartbeatAndNoByteIsAPathFaultTheLaneIsNotChargedFor.
func TestNoHeartbeatAndNoByteIsAPathFaultTheLaneIsNotChargedFor(t *testing.T) {
	start := time.Now()
	watch := NewWatch(raced(), beliefOf(400, 50), start)
	acted := 0
	for ms := 0; ms <= 20_000; ms += 50 {
		if verdict := watch.Silence(msIn(start, ms)); verdict.Hedge {
			acted = ms
			break
		}
	}
	if acted == 0 {
		t.Fatal("a stream that said nothing at all was never acted on")
	}
	if acted < int(deadPathFloor/time.Millisecond) {
		t.Skipf("the crossing at %dms is inside the dead-path floor, so there is no claim to make", acted)
	}
	if !watch.PathFault() {
		t.Fatal("PathFault = false; a stream with no sign of life says nothing about the lane")
	}
	if got := watch.Last().Reason; got != "no heartbeat" {
		t.Fatalf("reason = %q, want the path blamed", got)
	}
}

// TestAHeartbeatKeepsThePathAliveWhileTheLaneIsStillJudged: the comment line is
// proof about the PATH and about nothing else, so it clears the fault and moves
// no clock.
func TestAHeartbeatKeepsThePathAliveWhileTheLaneIsStillJudged(t *testing.T) {
	start := time.Now()
	beaten := NewWatch(raced(), beliefOf(400, 50), start)
	bare := NewWatch(raced(), beliefOf(400, 50), start)
	for ms := 50; ms <= 20_000; ms += 50 {
		beaten.Heartbeat(msIn(start, ms))
		mine, theirs := beaten.Silence(msIn(start, ms)), bare.Silence(msIn(start, ms))
		if mine != theirs {
			t.Fatalf("at %dms a heartbeat changed the answer: %+v against %+v", ms, mine, theirs)
		}
		if mine.Hedge {
			if beaten.PathFault() {
				t.Fatal("PathFault = true on a path that was heartbeating all along")
			}
			return
		}
	}
	t.Fatal("a stream that only ever heartbeat was never acted on")
}

// TestAVisibleTokenStartsTheWaitAgainAndAThoughtDoesNot is the measured defect:
// a run of reasoning is the endpoint writing where nobody can read.
func TestAVisibleTokenStartsTheWaitAgainAndAThoughtDoesNot(t *testing.T) {
	start := time.Now()
	watch := NewWatch(raced(), beliefOf(400, 50), start)
	for thought := 1; thought <= 100; thought++ {
		watch.Token(thought, 0, msIn(start, 4*thought))
	}
	if watch.Phase() != control.PhaseThinking {
		t.Fatalf("phase = %d after a hundred thoughts, want thinking", watch.Phase())
	}
	// A hundred thoughts bought no progress at all: the ceiling is still the
	// one the request went out under.
	if got := watch.DeadlineAt(); got.After(start.Add(RoleTalk.Ceiling())) {
		t.Fatalf("the deadline moved to %s past the request; thinking stopped the clock", got.Sub(start))
	}
	before := watch.DeadlineAt()
	watch.Token(101, 1, msIn(start, 500))
	if watch.Phase() != control.PhaseWriting {
		t.Fatalf("phase = %d after a word, want writing", watch.Phase())
	}
	if got := watch.DeadlineAt(); !got.After(before) {
		t.Fatalf("a word on the screen left the deadline where the thoughts had it, at %s", before.Sub(start))
	}
}

// TestASteadyLaneIsNeverHedged: a lane writing exactly as believed costs
// nobody a second request.
func TestASteadyLaneIsNeverHedged(t *testing.T) {
	start := time.Now()
	watch := NewWatch(raced(), beliefOf(400, 50), start)
	moment := 400
	watch.Token(1, 1, msIn(start, moment))
	for token := 2; token <= 400; token++ {
		moment += 20
		if verdict := watch.Token(token, token, msIn(start, moment)); verdict.Hedge {
			t.Fatalf("hedged at token %d on a lane writing exactly as believed", token)
		}
		if verdict := watch.Silence(msIn(start, moment+10)); verdict.Hedge {
			t.Fatalf("hedged ten milliseconds into an ordinary gap at token %d", token)
		}
	}
}

// TestAStallMidAnswerIsActedOnWhileItIsStillHappening: a lane that goes quiet
// delivers no token to notice it with, so the beat is what notices.
func TestAStallMidAnswerIsActedOnWhileItIsStillHappening(t *testing.T) {
	start := time.Now()
	watch := NewWatch(raced(), beliefOf(400, 50), start)
	watch.Token(1, 1, msIn(start, 400))
	acted := 0
	for ms := 420; ms <= 60_000; ms += 20 {
		if verdict := watch.Silence(msIn(start, ms)); verdict.Hedge {
			acted = ms - 400
			break
		}
	}
	if acted == 0 {
		t.Fatal("a stream that stopped writing was never acted on")
	}
	if acted > int(RoleTalk.Ceiling()/time.Millisecond) {
		t.Fatalf("the stall ran %dms, past a ceiling of %s", acted, RoleTalk.Ceiling())
	}
}

// TestTheTextOnTheScreenIsWhatBuysCommitment, which is what replaced sixty-four
// tokens.
//
// The rewrite term of the inequality grows with the answer, so an arm that has
// written a lot is left alone longer than one that has written a little — for
// the same reason, out of the same arithmetic, and without a constant that is
// right for one answer length and wrong for another.
func TestTheTextOnTheScreenIsWhatBuysCommitment(t *testing.T) {
	start := time.Now()
	// An alternative believed to write slowly, so redoing the answer there is
	// expensive in proportion to how much of it there is.
	choice := raced()
	choice.Frontier[1].Rate = 4
	// A role with the patience to let the arithmetic answer. On a talk turn the
	// ten-second ceiling gets there first, which is the invariant doing its job
	// and not the commitment rule failing to.
	stall := func(written int) int {
		plan := PlanFor(choice, PaceOf(beliefOf(400, 50)), RoleTalk, start)
		plan.Ceiling = time.Minute
		watch := Watching(plan)
		moment := 400
		for token := 1; token <= written; token++ {
			watch.Token(token, token, msIn(start, moment))
			moment += 20
		}
		for ms := moment; ms <= 120_000; ms += 20 {
			if watch.Silence(msIn(start, ms)).Hedge {
				return ms - (moment - 20)
			}
		}
		return -1
	}
	early, late := stall(5), stall(300)
	if early < 0 || late < 0 {
		t.Fatalf("a stall was never acted on: %dms and %dms", early, late)
	}
	if late <= early {
		t.Fatalf("three hundred words bought %dms of patience and five bought %dms", late, early)
	}
}

// TestWithNobodyToHedgeToTheWaitIsReportedRatherThanHedged: one lane is a real
// state and a common one, and silence is not an option.
func TestWithNobodyToHedgeToTheWaitIsReportedRatherThanHedged(t *testing.T) {
	start := time.Now()
	only := Choice{Only: []string{"A"}, Frontier: []Scored{{ID: ID{Model: "m", Lane: "A"}, TTFT: 400, Rate: 50}}}
	watch := NewWatch(only, beliefOf(400, 50), start)
	if watch.Alt() != "" {
		t.Fatalf("Alt = %q with nothing on the frontier but the head", watch.Alt())
	}
	reported := false
	for ms := 0; ms <= 30_000; ms += 50 {
		act := watch.Quiet(msIn(start, ms))
		if act.Kind == control.Hedge {
			t.Fatalf("hedged at %dms with no alternative named", ms)
		}
		reported = reported || act.Kind == control.Report
	}
	if !reported {
		t.Fatal("a request with nowhere to go said nothing about its own wait")
	}
}

// TestAPinnedChoiceAsksRatherThanHedging: a person who named a machine is owed
// that machine.
func TestAPinnedChoiceAsksRatherThanHedging(t *testing.T) {
	start := time.Now()
	choice := raced()
	choice.Only = []string{"A"}
	plan := PlanFor(choice, PaceOf(beliefOf(400, 50)), RoleTalk, start)
	plan.Pinned = true
	watch := Watching(plan)
	for ms := 0; ms <= 30_000; ms += 50 {
		switch act := watch.Quiet(msIn(start, ms)); act.Kind {
		case control.None:
		case control.Ask:
			if act.Lane != "B" {
				t.Fatalf("the offer named %q, want the lane the frontier did", act.Lane)
			}
			if !watch.Asked() || watch.Hedged() {
				t.Fatal("a pinned lane was overridden rather than asked")
			}
			return
		default:
			t.Fatalf("a pinned lane produced %d after %dms", act.Kind, ms)
		}
	}
	t.Fatal("a pinned lane that said nothing at all never raised an offer")
}

// TestTheDeadlineIsNeverInThePastAndNeverPastTheCeiling.
func TestTheDeadlineIsNeverInThePastAndNeverPastTheCeiling(t *testing.T) {
	start := time.Now()
	for _, test := range []struct {
		name   string
		belief Belief
	}{
		{"nothing believed", Belief{}},
		{"an ordinary lane", beliefOf(400, 50)},
		{"a lane believed to take an hour", beliefOf(3_600_000, 1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			watch := NewWatch(raced(), test.belief, start)
			for ms := 0; ms <= int(RoleTalk.Ceiling()/time.Millisecond); ms += 100 {
				now := msIn(start, ms)
				watch.Quiet(now)
				deadline := watch.DeadlineAt()
				if deadline.Before(now) {
					t.Fatalf("at %dms the deadline was %s in the past", ms, now.Sub(deadline))
				}
				if deadline.After(start.Add(RoleTalk.Ceiling())) {
					t.Fatalf("at %dms the deadline was %s, past the ceiling", ms, deadline.Sub(start))
				}
			}
			if got := watch.Deadline(); got <= 0 || got > RoleTalk.Ceiling() {
				t.Fatalf("Deadline = %s, want a wait inside the ceiling", got)
			}
		})
	}
}

// ── THE BUDGET ──────────────────────────────────────────────────────────────

func TestTheAllowanceIsCountedInRequestsAndNotInMinutes(t *testing.T) {
	now := time.Now()
	budget := NewBudget(2, 0)
	if !budget.Allow(now, 0) || !budget.Allow(now, 0) {
		t.Fatalf("a fresh budget refused one of its first two hedges")
	}
	if budget.Allow(now, 0) {
		t.Fatalf("a third hedge went out inside the same twenty requests")
	}
	// TIME ALONE BUYS NOTHING. This is the correction the simulator forced: a
	// slow batch of requests must not refill its own allowance while it runs.
	if budget.Allow(now.Add(time.Hour), 0) {
		t.Fatalf("an hour of waiting refilled an allowance that is counted in requests")
	}
	// Requests do. Nineteen of them still hold the first two hedges inside the
	// window; the twentieth slides the first one out.
	for range 19 {
		budget.NoteRequest(now)
	}
	if budget.Allow(now, 0) {
		t.Fatalf("a hedge went out with two still inside the last twenty requests")
	}
	budget.NoteRequest(now)
	if !budget.Allow(now, 0) {
		t.Fatalf("the oldest hedge never slid out of the window")
	}
}

func TestTheAllowanceHoldsAtAboutOneHedgeInTen(t *testing.T) {
	now := time.Now()
	budget := NewBudget(2, 0)
	hedges := 0
	for range 200 {
		if budget.Allow(now, 0) {
			hedges++
		}
		budget.NoteRequest(now)
	}
	// Two in the first twenty and one in every ten after them.
	if hedges < 18 || hedges > 22 {
		t.Fatalf("%d hedges in two hundred requests, want about one in ten", hedges)
	}
}

func TestAZeroAllowanceIsHowHedgingIsSwitchedOff(t *testing.T) {
	if NewBudget(0, 0.5).Allow(time.Now(), 0) {
		t.Fatalf("a budget of nothing allowed a hedge")
	}
	var nothing *Budget
	if nothing.Allow(time.Now(), 0) {
		t.Fatalf("a nil budget allowed a hedge")
	}
}

func TestTheShareRefusesOnceHedgingHasHadItsTenthOfTheBill(t *testing.T) {
	now := time.Now()
	budget := NewBudget(60, 0.10)
	budget.NoteSpend(1.00, now)
	if !budget.Allow(now, 0.05) {
		t.Fatalf("refused a five-cent hedge against a dollar of spending")
	}
	budget.NoteHedge(0.09, now)
	if budget.Allow(now, 0.05) {
		t.Fatalf("allowed a hedge that would take the share past a tenth")
	}
	// And an hour later the window has rolled: neither the spending nor the
	// hedging that was in it is judged any more.
	later := now.Add(2 * time.Hour)
	if !budget.Allow(later, 0.05) {
		t.Fatalf("the share was still judging an hour-old bill")
	}
}

func TestWithNothingSpentTheRequestAllowanceAloneGoverns(t *testing.T) {
	now := time.Now()
	budget := NewBudget(2, 0.10)
	if !budget.Allow(now, 0.02) {
		t.Fatalf("refused the first hedge of a session that has billed nothing yet")
	}
}
