package crewroute

import (
	"strings"
	"testing"
)

// THE OWNER'S TASK: an unmeasured model whose only route is a free pool, with
// published figures as good as anything measured, took the worker seat of an
// open-ended task and died in its first second. It must never beat the
// measured models on a price of zero.
func TestAFreeUnmeasuredModelNeverTakesASeatOnPrice(t *testing.T) {
	stranger := catalogRow("thinkingmachines/inkling-small", true, 0, 0, 60, 80, 70)
	stranger.Routes = []Route{{Provider: "openrouter", Send: "thinkingmachines/inkling-small:free", Kind: Free}}
	candidates := append(evidenceCandidates(), stranger)
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

// NO PRICE MAKES AN UNMEASURED MODEL WIN ON COST ALONE: its cost is weighed at
// no less than the cheapest measured model's, and its quality under the
// measured middle, so a stranger cheaper than everything measured still loses
// the open-ended worker seat to the model watched doing it.
func TestAnUnmeasuredModelIsWeighedAtTheCostFloor(t *testing.T) {
	cheap := catalogRow("somelab/cheap-coder", true, 0.01, 0.02, 60, 80, 70)
	d, err := Decide(Request{Class: OpenEnded, Candidates: append(evidenceCandidates(), cheap)})
	if err != nil {
		t.Fatal(err)
	}
	if got := d.Seat(Worker).Model; got != "z-ai/glm-5.3-flash" {
		t.Fatalf("open-ended worker = %s, want the measured flash", got)
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
	m, _ := Snapshot("z-ai/glm-5.3-flash")
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
	d, err := Decide(Request{Class: Bugfix, Candidates: evidenceCandidates(), Avoid: avoid})
	if err != nil {
		t.Fatal(err)
	}
	for _, pick := range d.Crew {
		if Lineage(pick.Model) == Lineage("z-ai/glm-5.3-flash") {
			t.Fatalf("%s went to the avoided model", pick.Seat)
		}
	}
	pinned, err := Decide(Request{Class: Bugfix, Candidates: evidenceCandidates(), Avoid: avoid,
		Pins: map[Seat]Pin{Worker: {Model: "z-ai/glm-5.3-flash", Send: "z-ai/glm-5.3-flash", Kind: Metered}}})
	if err != nil || pinned.Seat(Worker).Model != "z-ai/glm-5.3-flash" {
		t.Fatalf("a pin was overruled: %+v %v", pinned.Seat(Worker), err)
	}
	// Avoiding the only model there is still makes a crew.
	only := evidenceCandidates()[:1]
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

// A MODEL THAT PUBLISHES ONE FLATTERING INDEX AND NOTHING ELSE is not read as
// though it published three: the owner's free stranger had only a coding
// figure, and must not sit an agent-loop seat on it.
func TestAMissingIndexCountsAgainstTheModel(t *testing.T) {
	onlyCoding := catalogRow("thinkingmachines/inkling-small", true, 0.01, 0.02, 0, 52.9, 0)
	full := catalogRow("somelab/full", true, 0.01, 0.02, 52.9, 52.9, 52.9)
	if indexScore(Worker, onlyCoding.Model) >= indexScore(Worker, full.Model) {
		t.Fatal("a model with two missing indexes read as well as one that published them")
	}
	for _, seat := range []Seat{Worker, Checker} {
		if _, ok := eligible(prior(), Bugfix, seat, onlyCoding); ok {
			t.Errorf("an unmeasured model with no agentic index sat the %s seat", seat)
		}
	}
}

// THE OWNER'S CATALOG: glm-5.3-flash at $0.15/M in, the free stranger with
// no price and one index, no seat pinned. Every class keeps the stranger out,
// with free routes on or off.
func TestTheOwnersCatalogNeverSeatsTheFreeStranger(t *testing.T) {
	glm, _ := Snapshot("z-ai/glm-5.3-flash")
	if glm.PromptPrice != 1.5e-7 {
		t.Fatalf("the snapshot prices glm at %v, want the owner's $0.15/M", glm.PromptPrice)
	}
	stranger := Candidate{
		Model:  Model{ID: "thinkingmachines/inkling-small", Coding: 52.9, Context: 262_144, Tools: true},
		Routes: []Route{{Provider: "router", Send: "thinkingmachines/inkling-small:free", Kind: Free}},
	}
	for _, class := range Classes {
		d, err := Decide(Request{Class: class, Candidates: append(evidenceCandidates(), stranger)})
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
	cands := evidenceCandidates()
	dear := catalogRow("somelab/very-dear", false, 30, 150, 60, 80, 70)
	cands = append(cands, dear)
	ran := Decision{Class: OpenEnded, Crew: []Pick{
		{Seat: Worker, Model: "z-ai/glm-5.3-flash", Quality: 2.0},
		{Seat: Planner, Model: "z-ai/glm-5.3-flash", Quality: 0.6},
		{Seat: Checker, Model: "z-ai/glm-5.3-flash", Quality: 1.0},
	}}
	first, err := Decide(Request{Class: OpenEnded, Candidates: cands, Stronger: &ran})
	if err != nil {
		t.Fatal(err)
	}
	changed := 0
	for _, seat := range Seats {
		if first.Seat(seat).Quality < ran.Seat(seat).Quality-1e-9 {
			t.Fatalf("%s got weaker", seat)
		}
		if Lineage(first.Seat(seat).Model) != Lineage(ran.Seat(seat).Model) {
			changed++
		}
	}
	if changed != 1 || first.Seat(Checker).Model != "deepseek/deepseek-v4-flash" {
		t.Fatalf("one redo moved %d seats, checker %s; want one rung: the checker to v4-flash", changed, first.Seat(Checker).Model)
	}
	if len(first.Rungs) != 1 || !strings.Contains(first.Line("", -1), "checker glm-5.3-flash → deepseek-v4-flash") {
		t.Fatalf("the rung is not on the line: %q", first.Line("", -1))
	}
	second, err := Decide(Request{Class: OpenEnded, Candidates: cands, Stronger: &first})
	if err != nil {
		t.Fatal(err)
	}
	if second.Seat(Checker).Model != "moonshotai/kimi-k3" {
		t.Fatalf("the second redo put %s on the checker, want the next rung, kimi", second.Seat(Checker).Model)
	}
	for _, pick := range second.Crew {
		if pick.Model == dear.Model.ID {
			t.Fatalf("a redo jumped to the dearest model in the catalog on %s", pick.Seat)
		}
	}
}

// A REDO OF A TASK THAT NEVER STARTED is asked again on the next-best models at
// the same price of a point — not escalated, because nothing ran.
func TestARedoOfATaskThatNeverStartedTakesTheNextBestAtTheSameCost(t *testing.T) {
	cands := evidenceCandidates()
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
	cands := evidenceCandidates()
	best, _ := Decide(Request{Class: Bugfix, Candidates: cands, Effort: EffortBest})
	if len(best.Rungs) == 0 || best.Note != "" {
		t.Fatalf("--best on a fix changed the crew and said %v / %q", best.Rungs, best.Note)
	}
	only := cands[1:2] // kimi alone: nothing stronger to be had
	top, _ := Decide(Request{Class: Bugfix, Candidates: only, Effort: EffortBest})
	if top.Note != "best · already the strongest measured crew" || !strings.Contains(top.Line("", -1), top.Note) {
		t.Fatalf("--best with nothing stronger said %q", top.Line("", -1))
	}
}

// A LEARNED OFFSET IS RUNGS: each step is one rung from the knee's crew, and the
// crew never skips to the top.
func TestALearnedOffsetIsRungsNotAJump(t *testing.T) {
	cands := append(evidenceCandidates(), catalogRow("somelab/very-dear", false, 30, 150, 60, 80, 70))
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
