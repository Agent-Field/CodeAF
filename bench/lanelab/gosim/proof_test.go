package main

// These are the tests of the PROOF ROWS THEMSELVES, and they exist because a
// pass table nobody checks the arithmetic of is a pass table that will one day
// pass for the wrong reason. There are three of them and they are quick: what
// the table counts, that a plan from a cold store is still bounded, and that a
// staged stall is really acted on inside the ceiling on a real wire.
//
// The long run — five cases, three seeds, both doors — is `go run
// ./bench/lanelab/gosim -proof`, and REPORT.md carries what it said.

import (
	"math"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/home"
	"github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/aforge-v2/internal/lane/control"
)

// sheetFixture is the same sheet the committed run draws its world from.
func sheetFixture(t *testing.T) *world {
	t.Helper()
	got, err := loadWorld(filepath.Join("..", "sheets", "deepseek-deepseek-v4-flash.json"))
	if err != nil {
		t.Fatalf("the fixture would not load: %v", err)
	}
	return got
}

// TestThePassTableCountsWhatItSays is the arithmetic of §K's four criteria over
// a scripted set of trials, because every number this lane reports comes out of
// [summariseProof] and nothing else checks it.
func TestThePassTableCountsWhatItSays(t *testing.T) {
	ceiling := proofRole.Ceiling().Seconds()
	got := summariseProof(proofCases[1], []trial{
		// two faults, both acted on inside the ceiling, one with an arm
		{sick: true, acted: true, kind: control.Hedge, reason: "drift", action: 2, silence: 2,
			armed: true, usd: 0.002, waste: 0.001, answered: true},
		{sick: true, acted: true, kind: control.Report, reason: control.CeilingReason, action: ceiling, silence: ceiling,
			usd: 0.001, answered: true},
		// one fault acted on past the ceiling: the row that would fail the gate
		{sick: true, acted: true, kind: control.Report, reason: "drift", action: ceiling + 1,
			silence: ceiling + 1, usd: 0.001},
		// four healthy requests — the two below and the two thinking ones — of
		// which two are armed for no staged reason at all
		{usd: 0.001, answered: true},
		{acted: true, kind: control.Hedge, reason: "first token late", action: 0.7, silence: 0.7,
			armed: true, usd: 0.002, waste: 0.001, answered: true},
		// two legitimate thinking phases, one of which was armed
		{thought: true, survived: true, usd: 0.001, answered: true},
		{thought: true, acted: true, kind: control.Hedge, reason: "long think", action: 2, silence: 2,
			armed: true, usd: 0.002, waste: 0.001, answered: true},
	})
	for _, want := range []struct {
		what string
		got  float64
		want float64
	}{
		{"trials", float64(got.N), 7},
		{"faults", float64(got.Sick), 3},
		{"healthy", float64(got.Well), 4},
		{"acts in the fault window", float64(got.SickActs), 3},
		{"acts past the ceiling", float64(got.OverCeil), 1},
		{"arms", float64(got.Arms), 3},
		{"false hedges, per cent of the healthy half", got.FalseHedgePct, 50},
		{"spend overhead, per cent of the bill", got.SpendPct, 30},
		{"long thinks not armed, per cent", got.ThinkKeptPct, 50},
		{"reports, per cent of every act", got.ReportPct, 40},
		{"time-to-action, p50 of the fault window", got.ActionP50, ceiling},
	} {
		if want.got != want.want {
			t.Errorf("%s: got %g, want %g", want.what, want.got, want.want)
		}
	}
	if got.Whys["drift"] != 2 || got.Whys["long think"] != 1 {
		t.Errorf("the clocks that decided are not tallied: %v", got.Whys)
	}
}

// TestAColdStoreIsStillBounded is the invariant with nothing behind it at all:
// no choice, no belief, no alternative. It is the state the reported defect
// happened in, and the whole claim is that a plan built in it still names a
// moment, and that the moment is inside the role's ceiling.
func TestAColdStoreIsStillBounded(t *testing.T) {
	now := theMoment
	watch := lane.Watching(lane.PlanFor(lane.Choice{}, lane.Pace{}, proofRole, now))
	deadline := watch.Deadline()
	if deadline <= 0 {
		t.Fatalf("a cold store yielded no deadline at all, which is the defect this design exists to remove")
	}
	if deadline > proofRole.Ceiling() {
		t.Fatalf("deadline %v is past %s's ceiling of %v", deadline, proofRole, proofRole.Ceiling())
	}
	if act := watch.Quiet(now.Add(proofRole.Ceiling())); act.Kind == control.None {
		t.Fatalf("nothing was done at the ceiling: %+v", act)
	}
}

// TestAStalledLaneIsActedOnInsideTheCeiling drives the whole thing over a real
// socket at one seed and a handful of requests: the world, the stub, the real
// chooser, the real plan and the real controller. It is the shape of the
// committed run in miniature, and it is here so that a change which stops the
// proof rows acting at all fails the build rather than the report.
func TestAStalledLaneIsActedOnInsideTheCeiling(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	scen, ok := scenarioNamed(proofScenario)
	if !ok {
		t.Fatalf("no scenario called %q", proofScenario)
	}
	const requests = 8
	got := runProofSeed(sheetFixture(t), scen, proofCases[1], 7, requests, 100, false, paceShipped, storeCold, mixStress, policyWaiting, 0)
	if len(got) != requests {
		t.Fatalf("got %d trials, want %d", len(got), requests)
	}
	ceiling := proofRole.Ceiling().Seconds()
	acted := 0
	for index, one := range got {
		if !one.acted {
			continue
		}
		acted++
		if one.silence > ceiling {
			t.Errorf("request %d waited %.3fs of silence before anything was done, past the %gs ceiling",
				index, one.silence, ceiling)
		}
	}
	if acted == 0 {
		t.Fatal("a lane that went quiet for 30 seconds was never acted on")
	}
}

// TestAWarmedStoreLearnsItsPaceThroughTheObservationDoor is the load-bearing
// claim of the warmed arm, checked rather than asserted in prose: a store this
// program warmed knows each pair's pace because it WATCHED it, through
// [lane.Ledger.Note], and what it believes lands where the world really is.
//
// It also pins the reason the sheet's timing is not withheld from that arm. A
// lane's DRAW is published and never observed, so a store primed from the
// sheet's facts alone waits every lane against [lane.SpreadFloor] however many
// answers it has seen — which is the state [storeSeen] stages and the reason it
// is a diagnostic rather than a gate.
//
// THE TWO HALVES GET A HOME EACH. A ledger persists what it was taught and
// restores it on the next Reset, so a second store staged in the first one's
// home would be the first one wearing a different name.
func TestAWarmedStoreLearnsItsPaceThroughTheObservationDoor(t *testing.T) {
	w := sheetFixture(t)
	scen, ok := scenarioNamed(proofScenario)
	if !ok {
		t.Fatalf("no scenario called %q", proofScenario)
	}
	// The stalled row, which is the ordinary staging: the sheet, and a history.
	kase := proofCases[1]

	t.Run("the sheet and a history", func(t *testing.T) {
		t.Setenv(home.EnvVar, t.TempDir())
		lane.Default().Reset()
		defer lane.Default().Reset()
		warm(lane.Default().Ledger(), w, scen, kase, 7, true)

		tight := 0
		for _, l := range w.lanes {
			pace := lane.PaceFor(lane.ID{Model: w.model, Lane: l.name}, theMoment)
			if !pace.First.Known() {
				t.Fatalf("%s: a warmed store believes nothing about a pair it watched %d answers of",
					l.name, warmSightings)
			}
			// The world's own median, in milliseconds, against what the chain
			// came back with. THE TOLERANCE IS THE LANE'S OWN SPREAD, never a
			// flat factor: a hierarchy shrinks a pair whose every answer is
			// noisy toward what its parents say, which is the whole point of
			// having one, and CoreWeave — p50 587 ms, p90 5,247 ms — really is
			// believed at about half its median for that reason.
			_, spread := fit(l.ttft[0], l.ttft[2])
			slack := math.Max(2, math.Exp(spread))
			believed, truth := math.Exp(pace.First.Mu)*1000, math.Exp(l.ttftMu)
			if believed < truth/slack || believed > truth*slack {
				t.Errorf("%s: believed %.0f ms, the world's own median is %.0f ms, and one draw's "+
					"own spread is %.2f nats", l.name, believed, truth, spread)
			}
			if spread >= lane.SpreadFloor {
				continue
			}
			tight++
			if pace.First.Sigma >= lane.SpreadFloor {
				t.Errorf("%s: waited against %.3f nats where the sheet publishes %.3f and the pair "+
					"has been measured %d times", l.name, pace.First.Sigma, spread, warmSightings)
			}
		}
		if tight == 0 {
			t.Fatal("no lane on the fixture publishes a spread under the floor, so this proves nothing")
		}
	})

	t.Run("a history, and no published percentile", func(t *testing.T) {
		t.Setenv(home.EnvVar, t.TempDir())
		lane.Default().Reset()
		defer lane.Default().Reset()
		warm(lane.Default().Ledger(), w, scen, kase, 7, false)
		for _, l := range w.lanes {
			pace := lane.PaceFor(lane.ID{Model: w.model, Lane: l.name}, theMoment)
			if pace.First.Sigma < lane.SpreadFloor {
				t.Fatalf("%s: a pair nobody published a percentile about was waited against %.3f "+
					"nats, under the %.3f floor — the draw has found a source this bench does not "+
					"know about", l.name, pace.First.Sigma, lane.SpreadFloor)
			}
		}
	})
}

// TestTheWasteAndTheRescueAreCountedApart is the arithmetic of the split §K's
// spend clause is now read through. Money an arm cost on a request that was
// never in trouble is WASTE; money an arm cost rescuing a staged fault is what
// the mechanism exists to spend. They sum to the loser spend that was reported
// before, and the gate reads one of them.
func TestTheWasteAndTheRescueAreCountedApart(t *testing.T) {
	got := summariseProof(proofCases[2], []trial{
		// two healthy requests armed for no staged reason: two cents of waste
		{usd: 0.10, waste: 0.01, armed: true, answered: true},
		{usd: 0.10, waste: 0.01, armed: true, answered: true},
		// three staged faults rescued: six cents, and none of it waste
		{sick: true, usd: 0.10, waste: 0.02, armed: true, acted: true, kind: control.Hedge,
			reason: "drift", action: 3, silence: 3, sickThought: true},
		{sick: true, usd: 0.10, waste: 0.02, armed: true, acted: true, kind: control.Hedge,
			reason: "drift", action: 5, silence: 5, sickThought: true},
		{sick: true, usd: 0.10, waste: 0.02, armed: true, acted: true, kind: control.Hedge,
			reason: control.CeilingReason, action: 10, silence: 10, sickThought: true},
		// and one healthy request nobody armed
		{usd: 0.10, answered: true},
	})
	for _, want := range []struct {
		what string
		got  float64
		want float64
	}{
		{"the bill", got.USD, 0.60},
		{"loser spend, as it was reported before the split", got.SpendPct, 100 * 0.08 / 0.60},
		{"waste, on healthy requests", got.WastePct, 100 * 0.02 / 0.60},
		{"rescue, on staged faults", got.RescuePct, 100 * 0.06 / 0.60},
		{"arms on a healthy lane", float64(got.FalseArm), 2},
		{"arms on a staged fault", float64(got.RescueArm), 3},
		{"stalled runs of thought", float64(got.StallThinks), 3},
	} {
		if math.Abs(want.got-want.want) > 1e-9 {
			t.Errorf("%s: got %g, want %g", want.what, want.got, want.want)
		}
	}
	// A percentile of the stalled thoughts alone, and NOT of the row: the three
	// staged faults acted on at 3, 5 and 10 seconds have a median of five.
	if got.StallP50 == nil || *got.StallP50 != 5 {
		t.Errorf("time-to-action on a stalled thought, p50: got %v, want 5", got.StallP50)
	}
	// The two halves are the whole, which is what makes the split a reading of
	// one number rather than a second accounting of it.
	if math.Abs(got.WastePct+got.RescuePct-got.SpendPct) > 1e-9 {
		t.Errorf("waste %.4f + rescue %.4f is not the reported %.4f",
			got.WastePct, got.RescuePct, got.SpendPct)
	}
}

// TestTheNaturalMixStagesTheRateItNames pins the mix itself: one request in
// twenty is a fault, scattered rather than blocked, and never the first — which
// is the request the store is coldest for.
func TestTheNaturalMixStagesTheRateItNames(t *testing.T) {
	const total = 4000
	for _, mix := range []string{mixStress, mixNatural} {
		faults, first := 0, -1
		for index := range total {
			if sickAt(mix, index, total) {
				faults++
				if first < 0 {
					first = index
				}
			}
		}
		// COUNTED EXACTLY, because a rate compared with a tolerance is a rate
		// nobody can check. The natural mix skips request zero on purpose, so it
		// stages one fault fewer than the nominal rate over any run — which is
		// the rate the table prints, to the nearest request.
		want := total / 2
		if mix == mixNatural {
			want = total/naturalPeriod - 1
		}
		if faults != want {
			t.Errorf("%s: staged %d faults of %d, want %d (%.4f of requests, the table says %.4f)",
				mix, faults, total, want, float64(faults)/total, rateOfMix(mix))
		}
		if first == 0 {
			t.Errorf("%s: the first request of the run is a staged fault", mix)
		}
	}
}

// TestAvoidableIsWhatAControllerCouldHaveNotSpent is the arithmetic of the one
// cost figure a gate now reads. An arm on a healthy request bought nothing
// whether it won or lost; an arm that rescued a stall and ANSWERED it bought
// the answer and is not avoidable; an arm that went out on a stall and then
// lost to the lane it was rescuing is money gone.
func TestAvoidableIsWhatAControllerCouldHaveNotSpent(t *testing.T) {
	got := summariseProof(proofCases[1], []trial{
		// healthy, armed, and the arm won: still avoidable, nothing was wrong
		{usd: 0.10, waste: 0.01, armed: true, armPrice: 0.05, armWon: true, answered: true},
		// healthy, armed, and the arm lost: avoidable
		{usd: 0.10, waste: 0.05, armed: true, armPrice: 0.05, answered: true},
		// a rescue that answered: NOT avoidable, it bought the answer
		{sick: true, usd: 0.10, waste: 0.05, armed: true, armPrice: 0.05, armWon: true,
			acted: true, kind: control.Hedge, reason: "drift", action: 3, answered: true},
		// a rescue that lost to the lane it was rescuing: avoidable
		{sick: true, usd: 0.10, waste: 0.05, armed: true, armPrice: 0.05,
			acted: true, kind: control.Hedge, reason: "drift", action: 3, answered: true},
		// and a baseline retry, thrown away whole by the transport's guard
		{sick: true, usd: 0.20, waste: 0.10, avoidableRetry: 0.10, answered: true},
	})
	if want := 0.05 + 0.05 + 0.05 + 0.10; math.Abs(got.Avoidable-want) > 1e-9 {
		t.Errorf("avoidable = %g, want %g (two healthy arms, one lost rescue, one cut retry)",
			got.Avoidable, want)
	}
	if got.ArmsLost != 1 {
		t.Errorf("arms on a fault that lost = %d, want 1", got.ArmsLost)
	}
	// And it is strictly less than the loser spend, which is the whole reason
	// the two are different numbers.
	if got.Avoidable >= got.Waste+1e-9 {
		t.Errorf("avoidable %g is not under the loser spend %g", got.Avoidable, got.Waste)
	}
}

// TestTheTransportBoundClearsTheControllersCeiling is the one property the
// baseline arm depends on: the guard it waits for is the LAST resort, always
// well past the moment a controller would have acted. If it were not, the
// baseline would be a controller with a worse constant rather than a floor.
func TestTheTransportBoundClearsTheControllersCeiling(t *testing.T) {
	for _, wrote := range []bool{false, true} {
		bound := transportBound(proofRole, wrote)
		if bound <= proofRole.Ceiling() {
			t.Errorf("wrote=%v: the transport bound %v does not clear the %v ceiling",
				wrote, bound, proofRole.Ceiling())
		}
		if bound < 2*proofRole.Ceiling() {
			t.Errorf("wrote=%v: the transport bound %v is under twice the ceiling, which is the "+
				"headroom internal/provider floors it at", wrote, bound)
		}
	}
}
