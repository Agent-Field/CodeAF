package lane

import (
	"math"
	"testing"
	"time"
)

// ── THE FOUR LEVELS, TESTED AS ARITHMETIC ───────────────────────────────────
//
// Every claim the hierarchy makes is a claim about one of three things: how a
// surprise is shared out, how fast each level forgets, and what happens when a
// deployment stops being what it was. Nothing here touches a file or a clock —
// the moment is an argument, so a scenario written in hours runs in
// microseconds.

// waitChains is a first-token hierarchy with nothing in it yet.
func waitChains() *chains { return &chains{Pace: waitPace} }

// TestASurpriseMovesTheLevelThatKnowsNothing is the property the whole design
// turns on, and the reason a cold start needs no special case.
//
// A surprise is shared out in proportion to what each level does not know. The
// first sighting of a provider nobody has measured moves a[lane] a long way and
// μ hardly at all; the thousandth moves e[model, lane] and nothing else. Get
// this backwards and one odd answer from one deployment re-prices the world.
func TestASurpriseMovesTheLevelThatKnowsNothing(t *testing.T) {
	held := waitChains()
	const known, fresh = "Cloudflare", "CoreWeave"
	familiar := ID{Model: scriptedModel, Lane: known}
	// Two hundred consistent answers from one provider: the world's pace, this
	// provider and this model are all now known, and the deployment with them.
	for range 200 {
		held.fold(pairOf(familiar), everyLevel, math.Log(800), 0.05, noon)
	}
	before := held.look(pairOf(ID{Model: scriptedModel, Lane: fresh}), noon)

	// And now a provider nobody has ever measured answers, slowly.
	held.fold(pairOf(ID{Model: scriptedModel, Lane: fresh}), everyLevel, math.Log(6000), 0.05, noon)
	after := held.look(pairOf(ID{Model: scriptedModel, Lane: fresh}), noon)

	moved := func(level Level) float64 { return math.Abs(after[level].X - before[level].X) }
	if moved(LevelLane) <= moved(LevelWorld) {
		t.Fatalf("an unmeasured provider's own term moved %.4f and the world's pace moved %.4f",
			moved(LevelLane), moved(LevelWorld))
	}
	if moved(LevelWorld) > 0.05 {
		t.Fatalf("one answer from one new machine moved the world's pace by %.4f nats", moved(LevelWorld))
	}
	// And being sure is what made the difference: the certain level's share of
	// the surprise is its share of the variance, which is now almost none.
	if held.World.P >= levelVariance(LevelWorld)/10 {
		t.Fatalf("two hundred consistent answers left the world's pace at a variance of %.4f", held.World.P)
	}
}

// TestFourLevelsForgetAtFourRates is what "the belief adapts to the current
// time" means, said four times.
//
// One deployment's queue turns over in minutes, a provider's fleet in the half
// hour a capacity event lasts, a model's size in hours, and the world's pace in
// a day. THE ESTIMATE IS NEVER MOVED: there is no reason to think a lane got
// faster, only a reason to be less sure.
func TestFourLevelsForgetAtFourRates(t *testing.T) {
	const elapsed = 30 * time.Minute
	for level, halfLife := range levelHalfLife {
		held := node{X: 1.5, P: 0.001, At: noon}
		aged := stale(held, Level(level), noon.Add(elapsed))
		want := math.Exp2(elapsed.Seconds() / halfLife.Seconds())
		if got := aged.P / held.P; math.Abs(got-want) > 1e-9 {
			t.Errorf("level %d widened by %.6f over %s; its half-life of %s says %.6f",
				level, got, elapsed, halfLife, want)
		}
		if aged.X != held.X {
			t.Errorf("level %d moved its estimate from %.4f to %.4f by doing nothing", level, held.X, aged.X)
		}
	}
	// The four rates really are four, in the order the design gives them.
	if !(levelHalfLife[LevelPair] < levelHalfLife[LevelLane] &&
		levelHalfLife[LevelLane] < levelHalfLife[LevelModel] &&
		levelHalfLife[LevelModel] < levelHalfLife[LevelWorld]) {
		t.Fatalf("the half-lives are not ordered deployment, provider, model, world: %v", levelHalfLife)
	}
}

// TestAgeingStopsWhereThePriorStands is the other half of forgetting: a belief
// left alone for a week may become worthless and may not become worse than
// free. Without the ceiling the variance doubles for ever and a lane nobody has
// looked at in a fortnight is believed less than a lane nobody has ever heard
// of.
func TestAgeingStopsWhereThePriorStands(t *testing.T) {
	for level := range Level(Levels) {
		held := node{X: 1, P: levelVariance(level), At: noon}
		aged := stale(held, level, noon.Add(30*24*time.Hour))
		if ceiling := SheetWeight * levelVariance(level); aged.P > ceiling+1e-12 {
			t.Errorf("a month of silence left level %d at %.4f, past the ceiling of %.4f", level, aged.P, ceiling)
		}
	}
}

// TestAPairNobodyHasMeasuredStillHasASpreadToWaitAgainst is cold start, said as
// the controller would ask it. A chain that answered nothing here would be a
// request with no clock on it, which is the defect this wave exists to end.
func TestAPairNobodyHasMeasuredStillHasASpreadToWaitAgainst(t *testing.T) {
	held := waitChains()
	chain := held.look(pairOf(ID{Model: "vendor/model", Lane: "nobody-has-heard-of"}), noon)
	if !chain.Known() {
		t.Fatal("a pair nobody has measured had nothing to wait against")
	}
	life := chain.Survival(SpreadFloor, 1000)
	if !life.Known() || life.Sigma < SpreadFloor {
		t.Fatalf("the predictive spread came out at %.4f, under the prior of %.4f", life.Sigma, SpreadFloor)
	}
	if median := math.Exp(life.Mu); median < 0.3 || median > 5 {
		t.Fatalf("with nothing measured the first token was expected in %.2fs", median)
	}
}

// TestAChangePointResetsTheLeafAndNotItsParents is the step the exponential
// forgetting cannot catch, and the discipline that keeps catching it from
// slandering three levels that had nothing to do with it.
//
// A deployment is re-quantized, a region fails over, a provider adds capacity:
// the leaf is no longer what it was and must go back to what its parents say.
// μ, a and b are shared facts held with evidence from every other pair that
// touches them, and a step in one deployment is not evidence about any of them.
func TestAChangePointResetsTheLeafAndNotItsParents(t *testing.T) {
	held := waitChains()
	id := ID{Model: scriptedModel, Lane: "Cloudflare"}
	// A settled deployment: forty consistent answers, none of them a step.
	for n := range 40 {
		if held.note(pairOf(id), math.Log(800), 0.05, noon.Add(time.Duration(n)*time.Second)) {
			t.Fatalf("a steady lane was called a change point on answer %d", n)
		}
	}
	settled := held.look(pairOf(id), noon)

	// And now it is five times slower, answer after answer.
	shifted, at := false, noon
	for n := range 20 {
		at = noon.Add(time.Duration(40+n) * time.Second)
		if held.note(pairOf(id), math.Log(4000), 0.05, at) {
			shifted = true
			break
		}
	}
	if !shifted {
		t.Fatal("a lane that became five times slower was never called a change point")
	}
	after := held.look(pairOf(id), at)

	if after[LevelPair].X != 0 || math.Abs(after[LevelPair].P-levelVariance(LevelPair)) > 1e-12 {
		t.Fatalf("the leaf was not put back to its prior: %+v", after[LevelPair])
	}
	for _, level := range []Level{LevelWorld, LevelLane, LevelModel} {
		if after[level].P >= levelVariance(level) {
			t.Errorf("level %d was reset with the leaf: it is back at a variance of %.4f", level, after[level].P)
		}
		if after[level].X == 0 && settled[level].X != 0 {
			t.Errorf("level %d lost its estimate to a step in one deployment", level)
		}
	}
	// And the sums are cleared, so the next ordinary answer is not a second
	// alarm on the strength of the first one's evidence.
	if _, still := held.Drift[id.String()]; still {
		t.Fatal("the change point fired and left its own evidence behind it")
	}
}

// TestASheetRowIsAnOffsetAndTheBeliefLandsOnIt is the borrowing rule the design
// is most careful about, and the arithmetic that makes a published number
// readable back.
//
// The endpoints sheet is published PER MODEL and a row names one deployment.
// Folding it into μ or a[lane] would let one refresh move every belief this
// process holds — including its opinion of providers the sheet was not about —
// and the sheet refreshes on a beat, for ever.
//
// AND WHAT IT PREDICTS AFTERWARDS IS THE NUMBER IT WAS PUBLISHED WITH. A row is
// an ABSOLUTE first token and a belief is a SUM, so what the deployment's own
// level takes is the OFFSET from what the parents already say — the whole of
// it, not a share. Sharing it left the lane the sheet published at 768ms
// believed at about 1.2s, because μ holds most of the variance and may not
// move; a controller reading that wanted a second request on a lane it had just
// been told was fast.
func TestASheetRowIsAnOffsetAndTheBeliefLandsOnIt(t *testing.T) {
	held := waitChains()
	id := ID{Model: scriptedModel, Lane: "Cloudflare"}
	held.fold(modelOf(id), publishedLevels, math.Log(768), 0.22*SheetWeight, noon)
	if held.World.P != 0 || len(held.Lane) != 0 || len(held.Model) != 0 {
		t.Fatalf("a sheet row moved something wider than the deployment it names: world %+v, lanes %v, models %v",
			held.World, held.Lane, held.Model)
	}
	if len(held.Pair) != 1 {
		t.Fatalf("a sheet row did not reach the deployment it names: %v", held.Pair)
	}
	predicted, variance := held.look(pairOf(id), noon).Predict()
	if got := math.Exp(predicted); math.Abs(got-768) > 1 {
		t.Fatalf("the sheet published 768ms and the belief predicts %.0fms", got)
	}
	// AND IT IS CENTRED THERE WITHOUT BEING SURE OF IT. A half-hour aggregate at
	// a quarter of a sighting's weight says where a lane sits and not how
	// certain to be, so the spread is still the parents'.
	if variance <= levelVariance(LevelWorld) {
		t.Fatalf("one published row left the belief as certain as a measurement: variance %.3f", variance)
	}
	// A pair the sheet did not publish is untouched by one that it did.
	other, _ := held.look(pairOf(ID{Model: scriptedModel, Lane: "somebody-else"}), noon).Predict()
	if math.Abs(math.Exp(other)-1200) > 1 {
		t.Fatalf("a row about one deployment moved another to %.0fms", math.Exp(other))
	}
}

// TestAParentsQualityIsBorrowedAsALeanAndNeverAsCertainty is why the quality
// hierarchy is a prior rather than a sum.
//
// A provider that truncates on one model usually truncates on another, so a
// lane nobody has judged should start leaning the way its provider has been
// shown to lean. What it may NOT inherit is its provider's certainty: a lane
// gated out before it has answered once can never earn the answer that would
// let it back.
func TestAParentsQualityIsBorrowedAsALeanAndNeverAsCertainty(t *testing.T) {
	prior := qualityPrior("fp8")
	var shown tallies
	id := ID{Model: scriptedModel, Lane: "Truncator"}
	for n := range 60 {
		shown.observe(id, n%3 == 0, noon.Add(time.Duration(n)*time.Second), prior)
	}
	start := shown.start(ID{Model: "vendor/other", Lane: "Truncator"}, prior)
	if start.Mean() >= prior.Mean() {
		t.Fatalf("a lane behind a provider that refuses two answers in three started at %.3f", start.Mean())
	}
	if mass := start.A + start.B; math.Abs(mass-(prior.A+prior.B)) > 1e-9 {
		t.Fatalf("the borrowed prior carried %.2f observations of evidence rather than the prior's %.2f",
			mass, prior.A+prior.B)
	}
	// A provider nobody has shown anything about lends nothing.
	if quiet := (tallies{}).start(id, prior); quiet != prior {
		t.Fatalf("a parent with no evidence still moved the prior to %+v", quiet)
	}
}
