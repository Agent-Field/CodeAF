package main

// ── §K, MEASURED ────────────────────────────────────────────────────────────
//
// `docs/design/waiting/DESIGN.md` §K sets four pass criteria that are about the
// INVARIANT rather than about the average, and §J stages them as four
// scenarios. This file is those four scenarios and that table, driven against
// the same shipped code the rest of this program drives — the real chooser, the
// real ledger, the real [lane.PlanFor], and the real controller `internal/lane`
// installs at init — over `internal/lane/lanestub`.
//
// ── WHY IT IS NOT A FOURTH POLICY ───────────────────────────────────────────
//
// The three scenarios above COMPARE ARMS: three policies against one baseline,
// graded on a p90. These four compare nothing. Each row stages one state the
// design claims to bound and reports what the build did in it, because "no call
// waits longer than the role's ceiling" is not a quantity with a baseline — it
// held on every trial or it did not, and a ceiling that holds 99% of the time
// is not a ceiling. So a proof row reuses the scenario (λ, the token counts,
// the quality bar), the world, the one price table and the seeds, and brings
// its own staging and its own table.
//
// ── HOW EACH ROW IS STAGED ──────────────────────────────────────────────────
//
// Every row runs the `talk` scenario under [lane.RoleTalk], which is the role
// §K's first criterion names: the shortest ceiling in the table, and the role
// the reported defect happened under.
//
// Every row runs its fault for the MIDDLE HALF of its requests, exactly as the
// three scenarios above break their victim between request n/4 and 3n/4. That
// is what makes one run answer two criteria at once: the sick half is where
// time-to-action is measured, and the healthy half is where a false hedge would
// have to come from.
//
//   - COLD STORE — a fresh AFORGE_HOME, nothing primed, no sheet. The chooser
//     therefore has nothing to rank and returns an empty [lane.Choice], the
//     plan is built with no belief and no alternative, and the lane that serves
//     goes silent for [quietFor]. This is the reported defect's own state, and
//     it is staged with a silence as well as with a cold store because §K's
//     first criterion is the INTERSECTION of the two: a cold store with nothing
//     to wait for proves nothing about a clock.
//   - STALLED LANE — the sheet primed, the modal lane quiet [stallWords]
//     visible words into its answer ([lanestub.Profile.StallAfter] /
//     `StallFor`). The writing phase's own distribution is what has to catch
//     this one.
//   - THINKING MODEL — [lanestub.Profile.Reasoning] deltas before the first
//     visible word, at a rate slow enough that the run of thought lasts seconds
//     of the world. The healthy half is a legitimately long think that must NOT
//     be hedged; the sick half stalls INSIDE the run of thought and must be.
//   - PINNED LANE — a choice that sends `only:[pin]`, which is what
//     `internal/provider/lanes.go` builds for a pin and what
//     `internal/provider/waitplan.go` reads to set [control.Plan.Pinned]. Run
//     twice: with a reader for the offer, where the act is the question and
//     nothing is sent, and without one, where §E says a headless run borrows at
//     the ceiling once and says so.
//
// ── WHAT THIS BENCH CANNOT REACH ────────────────────────────────────────────
//
// "No reader" is `provider.OnPhase` having no registered reader, and this
// program does not import `internal/provider` — it never has, and a bench that
// grew an import into the package it is judging would be a bench that changed
// its own subject. So THE OFFER REGISTRY HALF OF §E IS NOT EXERCISED HERE: what
// is exercised is the controller's act, [control.Ask], and the two things a
// caller does with it. The reader row leaves the offer standing; the headless
// row converts it into the borrow §E describes. That conversion is THIS FILE's,
// not the build's, and REPORT.md says so beside the number.
//
// ── THE CLOCK, AND THE ONE PLACE THIS BENCH SPINS ───────────────────────────
//
// The wire runs [speedup] times faster than the world, as everywhere else here,
// and every figure reported is in the world's own units. That is a problem for
// exactly one measurement: a ten-second ceiling is a hundred milliseconds of
// wire, and a Go timer armed for a hundred milliseconds returns a fraction of a
// millisecond late — which is tens of milliseconds of the world, enough to make
// a ceiling that held exactly read as ten seconds and a bit. That would be this
// bench failing its own instrument and calling it a build failing its
// invariant. So [prover.ringAt] sleeps to within [spinWire] of the moment and
// spins the rest, and what is left over is measured on every act and printed
// with the table as the alarm's own lateness.

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/home"
	"github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/aforge-v2/internal/lane/control"
	"github.com/Agent-Field/aforge-v2/internal/lane/lanestub"
)

// ── THE THRESHOLDS, FROM §K ─────────────────────────────────────────────────
//
// They are spelled here with the design's own reasons beside them so that
// nobody has to hold the document open to read the table.
const (
	// gateFalseHedgePct is Dean & Barroso's measured figure for how much extra
	// traffic removes most of a p99. Above it the deadline is firing early and
	// the money is real.
	gateFalseHedgePct = 2.0
	// gateSpendOverheadPct is what the arms may add to the arm's own bill. The
	// sheet's price spread is 4.7x between the dearest lane and the median, so
	// three per cent is well inside the noise of choosing a different lane at
	// all.
	gateSpendOverheadPct = 3.0
	// gateLongThinkPct is the share of legitimate thinking phases that must
	// reach their first visible word with no arm behind them. It is the failure
	// mode this design most risks introducing.
	gateLongThinkPct = 95.0
)

// ── THE STAGING ─────────────────────────────────────────────────────────────

const (
	// quietFor is how long a staged lane goes silent. TWICE the ceiling of the
	// role these rows run under, so a wait that ended before it did was bounded
	// by the build and not by the script — and no longer than twice, because a
	// stream nobody was sent to rescue is READ OUT TO ITS END here, and every
	// second of the script is then a second of the run.
	quietFor = 20 * time.Second

	// thinkDeltas and thinkRate script a run of thought that lasts about five
	// and a half seconds of the world: long enough that a controller with no
	// duration model has to fall back on its liveness clock, and short enough to
	// be a LEGITIMATE think under a ten-second ceiling. A think longer than the
	// ceiling is acted on by construction (§B) and there is nothing to measure
	// about it.
	thinkDeltas = 12
	thinkRate   = 2.0
	// thinkVisible is how many visible words follow the thought. It is short
	// because what this row measures ends at the first of them, and a long
	// visible run at [thinkRate] would be a genuinely slow lane polluting the
	// false-hedge count of a row that is not about slow lanes.
	thinkVisible = 4

	// stallWords is how many visible words the stalled row writes before it goes
	// quiet. Enough that the stream is unambiguously in its writing phase, few
	// enough that the gap distribution it is judged against is still the
	// sheet's own.
	stallWords = 5

	// spinWire is how much of an alarm is spun rather than slept. Two
	// milliseconds is above the timer floor this machine was measured at, so the
	// sleep never overshoots into the spin's territory.
	spinWire = 2 * time.Millisecond

	// proofRequests and proofSeeds are the committed proof run, and they are
	// smaller than the ship gate's because these rows answer a bound rather than
	// a percentile: what a ceiling row needs is every trial, not a long tail.
	proofRequests = 150
	proofSeeds    = 3
)

// proofRole is the role every proof row runs under.
const proofRole = lane.RoleTalk

// The two doors onto one ledger a plan can be built against.
//
// `shipped` is [lane.PaceFor], which is what `internal/provider`'s planFor
// asks and therefore what the build really waits against: it prefers the
// four-level chain over the flat belief whenever the chain believes anything.
// `flat` is [lane.PaceOf] over the same ledger's flat belief, which is what
// [lane.PaceFor] falls back to when the chain believes nothing.
//
// THE SECOND ARM EXISTS BECAUSE THIS LANE MEASURED THE TWO DISAGREEING BY AN
// ORDER OF MAGNITUDE, and a table that reported only the first would say the
// controller acts too early without saying what it is acting on. REPORT.md
// carries the reproduction.
const (
	paceShipped = "shipped"
	paceFlat    = "flat"
)

// proofScenario is which of the three scenarios above the proof rows borrow
// their economics from. `talk` is the one a person is reading.
const proofScenario = "talk"

// ── THE THREE STORES A ROW CAN BE STAGED IN ─────────────────────────────────
//
// §J stages its rows in ONE state — a fresh process, either empty or primed
// from the sheet and never in its life having watched an answer — and every
// figure in the committed table is from that state. That is the state the
// reported defect happened in and it is the right state for an INVARIANT: a
// ceiling that only holds once the ledger knows something is not a ceiling.
//
// IT IS THE WRONG STATE FOR A COST. §K's false-hedge and spend bounds describe
// what the mechanism costs in the steady state a person actually sits in, and a
// process that has never measured a pair has to explore to find out which lane
// is quick — exploration the purse bounds and the bill records. Grading the
// steady-state bounds on an all-cold mix grades the design on its first minute
// forever. So the rows are run again over a WARMED store and the four bounds
// are enforced there; see REPORT.md, which says which arm decides what.
const (
	// storeCold is the staging §J wrote and the committed table measured: the
	// `cold store` row's home is empty and the other four are primed from the
	// sheet. No pair has ever been measured in any of them.
	storeCold = "cold"
	// storeWarmed is a session with an afternoon behind it: the sheet EVERY
	// process has — it is fetched on the beat whether or not anybody has asked
	// anything yet — and [warmSightings] real answers per pair folded in
	// through the real [lane.Ledger.Note] door before the first request.
	//
	// THE SHEET IS NOT WITHHELD HERE AND WITHHOLDING IT WOULD BE A DIFFERENT
	// CLAIM. A lane's DRAW — how much one answer varies around what is believed
	// about it — is published and not observed: `ledger.Draw` reads the distance
	// between a published p50 and p90 and this build has no other source for it,
	// so a store with the timing half of the sheet withheld waits every lane
	// against [lane.SpreadFloor] no matter how many answers it has watched. That
	// is [storeSeen], and it is reported rather than gated for exactly that
	// reason: it measures a state no shipped process is ever in.
	storeWarmed = "warmed"
	// storeSeen is the warmed store with the sheet's TIMING withheld and its
	// facts kept — a lane this process has watched and that nobody published a
	// percentile about. It is a DIAGNOSTIC and never a gated arm.
	storeSeen = "seen"
)

const (
	// warmSightings is how many real answers of each (model, lane) pair a warmed
	// store has watched before its first request. Sixty is an afternoon on a
	// busy model and it is comfortably past the point where the chain's own
	// variance falls under the lane's published dispersion, which is where the
	// steady state begins.
	warmSightings = 60
	// warmOver is how long that history is spread over. Two [lane.HalfLife]s:
	// long enough to be a session rather than a burst, short enough that the
	// oldest of it has not decayed to nothing by the time the first request goes
	// out.
	warmOver = 20 * time.Minute
)

// proofCase is one row of §J's e2e table.
type proofCase struct {
	name string
	why  string
	// prime says whether the sheet is folded into the ledger before the first
	// request. False is the cold store, and it is the whole of that row.
	prime bool
	// reasoning is how many thinking deltas the serving lane writes before its
	// first visible word; zero is a model that does not deliberate.
	reasoning int
	// stallAfter is which delta the fault window's silence follows. Zero stalls
	// the lane before it has said anything at all, which is the silent phase.
	stallAfter int
	// rate is how fast every lane in this row's world writes, in the world's
	// tokens a second, AND WHAT THE SHEET PUBLISHES ABOUT THEM. Zero leaves both
	// as the fixture has them.
	//
	// IT IS ONE FIELD AND NOT TWO ON PURPOSE. The liveness clock of §B judges a
	// gap between two thinking deltas against the lane's BELIEVED rate, so a run
	// of thought scripted at half a second a delta on a lane the sheet publishes
	// at thirty tokens a second is a STALLED think by the design's own
	// definition, and a row that staged one would be measuring §K's fourth
	// criterion against the third. A model that deliberates slowly and is
	// published as deliberating slowly is the legitimate long think.
	rate float64
	// pinned sends `only:[pin]`, which makes the act a question rather than a
	// rescue.
	pinned bool
	// reader says somebody is there to answer that question. With nobody there
	// §E has the offer become a borrow at the ceiling, once.
	reader bool
}

// proofCases are §J's four scenarios, with the pinned one run twice.
var proofCases = []proofCase{
	// NAMED FOR ITS FAULT AND NOT FOR ITS STORE, because the store is an arm of
	// this table now and the row is run in every one of them. On the cold arm it
	// is §K's own "stalled lane, cold store, talk role" — the only row that is
	// all three at once — and its home really is empty; on a warmed arm it is
	// the same silence over a ledger with an afternoon behind it.
	{name: "silent lane", why: "the lane says nothing at all, from the first instant"},
	{name: "stalled lane", why: "quiet five words into the answer", prime: true, stallAfter: stallWords},
	{name: "thinking model", why: "a long run of thought, and a stall inside one",
		prime: true, reasoning: thinkDeltas, stallAfter: thinkDeltas / 2, rate: thinkRate},
	{name: "pinned lane, a reader", why: "only:[pin], and somebody to answer the offer",
		prime: true, pinned: true, reader: true},
	{name: "pinned lane, no reader", why: "only:[pin], headless, so the offer is borrowed",
		prime: true, pinned: true},
}

// tokens is how long this row's answer is on the wire.
func (c proofCase) tokens() int {
	if c.reasoning > 0 {
		return thinkVisible
	}
	return streamTokens
}

// script is what this row does to the lane that is about to serve. Everything
// else about that lane — its price, its published percentiles, its draw — is
// untouched, because that is what makes the case interesting: the sheet still
// says the lane is quick.
func (c proofCase) script(profile *lanestub.Profile, speedup int, sick bool) {
	if c.reasoning > 0 {
		profile.Reasoning = c.reasoning
		profile.Rate = c.rate * float64(speedup)
		profile.Tokens = thinkVisible
	}
	if !sick {
		return
	}
	quiet := quietFor / time.Duration(speedup)
	if c.stallAfter > 0 {
		profile.StallAfter, profile.StallFor = c.stallAfter, quiet
		return
	}
	profile.TTFT = quiet
}

// ── WHAT ONE TRIAL COMES BACK WITH ──────────────────────────────────────────

// trial is one request watched all the way through, in the world's own units.
type trial struct {
	sick bool
	// acted and kind are the FIRST act this request raised, and only the first:
	// §K asks when the build stopped waiting, and everything after that moment
	// is a consequence rather than a decision.
	acted bool
	kind  control.Kind
	// action is time-to-action measured from the request going out, and silence
	// is `s` at that moment: how long the wait the controller ended had really
	// run. THEY ARE THE SAME FIGURE UNTIL A VISIBLE WORD HAS ARRIVED and a
	// different one after, because the ceiling bounds the SILENCE — a lane that
	// wrote five words and then stopped is bounded from the fifth word, not from
	// the request.
	action  float64
	silence float64
	wait    float64
	cost    float64
	// ttft is the wait before the SERVING stream's first token of any kind, in
	// seconds from the request going out, and zero when it never wrote one.
	//
	// IT IS WHAT THE LEDGER LEARNS FROM, AND IT IS NOT [trial.action]. This
	// file used to fold time-to-action in as the first-token wait, which is a
	// different quantity on every request and a MISSING one on every request
	// that never acted — so the belief was taught by the acts alone, at the
	// moment each one fired, and every act made the lane that served it look
	// slower than it is. The build stamps its first token on the first content
	// delta OR the first reasoning delta (`internal/provider/client.go`) and
	// folds that as `Sighting.TTFT`, so this does too: a run of thought that
	// began on time and stalled in the middle is a lane that STARTED on time,
	// and the gap clock is what has anything to say about the stall.
	ttft float64
	// late is how much of `action` was this bench's own alarm rather than the
	// build's decision, in milliseconds of the world.
	late float64
	// reason is the controller's own machine word for WHICH CLOCK decided: the
	// first-token belief, the gap between two deltas, the duration of a whole
	// thought, or the ceiling under all three.
	reason string
	// armed says a second request went on the wire, usd is every stream this
	// question put there charged through the one price table, and waste is what
	// the streams that did not answer cost.
	armed bool
	usd   float64
	waste float64
	// thought says a run of thought really BEGAN on this trial with nothing
	// staged wrong in it. A phase that never started is not a phase, and
	// counting one would answer §K's fourth criterion with requests that were
	// acted on before the model had said anything at all.
	thought bool
	// survived says that thought then reached its first visible word WITH NO ARM
	// behind it, which is §K's fourth criterion in its own words. An arm and not
	// an act: [control.Report] sends nothing, so a wait that was merely said out
	// loud is a thinking phase that finished on its own.
	survived bool
	// answered says a visible word arrived at all.
	answered bool
}

// ── ONE ROW, POOLED ─────────────────────────────────────────────────────────

// proofRow is one case pooled over every seed, in §K's own quantities.
type proofRow struct {
	Case     string `json:"case"`
	Why      string `json:"why"`
	N        int    `json:"n"`
	Sick     int    `json:"sick"`
	Well     int    `json:"healthy"`
	Acts     int    `json:"acts"`
	SickActs int    `json:"acts_in_the_fault_window"`
	Arms     int    `json:"arms"`
	FalseArm int    `json:"arms_on_a_healthy_lane"`
	Answered int    `json:"answered"`

	CeilingS   float64 `json:"ceiling_s"`
	ActionP50  float64 `json:"action_p50_s"`
	ActionP90  float64 `json:"action_p90_s"`
	ActionMax  float64 `json:"action_max_s"`
	OverCeil   int     `json:"actions_over_ceiling_since_sent"`
	OverSil    int     `json:"actions_over_ceiling_of_silence"`
	LateMedian float64 `json:"alarm_late_median_ms"`
	LateMax    float64 `json:"alarm_late_max_ms"`

	SilenceP50 float64 `json:"silence_p50_s"`
	SilenceP90 float64 `json:"silence_p90_s"`
	SilenceMax float64 `json:"silence_max_s"`

	FalseHedgePct float64 `json:"false_hedge_pct"`
	SpendPct      float64 `json:"spend_overhead_pct"`
	ReportPct     float64 `json:"report_share_pct"`
	Thinks        int     `json:"legit_thinking_phases"`
	ThinkKept     int     `json:"legit_thinking_phases_not_hedged"`
	ThinkKeptPct  float64 `json:"long_think_not_hedged_pct"`
	USD           float64 `json:"usd_total"`
	Waste         float64 `json:"usd_waste"`

	// Kinds is how many acts of each kind fired and Whys which clock decided
	// them, both in the controller's own words.
	Kinds map[string]int `json:"acts_by_kind"`
	Whys  map[string]int `json:"acts_by_clock"`
}

// ceilingWord is the role ceiling as every line of this table spells it.
func (r proofRow) ceilingWord() string { return fmt.Sprintf("%.0fs", r.CeilingS) }

// kindWords name the six acts. They are the machine's words and nothing but a
// table ever reads them.
var kindWords = map[control.Kind]string{
	control.None: "none", control.Hedge: "hedge", control.Ask: "ask",
	control.Report: "report", control.Escalate: "escalate", control.Commit: "commit",
}

// summariseProof pools one case's trials into §K's quantities.
func summariseProof(c proofCase, got []trial) proofRow {
	out := proofRow{Case: c.name, Why: c.why, N: len(got),
		CeilingS: proofRole.Ceiling().Seconds(), Kinds: map[string]int{}, Whys: map[string]int{}}
	var action, silence, late []float64
	for _, one := range got {
		out.USD += one.usd
		out.Waste += one.waste
		out.Answered += boolCount(one.answered)
		out.Sick += boolCount(one.sick)
		out.Well += boolCount(!one.sick)
		out.Thinks += boolCount(one.thought)
		out.ThinkKept += boolCount(one.thought && one.survived)
		out.Arms += boolCount(one.armed)
		out.FalseArm += boolCount(one.armed && !one.sick)
		if !one.acted {
			continue
		}
		out.Acts++
		out.Kinds[kindWords[one.kind]]++
		out.Whys[one.reason]++
		if !one.sick {
			continue
		}
		out.SickActs++
		action = append(action, one.action)
		silence = append(silence, one.silence)
		late = append(late, one.late)
		out.OverCeil += boolCount(one.action > out.CeilingS)
		out.OverSil += boolCount(one.silence > out.CeilingS)
	}
	out.ActionP50, out.ActionP90, out.ActionMax = pct(action, 0.50), pct(action, 0.90), pct(action, 1.0)
	out.SilenceP50, out.SilenceP90, out.SilenceMax = pct(silence, 0.50), pct(silence, 0.90), pct(silence, 1.0)
	out.LateMedian, out.LateMax = pct(late, 0.50), pct(late, 1.0)
	out.FalseHedgePct = share(out.FalseArm, out.Well)
	out.ReportPct = share(out.Kinds[kindWords[control.Report]], out.Acts)
	out.ThinkKeptPct = share(out.ThinkKept, out.Thinks)
	if out.USD > 0 {
		out.SpendPct = 100 * out.Waste / out.USD
	}
	return out
}

// boolCount is one when a thing happened, so that a tally is a sum rather than
// eight branches.
func boolCount(yes bool) int {
	if yes {
		return 1
	}
	return 0
}

// share is a percentage with an empty denominator answered as nothing rather
// than as a NaN nobody can read.
func share(part, whole int) float64 {
	if whole <= 0 {
		return 0
	}
	return 100 * float64(part) / float64(whole)
}

// ── THE RUN ─────────────────────────────────────────────────────────────────

// proveIt is the whole of the `-proof` run: the four scenarios, the pass table,
// the two figures §K reports without gating, and the raw rows if a file was
// named for them.
func proveIt(w *world, seeds []int, n, speedup int, trace bool, paces, stores []string, jsonOut string, began time.Time) {
	fmt.Printf("script:   the lane that is about to serve goes quiet for %v on the middle half "+
		"of every case's requests\n", quietFor)
	fmt.Printf("stores:   %v; a warmed one has watched %d answers of every pair over the %v "+
		"before the first request, folded through lane.Ledger.Note\n",
		stores, warmSightings, warmOver)
	fmt.Println()
	arms := make([]proofArm, 0, len(paces)*len(stores))
	for _, store := range stores {
		for _, pace := range paces {
			rows := runProof(w, seeds, n, speedup, trace, pace, store)
			arms = append(arms, proofArm{Pace: pace, Store: store, Rows: rows,
				Criteria: proofGates(rows, store)})
		}
	}
	rows, gates := arms[0].Rows, arms[0].Criteria
	for _, arm := range arms {
		fmt.Printf("── a %s store, and the plan waits against the %s belief ─────────────────────\n\n",
			arm.Store, arm.Pace)
		printProof(arm.Rows, arm.Criteria, seeds, n, speedup, arm.Store)
	}
	wall := time.Since(began)
	fmt.Printf("   wall %s\n", wall.Round(time.Second))
	if jsonOut == "" {
		return
	}
	blob, err := json.MarshalIndent(map[string]any{
		"model": w.model, "fetched_at": w.fetched, "seeds": seeds, "n_per_case_per_seed": n,
		"speedup": speedup, "role": string(proofRole), "ceiling_s": proofRole.Ceiling().Seconds(),
		"scenario": proofScenario, "quiet_for_s": quietFor.Seconds(),
		"stores": stores, "warm_sightings_per_pair": warmSightings, "warm_over_s": warmOver.Seconds(),
		"rows": rows, "criteria": gates, "arms": arms, "wall_seconds": wall.Seconds(),
	}, "", "  ")
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(jsonOut, blob, 0o644); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("\n   wrote %s\n", jsonOut)
}

// pacesFrom is which doors the proof rows are run against. Both, unless the
// caller named one: the two tables side by side are the whole of the argument
// about where a number in the first one came from.
func pacesFrom(named string) []string {
	switch named {
	case "":
		return []string{paceShipped, paceFlat}
	case paceShipped, paceFlat:
		return []string{named}
	}
	log.Fatalf("gosim: no pace called %q; it is %q or %q", named, paceShipped, paceFlat)
	return nil
}

// storesFrom is which stores the proof rows are staged in. The cold one, which
// is what §J wrote and what the invariant is proved in, and the warmed one,
// which is what the cost bounds are graded on — unless the caller named one.
func storesFrom(named string) []string {
	switch named {
	case "":
		return []string{storeCold, storeWarmed}
	case storeCold, storeWarmed, storeSeen:
		return []string{named}
	}
	log.Fatalf("gosim: no store called %q; it is %q, %q or %q",
		named, storeCold, storeWarmed, storeSeen)
	return nil
}

// proofArm is one whole table: the rows and the four criteria, for one of the
// two doors a plan can be built against, in one of the stores it can be staged
// in.
type proofArm struct {
	Pace     string      `json:"pace"`
	Store    string      `json:"store"`
	Rows     []proofRow  `json:"rows"`
	Criteria []proofGate `json:"criteria"`
}

// runProof is the whole of §K: every case, every seed, pooled.
func runProof(w *world, seeds []int, n, speedup int, trace bool, pace, store string) []proofRow {
	scen, ok := scenarioNamed(proofScenario)
	if !ok {
		log.Fatalf("gosim: no scenario called %q to run the proof rows in", proofScenario)
	}
	rows := make([]proofRow, 0, len(proofCases))
	for _, c := range proofCases {
		var all []trial
		for _, seed := range seeds {
			all = append(all, runProofSeed(w, scen, c, seed, n, speedup, trace, pace, store)...)
		}
		rows = append(rows, summariseProof(c, all))
	}
	return rows
}

// scenarioNamed is one of the three scenarios above, by name.
func scenarioNamed(name string) (scenario, bool) {
	for _, s := range scenarios {
		if s.name == name {
			return s, true
		}
	}
	return scenario{}, false
}

// runProofSeed is one case at one seed, in a home of its own.
//
// A HOME OF ITS OWN, AND A FRESH ONE PER SEED. The ledger writes every belief
// through a store that resolves under AFORGE_HOME on every call, so a run that
// did not move the state root would fold this program's lanes into the belief
// file of whoever ran it — and the cold-store row would not be cold on its
// second seed.
func runProofSeed(w *world, s scenario, c proofCase, seed, n, speedup int, trace bool, pace, store string) []trial {
	dir, err := os.MkdirTemp("", "gosim-proof-")
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
	switch store {
	case storeWarmed, storeSeen:
		warm(ledger, w, s, c, seed, store == storeWarmed)
	default:
		if c.prime {
			for _, l := range w.lanes {
				ledger.Prime(published(l, w.model, c), lane.SheetWeight)
			}
		}
	}

	stub := lanestub.New(w.model, w.stubLanes(w.shots(seed, c.name, 0, 0), "", "", speedup)...)
	defer stub.Close()

	p := &prover{
		world: w, scen: s, kase: c, seed: seed, stub: stub, ledger: ledger,
		budget: lane.DefaultBudget(), client: &http.Client{},
		speedup: speedup, total: n, at: theMoment, trace: trace, pace: pace,
	}
	out := make([]trial, 0, n)
	for index := 0; index < n; index++ {
		got, err := p.prove(index)
		if err != nil {
			log.Fatalf("proof %s seed %d request %d: %v", c.name, seed, index, err)
		}
		out = append(out, got)
	}
	return out
}

// warm folds a session's own history into the ledger THROUGH THE REAL
// OBSERVATION DOOR, and it is the load-bearing part of the warmed arm.
//
// EVERY BELIEF IN A WARMED STORE GOT THERE BY [lane.Ledger.Note] OR BY
// [lane.Ledger.Prime], in a home of this run's own. Nothing here writes a
// belief, a chain component, a variance or a store file directly, and nothing
// reaches inside `internal/lane` to shortcut the arithmetic — a warmed arm
// assembled that way would be grading the design against a state the design
// itself can never reach, which is the one way this table could pass for a
// reason nobody could act on.
//
// WHAT IS FOLDED IS THE WORLD'S OWN DRAW. The sightings come out of
// [world.shots] — the same generator the requests themselves are served from,
// keyed on a stream of its own so that warming a store does not shift the world
// a request then sees. A lane's history is therefore its true distribution and
// not a tidied version of it, tails included.
//
// TIMING ONLY, AND NO OUTCOMES. [lane.Ledger.NoteOutcome] is a second door and
// folding acceptances through it would move the quality gate as well, which
// changes WHICH LANE IS CHOSEN and would leave two variables between the arms.
// The quality prior a warmed lane holds is exactly the one a cold lane holds —
// set by [ledger.Prime] from the quantization — so what separates these arms is
// the pace belief and nothing else.
func warm(ledger lane.Ledger, w *world, s scenario, c proofCase, seed int, sheet bool) {
	// THE SHEET IS OLDER THAN THE ANSWERS. It is stamped at the start of the
	// history rather than at the moment of the request, because a belief that
	// was primed after it was measured would be aged backwards — and because a
	// half-hour aggregate really is the older of the two things a warm store
	// holds.
	began := theMoment.Add(-warmOver)
	for _, l := range w.lanes {
		row := published(l, w.model, c)
		row.At = began
		if !sheet {
			row = factsOnly(row)
		}
		ledger.Prime(row, lane.SheetWeight)
	}
	// The answers a session watched are as long as the scenario's own, which is
	// what makes the rate half of each sighting a rate: [ratedFloor] refuses to
	// rate anything shorter than thirty-two tokens, and a history that taught
	// the ledger nothing about how fast a lane writes would be half a history.
	tokens := s.answer()
	for draw := 0; draw < warmSightings; draw++ {
		shots := w.shots(seed, "warm|"+c.name, draw, 0)
		at := began.Add(time.Duration(int64(draw+1) * int64(warmOver) / int64(warmSightings)))
		for index, l := range w.lanes {
			rate := shots[index].rate
			if c.rate > 0 {
				// A LANE IS OBSERVED AT THE SPEED IT WRITES. See [proofCase.rate]:
				// a row whose world writes at a scripted rate must be believed at
				// that rate, or the liveness clock reads a legitimate think as a
				// stall.
				rate = c.rate
			}
			gen := time.Duration(float64(tokens) / rate * float64(time.Second))
			ledger.Note(lane.Sighting{
				ID:           lane.ID{Model: w.model, Lane: l.name},
				TTFT:         time.Duration(shots[index].ttftMs * float64(time.Millisecond)),
				Gen:          gen,
				Gap:          gen / time.Duration(tokens),
				Tokens:       tokens,
				PromptTokens: promptTokens,
				At:           at,
			})
		}
	}
}

// factsOnly is one row with its timing taken off and its facts left on, which
// [lane.Ledger.Prime] documents as a legal row: "a lane the sheet published no
// timing for is a lane we cannot score, not a lane we cannot judge". It is what
// [storeSeen] primes with.
func factsOnly(row lane.Row) lane.Row {
	row.TTFTp50, row.TTFTp75, row.TTFTp90, row.TTFTp99 = 0, 0, 0, 0
	row.Ratep50, row.Ratep75, row.Ratep90, row.Ratep99 = 0, 0, 0, 0
	row.At = time.Time{}
	return row
}

// published is the sheet row as the proof rows prime the ledger with it: the
// same row every other arm in this program is primed from, WITH THE MOMENT IT
// WAS PUBLISHED ON IT.
//
// THE MOMENT IS NOT DECORATION AND LEAVING IT OFF IS A MEASURABLE MISTAKE.
// [lane.Ledger.Prime] folds a row into the four-level chain only when the row
// carries one — a component stamped with a time that never happened is a
// component that can never be aged — while the FLAT belief is primed either
// way. And [lane.PaceFor] prefers the chain over the flat belief whenever the
// chain believes anything at all, which it does from its own prior. So a
// fixture primed with no moment leaves the flat belief knowing each lane's real
// median and leaves the controller waiting against a prior nobody ever fed:
// one second, with the four prior widths summed under it.
//
// The three scenarios above prime without a moment ([worldLane.row] sets none),
// which is a finding of this lane rather than something it fixes here — moving
// `world.go` would move the committed ship-gate table underneath a run nobody
// re-took. REPORT.md carries it.
func published(l worldLane, model string, c proofCase) lane.Row {
	row := l.row(model)
	if c.rate > 0 {
		// A LANE IS PUBLISHED AT THE SPEED IT WRITES. See [proofCase.rate].
		row.Ratep50, row.Ratep75, row.Ratep90, row.Ratep99 = c.rate, c.rate, c.rate, c.rate
	}
	// The sheet is a thirty-minute aggregate and this run's own moment is when
	// it is being read, so the row is stamped as published now rather than at
	// the fixture's `fetched_at`: a row stamped after the moment the request
	// carries would be a belief aged backwards.
	row.At = theMoment
	return row
}

// prover is one proof row at one seed.
type prover struct {
	world   *world
	scen    scenario
	kase    proofCase
	seed    int
	stub    *lanestub.Server
	ledger  lane.Ledger
	budget  *lane.Budget
	client  *http.Client
	speedup int
	total   int
	trace   bool
	// pace is which door the plan is built against: [paceShipped] or [paceFlat].
	pace string

	// at is the moment in the WORLD this request went out and wire the moment on
	// the socket it went out at; every world moment handed to the watch is the
	// second mapped through [prover.worldly] onto the first.
	at   time.Time
	wire time.Time
	// rates is how fast each lane is writing in the WORLD's tokens a second,
	// which is the world's own draw and never a measurement — see [instantRate]
	// for the measurement that made that necessary.
	rates map[string]float64
}

func (p *prover) scaled(d time.Duration) time.Duration  { return d / time.Duration(p.speedup) }
func (p *prover) worldly(d time.Duration) time.Duration { return d * time.Duration(p.speedup) }

// worldNow is where the world has got to, read off the socket's own clock.
func (p *prover) worldNow() time.Time { return p.at.Add(p.worldly(time.Since(p.wire))) }

// prove is one request: the choice, the plan, the stream, the controller on top
// of it, and whatever its first act led to.
func (p *prover) prove(index int) (trial, error) {
	sick := index >= p.total/4 && index < 3*p.total/4
	req := p.scen.request(p.world.model, p.at, p.total-index)
	choice := p.choose(req)
	head := p.headFor(choice)
	p.stub.Model(p.world.model, p.stage(p.world.shots(p.seed, "proof|"+p.kase.name, index, 0), head, sick)...)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	watch := lane.Watching(p.plan(choice, head))
	watch.SetExpectedTokens(p.kase.tokens())

	p.wire = time.Now()
	primary := p.open(ctx, choice.Order, choice.Only)
	defer primary.cancel()

	out := trial{sick: sick, usd: p.priceOf(head)}
	act, served, err := p.untilActed(watch, primary, &out)
	if err != nil {
		return trial{}, err
	}
	if served == "" {
		served = head
	}
	// AN ACT THAT SENDS NOTHING DOES NOT END THE REQUEST. [control.Report] says
	// the wait is real and sends nobody anywhere; an offer with a reader is a
	// question, not an override. The stream that was already in flight is still
	// the answer in both cases, so it is read out — which is also the only way
	// a run of thought ever finishes, and the only way the duration clock ever
	// learns what one costs.
	if !out.acted || !p.answer(ctx, act, primary, served, &out) {
		p.drain(primary, &out)
	}
	out.survived = out.thought && out.answered && !out.armed
	p.close(index, served, out)
	return out, nil
}

// choose is the routing answer this row asks for.
//
// THE PIN IS BUILT THE WAY THE TRANSPORT BUILDS ONE. `internal/provider`'s
// lanes.go turns a pin into `Choice{Only: […], Frontier: choice.Frontier}` — a
// demand for one machine with the frontier's other lanes still named, which is
// what gives an offer somewhere to point at. A pin that threw the frontier away
// would be a pin with no rescue to offer, and this row would prove nothing.
func (p *prover) choose(req lane.Request) lane.Choice {
	choice := lane.Default().Chooser().Choose(req)
	head := lane.HeadOf(choice)
	if !p.kase.pinned || head == "" {
		return choice
	}
	return lane.Choice{Only: []string{head}, Frontier: choice.Frontier}
}

// headFor is the lane about to serve. With no routing opinion at all — which is
// the cold store, on every request — the fake router answers from the head of
// its own list, so this program says which lane that is rather than letting the
// order the sheet happened to be written in decide what a row measures.
func (p *prover) headFor(choice lane.Choice) string {
	if head := lane.HeadOf(choice); head != "" {
		return head
	}
	if gated := p.world.gated(p.scen); len(gated) > 0 {
		return p.world.lanes[gated[0]].name
	}
	return p.world.lanes[0].name
}

// plan is what the controller is built with, assembled the way
// `internal/provider`'s planFor assembles it: [lane.PlanFor] for the head, what
// is believed about it, the role's ceiling and λ and the alternatives the
// frontier named — and then the things only a transport knows.
func (p *prover) plan(choice lane.Choice, head string) control.Plan {
	plan := lane.PlanFor(choice, p.paceFor(head), proofRole, p.at)
	plan.Pinned = len(choice.Only) > 0
	plan.Purse = lane.Spending(p.budget)
	plan.Think = lane.Thinks(p.world.model, "", p.at)
	return plan
}

// paceFor is what this plan waits against: the door the transport asks, or the
// flat belief underneath it. See [paceShipped].
func (p *prover) paceFor(head string) lane.Pace {
	id := lane.ID{Model: p.world.model, Lane: head}
	if p.pace == paceFlat {
		belief, _ := p.ledger.Belief(id)
		return lane.PaceOf(belief)
	}
	return lane.PaceFor(id, p.at)
}

// stage scripts the world for one request: every lane behaving as its draw
// says, with the lane about to serve declared first and carrying whatever this
// row stages on it.
func (p *prover) stage(shots []shot, head string, sick bool) []lanestub.Lane {
	staged := p.world.stubLanes(shots, head, "", p.speedup)
	p.rates = make(map[string]float64, len(staged))
	for at, l := range p.world.lanes {
		p.rates[l.name] = shots[at].rate
	}
	for index := range staged {
		if staged[index].Name != head {
			continue
		}
		p.kase.script(&staged[index].Profile, p.speedup, sick)
		if p.kase.rate > 0 {
			p.rates[head] = p.kase.rate
		}
	}
	return staged
}

// priceOf is what one whole request costs on one lane, through the one price
// function every arm in this program is charged through.
func (p *prover) priceOf(name string) float64 {
	l, index := p.world.find(name)
	if index < 0 {
		return 0
	}
	return l.price(p.scen)
}

// ── PHASE ONE: WATCH UNTIL SOMETHING IS DONE ────────────────────────────────

// untilActed drives the controller over the stream until it raises its first
// act, or until the answer arrives without one. It hands back the act, the lane
// that named itself, and the first error the wire produced.
//
// EVERY STREAM EVENT IS ONE [control.Reading], which is the shape
// `internal/provider`'s stream loop fills: a visible word, a hidden delta of a
// run of thought, or the router's own comment line — and the three are not
// interchangeable, which is the whole of why this design has one controller
// rather than three rule sets.
func (p *prover) untilActed(watch *lane.Watch, s *armed, out *trial) (control.Act, string, error) {
	served, rang := "", time.Time{}
	for {
		// A DEADLINE ALREADY RUNG AT IS NOT A NEW DEADLINE. The controller
		// answers with the moment it wants to be woken at and never with one in
		// the past, so a deadline that has not moved since the last ring is one
		// nothing will change about, and re-arming on it would be a beat
		// spinning on its own timer.
		ring := &alarm{}
		if at := watch.DeadlineAt(); at.After(rang) {
			ring = p.ringAt(at)
		}
		select {
		case seen := <-s.sights:
			ring.cancel()
			if served == "" && seen.lane != "" {
				served = seen.lane
				belief, _ := p.ledger.Belief(lane.ID{Model: p.world.model, Lane: served})
				watch.Serving(served, belief, seen.reading.At)
			}
			if seen.reading.Hidden > 0 && !out.sick {
				out.thought = true
			}
			p.sawToken(out, seen)
			if act := watch.Read(seen.reading); act.Kind != control.None {
				p.mark(out, act, seen.reading.At, 0)
				return act, served, nil
			}
		case <-ring.rings:
			// THE MOMENT ASKED ABOUT IS THE MOMENT THE CONTROLLER ASKED FOR, and
			// not the one this bench's timer got round to. A beat wakes at the
			// deadline and asks whether anything has arrived by then; the wire
			// has really reached that moment by the time the alarm rings, and
			// anything that arrived in the microseconds between is still sitting
			// in the channel to be read with its own stamp. What the timer cost
			// is measured beside it and printed with the table rather than
			// folded into the figure the ceiling is judged on.
			rang = ring.at
			late := p.worldNow().Sub(ring.at)
			if act := watch.Quiet(ring.at); act.Kind != control.None {
				p.mark(out, act, ring.at, late)
				return act, served, nil
			}
		case err := <-s.done:
			ring.cancel()
			return control.Act{}, served, err
		}
	}
}

// sawToken records one moment from the stream that is answering: whether a
// word a person can read has arrived, and — the first time anything at all
// does — how long this lane took to start, which is the only first-token
// measurement there is to fold back.
func (p *prover) sawToken(out *trial, seen sight) {
	if seen.reading.Visible <= 0 && seen.reading.Hidden <= 0 {
		return
	}
	if out.ttft == 0 {
		out.ttft = seen.reading.At.Sub(p.at).Seconds()
	}
	out.answered = out.answered || seen.reading.Visible > 0
}

// mark records the first act with the numbers the controller decided it on, and
// with how late this bench's own alarm was when it asked.
func (p *prover) mark(out *trial, act control.Act, asked time.Time, late time.Duration) {
	out.acted, out.kind, out.reason = true, act.Kind, act.Reason
	out.silence = act.Silence.Seconds()
	out.wait, out.cost = act.Wait, act.Cost
	out.action = asked.Sub(p.at).Seconds()
	out.late = float64(late) / float64(time.Millisecond)
}

// ── PHASE TWO: WHAT THE ACT LED TO ──────────────────────────────────────────

// answer carries out the first act.
//
// A HEDGE GOES OUT AS A DEMAND, to the lane the controller named, and the purse
// is SPENT here rather than where it was asked: [lane.Budget.Allow] counts the
// arm it allows, and the controller only ever asked whether one was affordable.
// An offer with a reader is left standing, because a pin is asked and this bench
// has nobody to answer with. An offer with nobody there becomes the borrow §E
// describes — and that conversion is this file's, not the build's.
func (p *prover) answer(ctx context.Context, act control.Act, primary *armed, served string, out *trial) bool {
	borrow := act.Kind == control.Ask && !p.kase.reader
	if act.Lane == "" || (act.Kind != control.Hedge && !borrow) {
		return false
	}
	price := p.priceOf(act.Lane)
	if !p.budget.Allow(p.at, price) {
		return false
	}
	p.budget.NoteHedge(price, p.at)
	out.armed = true
	out.usd += price
	arm := p.open(ctx, nil, []string{act.Lane})
	defer arm.cancel()
	// WHICHEVER STREAM SPEAKS FIRST TAKES THE VOICE, which is the shipped
	// one-voice rule, and the loser is charged the whole request: a stream
	// cancelled after its prompt was read has already been billed for it.
	select {
	case seen := <-arm.sights:
		// The ARM's first word is not the served lane's, so it answers the
		// request and teaches the ledger nothing about the lane it rescued.
		out.answered = out.answered || seen.reading.Visible > 0
		out.waste += p.priceOf(served)
	case seen := <-primary.sights:
		p.sawToken(out, seen)
		out.waste += price
	case <-arm.done:
		out.waste += price
	}
	return true
}

// drain reads a stream nobody was sent to rescue out to its end, which is what
// happens to it in the build: nothing was cancelled, so the answer still
// arrives, and a run of thought that finishes is one the duration clock can
// learn from.
func (p *prover) drain(one *armed, out *trial) {
	for {
		select {
		case seen := <-one.sights:
			p.sawToken(out, seen)
		case <-one.done:
			return
		}
	}
}

// close folds the request back: the sighting the ledger learns from, the run of
// thought the duration clock learns from, the purse's own denominator, and the
// world moving on by what this took plus the seconds somebody spends reading
// what arrived.
func (p *prover) close(index int, served string, out trial) {
	if p.kase.prime && out.ttft > 0 && out.answered {
		p.ledger.Note(lane.Sighting{
			ID:           lane.ID{Model: p.world.model, Lane: served},
			TTFT:         time.Duration(out.ttft * float64(time.Second)),
			Gen:          time.Duration(float64(p.kase.tokens()) / p.rateOf(served) * float64(time.Second)),
			Tokens:       p.kase.tokens(),
			PromptTokens: promptTokens,
			At:           p.at,
		})
	}
	// A RUN OF THOUGHT THAT FINISHED IS WHAT MAKES THE DURATION CLOCK A
	// MEASUREMENT rather than a prior. The build folds one in after every
	// thinking phase it sees; a bench that predicted from a chain nothing had
	// ever written to would be measuring the prior and calling it the build.
	if out.thought && out.answered {
		lane.NoteThought(p.world.model, "", thinkFor, p.at)
	}
	p.budget.NoteRequest(p.at)
	p.budget.NoteSpend(out.usd, p.at)
	if p.trace {
		fmt.Fprintf(os.Stderr, "proof %-22s/%d %4d  served %-14s %-36s action %7.2fs  s %7.2fs  $%.6f%s\n",
			p.kase.name, p.seed, index, served, kindWords[out.kind]+"/"+out.reason+fmt.Sprintf(" W%.2f A%.2f", out.wait, out.cost), out.action, out.silence, out.usd,
			map[bool]string{true: "  SICK", false: ""}[out.sick])
	}
	reading := float64(p.scen.visible) / lane.ReadRate * float64(time.Second)
	p.at = p.at.Add(p.worldly(time.Since(p.wire)) + time.Duration(reading))
}

// thinkFor is how long a whole scripted run of thought lasts in the world: the
// gaps between its deltas, of which there is one fewer than there are deltas.
const thinkFor = time.Duration(float64(thinkDeltas-1) / thinkRate * float64(time.Second))

// rateOf is how fast one lane is writing in the world's own tokens a second,
// floored so that a sighting is never charged with an infinite rate.
func (p *prover) rateOf(name string) float64 {
	if rate := p.rates[name]; rate > rateFloor {
		return rate
	}
	return rateFloor
}

// ── THE ALARM ───────────────────────────────────────────────────────────────

// alarm rings at a moment of the WORLD, accurate to microseconds of it. A zero
// alarm never rings, which is what a caller with no deadline to wait for gets.
type alarm struct {
	at    time.Time
	rings chan struct{}
	stop  chan struct{}
}

// cancel stops an alarm that is no longer wanted. It is safe on one that has
// already rung and on one that was never armed.
func (a *alarm) cancel() {
	if a.stop == nil {
		return
	}
	select {
	case <-a.stop:
	default:
		close(a.stop)
	}
}

// ringAt arms the deadline the controller asked to be woken at. See the note at
// the top of this file for why the last [spinWire] is spun rather than slept: a
// timer's own lateness at [speedup] is tens of milliseconds of the world, which
// is a measurable share of a ten-second ceiling.
func (p *prover) ringAt(at time.Time) *alarm {
	ring := &alarm{at: at, rings: make(chan struct{}), stop: make(chan struct{})}
	go func() {
		for {
			select {
			case <-ring.stop:
				return
			default:
			}
			left := p.scaled(at.Sub(p.worldNow()))
			switch {
			case left <= 0:
				close(ring.rings)
				return
			case left <= spinWire:
				runtime.Gosched()
			default:
				if !p.sleep(left-spinWire, ring.stop) {
					return
				}
			}
		}
	}()
	return ring
}

// sleep passes d unless the alarm is cancelled first, and says which happened.
func (p *prover) sleep(d time.Duration, stop <-chan struct{}) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-stop:
		return false
	}
}

// ── THE WIRE ────────────────────────────────────────────────────────────────

// sight is one moment of a stream as the read loop saw it, already in the
// world's own clock.
type sight struct {
	reading control.Reading
	lane    string
}

// armed is one request in flight.
type armed struct {
	sights chan sight
	done   chan error
	cancel context.CancelFunc
}

// open puts one request on the wire and reads it in the background, turning
// every event into the [control.Reading] the controller is driven by.
func (p *prover) open(ctx context.Context, order, only []string) *armed {
	inner, cancel := context.WithCancel(ctx)
	one := &armed{sights: make(chan sight, 4), done: make(chan error, 1), cancel: cancel}
	// THE END IS SENT ONCE AND THEN CLOSED, so that a second reader of a stream
	// that is already over — [prover.drain], on a request nobody was sent to
	// rescue — is told so immediately instead of waiting for an end that has
	// already happened.
	go func() {
		one.done <- p.read(inner, one, order, only)
		close(one.done)
	}()
	return one
}

// read is the wire itself: the preference in the router's own field names, and
// the stream back with the serving lane named on every chunk.
func (p *prover) read(ctx context.Context, one *armed, order, only []string) error {
	response, err := p.post(ctx, order, only)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		drained, _ := io.ReadAll(response.Body)
		return fmt.Errorf("the router answered %d: %s", response.StatusCode, strings.TrimSpace(string(drained)))
	}
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		if !strings.HasPrefix(line, "data: ") {
			// A comment line is the router's own heartbeat: proof about the
			// PATH and never about the endpoint.
			p.tell(ctx, one, sight{reading: control.Reading{At: p.worldNow(), Beat: true}})
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		if payload == "[DONE]" {
			return nil
		}
		seen, ok, err := p.decode(payload)
		if err != nil {
			return err
		}
		if ok {
			p.tell(ctx, one, seen)
		}
	}
	return scanner.Err()
}

// decode turns one frame into one moment, and says whether it was one at all: a
// usage frame and an empty delta are the router talking about the answer rather
// than writing it.
func (p *prover) decode(payload string) (sight, bool, error) {
	var chunk struct {
		Provider string `json:"provider"`
		Choices  []struct {
			Delta struct {
				Content   string `json:"content"`
				Reasoning string `json:"reasoning"`
			} `json:"delta"`
		} `json:"choices"`
		Usage *json.RawMessage `json:"usage"`
	}
	if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
		return sight{}, false, err
	}
	if chunk.Usage != nil || len(chunk.Choices) == 0 {
		return sight{}, false, nil
	}
	seen := sight{reading: control.Reading{At: p.worldNow()}, lane: chunk.Provider}
	switch delta := chunk.Choices[0].Delta; {
	case delta.Content != "":
		seen.reading.Visible = 1
	case delta.Reasoning != "":
		seen.reading.Hidden = 1
	default:
		return sight{}, false, nil
	}
	return seen, true, nil
}

// tell hands one moment to whoever is driving the controller, and gives up when
// nobody is listening any more — which is a stream that has been cancelled, and
// the ordinary end of a loser.
func (p *prover) tell(ctx context.Context, one *armed, s sight) {
	select {
	case one.sights <- s:
	case <-ctx.Done():
	}
}

// post is the request as the router's own field names spell it.
func (p *prover) post(ctx context.Context, order, only []string) (*http.Response, error) {
	body := map[string]any{
		"model":      p.world.model,
		"stream":     true,
		"max_tokens": p.kase.tokens(),
		"messages": []map[string]string{
			// The stub counts a prompt at four characters to the token off the
			// RAW JSON, quotes included, so this is exactly promptTokens.
			{"role": "user", "content": strings.Repeat("x", 4*promptTokens-2)},
		},
	}
	preference := map[string]any{}
	if len(order) > 0 {
		preference["order"] = order
	}
	if len(only) > 0 {
		preference["only"] = only
	}
	if len(preference) > 0 {
		body["provider"] = preference
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	ask, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.stub.URL()+"/chat/completions", strings.NewReader(string(encoded)))
	if err != nil {
		return nil, err
	}
	ask.Header.Set("Content-Type", "application/json")
	return p.client.Do(ask)
}

// ── THE TABLE ───────────────────────────────────────────────────────────────

// proofGate is one criterion of §K with the value that decided it.
//
// GATED SAYS WHETHER THE VERDICT COUNTS. A criterion that is measured and
// reported without deciding anything is not a criterion that passed, and it is
// not one that failed either; a table that printed it as one or the other would
// be a table that lied in whichever direction flattered the run.
type proofGate struct {
	Criterion string  `json:"criterion"`
	Threshold string  `json:"threshold"`
	Measured  string  `json:"measured"`
	Where     string  `json:"measured_on"`
	Value     float64 `json:"value"`
	Gated     bool    `json:"gated"`
	Pass      bool    `json:"pass"`
}

// proofGates is §K's four criteria, each read off the rows it is about, and
// which of them DECIDE depends on the store the arm was staged in.
//
// WHICH ROW ANSWERS WHICH CRITERION IS NOT ARBITRARY. The ceiling criterion
// names its own state — a stalled lane, a cold store, the talk role — so it is
// read off the silent row, which is the only one that is all three at once, and
// every other row's fault window is reported beside it. The false-hedge and the
// spend criteria are about a build's ordinary behaviour and are pooled over
// every row. The long-think criterion exists only where there is a think.
//
// ── WHICH ARM DECIDES WHAT, AND WHY ─────────────────────────────────────────
//
// ON A WARMED STORE ALL FOUR BOUNDS ARE ENFORCED. That is the steady state §K's
// numbers describe — two per cent of extra traffic is Dean & Barroso's figure
// for a system that knows its own service times — and it is the state a person
// sits in for all but the first minute of a session.
//
// ON A COLD STORE THE INVARIANT IS ENFORCED AND THE COSTS ARE REPORTED. A
// process that has never measured a pair cannot know which lane is quick, and
// the arms it sends to find out are EXPLORATION rather than false hedges: they
// are how the ledger stops being cold. What has to bound them is not §K's
// steady-state figure but the purse, which is the mechanism the design gives
// for exactly this — so the cold arms gate on the purse's own ceiling instead,
// and the two §K cost figures are printed beside it without a verdict. The
// ceiling and the long think are unchanged: neither is a cost and neither gets
// an allowance for being cold.
func proofGates(rows []proofRow, store string) []proofGate {
	var cold proofRow
	arms, well, thinks, kept := 0, 0, 0, 0
	usd, waste := 0.0, 0.0
	for _, r := range rows {
		if r.Case == proofCases[0].name {
			cold = r
		}
		arms += r.FalseArm
		well += r.Well
		thinks += r.Thinks
		kept += r.ThinkKept
		usd += r.USD
		waste += r.Waste
	}
	falsePct, thinkPct := share(arms, well), share(kept, thinks)
	spendPct := 0.0
	if usd > 0 {
		spendPct = 100 * waste / usd
	}
	inside := share(cold.SickActs-cold.OverCeil, cold.SickActs)
	steady := store != storeCold
	out := []proofGate{
		{Criterion: "time-to-action, stalled lane, " + store + " store, talk role",
			Threshold: "<= " + cold.ceilingWord() + " in 100%",
			Measured:  fmt.Sprintf("%.2f%% of %d acts, max %.2fs", inside, cold.SickActs, cold.ActionMax),
			Where:     "the silent row, fault window",
			Value:     inside, Gated: true, Pass: cold.SickActs > 0 && cold.OverCeil == 0},
		{Criterion: "false hedges on a healthy lane",
			Threshold: "<= 2% of requests",
			Measured:  fmt.Sprintf("%.2f%% of %d healthy requests", falsePct, well),
			Where:     "every case, healthy half",
			Value:     falsePct, Gated: steady, Pass: falsePct <= gateFalseHedgePct},
		{Criterion: "spend overhead",
			Threshold: "<= 3% of the arm's own bill",
			Measured:  fmt.Sprintf("%.2f%% of $%.4f", spendPct, usd),
			Where:     "every case",
			Value:     spendPct, Gated: steady, Pass: spendPct <= gateSpendOverheadPct},
	}
	if !steady {
		// THE PURSE IS WHAT BOUNDS EXPLORATION, so on a cold store it is what
		// the bill is graded against. The ceiling is read off the shipped budget
		// rather than restated here, because a figure written down twice is a
		// figure that will drift away from the one the build enforces.
		purse := 100 * lane.DefaultBudget().Share()
		out = append(out, proofGate{
			Criterion: "loser spend inside the purse",
			Threshold: fmt.Sprintf("<= %.0f%% of the bill, which lane.DefaultBudget() allows", purse),
			Measured:  fmt.Sprintf("%.2f%% of $%.4f", spendPct, usd),
			Where:     "every case",
			Value:     spendPct, Gated: true, Pass: spendPct <= purse})
	}
	return append(out, proofGate{
		Criterion: "long think, not hedged",
		Threshold: ">= 95% of thinking phases",
		Measured:  fmt.Sprintf("%.2f%% of %d thinking phases", thinkPct, thinks),
		Where:     "thinking model, healthy half",
		Value:     thinkPct, Gated: true, Pass: thinks > 0 && thinkPct >= gateLongThinkPct})
}

// printProof writes §K's pass table with the measured value beside every
// threshold, and then the two figures §K asks for and does not gate.
func printProof(rows []proofRow, gates []proofGate, seeds []int, n, speedup int, store string) {
	fmt.Println("── §K, the four scenarios ──────────────────────────────────────────────────────")
	fmt.Printf("   role %s, ceiling %s   seeds %v   requests per case per seed %d   pooled %d   wire %d× the world\n",
		proofRole, proofRole.Ceiling(), seeds, n, n*len(seeds), speedup)
	fmt.Printf("   store %s   %s\n", store, storyOf(store))
	fmt.Println("   the fault runs for the middle half of every case; the other half is the healthy one")
	fmt.Println()
	fmt.Printf("   %-24s%6s%6s%6s%6s%10s%10s%10s%7s%8s%8s\n",
		"case", "n", "sick", "acts", "arms", "act p50", "act p90", "act max", "over", "false%", "$over%")
	fmt.Printf("   %s\n", strings.Repeat("-", 104))
	for _, r := range rows {
		fmt.Printf("   %-24s%6d%6d%6d%6d%9.2fs%9.2fs%9.2fs%7d%8.2f%8.2f\n",
			r.Case, r.N, r.Sick, r.SickActs, r.Arms, r.ActionP50, r.ActionP90, r.ActionMax,
			r.OverCeil, r.FalseHedgePct, r.SpendPct)
	}
	fmt.Println()
	for _, r := range rows {
		fmt.Printf("     %-24s %s\n", r.Case, r.Why)
		fmt.Printf("     %-24s acts: %s   clocks: %s\n", "", kindTally(r.Kinds), whyTally(r.Whys))
		fmt.Printf("     %-24s over the ceiling: %d since sent, %d of silence   "+
			"alarm late %.0f/%.0f ms median/max   a word arrived on %d/%d\n",
			"", r.OverCeil, r.OverSil, r.LateMedian, r.LateMax, r.Answered, r.N)
	}
	fmt.Println()
	fmt.Println("── §K, the criteria ────────────────────────────────────────────────────────────")
	fmt.Println()
	fmt.Printf("   %-54s%-54s%-34s  verdict\n", "criterion", "threshold", "measured")
	fmt.Printf("   %s\n", strings.Repeat("-", 152))
	passed, gated := 0, 0
	for _, g := range gates {
		tag := "REPORTED"
		if g.Gated {
			gated++
			tag = "FAIL"
			if g.Pass {
				tag, passed = "PASS", passed+1
			}
		}
		fmt.Printf("   %-54s%-54s%-34s  %s\n", g.Criterion, g.Threshold, g.Measured, tag)
	}
	fmt.Printf("\n   %d of %d gated criteria pass; %d reported.\n\n", passed, gated, len(gates)-gated)
	fmt.Println("── reported, and not gated ─────────────────────────────────────────────────────")
	fmt.Println()
	fmt.Printf("   %-24s%14s%14s%14s%12s\n", "case", "s p50", "s p90", "s max", "report%")
	fmt.Printf("   %s\n", strings.Repeat("-", 78))
	for _, r := range rows {
		fmt.Printf("   %-24s%13.2fs%13.2fs%13.2fs%12.1f\n",
			r.Case, r.SilenceP50, r.SilenceP90, r.SilenceMax, r.ReportPct)
	}
	fmt.Println()
}

// storyOf is the one line that says what a store is, so that a table pasted
// into a report carries its own staging with it.
func storyOf(store string) string {
	switch store {
	case storeWarmed:
		return fmt.Sprintf("the sheet, and %d answers of every pair watched over the %v before "+
			"the first request", warmSightings, warmOver)
	case storeSeen:
		return fmt.Sprintf("the sheet's FACTS only — no published percentile — and %d answers "+
			"of every pair watched; diagnostic, not gated", warmSightings)
	default:
		return "the silent row empty and the other four primed from the sheet; no pair ever measured"
	}
}

// whyTally is which clock decided the acts, commonest first.
func whyTally(whys map[string]int) string {
	names := make([]string, 0, len(whys))
	for name := range whys {
		names = append(names, name)
	}
	sort.Slice(names, func(a, b int) bool { return whys[names[a]] > whys[names[b]] })
	out := make([]string, 0, len(names))
	for _, name := range names {
		out = append(out, fmt.Sprintf("%s %d", name, whys[name]))
	}
	if len(out) == 0 {
		return "none"
	}
	return strings.Join(out, ", ")
}

// kindTally is which acts fired, in the ladder's own order.
func kindTally(kinds map[string]int) string {
	out := make([]string, 0, len(kinds))
	for _, kind := range []control.Kind{control.Hedge, control.Ask, control.Report, control.Escalate, control.Commit} {
		if count := kinds[kindWords[kind]]; count > 0 {
			out = append(out, fmt.Sprintf("%s %d", kindWords[kind], count))
		}
	}
	if len(out) == 0 {
		return "none"
	}
	return strings.Join(out, ", ")
}
