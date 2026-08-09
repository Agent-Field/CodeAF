// Context policy — port of src/session/context-policy.ts.
//
// Detects context rot from cheap objective counters and decides when a retry
// should abandon the polluted trajectory for a FRESH context seeded with a
// distilled brief.
//
// The TS module is pure by construction: no clock, no RNG, no sorting. The one
// impure edge is buildDistilledBrief's `workspace` option, which calls
// blocker-ledger's synchronous loadOpenBlockers — that stays impure here too
// (ledgers.LoadOpenBlockers), and is exercised through a temp dir in the
// fixtures. Consequently there is no injectable clock/RNG in this package.
//
// Fidelity notes (deliberate, do not "fix"):
//
//   - Every number that reaches a reason string goes through
//     jscompat.FormatNumber, because these are JS template literals over JS
//     numbers. The degrade line is computed as `DEGRADE_FRACTION * budget` and
//     printed raw, so it prints 8.399999999999999 for band s and
//     33.599999999999994 for band xl. Precomputing or rounding those would
//     break byte parity.
//
//   - assessContext compares with JS `>` / `>=`. A NaN counter therefore fails
//     every comparison and falls through to "healthy" with "NaN" embedded in
//     the reason. Go's float64 comparisons have the same NaN semantics, so the
//     port is a literal transcription.
//
//   - turnBudgetFor on an off-union band returns `undefined` in TS. Go returns
//     NaN instead (see TurnBudgetFor). That keeps assessContext byte-identical
//     — every comparison against NaN is false, exactly as against undefined,
//     and the only branch that can print the value is the healthy one, where
//     TS prints `0.7 * undefined` = "NaN" too. The direct return value of
//     TurnBudgetFor is the only observable difference.
//
//   - ASSESSMENT_COUNTER_RX is hand-expanded: JS `\s` is a much larger class
//     than RE2 `\s` (it adds \v and the Unicode space separators plus U+FEFF),
//     and the JS `i` flag in non-unicode mode does ASCII-only canonicalization
//     while Go's `(?i)` does full Unicode simple folding (so `(?i)s` would
//     match U+017F 'ſ', which /s/i does not). Both are spelled out literally
//     below rather than relying on the shorthands.
//
//   - `input.openBlockers ?? ...` is NULLISH, not truthy: an explicitly-passed
//     EMPTY openBlockers array suppresses the workspace load. Go models that
//     with nil-vs-empty-slice, which is the one place in this package where
//     `[]T(nil)` and `[]T{}` are not interchangeable.
package contextpolicy

import (
	"math"
	"regexp"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/ledgers"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/sizeband"
)

// ---------------------------------------------------------------------------
// Per-band turn budgets. W5-TODO(knobs) on every entry in the TS original.

// TurnBudget mirrors `TURN_BUDGET: Readonly<Record<SizeBand, number>>`. It is
// an OrderedMap rather than a Go map so that any consumer that iterates it sees
// the TS object-literal key order (xs, s, m, l, xl) — a bare map would expose a
// randomized order. Like the TS `Readonly<>`, the readonly-ness is advisory:
// the underlying value is mutable at runtime in both languages.
var TurnBudget = func() *jscompat.OrderedMap[sizeband.SizeBand, float64] {
	m := jscompat.NewOrderedMap[sizeband.SizeBand, float64]()
	m.Set(sizeband.BandXS, 8)
	m.Set(sizeband.BandS, 12)
	m.Set(sizeband.BandM, 20)
	m.Set(sizeband.BandL, 32)
	m.Set(sizeband.BandXL, 48)
	return m
}()

// DegradeFraction mirrors DEGRADE_FRACTION: the fraction of the turn budget
// past which the context counts as degrading.
const DegradeFraction float64 = 0.7

// DegradeToolErrors mirrors DEGRADE_TOOL_ERRORS.
const DegradeToolErrors float64 = 3

// PollutedToolErrors mirrors POLLUTED_TOOL_ERRORS.
const PollutedToolErrors float64 = 6

// PollutedRepairRounds mirrors POLLUTED_REPAIR_ROUNDS.
const PollutedRepairRounds float64 = 2

// MaxFreshRetries mirrors MAX_FRESH_RETRIES: the FLAT cap for a run that is not
// making progress (attempt is 0-based).
const MaxFreshRetries float64 = 2

// MaxFreshRetriesHardCeiling mirrors MAX_FRESH_RETRIES_HARD_CEILING: the
// absolute ceiling on fresh-context relays.
const MaxFreshRetriesHardCeiling float64 = 6

// TurnBudgetFor mirrors turnBudgetFor. TS indexes a plain object, so an
// off-union band yields `undefined`; Go has no undefined in a float64 return,
// so this yields NaN — the value that behaves identically under every
// comparison and every arithmetic operation the module performs on it.
func TurnBudgetFor(band sizeband.SizeBand) float64 {
	v, ok := TurnBudget.Get(band)
	if !ok {
		return math.NaN()
	}
	return v
}

// ---------------------------------------------------------------------------
// assessContext

// Verdict is the "healthy" | "degrading" | "polluted" union on
// ContextAssessment. It is a plain string type, so a caller can hand RetryMode
// an off-union verdict exactly as untyped JS can.
type Verdict string

const (
	VerdictHealthy   Verdict = "healthy"
	VerdictDegrading Verdict = "degrading"
	VerdictPolluted  Verdict = "polluted"
)

// ContextAssessment mirrors the ContextAssessment interface. Field order is the
// TS declaration order, which is what JSON.stringify emits.
type ContextAssessment struct {
	Verdict Verdict `json:"verdict"`
	Reason  string  `json:"reason"`
}

// AssessContextInput mirrors the inline parameter object of assessContext.
// turns/toolErrors/repairRounds are JS numbers, hence float64: they may
// legitimately be fractional, NaN or ±Inf.
type AssessContextInput struct {
	Band         sizeband.SizeBand `json:"band"`
	Turns        float64           `json:"turns"`
	ToolErrors   float64           `json:"toolErrors"`
	RepairRounds float64           `json:"repairRounds"`
}

// AssessContext mirrors assessContext: classify trajectory health from cheap,
// objective counters. Pollution dominates degradation; within each tier the
// reason names the specific tripped signal.
func AssessContext(input AssessContextInput) ContextAssessment {
	band, turns, toolErrors, repairRounds := input.Band, input.Turns, input.ToolErrors, input.RepairRounds
	budget := TurnBudgetFor(band)

	// Polluted tier.
	if turns > budget {
		return ContextAssessment{
			Verdict: VerdictPolluted,
			Reason: "turns " + jscompat.FormatNumber(turns) +
				" > budget " + jscompat.FormatNumber(budget) +
				" for band " + string(band),
		}
	}
	if toolErrors >= PollutedToolErrors {
		return ContextAssessment{
			Verdict: VerdictPolluted,
			Reason: "toolErrors " + jscompat.FormatNumber(toolErrors) +
				" >= " + jscompat.FormatNumber(PollutedToolErrors),
		}
	}
	if repairRounds >= PollutedRepairRounds {
		return ContextAssessment{
			Verdict: VerdictPolluted,
			Reason: "repairRounds " + jscompat.FormatNumber(repairRounds) +
				" >= " + jscompat.FormatNumber(PollutedRepairRounds),
		}
	}

	// Degrading tier — the early warning.
	degradeTurns := DegradeFraction * budget
	if turns > degradeTurns {
		return ContextAssessment{
			Verdict: VerdictDegrading,
			Reason: "turns " + jscompat.FormatNumber(turns) +
				" > " + jscompat.FormatNumber(DegradeFraction) +
				" * budget (" + jscompat.FormatNumber(degradeTurns) +
				") for band " + string(band),
		}
	}
	if toolErrors >= DegradeToolErrors {
		return ContextAssessment{
			Verdict: VerdictDegrading,
			Reason: "toolErrors " + jscompat.FormatNumber(toolErrors) +
				" >= " + jscompat.FormatNumber(DegradeToolErrors),
		}
	}

	return ContextAssessment{
		Verdict: VerdictHealthy,
		Reason: "turns " + jscompat.FormatNumber(turns) +
			" <= " + jscompat.FormatNumber(degradeTurns) +
			", toolErrors " + jscompat.FormatNumber(toolErrors) +
			" < " + jscompat.FormatNumber(DegradeToolErrors) +
			", repairRounds " + jscompat.FormatNumber(repairRounds) +
			" < " + jscompat.FormatNumber(PollutedRepairRounds),
	}
}

// ---------------------------------------------------------------------------
// retryMode

// Mode is the "continue" | "fresh-context" | "give-up" union on RetryDecision.
// (The TS name for the union is inline; the Go type cannot be called RetryMode
// because that name belongs to the function.)
type Mode string

const (
	ModeContinue     Mode = "continue"
	ModeFreshContext Mode = "fresh-context"
	ModeGiveUp       Mode = "give-up"
)

// RetryDecision mirrors the RetryDecision interface, in TS declaration order.
type RetryDecision struct {
	Mode         Mode   `json:"mode"`
	DistillBrief bool   `json:"distillBrief"`
	Reason       string `json:"reason"`
}

// RetrySignal mirrors the RetrySignal interface. Every field is optional in TS,
// so every field is a pointer here: nil is `undefined`, and the distinction is
// load-bearing (a partial prevBlockers-only signal is NOT converging).
type RetrySignal struct {
	PrevBlockers      *float64 `json:"prevBlockers"`
	CurrBlockers      *float64 `json:"currBlockers"`
	HardMode          *bool    `json:"hardMode"`
	ObjectiveProgress *bool    `json:"objectiveProgress"`
}

// RetryMode mirrors retryMode. `signal` is variadic to model the TS default
// parameter `signal: RetrySignal = {}` — omitting it is the default path, and
// the zero-value RetrySignal is exactly `{}`. Extra elements past the first are
// ignored, matching JS's extra-argument behaviour.
func RetryMode(assessment ContextAssessment, attempt float64, signal ...RetrySignal) RetryDecision {
	var s RetrySignal
	if len(signal) > 0 {
		s = signal[0]
	}

	// The relay budget is elastic ONLY on genuine progress (a STRICT decrease in
	// blocker count over the last cycle, or a caller-supplied whole-run
	// convergence signal) or in hard mode; the absolute ceiling still bounds it.
	converging := (s.ObjectiveProgress != nil && *s.ObjectiveProgress) ||
		(s.PrevBlockers != nil &&
			s.CurrBlockers != nil &&
			*s.CurrBlockers < *s.PrevBlockers)
	cap := MaxFreshRetries
	// TS: `converging || signal.hardMode ? ... : ...` — hardMode is a TRUTHY
	// test, so undefined and false both fall through to the flat cap.
	if converging || (s.HardMode != nil && *s.HardMode) {
		cap = MaxFreshRetriesHardCeiling
	}
	if attempt >= cap {
		// `${signal.hardMode ?? false}` — nullish, so undefined prints "false".
		hardMode := false
		if s.HardMode != nil {
			hardMode = *s.HardMode
		}
		return RetryDecision{
			Mode:         ModeGiveUp,
			DistillBrief: false,
			Reason: "attempt " + jscompat.FormatNumber(attempt) +
				" >= cap " + jscompat.FormatNumber(cap) +
				" (converging=" + boolString(converging) +
				", hardMode=" + boolString(hardMode) +
				") — retries exhausted, escalate",
		}
	}
	if assessment.Verdict == VerdictHealthy {
		return RetryDecision{
			Mode:         ModeContinue,
			DistillBrief: false,
			Reason: "context healthy (" + assessment.Reason +
				"), attempt " + jscompat.FormatNumber(attempt) +
				" — keep going in place",
		}
	}
	return RetryDecision{
		Mode:         ModeFreshContext,
		DistillBrief: true,
		Reason: "context " + string(assessment.Verdict) +
			" (" + assessment.Reason +
			"), attempt " + jscompat.FormatNumber(attempt) +
			" < cap " + jscompat.FormatNumber(cap) +
			" — restart with distilled brief",
	}
}

// boolString is `${b}` for a JS boolean.
func boolString(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// ---------------------------------------------------------------------------
// buildDistilledBrief

// jsSpaceClass is the JS RegExp `\s` class spelled out: WhiteSpace ∪
// LineTerminator. RE2's `\s` is only [\t\n\f\r ], so the shorthand would miss
// \v and every Unicode space separator plus U+FEFF.
const jsSpaceClass = `[\t\n\v\f\r \x{00a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}]`

// assessmentCounterRx mirrors
//
//	/\b(?:turns|toolErrors|repairRounds)\b\s+\d+\s*(?:>=|<=|>|<|=)/i
//
// The `i` flag is expanded into explicit ASCII case classes instead of `(?i)`:
// JS canonicalization in non-unicode mode is ASCII-only (U+017F 'ſ' does NOT
// match /s/i), while Go's `(?i)` applies Unicode simple folding. `\b` and `\d`
// are ASCII in both engines and carry over unchanged. Only `.MatchString` is
// used, so RE2's leftmost-first alternation vs. JS backtracking cannot differ:
// both answer the same "does a match exist" question.
var assessmentCounterRx = regexp.MustCompile(
	`\b(?:[Tt][Uu][Rr][Nn][Ss]` +
		`|[Tt][Oo][Oo][Ll][Ee][Rr][Rr][Oo][Rr][Ss]` +
		`|[Rr][Ee][Pp][Aa][Ii][Rr][Rr][Oo][Uu][Nn][Dd][Ss])\b` +
		jsSpaceClass + `+[0-9]+` + jsSpaceClass + `*(?:>=|<=|>|<|=)`)

// rawTailCap mirrors the function-local `const RAW_TAIL_CAP = 3`.
const rawTailCap = 3

// BuildDistilledBriefInput mirrors the inline parameter object of
// buildDistilledBrief, in TS declaration order.
//
// Optional-field encoding:
//   - Ledger: nil or empty are equivalent (TS only tests `.length > 0`).
//   - RejectedPatchPath / Workspace: TS tests them for TRUTHINESS, so "" is the
//     absent case — a plain string is exact.
//   - OpenBlockers: TS uses `??`, so absence and emptiness DIFFER. nil means
//     absent (fall back to Workspace); a non-nil empty slice means "explicitly
//     none" and suppresses the workspace read.
type BuildDistilledBriefInput struct {
	TaskDescription     string                  `json:"taskDescription"`
	FailureSignals      []string                `json:"failureSignals"`
	AttemptedApproaches []string                `json:"attemptedApproaches"`
	Ledger              []ledgers.AttemptRecord `json:"ledger"`
	RejectedPatchPath   string                  `json:"rejectedPatchPath"`
	Workspace           string                  `json:"workspace"`
	OpenBlockers        []ledgers.OpenBlocker   `json:"openBlockers"`
}

// BuildDistilledBrief mirrors buildDistilledBrief: the ONLY payload allowed to
// cross from a rotten attempt into a fresh one.
func BuildDistilledBrief(input BuildDistilledBriefInput) string {
	openBlockers := input.OpenBlockers
	if openBlockers == nil {
		if input.Workspace != "" {
			openBlockers = ledgers.LoadOpenBlockers(input.Workspace)
		} else {
			openBlockers = []ledgers.OpenBlocker{}
		}
	}
	lines := []string{}
	lines = append(lines, "## Goal (unchanged from the original task)")
	lines = append(lines, input.TaskDescription)
	lines = append(lines, "")
	// Durable open-blocker re-injection, placed right below the goal.
	if len(openBlockers) > 0 {
		lines = append(lines, "## Unresolved blockers (durable ledger — carried across sessions)")
		lines = append(lines,
			"These are the OPEN blockers recorded on disk from prior audit cycles; they",
			"survived context loss. Resolve each before claiming done:",
		)
		for _, b := range openBlockers {
			lines = append(lines, "- ["+b.BlockerID+"] "+b.Text)
		}
		lines = append(lines, "")
	}
	lines = append(lines, "## What failed last time (verified signals)")
	concreteSignals := []string{}
	for _, s := range input.FailureSignals {
		if !assessmentCounterRx.MatchString(s) {
			concreteSignals = append(concreteSignals, s)
		}
	}
	if len(concreteSignals) == 0 {
		lines = append(lines, "- (no failure signals recorded)")
	} else {
		for _, s := range concreteSignals {
			lines = append(lines, "- "+s)
		}
	}
	lines = append(lines, "")

	ledger := input.Ledger
	if len(ledger) > 0 {
		lines = append(lines, "## Prior attempts (do not repeat)")
		for _, rec := range ledger {
			// `rec.outcome ? ... : ""` — a TRUTHY test, so an empty outcome
			// drops the arrow entirely.
			outcome := ""
			if rec.Outcome != "" {
				outcome = " → " + rec.Outcome
			}
			lines = append(lines, "- [attempt "+jscompat.FormatNumber(rec.Attempt)+"] "+rec.Approach+outcome)
			if rec.Evidence != nil && *rec.Evidence != "" {
				lines = append(lines, "  evidence: "+*rec.Evidence)
			}
		}
		lines = append(lines, "")
	}

	if input.RejectedPatchPath != "" {
		lines = append(lines, "## Rejected patch")
		lines = append(lines, "- A prior attempt's diff exists at "+input.RejectedPatchPath+"; do NOT resubmit it as-is.")
		lines = append(lines, "")
	}

	// The raw tool-call tail is WEAK context; a non-empty ledger SUPERSEDES it.
	if len(ledger) == 0 {
		lines = append(lines, "## Recent tool calls (context only, not curated approaches)")
		if len(input.AttemptedApproaches) == 0 {
			lines = append(lines, "- (no recent tool calls recorded)")
		} else {
			// `.slice(-RAW_TAIL_CAP)`: the last few, or all of them when fewer.
			start := len(input.AttemptedApproaches) - rawTailCap
			if start < 0 {
				start = 0
			}
			for _, a := range input.AttemptedApproaches[start:] {
				lines = append(lines, "- "+a)
			}
		}
	}
	return strings.Join(lines, "\n")
}
