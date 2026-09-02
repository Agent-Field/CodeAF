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
	// gateAvoidablePct is what a controller may spend on arms that bought
	// nothing, as a share of the arm's own bill.
	//
	// ── WHERE THIS NUMBER COMES FROM, AND WHY IT IS NOT THREE ───────────────
	//
	// §K's spend clause was a flat 3% of the bill and it is withdrawn on the
	// natural mix as infeasible by arithmetic: a rescue is a whole second
	// request, so rescue overhead is about the fault rate whatever the policy
	// is, and at one fault in twenty no controller that rescues stalls — not
	// even one that armed the broken requests and only those — can come in
	// under three per cent. The baseline arm measures that floor rather than
	// arguing it.
	//
	// What a controller CAN be held to is the avoidable half: arms on healthy
	// requests, and arms on faults that then lost to the lane they were
	// rescuing. That was measured at 0.85% and 0.64% on the two seed sets, a
	// spread of about two tenths of a point, so a ceiling of 3% would tolerate
	// a three-and-a-half-fold regression before it said anything — which is a
	// gate that cannot fail and therefore is not a gate.
	//
	// THE CEILING IS THE MEASUREMENT TIMES A HALF AGAIN: max(0.85, 0.64) × 1.5
	// = 1.28%, rounded to 1.3%. Half again is roughly six times the observed
	// seed-to-seed spread, so an honest seed cannot fail it; anything larger is
	// a real regression and this gate is meant to catch one. It is a figure
	// about THIS bench's world and it moves when the measurement does — which
	// is the point of writing the derivation down beside it.
	gateAvoidablePct = 1.3
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

// ── THE BASELINE: A BUILD WITH NO WAITING POLICY AT ALL ─────────────────────
//
// Every figure above compares this design against its own thresholds. None of
// them answers the question a person actually has, which is whether the
// mechanism is worth its money AT ALL — and that question has a floor, not a
// threshold: what does the same workload cost, and how long does it take, with
// no controller, no deadline and no arms?
//
// So there is a second policy. It routes exactly as the other one does — the
// chooser is the same, because routing and waiting are two questions and only
// one of them is under test — and then it WAITS. If the wire goes quiet longer
// than the transport's own guard allows, the attempt is thrown away whole and
// re-sent to the next lane, serially, up to [serialAttempts]. That is what this
// build did before this design and it is what any build without one does.
//
// IT IS NOT AN ARM THAT LOSES. A hedge pays for two streams and keeps whichever
// speaks first; a retry pays for the first stream, throws it away, and then pays
// for the second. The two cost the same money and buy very different waits, and
// which of them is the better trade is exactly what this pair of arms measures.
const (
	// policyWaiting is this design: one controller on every call.
	policyWaiting = "waiting"
	// policyBaseline is the floor: no controller, and the transport's guard is
	// the only thing that ever acts.
	policyBaseline = "baseline"
)

// serialAttempts is how many lanes a build with no waiting policy will try
// before it gives up. It is the retry model the rest of this lab already
// assumes — "a refused answer costs exactly one retry on the next lane, at most
// three attempts" — said once here for the arm that actually retries.
const serialAttempts = 3

// The transport's own silence bounds, as `internal/provider/streamguard.go`
// derives them for a role: 90 s to a first delta, 45 s mid-stream, both scaled
// by the role's patience (its ceiling over [lane.VisiblePatience]) and floored
// at twice its ceiling.
//
// THEY ARE RESTATED HERE AND THAT IS A KNOWN WEAKNESS OF THIS BENCH. `gosim`
// does not import `internal/provider` — it never has, and a bench that grew an
// import into a package it is judging would be a bench that changed its own
// subject — so these three figures are a copy, and a change to them that this
// file did not follow would make the baseline arm quietly wrong. REPORT.md says
// so under "where this model is wrong". The law that they clear the
// controller's ceiling lives in `internal/provider`, where it belongs.
const (
	transportFirstBound = 90 * time.Second
	transportGapBound   = 45 * time.Second
	transportHeadroom   = 2
)

// transportBound is how long the wire may be silent before the guard throws the
// whole attempt away: the first-delta bound while nothing has arrived, the
// mid-stream one after something has.
func transportBound(role lane.Role, wrote bool) time.Duration {
	patience := float64(role.Ceiling()) / float64(lane.VisiblePatience)
	gap := time.Duration(float64(transportGapBound) * patience)
	floor := time.Duration(transportHeadroom) * role.Ceiling()
	if floor > gap {
		floor = gap
	}
	if wrote {
		return max(gap, floor)
	}
	return max(time.Duration(float64(transportFirstBound)*patience), floor)
}

// ── THE TWO MIXES A ROW CAN BE RUN AT ───────────────────────────────────────
//
// §J stages its fault for the MIDDLE HALF of every row, which makes half of
// every arm's requests a staged fault. That is the right shape for an INVARIANT
// — a ceiling wants every trial it can get of the state it bounds — and it is
// the wrong shape for a COST that is a share of the bill, because a rescue of a
// genuinely broken request is charged to the same numerator as an arm nobody
// needed. On a mix that is half faults, most of the numerator is the mechanism
// working.
//
// So the rows are also run at a rate a person would recognise. Both mixes are
// kept and neither replaces the other: the stress mix is where the invariant
// and the false-hedge rate are measured, and the natural one is where a total
// bill means anything.
const (
	// mixStress is §J's own staging: the fault runs for the middle half of every
	// row, so half of every arm's requests are broken. It is a stress rig and it
	// is named one.
	mixStress = "stress"
	// mixNatural is one request in [naturalPeriod] staged as a fault —
	// [naturalRate] — scattered rather than contiguous, because isolated faults
	// are what a real afternoon has and a block of them is what a stress rig
	// has.
	mixNatural = "natural"

	// naturalPeriod is how many requests apart the natural mix's faults are. It
	// is a period rather than a die roll so that every seed and every row sees
	// the SAME number of faults and a cost comparison is not partly a comparison
	// of how many things broke.
	naturalPeriod = 20
)

// naturalRate is the natural mix's fault rate, as a share of one. It is derived
// from the period rather than written twice.
const naturalRate = 1.0 / float64(naturalPeriod)

// rateOfMix is what share of a mix's requests are staged faults.
func rateOfMix(mix string) float64 {
	if mix == mixNatural {
		return naturalRate
	}
	return 0.5
}

// sickAt says whether this request is the staged fault, for one mix.
//
// The stress mix breaks the MIDDLE HALF, exactly as the three scenarios above
// break their victim between n/4 and 3n/4, so the committed tables stay
// comparable. The natural mix breaks every [naturalPeriod]th request, offset so
// that no fault lands on request zero — the first request of a run is the one
// the store is coldest for, and staging the fault there would confound the two
// things this table separates.
func sickAt(mix string, index, total int) bool {
	if mix == mixNatural {
		return index > 0 && index%naturalPeriod == 0
	}
	return index >= total/4 && index < 3*total/4
}

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
	// armPrice is what the second request cost and armWon whether it was the
	// one that answered. THE TWO TOGETHER ARE WHAT MAKES AN OVERHEAD AVOIDABLE
	// OR NOT: an arm on a healthy request bought nothing whether it won or lost,
	// and an arm that RESCUED a stall bought the answer — what is avoidable
	// there is only an arm that then lost the race to the lane it was rescuing.
	armPrice float64
	armWon   bool
	// avoidableRetry is what [policyBaseline] threw away: a stream the transport
	// cut, billed in full and answering nothing. It is the baseline's own
	// avoidable overhead, and it is the figure the waiting arm's has to be read
	// against.
	avoidableRetry float64
	// took is how long the whole request took, in the world's seconds, so that a
	// bill can be read beside the wait it bought.
	took float64
	// thought says a run of thought really BEGAN on this trial with nothing
	// staged wrong in it. A phase that never started is not a phase, and
	// counting one would answer §K's fourth criterion with requests that were
	// acted on before the model had said anything at all.
	thought bool
	// sickThought is the same run of thought on a request whose fault is staged
	// INSIDE it: the stalled thinking phase, which is the one case this table
	// reports time-to-action for on its own.
	sickThought bool
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

	// AND THE LOSER SPEND SPLIT BY WHAT IT BOUGHT. §K's spend clause was
	// written to catch WASTE — money an arm cost on a request that was never in
	// trouble — and on a mix that is half staged faults it mostly bills RESCUES,
	// which is the mechanism doing its job. The two are one subtraction apart
	// and reporting only their sum makes them impossible to tell apart, which is
	// how a correct rescue came to look like a defect.
	WasteWell float64 `json:"usd_waste_on_healthy"`
	WasteSick float64 `json:"usd_waste_on_faults"`
	WastePct  float64 `json:"waste_overhead_pct"`
	RescuePct float64 `json:"rescue_overhead_pct"`

	// ── AVOIDABLE, WHICH IS THE ONLY HALF A CONTROLLER CAN BE GRADED ON ─────
	//
	// Of the money a second request costs, some of it could not have been
	// spent differently by ANY policy and some of it could. An arm on a request
	// that was never in trouble bought nothing, won or lost. An arm that
	// rescued a genuine stall bought the answer — the first attempt was
	// unavoidable, because nothing knew it would stall, and the second was
	// necessary, because it is what answered. What is left over is an arm that
	// went out on a fault and then LOST to the lane it was rescuing: the stall
	// ended on its own and the money is gone.
	//
	// So Avoidable is every healthy arm plus every rescue arm that lost, and it
	// is the figure a gate can hold a controller to. The rest is the arithmetic
	// floor under any build that rescues stalls at all, which the baseline arm
	// measures rather than argues.
	Avoidable    float64 `json:"usd_avoidable"`
	AvoidablePct float64 `json:"avoidable_overhead_pct"`
	ArmsLost     int     `json:"arms_on_a_fault_that_lost"`

	// Bill is what one request cost on average and TookP50/TookP90 how long one
	// took, in the world's seconds. They are what a baseline is compared on:
	// a policy is worth having if it answers sooner without paying more.
	Bill    float64 `json:"usd_per_request"`
	TookP50 float64 `json:"took_p50_s"`
	TookP90 float64 `json:"took_p90_s"`
	// AND THE WAIT ON THE REQUESTS THAT WENT WRONG, which is the only place two
	// waiting policies differ at all. At one fault in twenty a whole-arm p90 is
	// a healthy request by construction, so a comparison read off it would be a
	// comparison of two identical halves.
	TookSickP50 float64 `json:"took_on_a_fault_p50_s"`
	TookSickP90 float64 `json:"took_on_a_fault_p90_s"`
	RescueArm   int     `json:"arms_on_a_staged_fault"`
	StallThinks int     `json:"stalled_thinking_phases"`
	// StallP50 and StallP90 are time-to-action for the one case a person
	// reported: a run of thought that stopped. They are the fault window of the
	// thinking row and nothing else, kept separately because the row's own
	// percentiles pool them with the requests that never reached a thought.
	//
	// THEY ARE POINTERS BECAUSE FOUR ROWS OF FIVE HAVE NO SUCH CASE AT ALL, and
	// a percentile of nothing is not zero. Zero would read as "acted instantly",
	// which is the opposite of "never happened"; nil is `null` in the file and
	// nothing in the table, which is what [share] does with an empty denominator
	// and for the same reason.
	StallP50 *float64 `json:"stalled_think_action_p50_s"`
	StallP90 *float64 `json:"stalled_think_action_p90_s"`

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
	var action, silence, late, stalled, took, tookSick []float64
	for _, one := range got {
		out.USD += one.usd
		out.Waste += one.waste
		if one.sick {
			out.WasteSick += one.waste
		} else {
			out.WasteWell += one.waste
		}
		out.Answered += boolCount(one.answered)
		out.Sick += boolCount(one.sick)
		out.Well += boolCount(!one.sick)
		out.Thinks += boolCount(one.thought)
		out.ThinkKept += boolCount(one.thought && one.survived)
		out.Arms += boolCount(one.armed)
		out.FalseArm += boolCount(one.armed && !one.sick)
		out.RescueArm += boolCount(one.armed && one.sick)
		out.ArmsLost += boolCount(one.armed && one.sick && !one.armWon)
		if one.armed && (!one.sick || !one.armWon) {
			out.Avoidable += one.armPrice
		}
		out.Avoidable += one.avoidableRetry
		if one.took > 0 {
			took = append(took, one.took)
			if one.sick {
				tookSick = append(tookSick, one.took)
			}
		}
		// THE ONE CASE A PERSON COMPLAINED ABOUT, counted on its own: a run of
		// thought that really began and then stopped. `thought` is only set when
		// the model reached its reasoning at all, so a fault that struck before
		// the thinking started is not one of these.
		if one.sickThought && c.reasoning > 0 {
			out.StallThinks++
			if one.acted {
				stalled = append(stalled, one.action)
			}
		}
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
	// A PERCENTILE OF NOTHING IS NOT A NUMBER, and [pct] says so with a NaN. An
	// arm with no controller raises no acts at all, so these three samples are
	// empty by construction there — and a NaN in a struct is a file that will
	// not marshal, which is how the last run printed its tables and wrote no
	// artifact. They are left at zero, which is unambiguous beside the act count
	// standing next to them in the same row, and the table prints a dash rather
	// than a figure when there was nothing to take a percentile of.
	if len(action) > 0 {
		out.ActionP50, out.ActionP90, out.ActionMax = pct(action, 0.50), pct(action, 0.90), pct(action, 1.0)
	}
	if len(silence) > 0 {
		out.SilenceP50, out.SilenceP90, out.SilenceMax = pct(silence, 0.50), pct(silence, 0.90), pct(silence, 1.0)
	}
	if len(late) > 0 {
		out.LateMedian, out.LateMax = pct(late, 0.50), pct(late, 1.0)
	}
	if len(took) > 0 {
		out.TookP50, out.TookP90 = pct(took, 0.50), pct(took, 0.90)
	}
	if len(tookSick) > 0 {
		out.TookSickP50, out.TookSickP90 = pct(tookSick, 0.50), pct(tookSick, 0.90)
	}
	if len(stalled) > 0 {
		fifty, ninety := pct(stalled, 0.50), pct(stalled, 0.90)
		out.StallP50, out.StallP90 = &fifty, &ninety
	}
	out.FalseHedgePct = share(out.FalseArm, out.Well)
	out.ReportPct = share(out.Kinds[kindWords[control.Report]], out.Acts)
	out.ThinkKeptPct = share(out.ThinkKept, out.Thinks)
	if out.USD > 0 {
		out.SpendPct = 100 * out.Waste / out.USD
		out.WastePct = 100 * out.WasteWell / out.USD
		out.RescuePct = 100 * out.WasteSick / out.USD
		out.AvoidablePct = 100 * out.Avoidable / out.USD
	}
	if out.N > 0 {
		out.Bill = out.USD / float64(out.N)
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
func proveIt(w *world, seeds []int, n, speedup int, trace bool, paces, stores, mixes []string, jsonOut string, began time.Time) {
	fmt.Printf("script:   the lane that is about to serve goes quiet for %v on the middle half "+
		"of every case's requests\n", quietFor)
	fmt.Printf("stores:   %v; a warmed one has watched %d answers of every pair over the %v "+
		"before the first request, folded through lane.Ledger.Note\n",
		stores, warmSightings, warmOver)
	fmt.Printf("mixes:    %v; %s stages a fault on %.0f%% of requests and %s on %.0f%% "+
		"(one in %d, scattered)\n",
		mixes, mixStress, 100*rateOfMix(mixStress), mixNatural, 100*rateOfMix(mixNatural), naturalPeriod)
	fmt.Println()
	arms := make([]proofArm, 0, len(paces)*len(stores)*len(mixes))
	for _, mix := range mixes {
		for _, store := range stores {
			if !staged(store, mix) {
				continue
			}
			for _, pace := range paces {
				rows := runProof(w, seeds, n, speedup, trace, pace, store, mix, policyWaiting)
				arms = append(arms, proofArm{Pace: pace, Store: store, Mix: mix,
					Policy: policyWaiting, FaultPct: 100 * rateOfMix(mix), Rows: rows,
					Criteria: proofGates(rows, store, mix, policyWaiting)})
			}
			// AND THE FLOOR, ON THE ARM THE DECISION RESTS ON. The baseline is
			// run where the policy arm is graded — a warmed store at the natural
			// rate — because that is the only place the two are answering the
			// same question. Running it beside every arm would quadruple the
			// wall for three tables nothing reads.
			if store == storeWarmed && mix == mixNatural {
				rows := runProof(w, seeds, n, speedup, trace, paceShipped, store, mix, policyBaseline)
				arms = append(arms, proofArm{Pace: paceShipped, Store: store, Mix: mix,
					Policy: policyBaseline, FaultPct: 100 * rateOfMix(mix), Rows: rows,
					Criteria: proofGates(rows, store, mix, policyBaseline)})
			}
		}
	}
	rows, gates := arms[0].Rows, arms[0].Criteria
	for _, arm := range arms {
		if arm.Policy == policyBaseline {
			fmt.Printf("── a %s store at a %s mix, with NO WAITING POLICY AT ALL ───────────────────\n\n",
				arm.Store, arm.Mix)
		} else {
			fmt.Printf("── a %s store at a %s mix, and the plan waits against the %s belief ─────────\n\n",
				arm.Store, arm.Mix, arm.Pace)
		}
		printProof(arm.Rows, arm.Criteria, seeds, n, speedup, arm.Store, arm.Mix, arm.Policy)
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
		"mixes": mixes, "natural_fault_rate": naturalRate,
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

// mixesFrom is which fault rates the rows are run at: both, unless the caller
// named one.
func mixesFrom(named string) []string {
	switch named {
	case "":
		return []string{mixStress, mixNatural}
	case mixStress, mixNatural:
		return []string{named}
	}
	log.Fatalf("gosim: no mix called %q; it is %q or %q", named, mixStress, mixNatural)
	return nil
}

// staged says whether a store is worth running at a mix.
//
// THE COLD STORE IS A STRESS RIG AND IS RUN AS ONE. It is the staging §J wrote
// for the invariant, and its cost figures are reported rather than gated
// precisely because a store with nothing in it has nowhere to hedge to — see
// REPORT.md. Running it at the natural rate would add a quarter of an hour to
// every proof to produce two more reported numbers nothing reads.
func staged(store, mix string) bool { return store != storeCold || mix == mixStress }

// proofArm is one whole table: the rows and the four criteria, for one of the
// two doors a plan can be built against, in one of the stores it can be staged
// in.
type proofArm struct {
	Pace     string      `json:"pace"`
	Store    string      `json:"store"`
	Mix      string      `json:"mix"`
	Policy   string      `json:"policy"`
	FaultPct float64     `json:"staged_fault_pct"`
	Rows     []proofRow  `json:"rows"`
	Criteria []proofGate `json:"criteria"`
}

// bill is what one request cost on average across a whole arm, and waited is
// how long one took at the median and the ninetieth. They are what the baseline
// is read against and they are computed here so that the comparison is one
// function rather than a line in a report nobody can re-derive.
func (a proofArm) bill() float64 {
	usd, n := 0.0, 0
	for _, r := range a.Rows {
		usd += r.USD
		n += r.N
	}
	if n == 0 {
		return 0
	}
	return usd / float64(n)
}

func (a proofArm) waited() (p50, p90 float64) {
	var all []float64
	for _, r := range a.Rows {
		all = append(all, r.TookP50, r.TookP90)
	}
	if len(all) == 0 {
		return 0, 0
	}
	fifty, ninety := make([]float64, 0, len(a.Rows)), make([]float64, 0, len(a.Rows))
	for _, r := range a.Rows {
		fifty = append(fifty, r.TookP50)
		ninety = append(ninety, r.TookP90)
	}
	return pct(fifty, 0.50), pct(ninety, 0.90)
}

// runProof is the whole of §K: every case, every seed, pooled.
func runProof(w *world, seeds []int, n, speedup int, trace bool, pace, store, mix, policy string) []proofRow {
	scen, ok := scenarioNamed(proofScenario)
	if !ok {
		log.Fatalf("gosim: no scenario called %q to run the proof rows in", proofScenario)
	}
	rows := make([]proofRow, 0, len(proofCases))
	for _, c := range proofCases {
		var all []trial
		for _, seed := range seeds {
			all = append(all, runProofSeed(w, scen, c, seed, n, speedup, trace, pace, store, mix, policy)...)
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
func runProofSeed(w *world, s scenario, c proofCase, seed, n, speedup int, trace bool, pace, store, mix, policy string) []trial {
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
		speedup: speedup, total: n, at: theMoment, trace: trace, pace: pace, mix: mix,
		policy: policy,
	}
	out := make([]trial, 0, n)
	for index := 0; index < n; index++ {
		one := p.prove
		if policy == policyBaseline {
			one = p.serial
		}
		got, err := one(index)
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
	// mix is how often a request is a staged fault: [mixStress] or [mixNatural].
	mix string
	// policy is whether this request is watched at all: [policyWaiting] or
	// [policyBaseline].
	policy string

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
	sick := sickAt(p.mix, index, p.total)
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
	out.took = p.worldly(time.Since(p.wire)).Seconds()
	p.close(index, served, out)
	return out, nil
}

// serial is one request under [policyBaseline]: no controller, no deadline, no
// arms — the transport's own guard, and a re-send when it fires.
//
// THE FAULT BELONGS TO THE LANE THAT WAS CHOSEN, not to the attempt. A retry
// goes somewhere else and that somewhere else is healthy, which is the whole
// reason a retry is ever worth making; staging the fault again on every attempt
// would be measuring a bad afternoon rather than a bad lane.
func (p *prover) serial(index int) (trial, error) {
	sick := sickAt(p.mix, index, p.total)
	req := p.scen.request(p.world.model, p.at, p.total-index)
	choice := p.choose(req)
	victim := p.headFor(choice)
	out := trial{sick: sick}
	p.wire = time.Now()
	served, tried := victim, map[string]bool{}
	for attempt := 0; attempt < serialAttempts; attempt++ {
		tried[served] = true
		p.stub.Model(p.world.model, p.stage(
			p.world.shots(p.seed, "proof|"+p.kase.name, index, attempt), victim, sick)...)
		out.usd += p.priceOf(served)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		one := p.open(ctx, choice.Order, p.only(choice, served, attempt))
		cut := p.readOut(one, &out)
		one.cancel()
		cancel()
		if !cut {
			break
		}
		// A CUT STREAM IS THROWN AWAY WHOLE. The prompt was read and billed and
		// none of it became an answer, which is the difference between a retry
		// and a hedge: a hedge keeps whichever stream speaks first.
		out.waste += p.priceOf(served)
		out.avoidableRetry += p.priceOf(served)
		next := p.nextLane(tried)
		if next == "" {
			break
		}
		served = next
	}
	out.took = p.worldly(time.Since(p.wire)).Seconds()
	p.close(index, served, out)
	return out, nil
}

// only is what a serial attempt demands. The first goes out exactly as the
// waiting arm's does — the router's own order, so the two arms route
// identically — and a retry names the one lane it has moved to.
func (p *prover) only(choice lane.Choice, served string, attempt int) []string {
	if attempt == 0 {
		return choice.Only
	}
	return []string{served}
}

// nextLane is where a retry goes: the next lane past this scenario's gate that
// has not been tried.
func (p *prover) nextLane(tried map[string]bool) string {
	for _, index := range p.world.gated(p.scen) {
		if name := p.world.lanes[index].name; !tried[name] {
			return name
		}
	}
	return ""
}

// readOut reads one stream to its end under the TRANSPORT's guard alone, and
// says whether the guard cut it. It is the whole of [policyBaseline]'s waiting:
// there is no controller, no deadline and nothing to act on before the bound.
func (p *prover) readOut(one *armed, out *trial) bool {
	last, wrote := time.Now(), false
	for {
		left := p.scaled(transportBound(proofRole, wrote)) - time.Since(last)
		if left <= 0 {
			return true
		}
		guard := time.NewTimer(left)
		select {
		case seen := <-one.sights:
			guard.Stop()
			p.sawToken(out, seen)
			if seen.reading.Visible > 0 || seen.reading.Hidden > 0 {
				last, wrote = time.Now(), true
			}
		case <-one.done:
			guard.Stop()
			return false
		case <-guard.C:
			return true
		}
	}
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
			if seen.reading.Hidden > 0 {
				// A RUN OF THOUGHT REALLY BEGAN. On a healthy request that is
				// §K's fourth criterion's denominator; on a staged one it is the
				// case a person complained about — a thought that started and
				// then stopped — and the two are counted apart because one is a
				// phase that must be left alone and the other a phase that must
				// be acted on.
				if out.sick {
					out.sickThought = true
				} else {
					out.thought = true
				}
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
	out.armed, out.armPrice = true, price
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
		out.armWon = true
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
func proofGates(rows []proofRow, store, mix, policy string) []proofGate {
	var cold proofRow
	arms, well, thinks, kept := 0, 0, 0, 0
	usd, waste, onWell, onSick, avoid := 0.0, 0.0, 0.0, 0.0, 0.0
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
		onWell += r.WasteWell
		onSick += r.WasteSick
		avoid += r.Avoidable
	}
	falsePct, thinkPct := share(arms, well), share(kept, thinks)
	spendPct, wastePct, rescuePct, avoidPct := 0.0, 0.0, 0.0, 0.0
	if usd > 0 {
		spendPct = 100 * waste / usd
		wastePct = 100 * onWell / usd
		rescuePct = 100 * onSick / usd
		avoidPct = 100 * avoid / usd
	}
	inside := share(cold.SickActs-cold.OverCeil, cold.SickActs)
	steady, natural := store != storeCold, mix == mixNatural
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
		// SPEND, SPLIT BY WHAT IT BOUGHT, AND WHICH HALF DECIDES DEPENDS ON THE
		// MIX. §K's clause was written to catch WASTE — an arm on a request that
		// was never in trouble. On the stress mix, where half of everything is
		// broken by construction, the total is mostly RESCUES and the clause
		// cannot tell them apart, so the waste half is what is gated there and
		// the rescue half is reported under the purse. On the natural mix the
		// question is the one the clause was written to ask — what does this
		// mechanism add to a real bill — so the TOTAL is gated.
		// AVOIDABLE IS THE ONLY HALF A CONTROLLER IS GRADED ON, and it is gated
		// on the mix where a bill means something. §K's flat clause on the TOTAL
		// is withdrawn on the natural mix as infeasible by arithmetic — see
		// [gateAvoidablePct] and the baseline arm — and reported everywhere.
		{Criterion: "spend overhead, avoidable",
			Threshold: fmt.Sprintf("<= %.1f%% of the arm's own bill", gateAvoidablePct),
			Measured:  fmt.Sprintf("%.2f%% of $%.4f", avoidPct, usd),
			Where:     "healthy arms, and rescues that lost",
			Value:     avoidPct, Gated: steady && natural, Pass: avoidPct <= gateAvoidablePct},
		{Criterion: "spend overhead, total",
			Threshold: "reported; a rescue is a whole second request",
			Measured:  fmt.Sprintf("%.2f%% of $%.4f", spendPct, usd),
			Where:     "every case",
			Value:     spendPct, Gated: false},
		{Criterion: "spend overhead, waste on a healthy lane",
			Threshold: "<= 3% of the arm's own bill",
			Measured:  fmt.Sprintf("%.2f%% of $%.4f", wastePct, usd),
			Where:     "every case, healthy half",
			Value:     wastePct, Gated: steady && !natural, Pass: wastePct <= gateSpendOverheadPct},
		{Criterion: "spend overhead, rescues of staged faults",
			Threshold: "reported; the purse is what bounds it",
			Measured:  fmt.Sprintf("%.2f%% of $%.4f", rescuePct, usd),
			Where:     "every case, fault window",
			Value:     rescuePct, Gated: false},
	}
	// A BASELINE IS NOT GRADED. It has no controller, so every criterion above
	// is about a mechanism it does not have; what it is for is the two numbers
	// at the foot of its table, which is where the comparison lives.
	if policy == policyBaseline {
		for at := range out {
			out[at].Gated = false
		}
		return out
	}
	if !steady || !natural {
		// THE PURSE IS WHAT BOUNDS SPENDING NOTHING ELSE BOUNDS, and it is what
		// grades the bill wherever §K's own clause is not deciding it: on a cold
		// store, where the arms are exploration, and on the stress mix, where
		// the total is mostly correct rescues. The ceiling is read off the
		// shipped budget rather than restated here, because a figure written
		// down twice is a figure that will drift from the one the build
		// enforces.
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
func printProof(rows []proofRow, gates []proofGate, seeds []int, n, speedup int, store, mix, policy string) {
	fmt.Println("── §K, the four scenarios ──────────────────────────────────────────────────────")
	fmt.Printf("   role %s, ceiling %s   seeds %v   requests per case per seed %d   pooled %d   wire %d× the world\n",
		proofRole, proofRole.Ceiling(), seeds, n, n*len(seeds), speedup)
	fmt.Printf("   store %s   %s\n", store, storyOf(store))
	fmt.Printf("   mix %s   a fault is staged on %.0f%% of requests\n", mix, 100*rateOfMix(mix))
	if mix == mixNatural {
		fmt.Printf("   one request in %d of every case is the staged fault; the rest are healthy\n",
			naturalPeriod)
	} else {
		fmt.Println("   the fault runs for the middle half of every case; the other half is the healthy one")
	}
	fmt.Println()
	fmt.Printf("   %-24s%6s%6s%6s%6s%7s%7s%10s%10s%10s%7s%8s%8s%9s%9s\n",
		"case", "n", "sick", "acts", "arms", "well", "resc", "act p50", "act p90", "act max",
		"over", "false%", "$over%", "$waste%", "$resc%")
	fmt.Printf("   %s\n", strings.Repeat("-", 136))
	for _, r := range rows {
		act := fmt.Sprintf("%9.2fs%9.2fs%9.2fs", r.ActionP50, r.ActionP90, r.ActionMax)
		if r.SickActs == 0 {
			act = fmt.Sprintf("%10s%10s%10s", "—", "—", "—")
		}
		fmt.Printf("   %-24s%6d%6d%6d%6d%7d%7d%s%7d%8.2f%8.2f%9.2f%9.2f\n",
			r.Case, r.N, r.Sick, r.SickActs, r.Arms, r.FalseArm, r.RescueArm, act,
			r.OverCeil, r.FalseHedgePct, r.SpendPct, r.WastePct, r.RescuePct)
	}
	fmt.Println()
	for _, r := range rows {
		fmt.Printf("     %-24s %s\n", r.Case, r.Why)
		fmt.Printf("     %-24s acts: %s   clocks: %s\n", "", kindTally(r.Kinds), whyTally(r.Whys))
		alarm := fmt.Sprintf("alarm late %.0f/%.0f ms median/max", r.LateMedian, r.LateMax)
		if r.SickActs == 0 {
			alarm = "no alarm was ever armed"
		}
		fmt.Printf("     %-24s over the ceiling: %d since sent, %d of silence   %s"+
			"   a word arrived on %d/%d\n",
			"", r.OverCeil, r.OverSil, alarm, r.Answered, r.N)
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
		if r.SickActs == 0 {
			fmt.Printf("   %-24s%14s%14s%14s%12s\n", r.Case, "—", "—", "—", "—")
			continue
		}
		fmt.Printf("   %-24s%13.2fs%13.2fs%13.2fs%12.1f\n",
			r.Case, r.SilenceP50, r.SilenceP90, r.SilenceMax, r.ReportPct)
	}
	fmt.Println()
	// AND THE ONE CASE A PERSON REPORTED, ON ITS OWN LINE. A run of thought that
	// stopped is pooled into the thinking row's percentiles with every request
	// that was acted on before the model had said anything, and the two are not
	// the same wait. How often it happens at this mix is on the line with it,
	// because a time-to-action nobody can weigh is a number nobody can rule on.
	// THE BILL AND THE WAIT, ON EVERY TABLE, so that a policy arm and the
	// baseline can be read against each other without a spreadsheet.
	usd, n := 0.0, 0
	fifty, ninety := make([]float64, 0, len(rows)), make([]float64, 0, len(rows))
	for _, r := range rows {
		usd += r.USD
		n += r.N
		fifty = append(fifty, r.TookP50)
		ninety = append(ninety, r.TookP90)
	}
	sick50, sick90 := make([]float64, 0, len(rows)), make([]float64, 0, len(rows))
	for _, r := range rows {
		sick50 = append(sick50, r.TookSickP50)
		sick90 = append(sick90, r.TookSickP90)
	}
	if n > 0 {
		fmt.Printf("   the bill: $%.6f a request over %d requests   every request: %.2fs p50 / "+
			"%.2fs p90   ON A FAULT: %.2fs p50 / %.2fs p90   [%s]\n",
			usd/float64(n), n, pct(fifty, 0.50), pct(ninety, 0.90),
			pct(sick50, 0.50), pct(sick90, 0.90), policy)
	}
	for _, r := range rows {
		if r.StallThinks == 0 || r.StallP50 == nil {
			continue
		}
		fmt.Printf("   a stalled run of thought: %d of %d requests in its own row (%.2f%%), "+
			"%.2f%% of every request in this arm, acted on at %.2fs p50 / %.2fs p90   [%s]\n",
			r.StallThinks, r.N, share(r.StallThinks, r.N),
			share(r.StallThinks, r.N*len(rows)), *r.StallP50, *r.StallP90, r.Case)
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
