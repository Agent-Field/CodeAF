package lane

import (
	"math"
	"strings"
	"sync"
	"testing"
	"time"
)

// noon is the moment every scripted sighting and every scripted request in this
// package happens at or after. The ledger and the chooser have no clock, so a
// test that pins one is a test of the arithmetic rather than of the machine it
// ran on, and pinning it once here keeps the chooser's sampling reproducible.
var noon = time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)

// cloudflareRow is the sheet's account of one real lane, taken from the
// half-hour window this design was measured against.
func cloudflareRow() Row {
	return Row{
		ID: ID{Model: scriptedModel, Lane: "Cloudflare"},
		Facts: Facts{
			Tools: true, Quant: "fp8", MaxOut: 32_000, Context: 345_000,
			Uptime5m: 100, PriceIn: 0.0000009, PriceOut: 0.00000132, Caches: true,
		},
		TTFTp50: 768, TTFTp75: 892, TTFTp90: 1037, TTFTp99: 1749,
		Ratep50: 58, Ratep75: 71, Ratep90: 92, Ratep99: 121,
	}
}

// TestAPrimedLaneBelievesTheSheetsMedian is the first call of a fresh process:
// nothing has been measured and nothing is blind.
func TestAPrimedLaneBelievesTheSheetsMedian(t *testing.T) {
	l := newLedger()
	row := cloudflareRow()
	l.Prime(row, SheetWeight)

	belief, ok := l.Belief(row.ID)
	if !ok {
		t.Fatal("a primed lane was not believed in")
	}
	if math.Abs(belief.TTFT.Mean()-768) > 1e-9 {
		t.Fatalf("the first-token belief read back as %.4fms rather than the sheet's 768", belief.TTFT.Mean())
	}
	if math.Abs(belief.Rate.Mean()-58) > 1e-9 {
		t.Fatalf("the rate belief read back as %.4f rather than the sheet's 58 t/s", belief.Rate.Mean())
	}
	// The variance is the sheet's own spread, adopted outright: k says how much
	// less the public number weighs than ours, and there is nothing of ours yet
	// for it to weigh against.
	want := math.Log(1037/768.0) / z90
	if math.Abs(math.Sqrt(belief.TTFT.P)-want) > 1e-9 {
		t.Fatalf("the prior's spread came in at %.6f rather than %.6f", math.Sqrt(belief.TTFT.P), want)
	}
	if belief.Facts != row.Facts {
		t.Fatalf("the gate facts did not ride along: %+v", belief.Facts)
	}
	if belief.Quality != (Beta{A: 8, B: 1}) {
		t.Fatalf("an eight-bit lane started at %+v", belief.Quality)
	}
	// Nothing has been measured, so there is no moment to age from.
	if !belief.At.IsZero() {
		t.Fatalf("a lane nobody has used carried the moment %v", belief.At)
	}
}

// TestALaneServingFourBitWeightsStartsSkeptical is the one fact on the sheet
// that predicts a lane returning tool-call JSON the decoder refuses. Starting
// it optimistic means paying for the discovery on somebody's real turn.
func TestALaneServingFourBitWeightsStartsSkeptical(t *testing.T) {
	l := newLedger()
	row := cloudflareRow()
	row.ID.Lane, row.Facts.Quant = "AtlasCloud", "fp4"
	l.Prime(row, SheetWeight)
	belief, _ := l.Belief(row.ID)
	if belief.Quality != (Beta{A: 2, B: 2}) {
		t.Fatalf("a four-bit lane started at %+v rather than at an open question", belief.Quality)
	}
	if belief.Quality.Mean() >= (Beta{A: 8, B: 1}).Mean() {
		t.Fatal("four-bit weights were no more doubted than eight-bit ones")
	}
	for _, quant := range []string{"int4", "nf4", "q4_k_m", "FP4"} {
		if !fourBit(strings.ToLower(quant)) {
			t.Fatalf("%q was not read as four-bit weights", quant)
		}
	}
	for _, quant := range []string{"fp8", "bf16", "fp16", "int8", ""} {
		if fourBit(quant) {
			t.Fatalf("%q was read as four-bit weights", quant)
		}
	}
}

// TestASheetRefreshDoesNotHandBackOptimismAlreadyLost keeps the beat from
// undoing an afternoon of refused answers every five minutes.
func TestASheetRefreshDoesNotHandBackOptimismAlreadyLost(t *testing.T) {
	l := newLedger()
	row := cloudflareRow()
	l.Prime(row, SheetWeight)
	for range 5 {
		l.NoteOutcome(Outcome{ID: row.ID, Accepted: false, Reason: "tool_json", At: noon})
	}
	before, _ := l.Belief(row.ID)
	l.Prime(row, SheetWeight)
	after, _ := l.Belief(row.ID)
	if after.Quality != before.Quality {
		t.Fatalf("a refresh moved quality from %+v to %+v", before.Quality, after.Quality)
	}
	if after.Quality.Mean() > 0.7 {
		t.Fatalf("five refused answers left the lane believed %.2f usable", after.Quality.Mean())
	}
}

// TestASlowSightingMovesTheBeliefAndAFastOneMovesItBack is the filter doing the
// job the strike table did badly: no demotion, no cooldown, just evidence.
func TestASlowSightingMovesTheBeliefAndAFastOneMovesItBack(t *testing.T) {
	l := newLedger()
	row := cloudflareRow()
	l.Prime(row, SheetWeight)
	primed, _ := l.Belief(row.ID)

	l.Note(Sighting{ID: row.ID, TTFT: 4 * time.Second, Gen: 2 * time.Second, Tokens: 120, At: noon})
	slow, _ := l.Belief(row.ID)
	if !(slow.TTFT.Mean() > primed.TTFT.Mean()) {
		t.Fatalf("a four-second wait did not move the belief: %.0fms to %.0fms", primed.TTFT.Mean(), slow.TTFT.Mean())
	}
	if !(slow.TTFT.Mean() < 4000) {
		t.Fatal("one slow answer moved the belief the whole way, which is a strike and not a filter")
	}
	if !(slow.TTFT.P < primed.TTFT.P) {
		t.Fatal("a measurement did not sharpen the belief")
	}
	if slow.At != noon {
		t.Fatalf("the belief was stamped %v rather than with the sighting's own moment", slow.At)
	}
	// 120 tokens in two seconds is 60 t/s, above the sheet's 58.
	if !(slow.Rate.Mean() > primed.Rate.Mean()) {
		t.Fatalf("a brisk answer did not move the rate: %.2f to %.2f", primed.Rate.Mean(), slow.Rate.Mean())
	}

	l.Note(Sighting{ID: row.ID, TTFT: 700 * time.Millisecond, Gen: time.Second, Tokens: 60, At: noon.Add(time.Minute)})
	back, _ := l.Belief(row.ID)
	if !(back.TTFT.Mean() < slow.TTFT.Mean()) {
		t.Fatalf("a fast answer did not bring the belief back: %.0fms to %.0fms", slow.TTFT.Mean(), back.TTFT.Mean())
	}
}

// TestAgeingWidensTheBeliefAndStopsAtTheSheet is the law that replaces the
// penalty box. A belief nobody has fed goes vague until the sheet's own
// certainty is all that is left of it — worthless, never worse than free.
func TestAgeingWidensTheBeliefAndStopsAtTheSheet(t *testing.T) {
	belief := Posterior{X: math.Log(900), P: 0.05}
	after := age(belief, HalfLife, 0)
	if math.Abs(after.P-0.1) > 1e-12 {
		t.Fatalf("one half-life left the variance at %.4f rather than doubling it", after.P)
	}
	if after.X != belief.X {
		t.Fatal("ageing moved the estimate, which is a claim nobody measured")
	}
	if twice := age(belief, 2*HalfLife, 0); math.Abs(twice.P-0.2) > 1e-12 {
		t.Fatalf("two half-lives left the variance at %.4f", twice.P)
	}
	if clamped := age(belief, 100*HalfLife, 0.11); clamped.P != 0.11 {
		t.Fatalf("a belief left alone for a day widened past the public sheet, to %.4f", clamped.P)
	}
}

// TestAStaleBeliefIsMovedFurtherByTheSameAnswer is what ageing buys, seen from
// the outside: the same sighting teaches more when the belief was vaguer.
func TestAStaleBeliefIsMovedFurtherByTheSameAnswer(t *testing.T) {
	fresh, stale := newLedger(), newLedger()
	row := cloudflareRow()
	for _, l := range []*ledger{fresh, stale} {
		l.Prime(row, SheetWeight)
		l.Note(Sighting{ID: row.ID, TTFT: 800 * time.Millisecond, At: noon})
	}
	fresh.Note(Sighting{ID: row.ID, TTFT: 3 * time.Second, At: noon.Add(time.Second)})
	stale.Note(Sighting{ID: row.ID, TTFT: 3 * time.Second, At: noon.Add(30 * time.Minute)})

	quick, _ := fresh.Belief(row.ID)
	old, _ := stale.Belief(row.ID)
	if !(old.TTFT.Mean() > quick.TTFT.Mean()) {
		t.Fatalf("a stale belief was no more movable than a fresh one: %.0fms and %.0fms", old.TTFT.Mean(), quick.TTFT.Mean())
	}
}

// TestAProbeTouchesTheFirstTokenAndNeverTheRate is the probe's whole claim: it
// measured exactly our path to this lane just now, and it measured nothing at
// all about how fast the lane writes.
func TestAProbeTouchesTheFirstTokenAndNeverTheRate(t *testing.T) {
	l := newLedger()
	row := cloudflareRow()
	l.Prime(row, SheetWeight)
	before, _ := l.Belief(row.ID)

	l.Note(Sighting{ID: row.ID, TTFT: 300 * time.Millisecond, Gen: 50 * time.Millisecond, Tokens: 1, Probe: true, At: noon})
	after, _ := l.Belief(row.ID)
	if after.Rate != before.Rate {
		t.Fatalf("a one-token probe rated the lane: %+v became %+v", before.Rate, after.Rate)
	}
	if !(after.TTFT.Mean() < before.TTFT.Mean()) {
		t.Fatal("a quick probe did not move the first-token belief")
	}

	// And it is the sharpest claim there is, so it sharpens the belief more
	// than the same wait behind a real prompt does.
	ordinary := newLedger()
	ordinary.Prime(row, SheetWeight)
	ordinary.Note(Sighting{ID: row.ID, TTFT: 300 * time.Millisecond, PromptTokens: 2_000, At: noon})
	plain, _ := ordinary.Belief(row.ID)
	if !(after.TTFT.P < plain.TTFT.P) {
		t.Fatalf("a probe was worth no more than an ordinary answer: %.5f and %.5f", after.TTFT.P, plain.TTFT.P)
	}
}

// TestALongPromptIsANoisierClaimAboutTheLane is why R has a prompt bucket in
// it: most of a sixty-thousand-token wait is prefill that no endpoint could
// have avoided, and charging the lane for it is how a router learns to avoid
// the lane it gave the long conversations to.
func TestALongPromptIsANoisierClaimAboutTheLane(t *testing.T) {
	short, long := newLedger(), newLedger()
	row := cloudflareRow()
	short.Prime(row, SheetWeight)
	long.Prime(row, SheetWeight)
	short.Note(Sighting{ID: row.ID, TTFT: 4 * time.Second, PromptTokens: 1_000, At: noon})
	long.Note(Sighting{ID: row.ID, TTFT: 4 * time.Second, PromptTokens: 60_000, At: noon})

	brief, _ := short.Belief(row.ID)
	lengthy, _ := long.Belief(row.ID)
	if !(lengthy.TTFT.Mean() < brief.TTFT.Mean()) {
		t.Fatalf("a long prompt's wait counted for as much as a short one's: %.0fms and %.0fms",
			lengthy.TTFT.Mean(), brief.TTFT.Mean())
	}
	if promptNoise(4_000) != 1 || promptNoise(32_000) != 2 || promptNoise(120_000) != 4 {
		t.Fatal("the prompt buckets moved")
	}
}

// TestAnAnswerTooShortToRateIsNotRated keeps a handshake from being recorded as
// a throughput.
func TestAnAnswerTooShortToRateIsNotRated(t *testing.T) {
	l := newLedger()
	row := cloudflareRow()
	l.Prime(row, SheetWeight)
	before, _ := l.Belief(row.ID)
	l.Note(Sighting{ID: row.ID, TTFT: 800 * time.Millisecond, Gen: 100 * time.Millisecond, Tokens: ratedFloor - 1, At: noon})
	after, _ := l.Belief(row.ID)
	if after.Rate != before.Rate {
		t.Fatalf("a %d-token answer was rated: %+v became %+v", ratedFloor-1, before.Rate, after.Rate)
	}
	l.Note(Sighting{ID: row.ID, TTFT: 800 * time.Millisecond, Gen: time.Second, Tokens: ratedFloor, At: noon.Add(time.Second)})
	rated, _ := l.Belief(row.ID)
	if rated.Rate == before.Rate {
		t.Fatal("an answer long enough to rate was not rated")
	}
}

// TestASightingNobodyCanPlaceIsRefused is the ledger's own version of the
// anonymous-sighting law: an observation with no moment cannot be aged, and
// folding it in would stamp the belief with a moment that never happened.
func TestASightingNobodyCanPlaceIsRefused(t *testing.T) {
	l := newLedger()
	row := cloudflareRow()
	l.Prime(row, SheetWeight)
	before, _ := l.Belief(row.ID)
	l.Note(Sighting{ID: row.ID, TTFT: 9 * time.Second})
	after, _ := l.Belief(row.ID)
	if after.TTFT != before.TTFT {
		t.Fatal("a sighting with no moment was folded in anyway")
	}
	l.Note(Sighting{TTFT: time.Second, At: noon})
	l.NoteOutcome(Outcome{Accepted: false, At: noon})
	if len(l.Beliefs(scriptedModel)) != 1 {
		t.Fatal("an anonymous observation invented a lane")
	}
}

// TestAnOutcomeMovesQualityAndNotTheClock keeps a quality signal from telling
// the next sighting that the timing filters had been updated when they had not.
func TestAnOutcomeMovesQualityAndNotTheClock(t *testing.T) {
	l := newLedger()
	row := cloudflareRow()
	l.Prime(row, SheetWeight)
	l.Note(Sighting{ID: row.ID, TTFT: 800 * time.Millisecond, At: noon})
	l.NoteOutcome(Outcome{ID: row.ID, Accepted: false, Reason: "empty", At: noon.Add(20 * time.Minute)})
	belief, _ := l.Belief(row.ID)
	if belief.At != noon {
		t.Fatalf("an outcome moved the timing's own moment to %v", belief.At)
	}
	if belief.Quality != (Beta{A: 8, B: 2}) {
		t.Fatalf("a refused answer left quality at %+v", belief.Quality)
	}
	l.NoteOutcome(Outcome{ID: row.ID, Accepted: true, At: noon.Add(21 * time.Minute)})
	if belief, _ := l.Belief(row.ID); belief.Quality != (Beta{A: 9, B: 2}) {
		t.Fatalf("an accepted answer left quality at %+v", belief.Quality)
	}
}

// TestAnUnprimedLaneStillLearns is the case the sheet never covered: a lane the
// stream named that the sheet has never published a row for.
func TestAnUnprimedLaneStillLearns(t *testing.T) {
	l := newLedger()
	id := ID{Model: scriptedModel, Lane: "Somebody New"}
	l.Note(Sighting{ID: id, TTFT: 1200 * time.Millisecond, Gen: 2 * time.Second, Tokens: 100, At: noon})
	belief, ok := l.Belief(id)
	if !ok || !belief.Known() {
		t.Fatal("a lane with no sheet row learned nothing from being used")
	}
	if math.Abs(belief.TTFT.Mean()-1200) > 1e-9 {
		t.Fatalf("the first measurement was not adopted outright: %.2fms", belief.TTFT.Mean())
	}
	if math.Abs(belief.Rate.Mean()-50) > 1e-9 {
		t.Fatalf("the first rate was not adopted outright: %.2f t/s", belief.Rate.Mean())
	}
	if belief.TTFT.P != defaultSpread*defaultSpread {
		t.Fatalf("a lane with no published spread was given the variance %.4f", belief.TTFT.P)
	}
}

// TestTheLanesOfAModelComeBackInOneOrder keeps a picker from reshuffling its
// rows between two redraws.
func TestTheLanesOfAModelComeBackInOneOrder(t *testing.T) {
	l := newLedger()
	for _, name := range []string{"Parasail", "AtlasCloud", "Cloudflare", "Baidu"} {
		row := cloudflareRow()
		row.ID.Lane = name
		l.Prime(row, SheetWeight)
	}
	elsewhere := cloudflareRow()
	elsewhere.ID = ID{Model: "qwen/qwen3.5-9b", Lane: "Groq"}
	l.Prime(elsewhere, SheetWeight)

	got := l.Beliefs(scriptedModel)
	want := []string{"AtlasCloud", "Baidu", "Cloudflare", "Parasail"}
	if len(got) != len(want) {
		t.Fatalf("the model had %d lanes rather than %d", len(got), len(want))
	}
	for i, name := range want {
		if got[i].ID.Lane != name {
			t.Fatalf("the lanes came back as %v", lanesOf(got))
		}
	}
	if l.Beliefs("nobody/knows") != nil || l.Beliefs("") != nil {
		t.Fatal("a model nobody has used had lanes")
	}
}

func lanesOf(beliefs []Belief) []string {
	names := make([]string, 0, len(beliefs))
	for _, belief := range beliefs {
		names = append(names, belief.ID.Lane)
	}
	return names
}

// TestEightStreamsFinishingAtOnceIsOneLedger is the shape this really runs in:
// a swarm's answers land from every goroutine there is, and the ledger is the
// only mutable state in the package.
func TestEightStreamsFinishingAtOnceIsOneLedger(t *testing.T) {
	l := newLedger()
	row := cloudflareRow()
	l.Prime(row, SheetWeight)

	var wait sync.WaitGroup
	for worker := range 8 {
		wait.Add(1)
		go func(worker int) {
			defer wait.Done()
			id := row.ID
			if worker%2 == 1 {
				id.Lane = "CoreWeave"
			}
			for n := range 50 {
				at := noon.Add(time.Duration(n) * time.Second)
				l.Note(Sighting{ID: id, TTFT: time.Second, Gen: time.Second, Tokens: 60, PromptTokens: 900, At: at})
				l.NoteOutcome(Outcome{ID: id, Accepted: n%3 != 0, At: at})
				l.Prime(row, SheetWeight)
				l.Belief(id)
				l.Beliefs(scriptedModel)
			}
		}(worker)
	}
	wait.Wait()

	if got := len(l.Beliefs(scriptedModel)); got != 2 {
		t.Fatalf("eight goroutines left %d lanes", got)
	}
	for _, belief := range l.Beliefs(scriptedModel) {
		if !belief.Known() || belief.TTFT.P <= 0 {
			t.Fatalf("%s came out of the race unbelieved: %+v", belief.ID.Lane, belief)
		}
	}
}
