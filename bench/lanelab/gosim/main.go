// Command gosim replays `bench/lanelab`'s three scenarios against the SHIPPED
// lane router and prints the ship gate.
//
// ── WHY THERE IS A SECOND SIMULATOR ─────────────────────────────────────────
//
// `bench/lanelab/sim.py` is a reference model: it re-implements the design in
// Python, it is quick to change, and it found five of the seven corrections in
// ideation/provider-routing.md Part III. But a second implementation agreeing
// with the first is not evidence about the build. So this program drives the
// REAL registry — `lane.Default()`'s real ledger primed from the same sheet
// fixture, the real chooser, the real watch, the real budget — against
// `internal/lane/lanestub`, over the same three scenarios, the same eight
// seeds, and the same objective. THE SHIP DECISION IS TAKEN HERE
// (ideation/provider-routing.md, Part III, C6); the Python table is the bug
// report when the two disagree.
//
// It is a main package under bench/, so none of it is in the shipped binary.
//
//	go run ./bench/lanelab/gosim                       the committed run
//	go run ./bench/lanelab/gosim -requests 200         a quick one
//	go run ./bench/lanelab/gosim -json out.json        same, plus the raw table
//	go run ./bench/lanelab/gosim -scenario talk -policy belief+hedge
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/home"
	"github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/aforge-v2/internal/lane/lanestub"
)

// ── THE SHIP GATE ───────────────────────────────────────────────────────────
//
// The gate of ideation/provider-routing.md Part III, C3, in the design's own
// economics and not in a percentage from nowhere.
//
// THE SPEED HALF IS THE P90 OF WHAT THE SCENARIO ACTUALLY BUYS. For `work` and
// `offpath` that is the wait. For `talk` it is the FIRST TOKEN alone: above the
// reading rate every lane is the same speed to a person, so a talk turn's whole
// prize is the empty line before the stream starts, and gating it on the wait
// would grade the design partly on a term it has no way to move.
//
// THE MONEY HALF IS λ. A router that spends a dollar to buy more than λ seconds
// has, by the design's own arithmetic, made a good trade, so the clause IS that
// trade, per request: Δ$ ≤ Δ(mean wait)/λ. At λ = 0 the right-hand side is zero
// and the rule degenerates to "it must not cost more than the baseline", which
// is exactly what background work should demand.
//
// AND AT λ = 0 THE SPEED HALF IS A GUARD RATHER THAN A DEMAND. λ is the
// statement that a second is worth nothing; a router told that and then graded
// on seconds is being graded on the one term it was instructed to ignore, and
// no correct implementation of this design can pass such a clause — it would
// have to disobey the objective to do it. What is worth demanding of work
// nobody is waiting on is that the wait does not BLOW OUT while the bill comes
// down, so the clause becomes: no dearer than the baseline, and no slower than
// [gateWaitBlowout] times it. The ceiling is a factor rather than a percentage
// because at λ = 0 the two quantities are not commensurable — there is no
// exchange rate between them, which is precisely what λ = 0 says.
const gateP90Improve = 0.30

// gateWaitBlowout is how much slower than the baseline the p90 wait may be
// before a λ = 0 arm is refused. Two is the point at which "the same work,
// later" becomes "a different experience": a background answer that took twice
// as long is one somebody may still be waiting on when they come back to it,
// and no saving on the bill buys that back.
const gateWaitBlowout = 2.0

// baseline is the arm every other arm is graded against.
//
// IT IS NOT THE ONE THE DESIGN RETIRES, and that is a defect in this run rather
// than a choice — see [strikeNote]. `sim.py` grades against `strike-ledger`, the
// strike table in `internal/provider/velocity.go`; this program cannot reach
// that mechanism without editing shipped code, so it grades against the arm it
// can reach honestly, which is what a request gets today when nothing in the
// harness has an opinion.
const baseline = policyDefault

// strikeNote is why the `strike` arm is missing, printed with the table so that
// nobody has to go looking for it.
const strikeNote = "the `strike` arm (today's velocity ledger with sort: latency) is NOT RUN: " +
	"`velocityLedger`, its `observe` and its `preferences` are unexported and the one " +
	"process-wide instance has no exported reset, so a bench cannot prime it, clear it " +
	"between seeds, or drive it except through a whole provider.Client — and its cooldown " +
	"reads the wall clock, which a run on a divided clock cannot move. Reaching it needs a " +
	"seam added to internal/provider, which this lane may not touch. The gate below is " +
	"therefore taken against `default` and NOT against the mechanism the design retires."

// gateSpeed says which p90 each scenario's speed half reads.
var gateSpeed = map[string][2]string{
	"talk":    {"ttft_p90", "p90 first token"},
	"work":    {"wait_p90", "p90 wait"},
	"offpath": {"wait_p90", "p90 wait"},
}

// ── ONE CELL ────────────────────────────────────────────────────────────────

// cell is one (scenario, policy) pooled over every seed.
type cell struct {
	Scenario string `json:"scenario"`
	Policy   string `json:"policy"`
	N        int    `json:"n"`

	TTFTp50 float64 `json:"ttft_p50"`
	TTFTp90 float64 `json:"ttft_p90"`
	TTFTp99 float64 `json:"ttft_p99"`

	AnswerP50 float64 `json:"answer_p50"`
	AnswerP90 float64 `json:"answer_p90"`
	AnswerP99 float64 `json:"answer_p99"`

	WaitP50  float64 `json:"wait_p50"`
	WaitP90  float64 `json:"wait_p90"`
	WaitP99  float64 `json:"wait_p99"`
	WaitMean float64 `json:"wait_mean"`

	USDPerRequest  float64 `json:"usd_per_request"`
	USDPer1k       float64 `json:"usd_per_1k"`
	HedgePct       float64 `json:"hedge_pct"`
	HedgePerHundo  float64 `json:"hedges_per_100"`
	HedgeWaste1k   float64 `json:"hedge_waste_usd_per_1k"`
	Abandoned      int     `json:"streams_abandoned"`
	RetryPct       float64 `json:"retry_pct"`
	ModalLane      string  `json:"modal_lane"`
	ModalSharePct  float64 `json:"modal_share_pct"`
	LanesUsed      int     `json:"lanes_used"`
	SocketMedianMs float64 `json:"socket_median_ms"`
}

// verdict is one arm against the baseline in one scenario, with both deciding
// quantities, because a gate that prints only PASS or FAIL is a gate nobody can
// argue with.
type verdict struct {
	Scenario      string  `json:"scenario"`
	Baseline      string  `json:"baseline"`
	Policy        string  `json:"policy"`
	SpeedMetric   string  `json:"speed_metric"`
	SpeedImprove  float64 `json:"speed_improve"`
	DollarsPerReq float64 `json:"d_usd_per_request"`
	MeanWaitSaved float64 `json:"mean_wait_saved_s"`
	Budget        float64 `json:"usd_budget_per_request"`
	SpeedOK       bool    `json:"speed_ok"`
	MoneyOK       bool    `json:"money_ok"`
	Pass          bool    `json:"pass"`
}

func gateOne(s scenario, base, arm cell) verdict {
	key := gateSpeed[s.name]
	baseSpeed, armSpeed := base.TTFTp90, arm.TTFTp90
	if key[0] == "wait_p90" {
		baseSpeed, armSpeed = base.WaitP90, arm.WaitP90
	}
	improve := (baseSpeed - armSpeed) / baseSpeed
	dollars := arm.USDPerRequest - base.USDPerRequest
	saved := base.WaitMean - arm.WaitMean
	budget := 0.0
	// The speed clause is a DEMAND where somebody is waiting and a GUARD where
	// nobody is; see [gateWaitBlowout] for why the two cannot be the same rule.
	speedOK := armSpeed <= gateWaitBlowout*baseSpeed
	if s.lambda > 0 {
		budget = saved / s.lambda
		speedOK = improve >= gateP90Improve
	}
	return verdict{
		Scenario: s.name, Baseline: base.Policy, Policy: arm.Policy,
		SpeedMetric: key[1], SpeedImprove: improve,
		DollarsPerReq: dollars, MeanWaitSaved: saved, Budget: budget,
		SpeedOK: speedOK,
		MoneyOK: dollars <= budget,
		Pass:    speedOK && dollars <= budget,
	}
}

// ── MAIN ────────────────────────────────────────────────────────────────────

func main() {
	here, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
	sheetPath := flag.String("sheet", filepath.Join(here, "bench", "lanelab",
		"sheets", "deepseek-deepseek-v4-flash.json"), "the endpoint sheet to draw lanes from")
	requests := flag.Int("requests", 2000, "requests per cell per seed")
	seeds := flag.Int("seeds", 8, "how many seeds")
	seed0 := flag.Int("seed", 7, "the first seed; the rest are seed+2k, as sim.py's sweep does")
	seedList := flag.String("seeds-are", "",
		"the exact seeds to run, comma separated, instead of the seed+2k progression")
	only := flag.String("scenario", "", "run one scenario only: talk, work or offpath")
	arm := flag.String("policy", "", "run one policy only: default, belief or belief+hedge")
	speedup := flag.Int("speedup", 100, "how many times faster the wire runs than the world it describes")
	jsonOut := flag.String("json", "", "write the raw table here")
	trace := flag.Bool("trace", false, "write one line per request to standard error: asked, served, timed")
	proof := flag.Bool("proof", false,
		"run docs/design/waiting/DESIGN.md §K's four scenarios and its pass table instead of the ship gate")
	pace := flag.String("pace", "",
		"proof only: which belief the plan waits against, `shipped` or `flat`; both when unsaid")
	mix := flag.String("mix", "",
		"proof only: which fault rate the rows are run at, `stress` or `natural`; both when unsaid")
	store := flag.String("store", "",
		"proof only: which store the rows are staged in, `cold`, `warmed` or `seen`; "+
			"cold and warmed when unsaid")
	flag.Parse()

	// THE PROOF ROWS HAVE THEIR OWN SIZE and it is smaller, because they answer
	// a bound rather than a percentile: what a ceiling needs is every trial of a
	// staged fault and not a long tail of ordinary ones. A figure the caller
	// asked for out loud always wins.
	given := map[string]bool{}
	flag.Visit(func(f *flag.Flag) { given[f.Name] = true })
	if *proof && !given["requests"] {
		*requests = proofRequests
	}
	if *proof && !given["seeds"] {
		*seeds = proofSeeds
	}

	began := time.Now()

	world, err := loadWorld(*sheetPath)
	if err != nil {
		log.Fatal(err)
	}

	scens := scenarios
	if *only != "" {
		scens = nil
		for _, s := range scenarios {
			if s.name == *only {
				scens = append(scens, s)
			}
		}
		if len(scens) == 0 {
			log.Fatalf("no scenario called %q", *only)
		}
	}
	policies := []string{policyDefault, policyBelief, policyHedge}
	if *arm != "" {
		policies = nil
		for _, p := range []string{policyDefault, policyBelief, policyHedge} {
			if p == *arm {
				policies = append(policies, p)
			}
		}
		if len(policies) == 0 {
			log.Fatalf("no policy called %q", *arm)
		}
	}

	// THE EXACT SEEDS, WHEN A RULE NAMES THEM. A tiebreaker is only a
	// tiebreaker if the seeds it runs on are the ones that were written down
	// before it ran, and the progression above cannot express an arbitrary
	// three. It changes no default: unsaid, the sweep is exactly as it was.
	seedsRun := make([]int, 0, *seeds)
	if *seedList != "" {
		for _, word := range strings.Split(*seedList, ",") {
			seed, err := strconv.Atoi(strings.TrimSpace(word))
			if err != nil {
				log.Fatalf("gosim: %q is not a seed: %v", word, err)
			}
			seedsRun = append(seedsRun, seed)
		}
	} else {
		for k := 0; k < *seeds; k++ {
			seedsRun = append(seedsRun, *seed0+2*k)
		}
	}

	rel, _ := filepath.Rel(here, *sheetPath)
	if rel == "" {
		rel = *sheetPath
	}
	breakAt, healAt := *requests/4, 3*(*requests)/4
	fmt.Printf("sheet:    %s\n", rel)
	fmt.Printf("model:    %s\n", world.model)
	fmt.Printf("fetched:  %s\n", world.fetched)
	fmt.Printf("lanes:    %d on the sheet, %d with p50 timing\n", world.total, len(world.lanes))
	fmt.Printf("seeds:    %v    requests per cell per seed: %d    pooled per cell: %d\n",
		seedsRun, *requests, *requests*len(seedsRun))
	fmt.Printf("prompt:   %d tokens every request; read rate %g tok/s\n", promptTokens, lane.ReadRate)
	fmt.Printf("wire:     %d× faster than the world; %d tokens streamed per answer "+
		"(the rate filter's floor is %d)\n", *speedup, streamTokens, 32)
	if *proof {
		proveIt(world, seedsRun, *requests, *speedup, *trace,
			pacesFrom(*pace), storesFrom(*store), mixesFrom(*mix), *jsonOut, began)
		return
	}
	fmt.Printf("script:   the lane the router chose on request 0 goes to a %v first token at "+
		"request %d of every run and recovers at request %d\n",
		brokenTTFT, breakAt, healAt)
	fmt.Println()
	fmt.Printf("   NOTE: %s\n", wrap(strikeNote, 74, "         "))
	fmt.Println()

	var rows []cell
	var perSeed []verdict
	for _, s := range scens {
		gated := world.gated(s)
		names := make([]string, 0, len(gated))
		for _, index := range gated {
			names = append(names, world.lanes[index].name)
		}
		fmt.Printf("── %s  (%s) %s\n", s.name, s.why,
			strings.Repeat("─", max(3, 74-len(s.name)-len(s.why))))
		fmt.Printf("   lambda=%g s/$   visible=%d  hidden=%d  q_need=%g  tools=%s\n",
			s.lambda, s.visible, s.hidden, s.quality, yesno(s.tools))
		fmt.Printf("   %d/%d lanes past the SHIPPED capability gate: %s\n",
			len(gated), len(world.lanes), strings.Join(names, ", "))
		fmt.Printf("   the lane the router chose first, and therefore the lane this scenario breaks: %s\n",
			victimFor(world, s, *requests))
		fmt.Println()
		fmt.Printf("   %-19s%26s%26s%23s%9s%8s  %s\n", "policy",
			"TTFT p50/p90/p99 (ms)", "answer p50/p90/p99 (s)", "wait p50/p90 (s)",
			"$/1k", "hedge%", "modal lane")
		fmt.Printf("   %s\n", strings.Repeat("-", 123))

		perScenario := map[string]cell{}
		bySeed := map[string][]cell{}
		for _, p := range policies {
			pooled, seedCells := runCell(world, s, p, seedsRun, *requests, *speedup, *trace)
			rows = append(rows, pooled)
			perScenario[p] = pooled
			bySeed[p] = seedCells
			fmt.Printf("   %-19s%8.0f%9.0f%9.0f%9.2f%8.2f%9.2f%12.2f%11.2f%9.3f%8.1f  %s (%.0f%%)\n",
				pooled.Policy, pooled.TTFTp50, pooled.TTFTp90, pooled.TTFTp99,
				pooled.AnswerP50, pooled.AnswerP90, pooled.AnswerP99,
				pooled.WaitP50, pooled.WaitP90, pooled.USDPer1k, pooled.HedgePct,
				pooled.ModalLane, pooled.ModalSharePct)
		}
		fmt.Println()
		if s.visible > 0 {
			fast := 0
			for _, index := range gated {
				if world.lanes[index].rate[0] >= lane.ReadRate {
					fast++
				}
			}
			fmt.Printf("   %d/%d gated lanes write at or above %g tok/s at their median, and on those "+
				"the %d visible tokens add NOTHING to the wait — they are read as they arrive. "+
				"So this scenario is decided by the first token, which is what its gate reads.\n",
				fast, len(gated), lane.ReadRate, s.visible)
		} else {
			fmt.Println("   no visible tokens, so nothing is read as it arrives and the wait is the whole answer")
		}
		for _, p := range policies {
			one := perScenario[p]
			fmt.Printf("     %-19s wait p99 %8.2f s   $%.6f/request   %.1f hedges per 100   "+
				"%d streams abandoned   refused-and-retried %.1f%%   hedge waste $%.4f/1k   "+
				"lanes used: %d   socket median %.0f ms of world time\n",
				one.Policy, one.WaitP99, one.USDPerRequest, one.HedgePerHundo,
				one.Abandoned, one.RetryPct, one.HedgeWaste1k, one.LanesUsed, one.SocketMedianMs)
		}
		fmt.Println()

		// The per-seed verdicts, taken from the runs the table was already made
		// of rather than from a second set: a verdict that flips between draws
		// is not a verdict, and this is the only thing in this program that
		// reports whether it does.
		if _, ok := bySeed[baseline]; ok {
			for _, p := range policies {
				if p == baseline {
					continue
				}
				for k, sd := range seedsRun {
					v := gateOne(s, bySeed[baseline][k], bySeed[p][k])
					v.Policy = fmt.Sprintf("%s@%d", p, sd)
					perSeed = append(perSeed, v)
				}
			}
		}
	}

	// ── the gate ──
	fmt.Printf("── ship gate ── against %s: where lambda > 0 the scenario's p90 improves by >= %.0f%%,\n",
		baseline, gateP90Improve*100)
	fmt.Printf("   and where lambda = 0 the p90 wait merely must not exceed %.0fx the baseline's,\n", gateWaitBlowout)
	fmt.Println("   AND the extra dollars per request are no more than the mean seconds saved, priced at lambda")
	fmt.Println()
	fmt.Printf("   %-10s%-20s%-17s%9s%14s%14s   verdict\n",
		"scenario", "policy", "speed metric", "improve", "extra $/req", "budget $/req")
	fmt.Printf("   %s\n", strings.Repeat("-", 97))

	var gates []verdict
	by := map[string]cell{}
	for _, r := range rows {
		by[r.Scenario+"|"+r.Policy] = r
	}
	for _, s := range scens {
		base, ok := by[s.name+"|"+baseline]
		if !ok {
			fmt.Printf("   %-10s the baseline arm %q was not run, so nothing here can be graded\n",
				s.name, baseline)
			continue
		}
		for _, p := range policies {
			if p == baseline {
				continue
			}
			one, ok := by[s.name+"|"+p]
			if !ok {
				continue
			}
			v := gateOne(s, base, one)
			gates = append(gates, v)
			var why []string
			if !v.SpeedOK {
				why = append(why, "p90")
			}
			if !v.MoneyOK {
				why = append(why, "cost")
			}
			tag := "PASS"
			if !v.Pass {
				tag = "FAIL (" + strings.Join(why, "+") + ")"
			}
			fmt.Printf("   %-10s%-20s%-17s%+8.1f%%%+14.6f%14.6f   %s\n",
				v.Scenario, v.Policy, v.SpeedMetric, v.SpeedImprove*100,
				v.DollarsPerReq, v.Budget, tag)
		}
	}
	fmt.Println()
	design, passed := 0, 0
	for _, v := range gates {
		if v.Policy == policyHedge {
			design++
			if v.Pass {
				passed++
			}
		}
	}
	total := 0
	for _, v := range gates {
		if v.Pass {
			total++
		}
	}
	if design > 0 {
		fmt.Printf("   the design passes in %d/%d scenarios; %d/%d rows pass overall.\n",
			passed, design, total, len(gates))
	}
	fmt.Println()

	// ── is the verdict a property of the design or of the seed? ──
	if len(perSeed) > 0 {
		fmt.Println("── seed by seed ── is the verdict a property of the design or of the seed? ────────")
		fmt.Println()
		tally := map[string][2]int{}
		for _, v := range perSeed {
			name := strings.SplitN(v.Policy, "@", 2)[0]
			key := v.Scenario + "  " + name
			was := tally[key]
			if v.Pass {
				was[0]++
			}
			was[1]++
			tally[key] = was
		}
		keys := make([]string, 0, len(tally))
		for k := range tally {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Printf("   %-28s passes on %d/%d seeds\n", k, tally[k][0], tally[k][1])
		}
		fmt.Println()
	}

	wall := time.Since(began)
	fmt.Printf("   wall %s\n", wall.Round(time.Second))

	if *jsonOut != "" {
		payload := map[string]any{
			"sheet": filepath.Base(*sheetPath), "fetched_at": world.fetched,
			"model": world.model, "seeds": seedsRun, "n_per_seed": *requests,
			"speedup": *speedup, "stream_tokens": streamTokens,
			"baseline": baseline, "strike_note": strikeNote,
			"rows": rows, "ship_gate": gates, "per_seed_gate": perSeed,
			"wall_seconds": wall.Seconds(),
		}
		blob, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			log.Fatal(err)
		}
		if err := os.WriteFile(*jsonOut, blob, 0o644); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("\n   wrote %s\n", *jsonOut)
	}
}

// ── RUNNING ─────────────────────────────────────────────────────────────────

// runCell runs one (scenario, policy) over every seed and pools the requests.
func runCell(w *world, s scenario, policy string, seeds []int, n, speedup int, trace bool) (cell, []cell) {
	var all []record
	abandoned := 0
	each := make([]cell, 0, len(seeds))
	for _, seed := range seeds {
		got, cancels := run(w, s, policy, seed, n, speedup, trace)
		all = append(all, got...)
		abandoned += cancels
		each = append(each, summarise(s, policy, got, cancels))
	}
	pooled := summarise(s, policy, all, abandoned)
	return pooled, each
}

// victimFor is the lane the chooser picks with everything healthy, on a primed
// ledger and nothing else — which is the lane every arm of this scenario then
// watches go bad.
func victimFor(w *world, s scenario, n int) string {
	dir, err := os.MkdirTemp("", "gosim-victim-")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)
	if err := os.Setenv(home.EnvVar, dir); err != nil {
		log.Fatal(err)
	}
	lane.Default().Reset()
	defer lane.Default().Reset()
	ledger := lane.Default().Ledger()
	for _, l := range w.lanes {
		ledger.Prime(l.row(w.model), lane.SheetWeight)
	}
	if first := lane.Default().Chooser().Choose(s.request(w.model, theMoment, n)); len(first.Order) > 0 {
		return first.Order[0]
	}
	return ""
}

// theMoment is the Tuesday every run happens on. It is fixed rather than read
// from the wall for the reason [lane.Request.Now] exists at all: a choice that
// depended on the day it ran on would be a choice nobody could reproduce.
var theMoment = time.Date(2026, time.August, 30, 11, 0, 0, 0, time.UTC)

// run is one arm of one scenario at one seed, all the way through.
func run(w *world, s scenario, policy string, seed, n, speedup int, trace bool) ([]record, int) {
	// A HOME OF ITS OWN, FIRST. The ledger writes every belief through a store
	// and `lane.StorePath` resolves under AFORGE_HOME on every call, so a run
	// that did not move the state root would fold this program's lanes into the
	// belief file of whoever ran it and read them back on the next run. That is
	// the bug ideation/provider-routing.md Part III records at the end.
	dir, err := os.MkdirTemp("", "gosim-home-")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)
	if err := os.Setenv(home.EnvVar, dir); err != nil {
		log.Fatal(err)
	}
	lane.Default().Reset()
	defer lane.Default().Reset()

	ledger := lane.Default().Ledger()
	for _, l := range w.lanes {
		// The design's own k: the sheet is a thirty-minute aggregate over
		// everybody's prompts and ours are our own, so it pulls the belief
		// without drowning it.
		ledger.Prime(l.row(w.model), lane.SheetWeight)
	}

	// The lane the router would pick with everything healthy, which is the lane
	// this run then breaks — breaking one nobody was using would prove nothing.
	// It is decided once, from the chooser, and the SAME lane is broken in every
	// arm and at every seed, because a world that differed between arms would
	// not be one world. The chooser is pure, so this is the same answer here as
	// it is in the header printed before any of this ran.
	victim := ""
	if first := lane.Default().Chooser().Choose(s.request(w.model, theMoment, n)); len(first.Order) > 0 {
		victim = first.Order[0]
	}

	stub := lanestub.New(w.model, w.stubLanes(w.shots(seed, s.name, 0, 0), "", "", speedup)...)
	defer stub.Close()

	r := &router{
		world: w, scen: s, policy: policy, seed: seed,
		stub: stub, ledger: ledger,
		// THE DESIGN'S OWN BUDGET, and it is the shipped one rather than a
		// figure written here: two hedges in any twenty requests and a tenth of
		// recent spend. An arm that picked its own allowance would be measuring
		// a router nobody ships.
		budget:  lane.DefaultBudget(),
		client:  &http.Client{},
		speedup: speedup,
		total:   n,
		victim:  victim,
		breakAt: n / 4,
		healAt:  3 * n / 4,
		at:      theMoment,
		trace:   trace,
	}
	out := make([]record, 0, n)
	for index := 0; index < n; index++ {
		got, err := r.one(index)
		if err != nil {
			log.Fatalf("%s/%s seed %d request %d: %v", s.name, policy, seed, index, err)
		}
		out = append(out, got)
	}
	cancels := 0
	for _, l := range w.lanes {
		cancels += stub.Cancels(l.name)
	}
	return out, cancels
}

// ── SUMMARISING ─────────────────────────────────────────────────────────────

func summarise(s scenario, policy string, got []record, abandoned int) cell {
	out := cell{Scenario: s.name, Policy: policy, N: len(got), Abandoned: abandoned}
	if len(got) == 0 {
		return out
	}
	ttft := make([]float64, 0, len(got))
	wall := make([]float64, 0, len(got))
	wait := make([]float64, 0, len(got))
	socket := make([]float64, 0, len(got))
	mix := map[string]int{}
	usd, waste, hedges, retries := 0.0, 0.0, 0, 0
	for _, one := range got {
		ttft = append(ttft, one.ttftMs)
		wall = append(wall, one.wallS)
		wait = append(wait, one.waitS)
		socket = append(socket, one.socketMs)
		mix[one.lane]++
		usd += one.usd
		waste += one.wasteUSD
		if one.hedged {
			hedges++
		}
		if one.retries > 0 {
			retries++
		}
		out.WaitMean += one.waitS
	}
	out.WaitMean /= float64(len(got))
	out.TTFTp50, out.TTFTp90, out.TTFTp99 = pct(ttft, 0.50), pct(ttft, 0.90), pct(ttft, 0.99)
	out.AnswerP50, out.AnswerP90, out.AnswerP99 = pct(wall, 0.50), pct(wall, 0.90), pct(wall, 0.99)
	out.WaitP50, out.WaitP90, out.WaitP99 = pct(wait, 0.50), pct(wait, 0.90), pct(wait, 0.99)
	out.USDPerRequest = usd / float64(len(got))
	out.USDPer1k = out.USDPerRequest * 1000
	out.HedgePct = 100 * float64(hedges) / float64(len(got))
	out.HedgePerHundo = out.HedgePct
	out.HedgeWaste1k = waste / float64(len(got)) * 1000
	out.RetryPct = 100 * float64(retries) / float64(len(got))
	out.LanesUsed = len(mix)
	out.SocketMedianMs = pct(socket, 0.50)
	best, bestN := "", -1
	names := make([]string, 0, len(mix))
	for name := range mix {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if mix[name] > bestN {
			best, bestN = name, mix[name]
		}
	}
	out.ModalLane, out.ModalSharePct = best, 100*float64(bestN)/float64(len(got))
	return out
}

// pct is the nearest-rank percentile on the sorted sample, which is what
// `sim.py` reports: not interpolated, because a rank is a value that actually
// happened.
func pct(values []float64, q float64) float64 {
	if len(values) == 0 {
		return math.NaN()
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	k := int(math.Ceil(q*float64(len(sorted)))) - 1
	if k < 0 {
		k = 0
	}
	if k > len(sorted)-1 {
		k = len(sorted) - 1
	}
	return sorted[k]
}

func yesno(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// wrap folds a long note onto the terminal with a hanging indent, so a reader
// meets the reason the strike arm is missing rather than a wall.
func wrap(text string, width int, indent string) string {
	words := strings.Fields(text)
	var out strings.Builder
	line := 0
	for index, word := range words {
		if line > 0 && line+1+len(word) > width {
			out.WriteString("\n" + indent)
			line = 0
		} else if index > 0 {
			out.WriteString(" ")
			line++
		}
		out.WriteString(word)
		line += len(word)
	}
	return out.String()
}
