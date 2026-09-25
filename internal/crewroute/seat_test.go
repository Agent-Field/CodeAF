package crewroute

import (
	"strings"
	"testing"
)

// A FREE POOL OF A MODEL NOBODY PRICES never wins a seat on its price: it is
// weighed above the cheapest priced model, so with figures no better than a
// priced model's it does not take that model's seat.
func TestAFreeUnpricedModelNeverTakesASeatOnPrice(t *testing.T) {
	stranger := candidateOf(glmFlash)
	stranger.Model.ID = "thinkingmachines/inkling-small"
	stranger.Model.PromptPrice, stranger.Model.CompletionPrice, stranger.Model.CacheReadPrice = 0, 0, 0
	stranger.Routes = []Route{{Provider: "openrouter", Send: "thinkingmachines/inkling-small:free", Kind: Free}}
	candidates := append(catalogCandidates(), stranger)
	for _, class := range Classes {
		d, err := Decide(Request{Class: class, Candidates: candidates})
		if err != nil {
			t.Fatal(err)
		}
		for _, pick := range d.Crew {
			if pick.Model == stranger.Model.ID {
				t.Fatalf("%s %s went to the free stranger: %+v", class, pick.Seat, pick)
			}
		}
	}
}

// A MODEL THAT CANNOT DO THE SEAT'S WORK DOES NOT SIT IT: no tool calls, or a
// context too short for the seat.
func TestTheCapabilityFiltersKeepUnfitModelsOut(t *testing.T) {
	noTools := catalogRow("somelab/no-tools", true, 0.01, 0.02, 90, 90, 90)
	noTools.Model.Tools = false
	short := catalogRow("somelab/short", true, 0.01, 0.02, 90, 90, 90)
	short.Model.Context = 16_000
	for _, c := range []Candidate{noTools, short} {
		if _, err := Decide(Request{Class: Bugfix, Candidates: []Candidate{c}}); err == nil {
			t.Fatalf("%s sat a seat", c.Model.ID)
		}
	}
}

// ONE MODEL, THREE ROUTES: the free pool, a direct connection, and the
// default service. With the free pool trusted, it is the route; the seat's
// fallback is the SAME model on its next route, never another model.
func TestAModelOnThreeRoutesTakesTheRightOneAndFallsThroughFreeToPaid(t *testing.T) {
	m := glmFlash
	routes := func(freeFail float64) []Route {
		return []Route{
			{Provider: "openrouter", Send: "z-ai/glm-5.3-flash:free", Kind: Free, FailRate: freeFail},
			{Provider: "z-ai", Send: "z-ai/glm-5.3-flash", Kind: Metered},
			{Provider: "openrouter", Send: "openrouter/z-ai/glm-5.3-flash", Kind: Metered},
		}
	}
	one := func(freeFail float64) Decision {
		d, err := Decide(Request{Class: Bugfix, Candidates: []Candidate{{Model: m, Routes: routes(freeFail)}}})
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	d := one(0.05)
	worker := d.Seat(Worker)
	if worker.Kind != Free || worker.Send != "z-ai/glm-5.3-flash:free" {
		t.Fatalf("a reliable free route was not taken: %+v", worker)
	}
	ladder := d.Ladder[Worker]
	if len(ladder) < 2 || ladder[0].Model != worker.Model || ladder[0].Send != "z-ai/glm-5.3-flash" || ladder[1].Send != "openrouter/z-ai/glm-5.3-flash" {
		t.Fatalf("the ladder is %+v, want the same model's direct then default routes first", ladder)
	}
	moved := d.WithRung(Worker, ladder[0], "free pool limited")
	if moved.Seat(Worker).Send != "z-ai/glm-5.3-flash" || len(moved.Retried) != 1 || moved.Retried[0].Why != "free pool limited" {
		t.Fatalf("moving the worker gave %+v", moved.Seat(Worker))
	}
	// A free route this install has watched refuse most first calls costs more
	// than it saves, and the paid route is taken from the start.
	if got := one(0.95).Seat(Worker); got.Kind != Metered || got.Send != "z-ai/glm-5.3-flash" {
		t.Fatalf("an unreliable free route was still taken: %+v", got)
	}
}

// A FREE ROUTE IS NEVER PRICED AT ZERO: its expected cost is its refusals.
func TestAFreeRoutesExpectedCostIsItsRefusals(t *testing.T) {
	if got := routeCost(Route{Kind: Free}, 0.02, 0.02); got <= 0 {
		t.Fatalf("a free route costs %v", got)
	}
	if routeCost(Route{Kind: Free, FailRate: 0.9}, 0.02, 0.02) <= routeCost(Route{Kind: Free, FailRate: 0.1}, 0.02, 0.02) {
		t.Fatal("a route that refuses more was not dearer")
	}
}

// A MODEL THAT FAILED TO START IS KEPT OFF UNPINNED SEATS — while anything
// else can sit them — and never off a pin.
func TestAnAvoidedModelSitsNoUnpinnedSeat(t *testing.T) {
	avoid := map[string]bool{Lineage("z-ai/glm-5.3-flash"): true}
	d, err := Decide(Request{Class: Bugfix, Candidates: catalogCandidates(), Avoid: avoid})
	if err != nil {
		t.Fatal(err)
	}
	for _, pick := range d.Crew {
		if Lineage(pick.Model) == Lineage("z-ai/glm-5.3-flash") {
			t.Fatalf("%s went to the avoided model", pick.Seat)
		}
	}
	pinned, err := Decide(Request{Class: Bugfix, Candidates: catalogCandidates(), Avoid: avoid,
		Pins: map[Seat]Pin{Worker: {Model: "z-ai/glm-5.3-flash", Send: "z-ai/glm-5.3-flash", Kind: Metered}}})
	if err != nil || pinned.Seat(Worker).Model != "z-ai/glm-5.3-flash" {
		t.Fatalf("a pin was overruled: %+v %v", pinned.Seat(Worker), err)
	}
	// Avoiding the only model there is still makes a crew.
	only := catalogCandidates()[:1]
	if _, err := Decide(Request{Class: Bugfix, Candidates: only, Avoid: avoid}); err != nil {
		t.Fatalf("avoiding the only model left no crew: %v", err)
	}
}

// THE LINE: open-ended with its hyphen, the planner when it is another model,
// the free route named, and a seat that moved said once.
func TestTheLineSaysClassPlannerAndARetry(t *testing.T) {
	d := Decision{Class: OpenEnded, EstUSD: 0.05, Crew: []Pick{
		{Seat: Worker, Model: "z-ai/glm-5.3-flash", Provider: "openrouter", Kind: Free},
		{Seat: Planner, Model: "moonshotai/kimi-k3"},
		{Seat: Checker, Model: "moonshotai/kimi-k3"},
	}}
	line := d.Line("", -1)
	for _, want := range []string{"open-ended · ", "(openrouter · free)", " · planner kimi-k3 · checker kimi-k3"} {
		if !strings.Contains(line, want) {
			t.Errorf("line %q lacks %q", line, want)
		}
	}
	moved := d.WithRung(Worker, Pick{Model: "deepseek/deepseek-v4-flash"}, "credit unavailable on openrouter")
	if line := moved.Line("", -1); !strings.Contains(line, "running on fallback crew · worker glm-5.3-flash → deepseek-v4-flash (credit unavailable on openrouter)") {
		t.Errorf("the moved line does not say so: %q", line)
	}
	same := Decision{Class: Bugfix, Crew: []Pick{{Seat: Worker, Model: "a/x"}, {Seat: Planner, Model: "a/x"}, {Seat: Checker, Model: "a/y"}}}
	if strings.Contains(same.Line("", -1), "planner") {
		t.Errorf("a planner on the worker's model was named: %q", same.Line("", -1))
	}
}

// THE OWNER'S CATALOG: a free stranger with no price and one published index
// beside the priced models. Every class keeps it out.
func TestTheOwnersCatalogNeverSeatsTheFreeStranger(t *testing.T) {
	stranger := Candidate{
		Model:  Model{ID: "thinkingmachines/inkling-small", Coding: 52.9, Context: 262_144, Tools: true},
		Routes: []Route{{Provider: "router", Send: "thinkingmachines/inkling-small:free", Kind: Free}},
	}
	for _, class := range Classes {
		d, err := Decide(Request{Class: class, Candidates: append(catalogCandidates(), stranger)})
		if err != nil {
			t.Fatal(err)
		}
		for _, pick := range d.Crew {
			if Lineage(pick.Model) == Lineage(stranger.Model.ID) {
				t.Fatalf("%s %s went to the stranger", class, pick.Seat)
			}
		}
	}
}

// REDO CLIMBS ONE RUNG AT A TIME: monotone, one seat, the next model on the
// seat's front — never the top of the catalog in one step.
func TestRedoClimbsTheLadderOneRungAtATime(t *testing.T) {
	cands := frontierCandidates()
	dear := catalogRow("somelab/very-dear", false, 30, 150, 60, 85, 70)
	dear.Model.Released = released(2026, 9, 1)
	cands = append(cands, dear)
	ran, err := Decide(Request{Class: OpenEnded, Candidates: cands, Effort: EffortCheap})
	if err != nil {
		t.Fatal(err)
	}
	ran.Rungs, ran.Note = nil, ""
	prev := ran
	for step := 0; step < 3; step++ {
		next, err := Decide(Request{Class: OpenEnded, Candidates: cands, Stronger: &prev})
		if err != nil {
			t.Fatal(err)
		}
		changed := 0
		for _, seat := range Seats {
			if next.Seat(seat).Quality < prev.Seat(seat).Quality-1e-9 {
				t.Fatalf("step %d: %s got weaker", step, seat)
			}
			if Lineage(next.Seat(seat).Model) != Lineage(prev.Seat(seat).Model) {
				changed++
			}
			if next.Seat(seat).Model == dear.Model.ID && step == 0 {
				t.Fatalf("the first redo jumped to the dearest model in the catalog on %s", seat)
			}
		}
		if changed != 1 || len(next.Rungs) != 1 || !strings.Contains(next.Line("", -1), "→") {
			t.Fatalf("step %d moved %d seats: %q", step, changed, next.Line("", -1))
		}
		prev = next
	}
}

// A REDO OF A TASK THAT NEVER STARTED is asked again on the next-best models at
// the same price of a point — not escalated, because nothing ran.
func TestARedoOfATaskThatNeverStartedTakesTheNextBestAtTheSameCost(t *testing.T) {
	cands := catalogCandidates()
	ran, _ := Decide(Request{Class: Bugfix, Candidates: cands})
	again, err := Decide(Request{Class: Bugfix, Candidates: cands, Again: &ran})
	if err != nil {
		t.Fatal(err)
	}
	if again.Lambda != ran.Lambda {
		t.Fatalf("the retry moved λ from %v to %v", ran.Lambda, again.Lambda)
	}
	for _, seat := range Seats {
		if Lineage(again.Seat(seat).Model) == Lineage(ran.Seat(seat).Model) {
			t.Errorf("%s is the model that never started (%s)", seat, again.Seat(seat).Model)
		}
	}
}

// AN EFFORT WORD SAYS WHAT IT CHANGED, OR THAT IT CHANGED NOTHING.
func TestAnEffortWordSaysWhatItChanged(t *testing.T) {
	cands := catalogCandidates()
	best, _ := Decide(Request{Class: Bugfix, Candidates: cands, Effort: EffortBest})
	if len(best.Rungs) == 0 || best.Note != "" {
		t.Fatalf("--best on a fix changed the crew and said %v / %q", best.Rungs, best.Note)
	}
	only := []Candidate{candidateOf(kimiK3)} // nothing stronger to be had
	top, _ := Decide(Request{Class: Bugfix, Candidates: only, Effort: EffortBest})
	if top.Note != "best · already the strongest crew allowed" || !strings.Contains(top.Line("", -1), top.Note) {
		t.Fatalf("--best with nothing stronger said %q", top.Line("", -1))
	}
}

// A LEARNED OFFSET IS RUNGS: each step is one rung from the knee's crew, and the
// crew never skips to the top.
func TestALearnedOffsetIsRungsNotAJump(t *testing.T) {
	dear := catalogRow("somelab/very-dear", false, 30, 150, 60, 85, 70)
	dear.Model.Released = released(2026, 9, 1)
	cands := append(frontierCandidates(), dear)
	base, _ := Decide(Request{Class: Bugfix, Candidates: cands})
	one, _ := Decide(Request{Class: Bugfix, Candidates: cands, Steps: 1})
	moved := 0
	for _, seat := range Seats {
		if Lineage(one.Seat(seat).Model) != Lineage(base.Seat(seat).Model) {
			moved++
		}
		if one.Seat(seat).Model == "somelab/very-dear" {
			t.Fatalf("one learned step jumped to the dearest model on %s", seat)
		}
	}
	if moved != 1 {
		t.Fatalf("one learned step moved %d seats", moved)
	}
}

// A LINE LEADS WITH ITS STATE: a crew running on its fallback says so before
// the class and the seats, and a task that stopped before spending names no
// money — never a $0.000 that reads as a free success.
func TestTheLineLeadsWithTheFallbackAndNamesNoUnspentMoney(t *testing.T) {
	d := Decision{Class: Bugfix, EstUSD: 0.01, Crew: []Pick{
		{Seat: Worker, Model: "z-ai/glm-5.3-flash"}, {Seat: Planner, Model: "z-ai/glm-5.3-flash"}, {Seat: Checker, Model: "moonshotai/kimi-k3"},
	}}
	moved := d.WithRung(Worker, Pick{Model: "deepseek/deepseek-v4-flash"}, "credit unavailable on openrouter")
	line := moved.Line("", Unspent)
	if !strings.HasPrefix(line, "running on fallback crew · worker glm-5.3-flash → deepseek-v4-flash") {
		t.Errorf("the fallback is not first: %q", line)
	}
	if strings.Contains(line, "$") {
		t.Errorf("an unspent line names money: %q", line)
	}
}

// A REDO'S LINE SAYS EACH SEAT ONCE, WITH ITS CHANGE: a planner that moved is
// `planner glm-5.3-flash → kimi-k3` where the planner stands, not the
// planner and then its rung again.
func TestARedoLineSaysEachSeatOnce(t *testing.T) {
	d := Decision{Class: Bugfix, Crew: []Pick{
		{Seat: Worker, Model: "z-ai/glm-5.3-flash"}, {Seat: Planner, Model: "moonshotai/kimi-k3"}, {Seat: Checker, Model: "moonshotai/kimi-k3"},
	}, Rungs: []Retry{{Seat: Planner, From: "z-ai/glm-5.3-flash", To: "moonshotai/kimi-k3"}}}
	line := d.Line("", -1)
	if strings.Count(line, "planner") != 1 || !strings.Contains(line, "planner glm-5.3-flash → kimi-k3") {
		t.Errorf("the redo line reads %q", line)
	}
}

// A LONG LADDER IS ONE MOVE ON THE LINE: each seat says where it started,
// where it is, the first reason and how many it tried between, and the line
// stays within a card however many rungs were walked.
func TestALongLadderIsSaidAsItsNetMove(t *testing.T) {
	d := Decision{Class: Bugfix, EstUSD: 0.02, Crew: []Pick{
		{Seat: Worker, Model: "z-ai/glm-5.3-flash"}, {Seat: Planner, Model: "z-ai/glm-5.3-flash"}, {Seat: Checker, Model: "moonshotai/kimi-k3"},
	}}
	for _, seat := range []Seat{Worker, Planner} {
		why := "credit unavailable on openrouter"
		for _, m := range []string{"poolside/laguna-s-2.1", "poolside/laguna-xs-2.1", "nvidia/nemotron-3-super-120b-a12b", "google/gemma-4-31b-it"} {
			d = d.WithRung(seat, Pick{Model: m, Kind: Free}, why)
			why = "limit reached on openrouter"
		}
	}
	line := d.Line("", Unspent)
	if !strings.Contains(line, "worker glm-5.3-flash → gemma-4-31b-it (credit unavailable on openrouter; +3 tried)") || strings.Contains(line, "laguna") {
		t.Errorf("the ladder is not said as its net move: %q", line)
	}
	if n := len([]rune(line)); n > 260 {
		t.Errorf("the line is %d characters after a long ladder: %q", n, line)
	}
}

// A MODEL THAT PUBLISHES NOTHING BUT A PRICE IS NOT RANKED ON IT: a row with
// no index, no date and a near-zero price is neither a pick nor a rung, and a
// seat's last-rung rescue may still take it.
func TestAModelWithNoEvidenceIsNotPickedOnPrice(t *testing.T) {
	bare := Candidate{Model: Model{ID: "upstage/solar-mini4", PromptPrice: 1e-9, CompletionPrice: 1e-9, Tools: true},
		Routes: []Route{{Provider: "openrouter", Send: "upstage/solar-mini4", Kind: Metered}}}
	cands := append(catalogCandidates(), bare)
	for _, effort := range []Effort{"", EffortCheap} {
		d, err := Decide(Request{Class: Bugfix, Candidates: cands, Effort: effort})
		if err != nil {
			t.Fatal(err)
		}
		for _, pick := range d.Crew {
			if pick.Model == bare.Model.ID {
				t.Errorf("effort %q: the %s is a model with no evidence", effort, pick.Seat)
			}
			for _, rung := range d.Ladder[pick.Seat] {
				if rung.Model == bare.Model.ID {
					t.Errorf("effort %q: the %s's ladder holds a model with no evidence", effort, pick.Seat)
				}
			}
		}
	}
	rescue, err := Decide(Request{Class: Bugfix, Candidates: []Candidate{bare}, Rescue: true})
	if err != nil || rescue.Seat(Planner).Model != bare.Model.ID {
		t.Errorf("the rescue would not take it: %v %+v", err, rescue.Crew)
	}
}

// A ZERO OR UNKNOWN AMOUNT IS NOT DRAWN (the emptiness law): a run that made
// no call names no `$0.000` actual, a crew nothing could price names no
// `est $0.000`, and the segment goes with its separator rather than leaving
// one hanging. A real actual and a real estimate are both still said.
func TestTheLineDrawsNoZeroMoney(t *testing.T) {
	d := Decision{Class: Bugfix, EstUSD: 0.013, Crew: []Pick{
		{Seat: Worker, Model: "z-ai/glm-5.3-flash"}, {Seat: Planner, Model: "z-ai/glm-5.3-flash"}, {Seat: Checker, Model: "z-ai/glm-5.3-flash"},
	}}
	unpriced := d
	unpriced.EstUSD = 0
	for _, c := range []struct {
		line string
		want string
	}{
		{d.Line("", 0), "· est $0.013"},
		{d.Line("", -1), "· est $0.013"},
		{d.Line("", 0.004), "· $0.004 (est $0.013)"},
		{unpriced.Line("", -1), ""},
		{unpriced.Line("", 0), ""},
		{unpriced.Line("", 0.004), "· $0.004"},
	} {
		if strings.Contains(c.line, "$0.000") || strings.HasSuffix(c.line, "·") || strings.HasSuffix(c.line, "· ") || strings.Contains(c.line, "(est )") {
			t.Errorf("the line draws a zero or a dangling segment: %q", c.line)
		}
		if c.want != "" && !strings.HasSuffix(c.line, c.want) {
			t.Errorf("the line %q does not end on %q", c.line, c.want)
		}
		if c.want == "" && strings.Contains(c.line, "$") {
			t.Errorf("a line with nothing known names money: %q", c.line)
		}
	}
}
