package crewpick

import (
	"encoding/json"
	"math"
	"os"
	"slices"
	"testing"
)

// A fixture row in its own field names: a null index reads as zero —
// missing, the way the package reads it — and a null cache-read price reads
// as unpublished. The rows with unpublished indexes and unpublished cache
// prices stay in the fixture, so the exclusion rules have something to bite
// on.
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

// A row missing an index or priced at zero is not a candidate at all: it
// never reaches a seat and it raises no ceiling. Both rules bite where the
// floor cannot — the missing coding index leaves the mastermind seat its
// full quality, and a free model clears any floor.
func TestARowMissingAnIndexOrPricedAtZeroIsNoCandidate(t *testing.T) {
	good := []Candidate{model("a/m"), model("c/h")}
	good[1].Intelligence = 90
	good[1].PromptPrice = 2
	good[1].CompletionPrice = 8
	good[1].CacheReadPrice = 0.2

	broken := model("d/x")
	broken.Coding = 0
	broken.PromptPrice = 0.01
	broken.CompletionPrice = 0.02
	broken.HasCacheRead = false

	free := model("e/y")
	free.PromptPrice = 0

	pool := append(slices.Clone(good), broken, free)
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
