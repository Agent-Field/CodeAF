package crewpick

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"slices"
	"testing"
)

// A fixture row in its own field names: a null index reads as zero —
// missing, the way the package reads it — and a null cache-read price reads
// as unpublished. The partially published row and the rows with unpublished
// cache prices stay in the fixture, so the estimate and exclusion rules
// have something to bite on.
type row struct {
	ID    string   `json:"id"`
	Open  bool     `json:"open"`
	I     float64  `json:"I"`
	C     float64  `json:"C"`
	A     float64  `json:"A"`
	Pi    float64  `json:"pi"`
	Po    float64  `json:"po"`
	Cr    *float64 `json:"cr"`
	Ctx   int      `json:"ctx"`
	Img   bool     `json:"img"`
	Tools bool     `json:"tools"`
}

// loadCandidates reads the fixture into candidates.
func loadCandidates(t *testing.T) []Candidate {
	t.Helper()
	raw, err := os.ReadFile("testdata/candidates.json")
	if err != nil {
		t.Fatal(err)
	}
	var rows []row
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatal(err)
	}
	cands := make([]Candidate, 0, len(rows))
	for _, r := range rows {
		c := Candidate{
			ID:              r.ID,
			Open:            r.Open,
			Intelligence:    r.I,
			Coding:          r.C,
			Agentic:         r.A,
			PromptPrice:     r.Pi,
			CompletionPrice: r.Po,
			Context:         r.Ctx,
			Images:          r.Img,
			Tools:           r.Tools,
		}
		if r.Cr != nil {
			c.CacheReadPrice = *r.Cr
			c.HasCacheRead = true
		}
		cands = append(cands, c)
	}
	return cands
}

// model builds a candidate that can sit any seat: every index at 100, tools
// and images, a 200k window, and a published cache price.
func model(id string) Candidate {
	return Candidate{
		ID:              id,
		Open:            true,
		Intelligence:    100,
		Coding:          100,
		Agentic:         100,
		PromptPrice:     1,
		CompletionPrice: 4,
		CacheReadPrice:  0.1,
		HasCacheRead:    true,
		Context:         200_000,
		Images:          true,
		Tools:           true,
	}
}

// donors builds n fully measured candidates that can sit no seat — a window
// too small for any of them — so they feed the estimates and the ceilings
// and nothing else.
func donors(prefix string, n int) []Candidate {
	rows := make([]Candidate, 0, n)
	for i := range n {
		d := model(fmt.Sprintf("%s/d%d", prefix, i))
		d.Context = 1000
		rows = append(rows, d)
	}
	return rows
}

// wantCrew checks a pick against its worker|high|mastermind line.
func wantCrew(t *testing.T, name string, got Crew, want string) {
	t.Helper()
	line := got.Worker + "|" + got.High + "|" + got.Mastermind
	if line != want {
		t.Fatalf("%s = %s, want %s", name, line, want)
	}
}

// The fixture's front is a fixed property of its rows: seven crews in the
// open family, eighteen in all, and these are the picks each front reads.
// The all-family balanced crew is where money stops buying quality — a bill
// of 0.590 at quality 95.4.
func TestPresetsReadTheFixtureFront(t *testing.T) {
	cands := loadCandidates(t)
	shapes := DefaultShapes()

	open := Front(cands, shapes, Open)
	if len(open) != 7 {
		t.Fatalf("open front holds %d crews, want 7", len(open))
	}
	frugal, balanced, max := Presets(open)
	wantCrew(t, "open frugal", frugal, "deepseek/deepseek-v4-flash-0731|z-ai/glm-5.3-flash|z-ai/glm-5.3-flash")
	wantCrew(t, "open balanced", balanced, "z-ai/glm-5.3-flash|moonshotai/kimi-k3|z-ai/glm-5.3")
	wantCrew(t, "open max", max, "z-ai/glm-5.3|moonshotai/kimi-k3|z-ai/glm-5.3")

	all := Front(cands, shapes, All)
	if len(all) != 18 {
		t.Fatalf("all front holds %d crews, want 18", len(all))
	}
	frugal, balanced, max = Presets(all)
	wantCrew(t, "all frugal", frugal, "z-ai/glm-5.3-flash|google/gemini-3.8-flash|z-ai/glm-5.3-flash")
	wantCrew(t, "all balanced", balanced, "z-ai/glm-5.3-flash|anthropic/claude-fable-5.1|anthropic/claude-fable-5.1")
	wantCrew(t, "all max", max, "anthropic/claude-fable-5.1|openai/gpt-6-astra|anthropic/claude-fable-5.1")
	if math.Abs(balanced.Bill-0.590) > 0.005 {
		t.Fatalf("all balanced bill = %f, want about 0.590", balanced.Bill)
	}
	if math.Abs(balanced.Quality-95.4) > 0.05 {
		t.Fatalf("all balanced quality = %f, want about 95.4", balanced.Quality)
	}
}

// The high seat sees the same work the worker does, so a crew never draws it
// from the worker's own vendor. Every crew on both fixture fronts holds
// that line, and a pool that cannot field two vendors fields no crew at all.
func TestTheHighSeatNeverSharesAVendorWithTheWorker(t *testing.T) {
	cands := loadCandidates(t)
	for _, fam := range []Family{Open, All} {
		for _, crew := range Front(cands, DefaultShapes(), fam) {
			if vendor(crew.Worker) == vendor(crew.High) {
				t.Fatalf("worker %s and high %s share a vendor", crew.Worker, crew.High)
			}
		}
	}

	oneVendor := []Candidate{model("one/first"), model("one/second")}
	if front := Front(oneVendor, DefaultShapes(), All); len(front) != 0 {
		t.Fatalf("a one-vendor pool fielded %d crews, want none", len(front))
	}
}

// The floor keeps a seat's running to candidates that can do the job: a
// model whose quality falls below Floor of the seat's best in the family is
// not cheap and never reaches a crew. The boundary is inclusive — a
// candidate sitting exactly on the floor runs, and at a tenth of the price
// of anything above it, it runs first.
func TestTheFloorDropsAModelThatCannotDoTheJob(t *testing.T) {
	pool := []Candidate{model("a/m"), model("c/h")}

	// Cheap enough to be every crew's frugal worker, and short one index:
	// 70 of the worker seat's 100, 76 of the mastermind's, and ineligible
	// for the high seat, which needs images.
	cheap := model("b/w")
	cheap.Agentic = 40
	cheap.Images = false
	cheap.PromptPrice = 0.01
	cheap.CompletionPrice = 0.02
	cheap.HasCacheRead = false
	pool = append(pool, cheap)

	// Exactly on the worker seat's floor: 80 of 100.
	edge := model("e/b")
	edge.Agentic = 60
	edge.PromptPrice = 0.02
	edge.CompletionPrice = 0.04
	edge.HasCacheRead = false
	pool = append(pool, edge)

	front := Front(pool, DefaultShapes(), All)
	if len(front) == 0 {
		t.Fatal("the pool must field crews")
	}
	frugal, _, _ := Presets(front)
	if frugal.Worker != "e/b" {
		t.Fatalf("frugal worker = %s, want the on-floor candidate e/b", frugal.Worker)
	}
	for _, crew := range front {
		for _, id := range []string{crew.Worker, crew.High, crew.Mastermind} {
			if id == "b/w" {
				t.Fatalf("the below-floor model b/w reached a crew: %+v", crew)
			}
		}
	}
}

// A row carrying none of its three indexes, or priced at zero, is not a
// candidate at all: it never reaches a seat and it raises no ceiling. Both
// rules bite where the floor cannot — a free model would clear any floor,
// and a bare row carries no index to estimate from.
func TestARowWithNoIndexesAtAllOrNoPriceIsNoCandidate(t *testing.T) {
	good := []Candidate{model("a/m"), model("c/h")}
	good[1].Intelligence = 90
	good[1].PromptPrice = 2
	good[1].CompletionPrice = 8
	good[1].CacheReadPrice = 0.2

	bare := model("d/x")
	bare.Intelligence = 0
	bare.Coding = 0
	bare.Agentic = 0
	bare.PromptPrice = 0.01
	bare.CompletionPrice = 0.02
	bare.HasCacheRead = false

	free := model("e/y")
	free.PromptPrice = 0

	pool := append(slices.Clone(good), bare, free)
	front := Front(pool, DefaultShapes(), All)
	for _, crew := range front {
		for _, id := range []string{crew.Worker, crew.High, crew.Mastermind} {
			if id == "d/x" || id == "e/y" {
				t.Fatalf("a non-candidate row reached a crew: %+v", crew)
			}
		}
	}
	// The non-candidate rows leave the real candidates' front exactly what
	// it was without them.
	if want := Front(good, DefaultShapes(), All); !slices.Equal(front, want) {
		t.Fatalf("the non-candidate rows moved the front:\n got %+v\nwant %+v", front, want)
	}
}

// A model publishing only its intelligence index — a release scored in
// stages — is scored on it, with the coding and agentic indexes estimated
// from the donors, and at a hundredth of the price it takes the worker seat.
func TestAModelWithOnlyAnIntelligenceIndexIsChosen(t *testing.T) {
	pool := []Candidate{model("a/w"), model("c/h")}
	newRelease := model("b/x")
	newRelease.Coding = 0
	newRelease.Agentic = 0
	newRelease.PromptPrice = 0.01
	newRelease.CompletionPrice = 0.02
	newRelease.HasCacheRead = false
	pool = append(pool, newRelease)
	pool = append(pool, donors("v", 3)...)

	front := Front(pool, DefaultShapes(), All)
	if len(front) == 0 {
		t.Fatal("the pool must field crews")
	}
	frugal, _, _ := Presets(front)
	if frugal.Worker != "b/x" {
		t.Fatalf("frugal worker = %s, want the partially scored b/x", frugal.Worker)
	}
	want := [3][3]bool{{false, true, true}, {}, {false, true, true}}
	if frugal.Estimated != want {
		t.Fatalf("the b/x crew's Estimated = %v, want %v", frugal.Estimated, want)
	}
}

// A model publishing only its coding index — many an older model — is
// scored on it, with the intelligence and agentic indexes estimated, and at
// a hundredth of the price it takes the worker seat too.
func TestAModelWithOnlyACodingIndexIsChosen(t *testing.T) {
	pool := []Candidate{model("a/w"), model("c/h")}
	older := model("b/x")
	older.Intelligence = 0
	older.Agentic = 0
	older.PromptPrice = 0.01
	older.CompletionPrice = 0.02
	older.HasCacheRead = false
	pool = append(pool, older)
	pool = append(pool, donors("v", 3)...)

	front := Front(pool, DefaultShapes(), All)
	if len(front) == 0 {
		t.Fatal("the pool must field crews")
	}
	frugal, _, _ := Presets(front)
	if frugal.Worker != "b/x" {
		t.Fatalf("frugal worker = %s, want the partially scored b/x", frugal.Worker)
	}
	want := [3][3]bool{{true, false, true}, {}, {true, false, true}}
	if frugal.Estimated != want {
		t.Fatalf("the b/x crew's Estimated = %v, want %v", frugal.Estimated, want)
	}
}

// The donor estimate is the median of the missing index over the present
// one among the donors carrying both — not the mean, which the far-out
// fourth donor drags away — and the rows missing either index of the pair
// never donate. The row missing its intelligence index still raises the
// coding ceiling with its own 400.
func TestTheDonorEstimateIsTheMedianRatio(t *testing.T) {
	shape := SeatShape{Weights: [3]float64{0, 1, 0}}
	pool := []Candidate{
		{ID: "a/d1", Intelligence: 10, Coding: 10, Agentic: 10, PromptPrice: 1},
		{ID: "a/d2", Intelligence: 10, Coding: 20, Agentic: 10, PromptPrice: 1},
		{ID: "a/d3", Intelligence: 10, Coding: 30, Agentic: 10, PromptPrice: 1},
		{ID: "a/d4", Intelligence: 10, Coding: 300, Agentic: 10, PromptPrice: 1},
		{ID: "b/noc", Intelligence: 10, Agentic: 10, PromptPrice: 1},
		{ID: "b/noi", Coding: 400, Agentic: 10, PromptPrice: 1},
	}
	x := Candidate{ID: "c/x", Intelligence: 10, PromptPrice: 1}

	// The donors' coding over intelligence ratios run 1, 2, 3, 30: the
	// median reads 2.5 and the estimate 25 of a 400 ceiling, where the mean
	// would read 9 and the estimate 90.
	if got := SeatQuality(x, shape, pool); got != 6.25 {
		t.Fatalf("SeatQuality on the median estimate = %v, want 6.25", got)
	}
}

// With fewer than three donors carrying a pair, the estimate falls back to
// equal standing — the missing index read at the present one's share of
// the pool's ceilings — not to the median of two donors, and not to
// exclusion.
func TestFewerThanThreeDonorsEstimatesByEqualStanding(t *testing.T) {
	shape := SeatShape{Weights: [3]float64{0, 1, 0}}
	pool := []Candidate{
		{ID: "a/d1", Intelligence: 100, Coding: 50, Agentic: 10, PromptPrice: 1},
		{ID: "a/d2", Intelligence: 50, Coding: 100, Agentic: 10, PromptPrice: 1},
	}
	x := Candidate{ID: "b/x", Intelligence: 80, PromptPrice: 1}

	// Equal standing reads x's coding at x's share of the intelligence
	// ceiling, 80 of 100; the two donors' median would read 100.
	if got := SeatQuality(x, shape, pool); got != 80 {
		t.Fatalf("SeatQuality on equal standing = %v, want 80", got)
	}
}

// An estimate never crosses the pool's largest measured value of its
// index: the donors' coding over intelligence median runs 20, and 50 times
// that would read 1000 of a 300 ceiling.
func TestAnEstimateIsCappedAtTheLargestMeasuredIndex(t *testing.T) {
	shape := SeatShape{Weights: [3]float64{0, 1, 0}}
	pool := []Candidate{
		{ID: "a/d1", Intelligence: 10, Coding: 100, Agentic: 10, PromptPrice: 1},
		{ID: "a/d2", Intelligence: 10, Coding: 200, Agentic: 10, PromptPrice: 1},
		{ID: "a/d3", Intelligence: 10, Coding: 300, Agentic: 10, PromptPrice: 1},
	}
	x := Candidate{ID: "b/x", Intelligence: 50, PromptPrice: 1}

	if got := SeatQuality(x, shape, pool); got != 100 {
		t.Fatalf("SeatQuality on the capped estimate = %v, want 100", got)
	}
}

// The estimate is never silent: the crew says, for the model in each seat,
// which of its three indexes were scored by estimate and which measured.
// The worker publishes only its intelligence index and the mastermind only
// its coding one; the high seat is measured throughout.
func TestTheResultMarksEstimatedIndexes(t *testing.T) {
	pool := []Candidate{model("a/w"), model("c/h")}
	worker := model("b/x")
	worker.Coding = 0
	worker.Agentic = 0
	worker.Images = false
	worker.PromptPrice = 0.01
	worker.CompletionPrice = 0.02
	worker.HasCacheRead = false
	mind := model("d/m")
	mind.Intelligence = 0
	mind.Agentic = 0
	mind.Tools = false
	mind.PromptPrice = 0.005
	mind.CompletionPrice = 0.01
	mind.HasCacheRead = false
	pool = append(pool, worker, mind)
	pool = append(pool, donors("v", 3)...)

	front := Front(pool, DefaultShapes(), All)
	if len(front) == 0 {
		t.Fatal("the pool must field crews")
	}
	frugal, _, _ := Presets(front)
	if frugal.Worker != "b/x" || frugal.Mastermind != "d/m" {
		t.Fatalf("frugal = %s|%s|%s, want b/x on the worker seat and d/m on the mastermind's", frugal.Worker, frugal.High, frugal.Mastermind)
	}
	want := [3][3]bool{{false, true, true}, {}, {true, false, true}}
	if frugal.Estimated != want {
		t.Fatalf("the crew's Estimated = %v, want %v", frugal.Estimated, want)
	}
}

// Two candidates for a seat otherwise tied on quality and cost run to the
// one with fewer estimated indexes — even when its id reads later, and
// whichever way round the pool arrives.
func TestATieOnQualityAndCostRunsToFewerEstimatedIndexes(t *testing.T) {
	pool := []Candidate{model("a/w"), model("c/h")}
	// Twins for the mastermind seat, identical in every number the picker
	// reads, but aaa runs on two estimated indexes and zzz on none.
	aaa := model("aaa/two")
	aaa.Intelligence = 0
	aaa.Coding = 80
	aaa.Agentic = 0
	aaa.Tools = false
	aaa.Images = false
	aaa.PromptPrice = 0.1
	aaa.CompletionPrice = 0.2
	aaa.HasCacheRead = false
	zzz := aaa
	zzz.ID = "zzz/one"
	zzz.Intelligence = 80
	zzz.Agentic = 80
	pool = append(pool, aaa, zzz)
	pool = append(pool, donors("v", 3)...)

	front := Front(pool, DefaultShapes(), All)
	if len(front) == 0 {
		t.Fatal("the pool must field crews")
	}
	if front[0].Mastermind != "zzz/one" {
		t.Fatalf("a tie went to the estimated twin: %+v", front[0])
	}
	for _, crew := range front {
		if crew.Mastermind == "aaa/two" {
			t.Fatalf("the estimated twin survived its tie: %+v", crew)
		}
	}

	reversed := slices.Clone(pool)
	slices.Reverse(reversed)
	if two := Front(reversed, DefaultShapes(), All); !slices.Equal(front, two) {
		t.Fatal("the tie broke differently when the pool arrived reversed")
	}
}

// Estimates, caps and ties read the same whatever order the candidates
// arrive in: a pool of partially published models gives the same front
// shuffled as it does in hand order.
func TestPartialCandidatesGiveTheSameFrontInAnyOrder(t *testing.T) {
	newRelease := model("b/new")
	newRelease.Coding = 0
	newRelease.Agentic = 0
	newRelease.PromptPrice = 0.01
	newRelease.CompletionPrice = 0.02
	newRelease.HasCacheRead = false
	older := model("d/old")
	older.Intelligence = 0
	older.Agentic = 0
	older.Tools = false
	staged := model("e/staged")
	staged.Agentic = 0
	pool := []Candidate{model("a/w"), model("c/h"), newRelease, older, staged}
	pool = append(pool, donors("v", 3)...)

	one := Front(pool, DefaultShapes(), All)
	if len(one) == 0 {
		t.Fatal("the pool must field crews")
	}
	if one[0].Estimated == ([3][3]bool{}) {
		t.Fatal("the cheapest crew carries no estimated index; the partial candidates never reached it")
	}
	reversed := slices.Clone(pool)
	slices.Reverse(reversed)
	if two := Front(reversed, DefaultShapes(), All); !slices.Equal(one, two) {
		t.Fatal("the front changed when the partial candidates arrived reversed")
	}
	rotated := append(slices.Clone(pool[3:]), pool[:3]...)
	if three := Front(rotated, DefaultShapes(), All); !slices.Equal(one, three) {
		t.Fatal("the front changed when the pool arrived rotated")
	}
}

// An input with no index missing picks on measurements alone: the fixture's
// partially published row left out, both fronts hold the same sizes and
// the same frugal, balanced and max picks the whole fixture holds, and no
// estimated flag is set anywhere — the partial row never raised a ceiling,
// so leaving it out moves nothing measured.
func TestNothingMissingLeavesEveryPickUnchanged(t *testing.T) {
	cands := loadCandidates(t)
	var full []Candidate
	for _, c := range cands {
		if c.Intelligence > 0 && c.Coding > 0 && c.Agentic > 0 {
			full = append(full, c)
		}
	}
	shapes := DefaultShapes()

	open := Front(full, shapes, Open)
	if len(open) != 7 {
		t.Fatalf("the full open front holds %d crews, want 7", len(open))
	}
	frugal, balanced, max := Presets(open)
	wantCrew(t, "open frugal", frugal, "deepseek/deepseek-v4-flash-0731|z-ai/glm-5.3-flash|z-ai/glm-5.3-flash")
	wantCrew(t, "open balanced", balanced, "z-ai/glm-5.3-flash|moonshotai/kimi-k3|z-ai/glm-5.3")
	wantCrew(t, "open max", max, "z-ai/glm-5.3|moonshotai/kimi-k3|z-ai/glm-5.3")

	all := Front(full, shapes, All)
	if len(all) != 18 {
		t.Fatalf("the full all front holds %d crews, want 18", len(all))
	}
	frugal, balanced, max = Presets(all)
	wantCrew(t, "all frugal", frugal, "z-ai/glm-5.3-flash|google/gemini-3.8-flash|z-ai/glm-5.3-flash")
	wantCrew(t, "all balanced", balanced, "z-ai/glm-5.3-flash|anthropic/claude-fable-5.1|anthropic/claude-fable-5.1")
	wantCrew(t, "all max", max, "anthropic/claude-fable-5.1|openai/gpt-6-astra|anthropic/claude-fable-5.1")
	if math.Abs(balanced.Bill-0.590) > 0.005 {
		t.Fatalf("all balanced bill = %f, want about 0.590", balanced.Bill)
	}
	if math.Abs(balanced.Quality-95.4) > 0.05 {
		t.Fatalf("all balanced quality = %f, want about 95.4", balanced.Quality)
	}
	for _, front := range [][]Crew{open, all} {
		for _, crew := range front {
			if crew.Estimated != ([3][3]bool{}) {
				t.Fatalf("a fully measured pool raised an estimated flag: %+v", crew)
			}
		}
	}
}

// The knob runs from one end of the front to the other: at 0 the budget is
// the cheapest bill and the pick is frugal, at 1 it is the dearest and the
// pick is max.
func TestAtKnobAtTheEndsReadsFrugalAndMax(t *testing.T) {
	cands := loadCandidates(t)
	for _, fam := range []Family{Open, All} {
		front := Front(cands, DefaultShapes(), fam)
		frugal, _, max := Presets(front)
		if got := AtKnob(front, 0); got != frugal {
			t.Fatalf("AtKnob at 0 = %+v, want the frugal %+v", got, frugal)
		}
		if got := AtKnob(front, 1); got != max {
			t.Fatalf("AtKnob at 1 = %+v, want the max %+v", got, max)
		}
	}
}

// The front is a property of the candidates, not of their order: the running
// for each seat is read in id order, so the same list gives the same front
// however it arrives.
func TestTheSameCandidatesGiveTheSameFrontInAnyOrder(t *testing.T) {
	cands := loadCandidates(t)
	shapes := DefaultShapes()
	one := Front(cands, shapes, All)

	reversed := slices.Clone(cands)
	slices.Reverse(reversed)
	if two := Front(reversed, shapes, All); !slices.Equal(one, two) {
		t.Fatal("the front changed when the candidates arrived reversed")
	}
}

// Two crews that tie in (bill, quality) are one point on the front, and the
// point keeps the crew whose ids read first — worker, then high, then
// mastermind — whichever way round the pool arrived.
func TestTiesInBillAndQualityBreakInIdOrder(t *testing.T) {
	// Two candidates identical in every way but the id, and able to sit
	// only the mastermind seat: every crew one finishes ties with the crew
	// the other finishes.
	aaa := model("aaa/x")
	aaa.Tools = false
	aaa.Images = false
	aaa.PromptPrice = 0.1
	aaa.CompletionPrice = 0.2
	aaa.HasCacheRead = false
	bbb := aaa
	bbb.ID = "bbb/x"

	pool := []Candidate{model("a/m"), model("c/h"), aaa, bbb}
	one := Front(pool, DefaultShapes(), All)
	if len(one) == 0 {
		t.Fatal("the pool must field crews")
	}
	for _, crew := range one {
		if crew.Mastermind != "aaa/x" {
			t.Fatalf("a tie went to the wrong twin: %+v", crew)
		}
	}

	reversed := slices.Clone(pool)
	slices.Reverse(reversed)
	if two := Front(reversed, DefaultShapes(), All); !slices.Equal(one, two) {
		t.Fatal("the tie broke differently when the pool arrived reversed")
	}
}

// The seat's cost is its token mix priced: the prompt price blended with the
// cache-read price by the cache share, and the completion price spread over
// the input-to-output ratio. A model that publishes no cache-read price pays
// the prompt price on the cache share too.
func TestSeatCostPricesTheTokenMix(t *testing.T) {
	shape := SeatShape{InOut: 2, CacheShare: 0.5}
	published := Candidate{PromptPrice: 2, CompletionPrice: 8, CacheReadPrice: 0.5, HasCacheRead: true}
	if got := SeatCost(published, shape); got != 5.25 {
		t.Fatalf("SeatCost with a published cache price = %f, want 5.25", got)
	}
	unpublished := Candidate{PromptPrice: 2, CompletionPrice: 8}
	if got := SeatCost(unpublished, shape); got != 6 {
		t.Fatalf("SeatCost with no published cache price = %f, want 6", got)
	}
}

// Quality reads each index against the pool's best for that index, so the
// same candidate scores lower in a stronger pool. A row that is not a
// candidate scores nothing, and so does any row against a pool with no
// candidates to raise a scale.
func TestSeatQualityReadsAgainstThePoolsBest(t *testing.T) {
	shape := SeatShape{Weights: [3]float64{0.25, 0.25, 0.5}}
	pool := []Candidate{
		{ID: "best", Intelligence: 100, Coding: 100, Agentic: 100, PromptPrice: 1},
		{ID: "half", Intelligence: 50, Coding: 100, Agentic: 100, PromptPrice: 1},
	}
	if got := SeatQuality(pool[0], shape, pool); got != 100 {
		t.Fatalf("the pool's best scored %f, want 100", got)
	}
	if got := SeatQuality(pool[1], shape, pool); got != 87.5 {
		t.Fatalf("the half scored %f, want 87.5", got)
	}

	stronger := append(slices.Clone(pool),
		Candidate{ID: "top", Intelligence: 200, Coding: 100, Agentic: 100, PromptPrice: 1})
	if got := SeatQuality(pool[1], shape, stronger); got != 81.25 {
		t.Fatalf("the half scored %f against a stronger pool, want 81.25", got)
	}

	blank := Candidate{ID: "blank", Coding: 0, PromptPrice: 1}
	if got := SeatQuality(blank, shape, pool); got != 0 {
		t.Fatalf("a row missing an index scored %f, want 0", got)
	}
	if got := SeatQuality(pool[0], shape, []Candidate{blank}); got != 0 {
		t.Fatalf("a pool with no candidates scored %f, want 0", got)
	}
}
