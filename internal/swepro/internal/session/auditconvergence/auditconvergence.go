// Package auditconvergence is a faithful Go port of swe-pro's
// src/session/audit-convergence.ts (commit 3b25a1a) — the W7a severity-tiered
// audit convergence policy.
//
// The TS module is PURE: no I/O, no Date.now, no Math.random. Nothing here
// needs an injectable clock or RNG.
//
// Fidelity notes (deliberate, do not "fix"):
//
//   - The two exported regexes are JS `/…/i` literals. Go's `(?i)` performs
//     UNICODE simple case folding, which JS does NOT do for a non-`u` regex:
//     ECMA-262 Canonicalize() refuses any fold whose result crosses the
//     ASCII boundary, so U+212A KELVIN SIGN never matches `k` and U+017F
//     LATIN SMALL LETTER LONG S never matches `s`. Every literal in both
//     patterns is ASCII, so the exact JS semantics are reproduced by
//     ASCII-lowercasing the SUBJECT and matching an all-lowercase RE2
//     pattern (see Pattern.Test / asciiLower). `(?i)` would over-match.
//
//   - Both patterns are used only through `.test()`, i.e. an existence check.
//     RE2's leftmost-longest submatch rules differ from JS backtracking, but
//     "does a match exist" is identical, so the alternation order and the
//     greedy `[^.\n]*` need no special handling.
//
//   - `\b` in RE2 is the ASCII word boundary — the same set JS uses for a
//     non-`u` regex (ECMA-262 WordCharacters() adds no extra characters
//     unless BOTH the `i` and `u` flags are set).
//
//   - Optional TS fields (`file?`, `line?`, `step?`, `severity?`,
//     `machineOverride?`) carry `,omitempty` — this is the ONE place the port
//     deviates from the repo-wide "no omitempty" rule, because JSON.stringify
//     DROPS undefined-valued keys and the fixture parity gate is byte-for-byte.
//     Required fields (`detail`, `action`, `reason`, the cycle counts) have no
//     omitempty. Known residual gap: an EXPLICIT `null` (as opposed to
//     `undefined`) in an optional field round-trips as "absent" here where
//     JSON.stringify would echo `null`. The module's own logic is unaffected —
//     every read of those fields goes through `?? ""`, which treats null and
//     undefined identically.
//
//   - assessConvergence's `cycles` is `[]*ConvergenceCycle` and
//     partitionBlockers' input is `[]*SeverityBlocker` so that a null element
//     reaches the same `!latest` / `blocker?.` guards the TS has. Where TS
//     would throw a TypeError on a null element (sameKeySet, the
//     cleanup-eligible filter) Go panics on the nil deref — same failure mode.
package auditconvergence

import (
	"regexp"
	"strings"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

// BlockerSeverity mirrors `type BlockerSeverity = "correctness" | "hygiene" | "polish"`.
type BlockerSeverity string

const (
	SeverityCorrectness BlockerSeverity = "correctness"
	SeverityHygiene     BlockerSeverity = "hygiene"
	SeverityPolish      BlockerSeverity = "polish"
)

// SeverityBlocker is the blocker shape this module reasons over — a structural
// subset of the auditor verdict's blocker object (auditor-gate.ts). Severity is
// the optional explicit auditor tag; it is *BlockerSeverity rather than a
// closed enum because the TS accepts (and deliberately distrusts) malformed
// tags such as "cosmetic" or "".
//
// Field order is the TS interface declaration order and is part of the parity
// contract — encoding/json emits declaration order.
type SeverityBlocker struct {
	File     *string          `json:"file,omitempty"`
	Line     *float64         `json:"line,omitempty"`
	Step     *float64         `json:"step,omitempty"`
	Detail   string           `json:"detail"`
	Severity *BlockerSeverity `json:"severity,omitempty"`
}

// ── JS /…/i regexes ──────────────────────────────────────────────────────

// Pattern is a JavaScript `/…/i` regular expression whose literals are all
// ASCII. Test() reproduces ECMA-262 Canonicalize() for a non-`u` regex exactly
// (see the package doc); Source() returns the JS `.source` string verbatim so
// the port can be diffed against the TS by eye.
type Pattern struct {
	source string
	re     *regexp.Regexp
}

// newPattern compiles a JS `/…/i` source under RE2. The subject-lowercasing
// trick is only sound when the pattern itself has no uppercase ASCII (both TS
// patterns are all-lowercase), so that is asserted rather than assumed — the
// source is compiled VERBATIM, never lowercased, because lowering a pattern
// would silently rewrite escapes such as `\W` into `\w`.
func newPattern(jsSource string) *Pattern {
	for i := 0; i < len(jsSource); i++ {
		if c := jsSource[i]; c >= 'A' && c <= 'Z' {
			panic("auditconvergence: JS /…/i source must be all-lowercase ASCII: " + jsSource)
		}
	}
	return &Pattern{source: jsSource, re: regexp.MustCompile(jsSource)}
}

// Test mirrors RegExp.prototype.test for a `/…/i` (no `g`, so stateless).
func (p *Pattern) Test(s string) bool { return p.re.MatchString(asciiLower(s)) }

// Source mirrors RegExp.prototype.source.
func (p *Pattern) Source() string { return p.source }

// String mirrors RegExp.prototype.toString.
func (p *Pattern) String() string { return "/" + p.source + "/i" }

// asciiLower lowercases A-Z and nothing else. Byte-wise is safe: every UTF-8
// continuation/lead byte of a multi-byte rune is >= 0x80 and can never land in
// 'A'..'Z', so non-ASCII runes are left byte-identical.
func asciiLower(s string) string {
	if strings.IndexFunc(s, func(r rune) bool { return r >= 'A' && r <= 'Z' }) < 0 {
		return s
	}
	b := []byte(s)
	for i := 0; i < len(b); i++ {
		if c := b[i]; c >= 'A' && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}

// HYGIENE_PATTERN mirrors the exported TS constant of the same name. GENERAL
// surface patterns only — leftover build/scratch/backup artifacts, dead code,
// and explicit "remove the temporary/probe/scratch/debug thing" asks. No repo
// names, no issue ids, no domain terms.
var HYGIENE_PATTERN = newPattern(
	`\.bak\b|\.orig\b|\.tmp\b|leftover|scratch|debug file|backup file|remove\b[^.\n]*\b(temporary|probe|scratch|debug)\b|stray file|dead code|commented[- ]out|left in place`,
)

// POLISH_PATTERN mirrors the exported TS constant of the same name. GENERAL
// surface patterns only — naming / wording / comment / style polish.
var POLISH_PATTERN = newPattern(
	`\bnaming\b|\btypo\b|\bcomment\b|\bdocstring\b|\bstyle\b|\bformatting\b|\bwording\b|\brename\b|\bwhitespace\b`,
)

// ── classification ───────────────────────────────────────────────────────

// ClassifyBlockerSeverity is the fallback classifier for an UNTAGGED blocker.
// General pattern lists only; when neither matches (or the text is ambiguous)
// the severity is "correctness" — the quality floor. Never downgrades on doubt.
// Hygiene is checked before polish so a leftover file whose detail also
// mentions naming is still treated as hygiene.
//
// The TS body opens with `const text = detail ?? ""`; the declared parameter
// type is `string`, and a Go string cannot be nil, so the coalesce is a no-op
// here (both a JS `null` and a Go zero string yield "").
func ClassifyBlockerSeverity(detail string) BlockerSeverity {
	text := detail
	if HYGIENE_PATTERN.Test(text) {
		return SeverityHygiene
	}
	if POLISH_PATTERN.Test(text) {
		return SeverityPolish
	}
	return SeverityCorrectness
}

// EffectiveSeverity is the effective severity of a blocker: an explicit,
// well-formed auditor tag wins; otherwise fall back to the classifier. An
// unknown/malformed tag is NOT trusted — it falls through to the classifier
// (and therefore, on doubt, to "correctness").
func EffectiveSeverity(blocker *SeverityBlocker) BlockerSeverity {
	var tag *BlockerSeverity
	if blocker != nil {
		tag = blocker.Severity
	}
	if tag != nil && (*tag == SeverityCorrectness || *tag == SeverityHygiene || *tag == SeverityPolish) {
		return *tag
	}
	detail := ""
	if blocker != nil {
		detail = blocker.Detail
	}
	return ClassifyBlockerSeverity(detail)
}

// PartitionedBlockers mirrors the TS interface; field order is the TS
// declaration order (which is also the object-literal order in
// partitionBlockers) and is part of the parity contract.
type PartitionedBlockers struct {
	Correctness []*SeverityBlocker `json:"correctness"`
	Hygiene     []*SeverityBlocker `json:"hygiene"`
	Polish      []*SeverityBlocker `json:"polish"`
}

// PartitionBlockers splits a blocker list into severity tiers by
// EffectiveSeverity.
func PartitionBlockers(blockers []*SeverityBlocker) PartitionedBlockers {
	out := PartitionedBlockers{
		Correctness: []*SeverityBlocker{},
		Hygiene:     []*SeverityBlocker{},
		Polish:      []*SeverityBlocker{},
	}
	for _, b := range blockers {
		switch EffectiveSeverity(b) {
		case SeverityCorrectness:
			out.Correctness = append(out.Correctness, b)
		case SeverityHygiene:
			out.Hygiene = append(out.Hygiene, b)
		case SeverityPolish:
			out.Polish = append(out.Polish, b)
		}
	}
	return out
}

// BlockerKey is the stable cross-cycle identity for a blocker, used for
// stagnation detection.
func BlockerKey(blocker *SeverityBlocker) string {
	file := ""
	line := ""
	detail := ""
	if blocker != nil {
		if blocker.File != nil {
			file = *blocker.File
		}
		if blocker.Line != nil {
			// `${line}` where line is `number | ""` — String(number).
			line = jscompat.FormatNumber(*blocker.Line)
		}
		detail = blocker.Detail
	}
	return file + ":" + line + ":" + jscompat.Trim(detail)
}

// ── convergence ──────────────────────────────────────────────────────────

// AUDIT_CLEANUP_MAX_CYCLES_DEFAULT is the default diminishing-returns cap:
// after this many prior cleanup-eligible cycles, residual non-correctness
// blockers are accepted-with-notes rather than looped on. Mirrors the
// AUDIT_CLEANUP_MAX_CYCLES knob default (knobs.ts).
const AUDIT_CLEANUP_MAX_CYCLES_DEFAULT float64 = 1

// ConvergenceCycle is one row of per-run audit history: the severity breakdown
// of a single audit cycle's blockers plus their stable keys (for stagnation
// detection). The counts are TS `number`s, i.e. float64 — the reason strings
// interpolate them with String(), so a non-integral count must print exactly
// as V8 would.
type ConvergenceCycle struct {
	CorrectnessCount float64  `json:"correctnessCount"`
	HygieneCount     float64  `json:"hygieneCount"`
	PolishCount      float64  `json:"polishCount"`
	BlockerKeys      []string `json:"blockerKeys"`
}

// ConvergenceAction mirrors `type ConvergenceAction = "continue" | "cleanup" | "accept"`.
type ConvergenceAction string

const (
	ActionContinue ConvergenceAction = "continue"
	ActionCleanup  ConvergenceAction = "cleanup"
	ActionAccept   ConvergenceAction = "accept"
)

// ConvergenceAssessment mirrors the TS interface. MachineOverride is W11b:
// true ONLY for the machine-verified fixed-point accept — it tells the
// caller's zero-correctness invariant guard that this accept is backed by
// machine evidence (unchanged tree + passing contract), not LLM opinion.
type ConvergenceAssessment struct {
	Action          ConvergenceAction `json:"action"`
	Reason          string            `json:"reason"`
	MachineOverride *bool             `json:"machineOverride,omitempty"`
}

// FixedPointEvidence is W11b machine-evidence for the LATEST cycle.
// TreeUnchanged — the work tree is byte-identical to what the PREVIOUS audit
// cycle saw (a full fix round produced zero edits). ContractPassed — the
// coder's acceptance contract passed this cycle (machine-run, not LLM opinion).
type FixedPointEvidence struct {
	TreeUnchanged  bool `json:"treeUnchanged"`
	ContractPassed bool `json:"contractPassed"`
}

// AssessConvergenceInput is the anonymous object type assessConvergence takes.
type AssessConvergenceInput struct {
	Cycles           []*ConvergenceCycle `json:"cycles"`
	MaxCleanupCycles float64             `json:"maxCleanupCycles"`
	FixedPoint       *FixedPointEvidence `json:"fixedPoint,omitempty"`
}

// sameKeySet is true when two key sets are non-empty and identical.
func sameKeySet(a, b []string) bool {
	if len(a) == 0 || len(b) == 0 {
		return false
	}
	sa := make(map[string]struct{}, len(a))
	for _, k := range a {
		sa[k] = struct{}{}
	}
	sb := make(map[string]struct{}, len(b))
	for _, k := range b {
		sb[k] = struct{}{}
	}
	if len(sa) != len(sb) {
		return false
	}
	for k := range sa {
		if _, ok := sb[k]; !ok {
			return false
		}
	}
	return true
}

// AssessConvergence decides the convergence action from the per-run cycle
// history.
//
// Rules:
//   - Latest cycle has correctness > 0 ⇒ "continue" (the exact existing path).
//   - Latest has 0 correctness but hygiene+polish > 0 ⇒ "cleanup" while the
//     number of PRIOR cleanup-eligible cycles is < maxCleanupCycles, else
//     "accept" (the residual non-correctness blockers are recorded in the
//     reason so they are logged, not looped on). A "cleanup-eligible" cycle is
//     one that itself had 0 correctness and > 0 hygiene+polish.
//   - Stagnation: if the latest two cycles carry the identical, non-empty
//     blocker-key set, keep the action "continue" with reason "stagnant" — the
//     existing give-up machinery (relay budget) handles a non-converging run;
//     no new give-up path is introduced here, and a stuck state is never
//     masked as "accept".
//
// Pure: same inputs ⇒ same output. The kill switch (maxCleanupCycles handling)
// is enforced by the caller — AssessConvergence follows the literal rule above.
func AssessConvergence(input AssessConvergenceInput) ConvergenceAssessment {
	cycles := input.Cycles
	var latest *ConvergenceCycle
	if len(cycles) > 0 {
		latest = cycles[len(cycles)-1]
	}
	if latest == nil {
		return ConvergenceAssessment{Action: ActionContinue, Reason: "n/a: no audit cycle history yet"}
	}

	// ── W11b fixed-point rule (numerical-analysis style): if a WHOLE fix cycle
	// changed nothing on disk AND the machine contract passes, iterating further
	// is provably ceremony — the same inputs will produce the same audit. Accept
	// with the residual blockers surfaced as notes instead of burning cycles into
	// the give-up machinery. Machine evidence only: BOTH signals are required,
	// and a failing/absent contract falls through to the existing paths (the
	// quality floor cannot drop below what the contract can see; observed on a
	// live run where phantom blockers looped 3 full cycles on an unchanged,
	// contract-passing 1-line fix).
	if len(cycles) >= 2 && input.FixedPoint != nil && input.FixedPoint.TreeUnchanged && input.FixedPoint.ContractPassed {
		t := true
		return ConvergenceAssessment{
			Action: ActionAccept,
			Reason: "machine-verified fixed point: fix cycle changed nothing on disk and the acceptance " +
				"contract passes — residual blockers recorded as notes (further cycles cannot change the outcome)",
			MachineOverride: &t,
		}
	}

	// Stagnation takes precedence: a non-converging loop is handed back to the
	// existing give-up machinery via "continue", never accepted or re-cleaned.
	if len(cycles) >= 2 {
		prev := cycles[len(cycles)-2]
		if sameKeySet(latest.BlockerKeys, prev.BlockerKeys) {
			return ConvergenceAssessment{Action: ActionContinue, Reason: "stagnant: identical blocker set across the last two cycles"}
		}
	}

	// Correctness blockers present ⇒ the exact existing fix path, unchanged.
	if latest.CorrectnessCount > 0 {
		return ConvergenceAssessment{
			Action: ActionContinue,
			Reason: jscompat.FormatNumber(latest.CorrectnessCount) + " correctness blocker(s) present",
		}
	}

	nonCorrectness := latest.HygieneCount + latest.PolishCount
	if nonCorrectness > 0 {
		priorCleanupEligible := 0
		for _, c := range cycles[:len(cycles)-1] {
			if c.CorrectnessCount == 0 && c.HygieneCount+c.PolishCount > 0 {
				priorCleanupEligible++
			}
		}
		if float64(priorCleanupEligible) < input.MaxCleanupCycles {
			return ConvergenceAssessment{
				Action: ActionCleanup,
				Reason: "0 correctness, " + jscompat.FormatNumber(nonCorrectness) +
					" hygiene/polish blocker(s); batched cleanup pass " +
					jscompat.FormatNumber(float64(priorCleanupEligible)+1) + "/" +
					jscompat.FormatNumber(input.MaxCleanupCycles),
			}
		}
		return ConvergenceAssessment{
			Action: ActionAccept,
			Reason: "diminishing returns: " + jscompat.FormatNumber(nonCorrectness) +
				" residual non-correctness blocker(s) after " +
				jscompat.FormatNumber(float64(priorCleanupEligible)) +
				" cleanup cycle(s) — accepted with notes",
		}
	}

	// No blockers of any severity on the latest cycle: nothing to loop on.
	// (Should not arise on a fail-with-blockers path, but defends an empty
	// latest cycle.)
	return ConvergenceAssessment{Action: ActionContinue, Reason: "n/a: latest cycle has no blockers"}
}
