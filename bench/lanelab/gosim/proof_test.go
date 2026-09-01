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
	got := runProofSeed(sheetFixture(t), scen, proofCases[1], 7, requests, 100, false, paceShipped, storeCold)
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
