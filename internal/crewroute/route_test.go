package crewroute

import (
	"errors"
	"math"
	"strings"
	"testing"
	"time"
)

// The evidence table's own model set, priced as the catalog published it on the day
// the evidence was measured, each reachable on OpenRouter only.
func evidenceCandidates() []Candidate {
	var out []Candidate
	for _, id := range []string{"z-ai/glm-5.3-flash", "moonshotai/kimi-k3", "deepseek/deepseek-v4-flash"} {
		m, _ := Snapshot(id)
		out = append(out, Candidate{Model: m, Routes: []Route{{Provider: "openrouter", Send: id, Kind: Metered}}})
	}
	return out
}

// catalogRow is an unmeasured model with published figures.
func catalogRow(id string, open bool, in, out float64, intel, coding, agentic float64) Candidate {
	return Candidate{
		Model: Model{ID: id, Open: open, PromptPrice: in / 1e6, CompletionPrice: out / 1e6, CacheReadPrice: in / 1e7,
			Intelligence: intel, Coding: coding, Agentic: agentic, Context: 1_000_000, Tools: true},
		Routes: []Route{{Provider: "openrouter", Send: id, Kind: Metered}},
	}
}

func TestThePriorTableParsesAndIsSmall(t *testing.T) {
	if len(priorJSON) > 2<<20 {
		t.Fatalf("prior.json is %d bytes; the embedded table must stay under 2 MB", len(priorJSON))
	}
	tab := prior()
	if tab.Knee <= 0 || len(tab.Shapes) != 3 || len(tab.Cells) == 0 {
		t.Fatalf("prior table incomplete: knee %v, %d shapes, %d cells", tab.Knee, len(tab.Shapes), len(tab.Cells))
	}
}

// THE MEASURED CREWS ADD BACK UP TO WHAT WAS MEASURED, and cost about what
// they cost. If a split or a shape moves, this is where it says so.
func TestTheTableReproducesTheEvidence(t *testing.T) {
	tab := prior()
	flash, _ := Snapshot("z-ai/glm-5.3-flash")
	kimi, _ := Snapshot("moonshotai/kimi-k3")
	v4, _ := Snapshot("deepseek/deepseek-v4-flash")
	crew := func(class Class, w, p, c Model) (q, usd float64) {
		for seat, m := range map[Seat]Model{Worker: w, Planner: p, Checker: c} {
			v, _ := tab.quality(class, seat, m)
			q += v
			usd += tab.seatCost(seat, m)
		}
		return q, usd
	}
	cases := []struct {
		name       string
		class      Class
		w, p, c    Model
		quality    float64
		usd, slack float64
	}{
		{"fix, all flash", Bugfix, flash, flash, flash, 6.57, 0.023, 0.006},
		{"fix, all kimi", Bugfix, kimi, kimi, kimi, 6.86, 0.351, 0.06},
		{"open-ended, all flash", OpenEnded, flash, flash, flash, 3.60, 0.023, 0.006},
		{"open-ended, kimi checker", OpenEnded, flash, flash, kimi, 7.60, 0.117, 0.01},
		{"open-ended, v4-flash checker", OpenEnded, flash, flash, v4, 5.70, 0.02, 0.01},
	}
	for _, tc := range cases {
		q, usd := crew(tc.class, tc.w, tc.p, tc.c)
		if math.Abs(q-tc.quality) > 1e-6 {
			t.Errorf("%s: quality %.3f, measured %.2f", tc.name, q, tc.quality)
		}
		if math.Abs(usd-tc.usd) > tc.slack {
			t.Errorf("%s: cost $%.4f, measured about $%.3f", tc.name, usd, tc.usd)
		}
	}
}

// THE ROUTED POLICY, read off prices rather than branches: a fix goes to the
// cheapest crew, open-ended work to the cheapest crew with the strong checker.
func TestTheKneeRoutesFixesCheapAndOpenEndedToAStrongChecker(t *testing.T) {
	cands := evidenceCandidates()
	cases := []struct {
		class   Class
		checker string
	}{
		{Bugfix, "z-ai/glm-5.3-flash"},
		{OpenEnded, "moonshotai/kimi-k3"},
	}
	for _, tc := range cases {
		d, err := Decide(Request{Class: tc.class, Candidates: cands})
		if err != nil {
			t.Fatal(err)
		}
		if got := d.Seat(Worker).Model; got != "z-ai/glm-5.3-flash" {
			t.Errorf("%s: worker %s, want glm-5.3-flash", tc.class, got)
		}
		if got := d.Seat(Planner).Model; got != "z-ai/glm-5.3-flash" {
			t.Errorf("%s: planner %s, want glm-5.3-flash", tc.class, got)
		}
		if got := d.Seat(Checker).Model; got != tc.checker {
			t.Errorf("%s: checker %s, want %s", tc.class, got, tc.checker)
		}
	}
}

func TestEffortMovesOnlyWhereTheEvidenceSaysItPays(t *testing.T) {
	cands := evidenceCandidates()
	best, _ := Decide(Request{Class: Bugfix, Candidates: cands, Effort: EffortBest})
	if best.Seat(Worker).Model != "moonshotai/kimi-k3" || best.Seat(Planner).Model != "moonshotai/kimi-k3" {
		t.Errorf("--best on a fix: %+v, want kimi worker and planner", best.Crew)
	}
	// The checker adds nothing on a fix, so even --best keeps the cheaper one.
	if best.Seat(Checker).Model != "z-ai/glm-5.3-flash" {
		t.Errorf("--best on a fix put %s in the checker seat; the evidence says a strong checker adds nothing there", best.Seat(Checker).Model)
	}
	cheap, _ := Decide(Request{Class: OpenEnded, Candidates: cands, Effort: EffortCheap})
	if cheap.Seat(Checker).Model != "deepseek/deepseek-v4-flash" {
		t.Errorf("--cheap on open-ended work: checker %s, want the near-free v4-flash checker", cheap.Seat(Checker).Model)
	}
}

// A MODEL NOBODY MEASURED IS NEVER BELIEVED BETTER THAN ONE SOMEBODY DID, so
// a dear frontier row does not take a seat off the measured best even at
// --best, and at the knee its price keeps it out.
func TestAnUnmeasuredFrontierModelDoesNotOutrankTheEvidence(t *testing.T) {
	cands := append(evidenceCandidates(),
		catalogRow("anthropic/claude-opus-5", false, 5, 25, 50.8, 78, 56.5),
		catalogRow("anthropic/claude-fable-5.1", false, 10, 50, 53.4, 81.6, 57.9))
	for _, effort := range []Effort{EffortKnee, EffortBest} {
		for _, class := range []Class{Bugfix, OpenEnded} {
			d, err := Decide(Request{Class: class, Candidates: cands, Effort: effort})
			if err != nil {
				t.Fatal(err)
			}
			for _, pick := range d.Crew {
				if strings.HasPrefix(pick.Model, "anthropic/") {
					t.Errorf("%s/%q: %s seat went to unmeasured %s", class, effort, pick.Seat, pick.Model)
				}
			}
		}
	}
}

// WITH ONLY UNMEASURED MODELS the catalog prior still ranks them, and the
// router still answers rather than refusing.
func TestUnmeasuredModelsAreRankedByTheirPublishedFigures(t *testing.T) {
	cands := []Candidate{
		catalogRow("acme/weak", true, 0.05, 0.1, 10, 20, 10),
		catalogRow("acme/strong", true, 0.3, 1.2, 50, 80, 60),
	}
	d, err := Decide(Request{Class: OpenEnded, Candidates: cands, Effort: EffortBest})
	if err != nil {
		t.Fatal(err)
	}
	if d.Seat(Checker).Model != "acme/strong" {
		t.Errorf("--best checker %s, want acme/strong", d.Seat(Checker).Model)
	}
	if d.Seat(Checker).Measured {
		t.Error("an unmeasured model's pick claims to be measured")
	}
}

func TestAPinnedSeatAlwaysRunsItsPin(t *testing.T) {
	cands := evidenceCandidates()
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
	if d.Seat(Worker).Pinned || d.Seat(Worker).Model != "z-ai/glm-5.3-flash" {
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
	flash, _ := Snapshot("z-ai/glm-5.3-flash")
	cands := []Candidate{{Model: flash, Routes: []Route{
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
	cands := evidenceCandidates()
	first, _ := Decide(Request{Class: Bugfix, Candidates: cands})
	again, err := Decide(Request{Class: Bugfix, Candidates: cands, Stronger: &first})
	if err != nil {
		t.Fatal(err)
	}
	if again.Quality <= first.Quality {
		t.Fatalf("redo stronger: %.2f is not above %.2f", again.Quality, first.Quality)
	}
	pins := map[Seat]Pin{Worker: {Model: "z-ai/glm-5.3-flash"}}
	first, _ = Decide(Request{Class: Bugfix, Candidates: cands, Pins: pins})
	again, err = Decide(Request{Class: Bugfix, Candidates: cands, Pins: pins, Stronger: &first})
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
		{Seat: Worker, Quality: 99}, {Seat: Planner, Quality: 99}, {Seat: Checker, Quality: 99},
	}
	if _, err := Decide(Request{Class: OpenEnded, Candidates: cands, Stronger: &best}); !errors.Is(err, ErrStrongest) {
		t.Errorf("redo of the strongest crew: %v, want ErrStrongest", err)
	}
}

func TestLearnedStepsStartAFixHigher(t *testing.T) {
	cands := evidenceCandidates()
	d, _ := Decide(Request{Class: Bugfix, Candidates: cands, Steps: 2})
	if d.Seat(Worker).Model != "moonshotai/kimi-k3" {
		t.Errorf("two learned steps on a fix: worker %s, want kimi", d.Seat(Worker).Model)
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
	// Near the cap an open-ended task drops the dear checker.
	cands := evidenceCandidates()
	mult, _ := Pace(9.9, 10)
	d, _ := Decide(Request{Class: OpenEnded, Candidates: cands, Pace: mult})
	if d.Seat(Checker).Model == "moonshotai/kimi-k3" {
		t.Errorf("at 99%% of the cap the checker is still kimi (λ %.0f)", d.Lambda)
	}
}

func TestGapsNameAMissingStrongChecker(t *testing.T) {
	if gaps := Gaps(evidenceCandidates()); len(gaps) != 0 {
		t.Errorf("the evidence set has a strong checker, got gaps %+v", gaps)
	}
	flashOnly := evidenceCandidates()[:1]
	gaps := Gaps(flashOnly)
	if len(gaps) != 1 || gaps[0].Seat != Checker || gaps[0].Class != OpenEnded {
		t.Errorf("flash alone: gaps %+v, want the open-ended checker", gaps)
	}
}

func TestTheDecisionLine(t *testing.T) {
	cands := evidenceCandidates()
	d, _ := Decide(Request{Class: OpenEnded, Candidates: cands, Pins: map[Seat]Pin{Checker: {Model: "moonshotai/kimi-k3"}}})
	got := d.Line("📌", 0.108)
	want := "open-ended · worker glm-5.3-flash (openrouter) · checker 📌 kimi-k3 · $0.108 (est $0.121)"
	if got != want {
		t.Errorf("line\n got %q\nwant %q", got, want)
	}
	if got := d.Line("📌", -1); !strings.HasSuffix(got, " · est $0.121") {
		t.Errorf("line before the run ends: %q", got)
	}
}

// THE ROUTER'S OWN BUDGET: under two milliseconds a decision, against a
// catalog the size of the real one, classification included.
func TestADecisionTakesUnderTwoMilliseconds(t *testing.T) {
	cands := evidenceCandidates()
	for i := 0; i < 600; i++ {
		cands = append(cands, catalogRow("acme/m"+string(rune('a'+i%26))+strings.Repeat("x", i%7), i%2 == 0,
			0.1+float64(i%30)/10, 0.5+float64(i%40)/5, 20+float64(i%35), 40+float64(i%45), 20+float64(i%40)))
	}
	task := Task{Text: "fix: crash when the config has no trailing newline\n\nTraceback (most recent call last):\n  ...\nValueError: bad"}
	start := time.Now()
	const runs = 200
	for i := 0; i < runs; i++ {
		if _, err := Decide(Request{Task: task, Candidates: cands}); err != nil {
			t.Fatal(err)
		}
	}
	if per := time.Since(start) / runs; per > 2*time.Millisecond {
		t.Errorf("a decision took %v; the budget is 2ms", per)
	}
}

func TestRouteIsDeterministicWhateverTheOrder(t *testing.T) {
	cands := append(evidenceCandidates(), catalogRow("acme/twin-a", true, 0.15, 0.5, 41.8, 71.5, 50.9), catalogRow("acme/twin-b", true, 0.15, 0.5, 41.8, 71.5, 50.9))
	a, _ := Decide(Request{Class: Other, Candidates: cands})
	reversed := make([]Candidate, len(cands))
	for i := range cands {
		reversed[len(cands)-1-i] = cands[i]
	}
	b, _ := Decide(Request{Class: Other, Candidates: reversed})
	for _, seat := range Seats {
		if a.Seat(seat).Model != b.Seat(seat).Model {
			t.Errorf("%s: %s one way, %s the other", seat, a.Seat(seat).Model, b.Seat(seat).Model)
		}
	}
}
