package lane

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/home"
)

// ── THE ONE PIECE OF ARITHMETIC THIS WAVE SHIPPED ───────────────────────────

// TestThroughputStopsBeingWorthAnythingAtReadingSpeed is the fact the whole
// objective turns on: for text a person reads, two lanes above the reading rate
// are the SAME SPEED, and the cheaper one should therefore win.
func TestThroughputStopsBeingWorthAnythingAtReadingSpeed(t *testing.T) {
	quick := PerceivedSeconds(0.8, 75, 400, 0)
	slower := PerceivedSeconds(0.8, 58, 400, 0)
	if quick != slower {
		t.Fatalf("75 t/s felt like %.3fs and 58 t/s like %.3fs; above reading speed they are the same wait", quick, slower)
	}
	want := 0.8 + 400/ReadRate
	if math.Abs(quick-want) > 1e-9 {
		t.Fatalf("a visible answer was priced at %.3fs rather than at reading speed's %.3fs", quick, want)
	}
}

// TestHiddenTokensAreWorthTheirFullRate is the other half: reasoning and
// tool-call JSON are pure waiting, so a faster lane really is faster for work.
func TestHiddenTokensAreWorthTheirFullRate(t *testing.T) {
	quick := PerceivedSeconds(0.8, 75, 0, 2000)
	slower := PerceivedSeconds(0.8, 27, 0, 2000)
	if !(quick < slower) {
		t.Fatalf("hidden work felt the same at 75 and 27 t/s: %.2fs and %.2fs", quick, slower)
	}
	if math.Abs(quick-(0.8+2000.0/75)) > 1e-9 {
		t.Fatalf("hidden tokens were not priced at the full rate: %.3f", quick)
	}
}

// TestALaneThatNeverFinishesIsSaidToNeverFinish keeps a zero rate from becoming
// a large finite number somebody then compares against another lane.
func TestALaneThatNeverFinishesIsSaidToNeverFinish(t *testing.T) {
	if !math.IsInf(PerceivedSeconds(1, 0, 100, 0), 1) {
		t.Fatal("a lane writing nothing was given a finite wait")
	}
}

// ── THE FILTER ──────────────────────────────────────────────────────────────

// TestAPosteriorThatKnowsNothingSaysSo holds the emptiness law at the one place
// it would be most tempting to break: a router steering on an invented number
// draws it in the picker as a fact.
func TestAPosteriorThatKnowsNothingSaysSo(t *testing.T) {
	var nothing Posterior
	if nothing.Known() || nothing.Mean() != 0 || nothing.Quantile(1.28) != 0 {
		t.Fatalf("an unbelieved posterior answered with numbers: %+v", nothing)
	}
	if (Beta{}).Known() || (Beta{}).Mean() != 0 {
		t.Fatal("an unobserved quality answered with a number")
	}
}

// TestTheFirstObservationIsAdoptedOutright is the limit that a naive gain would
// get wrong: with no belief the gain is zero and the first measurement would be
// thrown away.
func TestTheFirstObservationIsAdoptedOutright(t *testing.T) {
	got := Posterior{}.Update(math.Log(800), 0.25)
	if math.Abs(got.X-math.Log(800)) > 1e-12 || got.P != 0.25 {
		t.Fatalf("the first sighting was not adopted: %+v", got)
	}
	if math.Abs(got.Mean()-800) > 1e-9 {
		t.Fatalf("the belief read back as %.3f rather than 800ms", got.Mean())
	}
}

// TestAnObservationMovesTheBeliefAndSharpensIt is the update itself.
func TestAnObservationMovesTheBeliefAndSharpensIt(t *testing.T) {
	before := Posterior{X: math.Log(800), P: 0.25}
	after := before.Update(math.Log(3200), 0.25)
	if !(after.X > before.X) {
		t.Fatal("a slower sighting did not move the belief slower")
	}
	if !(after.X < math.Log(3200)) {
		t.Fatal("one slow sighting moved the belief the whole way, which is a strike and not a filter")
	}
	if !(after.P < before.P) {
		t.Fatalf("an observation did not sharpen the belief: %.4f to %.4f", before.P, after.P)
	}
	// Equal noise and equal variance is the halfway case, in the log domain:
	// the geometric mean of 800 and 3200.
	if math.Abs(after.Mean()-1600) > 1e-6 {
		t.Fatalf("the halfway belief read back as %.2f rather than 1600ms", after.Mean())
	}
}

// TestForgettingIsLosingConfidenceAndNeverChangingTheEstimate is the law that
// replaces the penalty box: a belief nobody has fed goes vague, not wrong.
func TestForgettingIsLosingConfidenceAndNeverChangingTheEstimate(t *testing.T) {
	before := Posterior{X: math.Log(4000), P: 0.2}
	after := before.Predict(10*time.Minute, 10*time.Minute)
	if after.X != before.X {
		t.Fatal("ageing moved the estimate, which is a claim nobody measured")
	}
	if math.Abs(after.P-0.4) > 1e-9 {
		t.Fatalf("one half-life left the variance at %.4f rather than doubling it", after.P)
	}
	if math.Abs(before.Predict(20*time.Minute, 10*time.Minute).P-0.8) > 1e-9 {
		t.Fatal("two half-lives did not double twice")
	}
	if (Posterior{}).Predict(time.Hour, time.Minute).Known() {
		t.Fatal("ageing invented a belief out of nothing")
	}
}

// TestTheTailIsWhereTheBeliefIsAsked pins the quantile, which is how a lane
// that is fastest at the median and awful at the ninety-ninth percentile loses.
func TestTheTailIsWhereTheBeliefIsAsked(t *testing.T) {
	belief := Posterior{X: math.Log(430), P: 0.9}
	if !(belief.Quantile(1.2816) > belief.Mean()*3) {
		t.Fatalf("a wide belief had a tame tail: median %.0f, p90 %.0f", belief.Mean(), belief.Quantile(1.2816))
	}
	if !(belief.Quantile(-1.2816) < belief.Mean()) {
		t.Fatal("the lower quantile was not below the median")
	}
}

// TestSurpriseIsMeasuredInSpreadsAndNotInSeconds is what a strike wanted to be.
func TestSurpriseIsMeasuredInSpreadsAndNotInSeconds(t *testing.T) {
	brisk := Posterior{X: math.Log(400), P: 0.05}
	slow := Posterior{X: math.Log(4000), P: 0.05}
	twoSeconds := math.Log(2000)
	if !(brisk.Innovation(twoSeconds, 0.05) > 3) {
		t.Fatal("two seconds from a lane whose normal is 400ms was not surprising")
	}
	if !(slow.Innovation(twoSeconds, 0.05) < 0) {
		t.Fatal("two seconds from a lane whose normal is four seconds was not a pleasant surprise")
	}
}

// TestQualityCountsRatherThanRates keeps "nine out of ten" and "nine hundred
// out of a thousand" from being the same belief.
func TestQualityCountsRatherThanRates(t *testing.T) {
	thin := Beta{A: 9, B: 1}
	thick := Beta{A: 900, B: 100}
	if math.Abs(thin.Mean()-thick.Mean()) > 1e-12 {
		t.Fatal("the two means should agree; it is the counts that differ")
	}
	if got := thin.Observe(false); got.B != 2 || got.A != 9 {
		t.Fatalf("a refused answer was not counted: %+v", got)
	}
}

// TestASightingRatesItselfInOnePlace keeps two callers from computing a rate
// two ways.
func TestASightingRatesItselfInOnePlace(t *testing.T) {
	got := Sighting{Tokens: 120, Gen: 2 * time.Second}.Rate()
	if got != 60 {
		t.Fatalf("120 tokens in two seconds rated %.2f", got)
	}
	if (Sighting{Tokens: 120}).Rate() != 0 || (Sighting{Gen: time.Second}).Rate() != 0 {
		t.Fatal("an unratable answer was given a rate")
	}
}

// TestAnAnonymousSightingNamesNothing keeps a measurement nobody can attribute
// out of some innocent lane's belief.
func TestAnAnonymousSightingNamesNothing(t *testing.T) {
	if !(ID{Model: "m"}).Zero() || !(ID{Lane: "l"}).Zero() {
		t.Fatal("half an id was accepted as an id")
	}
	if (ID{Model: "m", Lane: "l"}).String() != "m|l" {
		t.Fatal("the key form changed")
	}
}

// ── THE REGISTRY ────────────────────────────────────────────────────────────

// TestEverySeamAnswersAndNoneOfThemInventsANumber is the contract every lane of
// this wave builds against: the seams are filled in from the first compile, and
// what they answer with before anybody has built them is honest emptiness.
func TestEverySeamAnswersAndNoneOfThemInventsANumber(t *testing.T) {
	// The sheet and the store are real files now, so this asks the seams on a
	// machine that has never routed anything rather than on whatever the
	// person running the test happens to have believed this morning.
	t.Setenv(home.EnvVar, t.TempDir())
	registry := Default()
	if registry.Sheet() == nil || registry.Ledger() == nil || registry.Chooser() == nil ||
		registry.Prober() == nil || registry.Store() == nil {
		t.Fatal("a seam answered with nil, which every caller would then have to check")
	}
	if rows := registry.Sheet().Rows("any/model"); rows != nil {
		t.Fatalf("the empty sheet published %d rows", len(rows))
	}
	if err := registry.Sheet().Refresh(context.Background(), "any/model"); err == nil {
		t.Fatal("a refresh that fetched nothing reported success")
	}
	if _, ok := registry.Ledger().Belief(ID{Model: "m", Lane: "l"}); ok {
		t.Fatal("the empty ledger believed something")
	}
	registry.Ledger().Note(Sighting{ID: ID{Model: "m", Lane: "l"}, TTFT: time.Second})
	if _, ok := registry.Ledger().Belief(ID{Model: "m", Lane: "l"}); ok {
		t.Fatal("the empty ledger is meant to forget, honestly, not to pretend")
	}
	if choice := registry.Chooser().Choose(Request{Model: "m"}); !choice.Empty() || choice.Why != "" {
		t.Fatalf("the empty chooser had an opinion: %+v", choice)
	}
	// A belief file that has never been written is no beliefs and no error —
	// "nothing was kept" rather than "nothing can be kept", which is the
	// distinction [ErrNoStore] exists to hold.
	if kept, err := registry.Store().Load(); err != nil || len(kept) != 0 {
		t.Fatalf("a machine that has routed nothing loaded %d beliefs and %v", len(kept), err)
	}
}

// TestABudgetThatCannotCountRefuses is the one empty implementation that could
// cost somebody money if it guessed.
func TestABudgetThatCannotCountRefuses(t *testing.T) {
	if NewBudget(6, 0.1).Allow(time.Now(), 0.0001) {
		t.Fatal("an un-built hedge budget allowed a hedge")
	}
}

// TestAWatchWithoutAVerdictStillWatches keeps the shape real while lane L-C has
// not filled it in: the numbers are kept and nothing is spent on them.
func TestAWatchWithoutAVerdictStillWatches(t *testing.T) {
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	watch := NewWatch(Choice{Deadline: 1200 * time.Millisecond, Alt: "CoreWeave"}, Belief{}, now)
	if watch.Deadline() != 1200*time.Millisecond || watch.Alt() != "CoreWeave" {
		t.Fatal("the watch lost the choice it was started from")
	}
	if verdict := watch.Token(64, now.Add(time.Second)); verdict.Hedge {
		t.Fatal("an un-built watch asked for a hedge")
	}
	if verdict := watch.Silence(now.Add(time.Minute)); verdict.Hedge {
		t.Fatal("an un-built watch asked for a hedge on silence")
	}
	if watch.Hedged() {
		t.Fatal("a watch that hedged nothing said it had")
	}
}

// TestASwappedSeamIsPutBack is how every lane's tests will use the registry.
func TestASwappedSeamIsPutBack(t *testing.T) {
	registry := Default()
	defer registry.Reset()
	registry.SetChooser(fixedChooser{})
	if got := registry.Chooser().Choose(Request{Model: "m"}); len(got.Only) != 1 {
		t.Fatal("the installed chooser was not asked")
	}
	registry.SetChooser(nil)
	if got := registry.Chooser().Choose(Request{Model: "m"}); !got.Empty() {
		t.Fatal("clearing a seam left the old one in place")
	}
}

// fixedChooser is a test double, and it lives in a test file for the reason the
// structural test states: concrete implementations are constructed in the
// registry and nowhere else.
type fixedChooser struct{}

func (fixedChooser) Choose(Request) Choice { return Choice{Only: []string{"CoreWeave"}} }

// TestTheStorePathIsStatedOnce keeps the file from being spelled twice.
func TestTheStorePathIsStatedOnce(t *testing.T) {
	if got := StorePath(); got == "" || got[len(got)-len("v3/lanes.json"):] != "v3/lanes.json" {
		t.Fatalf("beliefs sleep at %q", got)
	}
}
