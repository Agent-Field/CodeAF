package crewroute

import (
	"errors"
	"math"
	"math/rand"
	"strings"
	"testing"
	"time"
)

func TestThePriorWeightsParseAndAreSmall(t *testing.T) {
	if len(priorJSON) > 2<<20 {
		t.Fatalf("prior.json is %d bytes; the embedded weights must stay under 2 MB", len(priorJSON))
	}
	w := load()
	if w.Knee <= 0 || len(w.Shapes) != 3 || len(w.Features) == 0 {
		t.Fatalf("weights incomplete: knee %v, %d shapes, %d features", w.Knee, len(w.Shapes), len(w.Features))
	}
	for _, class := range Classes {
		for _, seat := range Seats {
			if _, ok := w.Link[class].Seats[seat]; !ok {
				t.Errorf("no link for %s/%s", class, seat)
			}
		}
	}
}

// THE WEIGHTS CARRY NO MODEL OF THEIR OWN: every model, the ones a person
// knows best included, is scored from its catalog row.
func TestTheWeightsCarryNoModelRows(t *testing.T) {
	for _, m := range []Model{glmFlash, kimiK3, v4Flash} {
		if strings.Contains(string(priorJSON), m.ID) || strings.Contains(string(priorJSON), ShortModel(m.ID)) {
			t.Errorf("prior.json names %s", m.ID)
		}
	}
}

// THE ROUTED POLICY, read off the weights and the prices rather than branches:
// open-ended work pays more for ability in every seat than a fix does, so
// there are prices at which open-ended work buys the stronger checker and a
// fix does not — and none at which a fix buys it and open-ended work does not.
func TestOpenEndedWorkBuysAStrongerCheckerAtPricesAFixDoesNot(t *testing.T) {
	tab := prior()
	for _, seat := range Seats {
		if fix, open := tab.linkOf(Bugfix).Seats[seat].Slope, tab.linkOf(OpenEnded).Seats[seat].Slope; open <= fix {
			t.Errorf("%s: open-ended slope %.3f not above the fix's %.3f", seat, open, fix)
		}
	}
	weak := candidateOf(glmFlash)
	split := false
	for mult := 0.25; mult <= 64; mult *= 1.25 {
		strong := glm53
		strong.ID = "acme/strong-checker"
		strong.PromptPrice, strong.CompletionPrice, strong.CacheReadPrice = glmFlash.PromptPrice*mult, glmFlash.CompletionPrice*mult, glmFlash.CacheReadPrice*mult
		cands := []Candidate{weak, candidateOf(strong)}
		fix, err := Decide(Request{Class: Bugfix, Candidates: cands})
		if err != nil {
			t.Fatal(err)
		}
		open, err := Decide(Request{Class: OpenEnded, Candidates: cands})
		if err != nil {
			t.Fatal(err)
		}
		fixBuys, openBuys := fix.Seat(Checker).Model == strong.ID, open.Seat(Checker).Model == strong.ID
		if fixBuys && !openBuys {
			t.Errorf("at %.2fx the flash price a fix buys the strong checker and open-ended work does not", mult)
		}
		split = split || (openBuys && !fixBuys)
	}
	if !split {
		t.Error("no price at which open-ended work buys the stronger checker and a fix does not")
	}
	for _, cands := range [][]Candidate{catalogCandidates(), frontierCandidates()} {
		fix, _ := Decide(Request{Class: Bugfix, Candidates: cands})
		open, _ := Decide(Request{Class: OpenEnded, Candidates: cands})
		if fix.EstUSD >= open.EstUSD {
			t.Errorf("a fix estimates $%.3f, open-ended work $%.3f: the fix should be the cheaper crew", fix.EstUSD, open.EstUSD)
		}
	}
}

// A CHEAPER MODEL AT LEAST AS GOOD ON EVERY PUBLISHED INDEX NEVER LOSES. For
// rows that publish the same indexes, A no dearer than B and at least as good
// on each of them, B is never scored above A in any seat nor cheaper, and a
// decision over the two never seats B — whatever their release dates,
// context, licence or family say.
func TestADominatedModelNeverBeatsItsDominator(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	names := []string{"deepseek/deepseek-v9-flash", "z-ai/glm-9", "acme/model", "moonshotai/kimi-k9", "anthropic/claude-opus-9"}
	for i := 0; i < 400; i++ {
		var a, b Model
		b = Model{ID: names[rng.Intn(len(names))], Open: rng.Intn(2) == 0, Tools: true,
			PromptPrice: (0.05 + rng.Float64()*5) / 1e6, CompletionPrice: (0.1 + rng.Float64()*25) / 1e6,
			Context: 1 << (17 + rng.Intn(4)), Released: released(2025+rng.Intn(2), time.Month(1+rng.Intn(12)), 1+rng.Intn(28))}
		b.CacheReadPrice = b.PromptPrice / 10
		a = Model{ID: names[rng.Intn(len(names))] + "-a", Open: rng.Intn(2) == 0, Tools: true,
			PromptPrice: b.PromptPrice * rng.Float64(), CompletionPrice: b.CompletionPrice * rng.Float64(),
			Context: 1 << (17 + rng.Intn(4)), Released: released(2025+rng.Intn(2), time.Month(1+rng.Intn(12)), 1+rng.Intn(28))}
		a.CacheReadPrice = a.PromptPrice / 10
		fields := []struct {
			get func(*Model) *float64
			lo  float64
		}{{func(m *Model) *float64 { return &m.Intelligence }, 20}, {func(m *Model) *float64 { return &m.Coding }, 40},
			{func(m *Model) *float64 { return &m.Agentic }, 20}, {func(m *Model) *float64 { return &m.ArenaElo }, 1150}}
		published := 0
		for _, f := range fields {
			if rng.Intn(2) == 0 {
				continue
			}
			published++
			v := f.lo + rng.Float64()*f.lo*0.8
			*f.get(&b) = v
			*f.get(&a) = v + rng.Float64()*f.lo*0.2
		}
		if published == 0 {
			a.Coding, b.Coding = 70, 60
		}
		tab := prior()
		for _, class := range []Class{Bugfix, OpenEnded, Other} {
			for _, seat := range Seats {
				qa, _ := tab.quality(class, seat, a)
				qb, _ := tab.quality(class, seat, b)
				if qb > qa+1e-9 {
					t.Fatalf("%s %s: dominated %+v scored %.4f above %+v at %.4f", class, seat, b, qb, a, qa)
				}
				if tab.classCost(class, seat, b) < tab.classCost(class, seat, a) {
					t.Fatalf("%s %s: the dominated model costs less", class, seat)
				}
			}
			d, err := Decide(Request{Class: class, Candidates: []Candidate{candidateOf(b), candidateOf(a)}})
			if err != nil {
				continue
			}
			for _, p := range d.Crew {
				if p.Model == b.ID {
					t.Fatalf("%s: %s seated the dominated %+v over %+v", class, p.Seat, b, a)
				}
			}
		}
	}
	// The row that started it: a flash model no dearer on any price than
	// another and better on every index the other publishes, which publishes
	// one index to its four.
	thin := Model{ID: "deepseek/deepseek-v4.1-flash", Open: true, PromptPrice: 1.5e-7, CompletionPrice: 6e-7, CacheReadPrice: glmFlash.CacheReadPrice,
		Intelligence: 39.5, Context: 1048576, Released: released(2026, 9, 10), Tools: true}
	for _, class := range []Class{Bugfix, OpenEnded} {
		for _, effort := range []Effort{EffortCheap, EffortKnee} {
			d, err := Decide(Request{Class: class, Effort: effort, Candidates: []Candidate{candidateOf(thin), candidateOf(glmFlash)}})
			if err != nil {
				t.Fatal(err)
			}
			if got := d.Seat(Worker).Model; got != glmFlash.ID {
				t.Errorf("%s %q: worker %s over %s", class, effort, got, glmFlash.ID)
			}
		}
	}
}

// THE SUPPORT SEATS OF A FIX STILL PAY FOR ABILITY: a model that publishes one
// middling index at a fraction of a cent does not check a fix while a model
// strong on every index costs a few tenths of a cent more.
func TestAFixIsNotCheckedByWhateverIsCheapest(t *testing.T) {
	if s := prior().linkOf(Bugfix).Seats[Checker].Slope; s <= 0 {
		t.Fatalf("a fix's checker pays nothing for ability (slope %.3f)", s)
	}
	thin := Model{ID: "inclusionai/ling-3.0-flash", Open: true, PromptPrice: 2.1e-8, CompletionPrice: 6.3e-8, CacheReadPrice: 4.2e-9,
		Coding: 50.6, Context: 262144, Released: released(2026, 7, 23), Tools: true}
	d, err := Decide(Request{Class: Bugfix, Candidates: []Candidate{candidateOf(thin), candidateOf(glmFlash)}})
	if err != nil {
		t.Fatal(err)
	}
	if got := d.Seat(Checker).Model; got != glmFlash.ID {
		t.Errorf("a fix's checker is %s", got)
	}
	// A CHECKER'S MEAN ABILITY MUST REACH THE FLOOR when any candidate's does.
	tab := forDecision([]Candidate{candidateOf(thin), candidateOf(v4Flash), candidateOf(glmFlash)}, nil)
	for _, m := range []Model{thin, v4Flash, glmFlash} {
		a := tab.abilityOf(m)
		if pick, ok := eligible(tab, Bugfix, Checker, candidateOf(m)); ok && a.U < tab.UFloor {
			t.Errorf("%s may check with mean ability %.3f under the floor %.3f", pick.Model, a.U, tab.UFloor)
		}
	}
}

// --BEST BUYS QUALITY IN EVERY SEAT, up to the task limit: no seat of a best
// crew is left on a model another candidate outscores there.
func TestBestBuysTheStrongestModelInEverySeat(t *testing.T) {
	cands := frontierCandidates()
	for _, class := range []Class{Bugfix, OpenEnded} {
		d, err := Decide(Request{Class: class, Candidates: cands, Effort: EffortBest, TaskCap: 5})
		if err != nil {
			t.Fatal(err)
		}
		tab := forDecision(cands, nil)
		for _, p := range d.Crew {
			for _, c := range cands {
				if other, ok := eligible(tab, class, p.Seat, c); ok && other.Quality > p.Quality+1e-9 {
					t.Errorf("%s best: %s is %s at %.3f while %s scores %.3f", class, p.Seat, p.Model, p.Quality, other.Model, other.Quality)
				}
			}
		}
	}
}

// WITH ONLY CATALOG METADATA, a model whose published indexes are stronger
// wins the seat from one at the same price, context and release date.
func TestAStrongerIndexedSimilarPricedModelWinsASeat(t *testing.T) {
	weak := catalogRow("acme/coder-a", true, 0.3, 1.2, 35, 60, 35)
	strong := catalogRow("zeta/coder-b", true, 0.3, 1.2, 48, 76, 55)
	for _, c := range []*Candidate{&weak, &strong} {
		c.Model.Released = released(2026, 8, 1)
	}
	for _, class := range []Class{OpenEnded, Other} {
		d, err := Decide(Request{Class: class, Candidates: []Candidate{weak, strong}})
		if err != nil {
			t.Fatal(err)
		}
		for _, seat := range []Seat{Worker, Checker} {
			if got := d.Seat(seat).Model; got != strong.Model.ID {
				t.Errorf("%s %s went to %s, want the stronger-indexed model", class, seat, got)
			}
		}
	}
}

// A MODEL WITH NO USABLE METADATA is not scored, so it is neither a pick nor a
// rung — however cheap — and it still sits a seat a person pins it to.
func TestAModelWithNoUsableMetadataIsNotPickedUnlessPinned(t *testing.T) {
	bare := Candidate{Model: Model{ID: "somelab/mystery", Context: 1_000_000, Tools: true},
		Routes: []Route{{Provider: "openrouter", Send: "somelab/mystery", Kind: Metered}}}
	if Scorable(bare.Model) {
		t.Fatal("a row with nothing but a context window was scored")
	}
	cands := append(catalogCandidates(), bare)
	for _, class := range Classes {
		for _, effort := range []Effort{EffortCheap, EffortKnee, EffortBest} {
			d, err := Decide(Request{Class: class, Candidates: cands, Effort: effort})
			if err != nil {
				t.Fatal(err)
			}
			for _, pick := range d.Crew {
				if pick.Model == bare.Model.ID {
					t.Errorf("%s/%q: the %s went to a model with no usable metadata", class, effort, pick.Seat)
				}
				for _, rung := range d.Ladder[pick.Seat] {
					if rung.Model == bare.Model.ID {
						t.Errorf("%s/%q: the %s's ladder holds it", class, effort, pick.Seat)
					}
				}
			}
		}
	}
	d, err := Decide(Request{Class: Bugfix, Candidates: cands, Pins: map[Seat]Pin{
		Worker: {Model: bare.Model.ID, Send: bare.Model.ID, Kind: Metered},
	}})
	if err != nil || d.Seat(Worker).Model != bare.Model.ID || !d.Seat(Worker).Pinned {
		t.Fatalf("the pin was not honoured: %+v %v", d.Seat(Worker), err)
	}
}

// A MISSING INDEX WIDENS THE READING: a row that publishes one index is read
// with more doubt than one that publishes all three.
func TestAMissingIndexWidensTheVariance(t *testing.T) {
	one := catalogRow("acme/one-index", true, 0.3, 1.2, 0, 70, 0).Model
	all := catalogRow("acme/all-indexes", true, 0.3, 1.2, 45, 70, 50).Model
	w := load()
	if a, b := w.abilityOf(one), w.abilityOf(all); a.VarTheta <= b.VarTheta {
		t.Errorf("one index read with variance %.4g, three with %.4g", a.VarTheta, b.VarTheta)
	}
}

// A ROW THAT GAINS INDEXES IS READ AGAIN: nothing about a model is remembered
// between decisions, so the next catalog refresh re-scores it — from its
// indexes now, and with less doubt.
func TestARowThatGainsIndexesIsRescored(t *testing.T) {
	bare := glmFlash
	bare.Intelligence, bare.Coding, bare.Agentic, bare.ArenaElo = 0, 0, 0, 0
	w := load()
	before, after := w.abilityOf(bare), w.abilityOf(glmFlash)
	if before.Indexed || !after.Indexed {
		t.Fatalf("index path: before %v, after %v", before.Indexed, after.Indexed)
	}
	if after.VarTheta >= before.VarTheta {
		t.Errorf("published indexes did not narrow the reading: variance %.3f then %.3f", before.VarTheta, after.VarTheta)
	}
	qb, _ := prior().quality(OpenEnded, Worker, bare)
	qa, _ := prior().quality(OpenEnded, Worker, glmFlash)
	if qa <= qb {
		t.Errorf("strong published indexes scored %.3f, not above the bare row's %.3f", qa, qb)
	}
}

// A ROW WITH NO PUBLISHED INDEX IS NEVER SCORED ABOVE THE POPULATION'S MEAN,
// however new it is: its date and family widen the reading instead.
func TestANewRowWithoutIndexesIsNotScoredAboveAverage(t *testing.T) {
	w := load()
	pop := w.Mean[0]*w.Scale[0] + w.Loc[0]
	fresh := Model{ID: "openai/gpt-9-luna-pro", PromptPrice: 1e-7, CompletionPrice: 5e-7, Context: 1050000,
		Released: released(2026, 9, 22), Tools: true}
	if a := w.abilityOf(fresh); a.Theta > pop+1e-9 || a.Indexed {
		t.Errorf("an index-less row read at ability %.3f over the population's %.3f", a.Theta, pop)
	}
	if d, err := Decide(Request{Class: Bugfix, Candidates: []Candidate{candidateOf(fresh), candidateOf(glmFlash)}}); err != nil || d.Seat(Worker).Model != glmFlash.ID {
		t.Errorf("worker %s over the indexed flash model (%v)", d.Seat(Worker).Model, err)
	}
}

// THIS INSTALL'S OUTCOMES MOVE A SCORE WITHOUT FREEZING IT: the learned move
// adds to the model's reading, and the reading still follows its catalog row.
func TestInstallEvidenceMovesAScoreWithoutFreezingIt(t *testing.T) {
	key := LearnKey(OpenEnded, Checker, glmFlash.ID)
	plain, _ := Decide(Request{Class: OpenEnded, Candidates: []Candidate{candidateOf(glmFlash)}})
	learned, _ := Decide(Request{Class: OpenEnded, Candidates: []Candidate{candidateOf(glmFlash)}, Learned: map[string]float64{key: 0.5}})
	if got := learned.Seat(Checker).Quality - plain.Seat(Checker).Quality; math.Abs(got-0.5) > 1e-9 || learned.Seat(Checker).Learned != 0.5 {
		t.Fatalf("a learned +0.5 moved the checker by %.3f (recorded %.3f)", got, learned.Seat(Checker).Learned)
	}
	cheaper := glmFlash
	cheaper.Agentic = 30
	moved, _ := Decide(Request{Class: OpenEnded, Candidates: []Candidate{candidateOf(cheaper)}, Learned: map[string]float64{key: 0.5}})
	if moved.Seat(Checker).Quality == learned.Seat(Checker).Quality {
		t.Error("with a learned move in place, a changed catalog row no longer moves the score")
	}
}

// A redo lowers what a model is worth to this install and an accepted task
// raises it: the learned move changes who sits the seat.
func TestALearnedMoveCanChangeThePick(t *testing.T) {
	cands := catalogCandidates()
	base, _ := Decide(Request{Class: OpenEnded, Candidates: cands})
	was := base.Seat(Checker).Model
	down, _ := Decide(Request{Class: OpenEnded, Candidates: cands, Learned: map[string]float64{LearnKey(OpenEnded, Checker, was): -3}})
	if down.Seat(Checker).Model == was {
		t.Errorf("a checker this install marked down by 3 points still sits the seat")
	}
}

// FRONTIER MODELS ARE CANDIDATES: --best may pick them, and no crew is
// chosen whose estimate is over the task limit.
func TestBestNeverPicksACrewOverTheTaskLimit(t *testing.T) {
	cands := frontierCandidates()
	best, err := Decide(Request{Class: OpenEnded, Candidates: cands, Effort: EffortBest, TaskCap: 5})
	if err != nil {
		t.Fatal(err)
	}
	frontier := false
	for _, pick := range best.Crew {
		if strings.HasPrefix(pick.Model, "anthropic/") || strings.HasPrefix(pick.Model, "openai/") {
			frontier = true
		}
	}
	if !frontier {
		t.Errorf("--best on open-ended work picked no frontier model: %s", best.Line("", -1))
	}
	for _, limit := range []float64{5, 1, 0.2} {
		for _, class := range Classes {
			for _, effort := range []Effort{EffortKnee, EffortBest} {
				for _, factor := range []float64{0, 1, 2.5} {
					d, err := Decide(Request{Class: class, Candidates: cands, Effort: effort, TaskCap: limit, CostFactor: factor})
					if err != nil {
						t.Fatal(err)
					}
					if est := crewEst(prior(), class, d.Crew, factor); est > limit+1e-9 {
						t.Errorf("%s/%q at $%v (factor %v): crew estimated $%.3f: %s", class, effort, limit, factor, est, d.Line("", -1))
					}
				}
			}
		}
	}
	tight, _ := Decide(Request{Class: OpenEnded, Candidates: cands, Effort: EffortBest, TaskCap: 0.2})
	if !strings.Contains(tight.Note, "task limit") {
		t.Errorf("a crew held under the limit does not say so: %q", tight.Note)
	}
	if est := tight.EstUSD; est > 0.2+1e-9 {
		t.Errorf("held crew estimates $%.3f", est)
	}
}

func TestAPinnedSeatAlwaysRunsItsPin(t *testing.T) {
	cands := catalogCandidates()
	d, err := Decide(Request{Class: Bugfix, Candidates: cands, Pins: map[Seat]Pin{
		Checker: {Model: "moonshotai/kimi-k3", Send: "moonshotai/kimi-k3", Kind: Metered},
	}})
	if err != nil {
		t.Fatal(err)
	}
	checker := d.Seat(Checker)
	if !checker.Pinned || checker.Model != "moonshotai/kimi-k3" {
		t.Errorf("checker %+v, want the kimi pin", checker)
	}
	if d.Seat(Worker).Pinned {
		t.Errorf("an unpinned worker was not routed: %+v", d.Seat(Worker))
	}
	// A pin the candidates do not carry still sits its seat on its own send.
	d, err = Decide(Request{Class: Bugfix, Candidates: cands, Pins: map[Seat]Pin{
		Worker: {Model: "ollama/qwen3-coder", Provider: "ollama", Send: "ollama/qwen3-coder", Kind: Local},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if w := d.Seat(Worker); w.Send != "ollama/qwen3-coder" || w.CostUSD != 0 || !w.Pinned {
		t.Errorf("outside pin: %+v", w)
	}
}

func TestTheCheapestRouteWinsAndAPlanCostsNothing(t *testing.T) {
	cands := []Candidate{{Model: glmFlash, Routes: []Route{
		{Provider: "openrouter", Send: "openrouter/z-ai/glm-5.3-flash", Kind: Metered},
		{Provider: "z-ai", Send: "z-ai/glm-5.3-flash", Kind: Plan},
	}}}
	d, err := Decide(Request{Class: Bugfix, Candidates: cands})
	if err != nil {
		t.Fatal(err)
	}
	for _, pick := range d.Crew {
		if pick.Provider != "z-ai" || pick.CostUSD != 0 {
			t.Errorf("%s: %+v, want the coding plan at no marginal cost", pick.Seat, pick)
		}
	}
	if d.EstUSD != 0 {
		t.Errorf("a crew entirely on a plan estimates $%v", d.EstUSD)
	}
	// And a pinned provider is kept even when it is the dearer route.
	d, _ = Decide(Request{Class: Bugfix, Candidates: cands, Pins: map[Seat]Pin{
		Worker: {Model: "z-ai/glm-5.3-flash", Provider: "openrouter"},
	}})
	if w := d.Seat(Worker); w.Provider != "openrouter" || w.Send != "openrouter/z-ai/glm-5.3-flash" || w.CostUSD == 0 {
		t.Errorf("@openrouter pin: %+v", w)
	}
}

func TestNoCandidateIsAnErrorNamingTheSeat(t *testing.T) {
	_, err := Decide(Request{Class: Bugfix})
	var missing NoCandidateError
	if !errors.As(err, &missing) || missing.Seat != Worker {
		t.Fatalf("err %v, want a NoCandidateError for the worker", err)
	}
}

func TestRedoStrongerEscalatesOnlyUnpinnedSeats(t *testing.T) {
	cands := catalogCandidates()
	first, _ := Decide(Request{Class: Bugfix, Candidates: cands})
	again, err := Decide(Request{Class: Bugfix, Candidates: cands, Stronger: &first})
	if err != nil {
		t.Fatal(err)
	}
	if again.Quality <= first.Quality {
		t.Fatalf("redo stronger: %.2f is not above %.2f", again.Quality, first.Quality)
	}
	pins := map[Seat]Pin{Worker: {Model: "z-ai/glm-5.3-flash"}}
	first, _ = Decide(Request{Class: OpenEnded, Candidates: cands, Pins: pins, Effort: EffortCheap})
	first.Rungs, first.Note = nil, ""
	again, err = Decide(Request{Class: OpenEnded, Candidates: cands, Pins: pins, Stronger: &first})
	if err != nil {
		t.Fatal(err)
	}
	if w := again.Seat(Worker); !w.Pinned || w.Model != "z-ai/glm-5.3-flash" {
		t.Errorf("redo moved a pinned worker: %+v", w)
	}
	// Every seat pinned: the pins stay, and the one run steps over them.
	all := map[Seat]Pin{Worker: {Model: "z-ai/glm-5.3-flash"}, Planner: {Model: "z-ai/glm-5.3-flash"}, Checker: {Model: "z-ai/glm-5.3-flash"}}
	first, _ = Decide(Request{Class: OpenEnded, Candidates: cands, Pins: all})
	again, err = Decide(Request{Class: OpenEnded, Candidates: cands, Pins: all, Stronger: &first})
	if err != nil {
		t.Fatal(err)
	}
	if !again.OneOff || again.Quality <= first.Quality {
		t.Errorf("all-pinned redo: one-off %v, quality %.2f over %.2f", again.OneOff, again.Quality, first.Quality)
	}
	// And the strongest crew there is has nowhere to go.
	best, _ := Decide(Request{Class: OpenEnded, Candidates: cands, Effort: EffortBest})
	best.Crew = []Pick{
		{Seat: Worker, Model: "acme/top", Quality: 99}, {Seat: Planner, Model: "acme/top", Quality: 99}, {Seat: Checker, Model: "acme/top", Quality: 99},
	}
	if _, err := Decide(Request{Class: OpenEnded, Candidates: cands, Stronger: &best}); !errors.Is(err, ErrStrongest) {
		t.Errorf("redo of the strongest crew: %v, want ErrStrongest", err)
	}
}

func TestLearnedStepsStartAFixHigher(t *testing.T) {
	cands := catalogCandidates()
	base, _ := Decide(Request{Class: Bugfix, Candidates: cands})
	d, _ := Decide(Request{Class: Bugfix, Candidates: cands, Steps: 2})
	if d.Quality <= base.Quality {
		t.Errorf("two learned steps on a fix: quality %.2f, not above the knee's %.2f", d.Quality, base.Quality)
	}
}

func TestPaceGrowsAsTheCapNears(t *testing.T) {
	cases := []struct {
		spent, cap, want float64
		atCap            bool
	}{
		{0, 0, 1, false},
		{1, 10, 1, false},
		{5, 10, 1, false},
		{7.5, 10, 2, false},
		{9, 10, 5, false},
		{10, 10, 1, true},
		{12, 10, 1, true},
	}
	for _, tc := range cases {
		got, at := Pace(tc.spent, tc.cap)
		if math.Abs(got-tc.want) > 1e-9 || at != tc.atCap {
			t.Errorf("Pace(%v, %v) = %v, %v; want %v, %v", tc.spent, tc.cap, got, at, tc.want, tc.atCap)
		}
	}
	// Near the cap an open-ended task takes a cheaper crew.
	cands := frontierCandidates()
	plain, _ := Decide(Request{Class: OpenEnded, Candidates: cands})
	mult, _ := Pace(9.9, 10)
	d, _ := Decide(Request{Class: OpenEnded, Candidates: cands, Pace: mult})
	if d.EstUSD >= plain.EstUSD {
		t.Errorf("at 99%% of the cap the crew still estimates $%.3f against $%.3f (λ %.0f)", d.EstUSD, plain.EstUSD, d.Lambda)
	}
}

func TestGapsNameAMissingStrongChecker(t *testing.T) {
	if gaps := Gaps(catalogCandidates()); len(gaps) != 0 {
		t.Errorf("a set with a strong checker has gaps %+v", gaps)
	}
	gaps := Gaps([]Candidate{candidateOf(v4Flash)})
	if len(gaps) != 1 || gaps[0].Seat != Checker || gaps[0].Class != OpenEnded {
		t.Errorf("v4-flash alone: gaps %+v, want the open-ended checker", gaps)
	}
}

func TestTheDecisionLine(t *testing.T) {
	cands := catalogCandidates()
	d, _ := Decide(Request{Class: OpenEnded, Candidates: cands, Pins: map[Seat]Pin{Checker: {Model: "moonshotai/kimi-k3"}}})
	got := d.Line("📌", 0.108)
	want := "open-ended · worker " + ShortModel(d.Seat(Worker).Model) + " (openrouter)"
	if !strings.HasPrefix(got, want) || !strings.Contains(got, " · checker 📌 kimi-k3 · $0.108 (est "+Money(d.EstUSD)+")") {
		t.Errorf("line %q", got)
	}
	if got := d.Line("📌", -1); !strings.HasSuffix(got, " · est "+Money(d.EstUSD)) {
		t.Errorf("line before the run ends: %q", got)
	}
}

// THE ROUTER'S OWN BUDGET: under two milliseconds a decision, against a
// catalog the size of the real one, classification included.
func TestADecisionTakesUnderTwoMilliseconds(t *testing.T) {
	cands := frontierCandidates()
	for i := 0; i < 600; i++ {
		row := catalogRow("acme/m"+string(rune('a'+i%26))+strings.Repeat("x", i%7)+string(rune('a'+i/26)), i%2 == 0,
			0.1+float64(i%30)/10, 0.5+float64(i%40)/5, 20+float64(i%35), 40+float64(i%45), 20+float64(i%40))
		if i%3 == 0 {
			row.Model.Intelligence, row.Model.Agentic = 0, 0
		}
		row.Model.Released = released(2025, time.Month(1+i%12), 1+i%28)
		cands = append(cands, row)
	}
	task := Task{Text: "fix: crash when the config has no trailing newline\n\nTraceback (most recent call last):\n  ...\nValueError: bad"}
	start := time.Now()
	const runs = 200
	for i := 0; i < runs; i++ {
		if _, err := Decide(Request{Task: task, Candidates: cands, TaskCap: 5}); err != nil {
			t.Fatal(err)
		}
	}
	if per := time.Since(start) / runs; per > 2*time.Millisecond {
		t.Errorf("a decision took %v; the budget is 2ms", per)
	}
}

func TestRouteIsDeterministicWhateverTheOrder(t *testing.T) {
	cands := append(frontierCandidates(), catalogRow("acme/twin-a", true, 0.15, 0.5, 41.8, 71.5, 50.9), catalogRow("acme/twin-b", true, 0.15, 0.5, 41.8, 71.5, 50.9))
	for _, class := range Classes {
		a, _ := Decide(Request{Class: class, Candidates: cands})
		reversed := make([]Candidate, len(cands))
		for i := range cands {
			reversed[len(cands)-1-i] = cands[i]
		}
		b, _ := Decide(Request{Class: class, Candidates: reversed})
		for _, seat := range Seats {
			if a.Seat(seat).Model != b.Seat(seat).Model {
				t.Errorf("%s %s: %s one way, %s the other", class, seat, a.Seat(seat).Model, b.Seat(seat).Model)
			}
		}
	}
}

// THE PORT READS THE WEIGHTS AS THEY WERE FITTED: the family key drops
// version numbers and mostly-numeric tokens.
func TestAFamilyIsTheNameWithoutItsVersion(t *testing.T) {
	for id, want := range map[string]string{
		"z-ai/glm-5.3-flash":         "z-ai/glm-flash",
		"moonshotai/kimi-k3":         "moonshotai/kimi",
		"deepseek/deepseek-v4-flash": "deepseek/deepseek-flash",
		"anthropic/claude-opus-5":    "anthropic/claude-opus",
		"qwen/qwen3-coder-30b-a3b":   "qwen/qwen-coder",
	} {
		if got := familyOf(id); got != want {
			t.Errorf("familyOf(%q) = %q, want %q", id, got, want)
		}
	}
}
