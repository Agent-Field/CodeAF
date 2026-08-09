// Wald sequential probability ratio test for flaky verification — port of
// src/session/sprt.ts.
//
// Pure and deterministic: observations are the only state transitions. The TS
// module reads neither Date.now nor Math.random, so there is no injectable
// clock here.
//
// Two surfaces on the same test:
//   - CreateSprt — a stateful stream API (observe pass/fail one at a time),
//     used by the streaming flaky-check path.
//   - SprtDecision / NextSampleNeeded — a stateless snapshot API that
//     adjudicates a (successes, trials) tally in one shot.
//
// Convention for the snapshot API: a "success" is a HEALTHY observation. p0 is
// the flaky/bad per-run success rate (H0, e.g. 0.5), p1 the healthy rate
// (H1, e.g. 0.95). "accept" ⇒ accept H1 (test is healthy); "reject" ⇒ accept H0
// (test is flaky/broken); "continue" ⇒ inconclusive, sample again.
//
// Fidelity notes (deliberate, do not "fix"):
//   - Every optional TS field (`alpha?`, `beta?`, `maxTrials?`) is a *float64
//     so that "absent" (nil ⇒ `??` default) stays distinct from an explicitly
//     supplied NaN (which reaches the finiteness guard and throws). Nullish
//     coalescing, not truthiness: an explicit 0 is kept and then rejected.
//   - maxTrials is a float64, not an int: TS validates it with
//     Number.isInteger, so 2.5 and NaN are reachable inputs that must throw.
//   - sprtPlanFor's TS `switch` has no default arm, so an out-of-union kind
//     returns `undefined` at runtime. The Go port returns *SprtOptions and
//     yields nil for that case rather than inventing a zero value.
//   - The two surfaces validate p1 DIFFERENTLY: createSprt demands p1 > p0,
//     the snapshot path only demands p1 != p0. Kept as-is.
//   - The snapshot LLR is H1-over-H0 (successes carry ln(p1/p0)); the stream
//     LLR is inverted (a PASS *lowers* it). The two surfaces therefore
//     disagree on sign. Kept as-is.
//   - Float expression shape and accumulation order are preserved literally so
//     the >= / <= boundary comparisons land on the same side as V8's.
package sprt

import (
	"math"
	"strings"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

// RangeError mirrors the JS RangeError this module throws. The TS module does
// not export an error type (it throws the global), but Go callers need a
// typed target for errors.As.
type RangeError struct {
	Message string
}

func (e *RangeError) Error() string { return e.Message }

func newRangeError(msg string) *RangeError { return &RangeError{Message: msg} }

// SprtOptions is the TS `SprtOptions` interface.
type SprtOptions struct {
	// P0 is the tolerated per-run failure rate under H0.
	P0 float64 `json:"p0"`
	// P1 is the per-run failure rate under H1.
	P1 float64 `json:"p1"`
	// Alpha is the type-I error probability.
	Alpha *float64 `json:"alpha"`
	// Beta is the type-II error probability.
	Beta *float64 `json:"beta"`
	// MaxTrials is the maximum number of observations before forcing a
	// decision.
	MaxTrials *float64 `json:"maxTrials"`
}

// MarshalJSON reproduces JSON.stringify of the TS object literals: optional
// properties that are absent produce no key at all (JS `undefined`), and
// numbers use V8's shortest round-trip form (NaN/±Infinity become null).
func (o SprtOptions) MarshalJSON() ([]byte, error) {
	var b strings.Builder
	b.WriteString(`{"p0":`)
	b.WriteString(jsonNumber(o.P0))
	b.WriteString(`,"p1":`)
	b.WriteString(jsonNumber(o.P1))
	if o.Alpha != nil {
		b.WriteString(`,"alpha":`)
		b.WriteString(jsonNumber(*o.Alpha))
	}
	if o.Beta != nil {
		b.WriteString(`,"beta":`)
		b.WriteString(jsonNumber(*o.Beta))
	}
	if o.MaxTrials != nil {
		b.WriteString(`,"maxTrials":`)
		b.WriteString(jsonNumber(*o.MaxTrials))
	}
	b.WriteString(`}`)
	return []byte(b.String()), nil
}

func jsonNumber(f float64) string {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return "null"
	}
	return jscompat.FormatNumber(f)
}

// SprtObservation is the TS `SprtObservation` union.
type SprtObservation string

const (
	ObservationAcceptPass SprtObservation = "accept-pass"
	ObservationAcceptFail SprtObservation = "accept-fail"
	ObservationContinue   SprtObservation = "continue"
)

// Forced decisions returned by (*Sprt).ForceDecision. The TS union is
// anonymous, so these are plain strings.
const (
	ForcedPass = "pass"
	ForcedFail = "fail"
)

const (
	defaultAlpha     = 0.01
	defaultBeta      = 0.05
	defaultMaxTrials = 8
)

// Sprt is the closure object returned by the TS createSprt.
type Sprt struct {
	options       SprtOptions
	maxTrials     float64
	upperBoundary float64
	lowerBoundary float64
	currentLlr    float64
	currentTrials float64
	terminal      SprtObservation
}

// CreateSprt ports createSprt. The TS version throws RangeError; Go returns
// (*RangeError) as the error.
func CreateSprt(options SprtOptions) (*Sprt, error) {
	alpha := float64(defaultAlpha)
	if options.Alpha != nil {
		alpha = *options.Alpha
	}
	beta := float64(defaultBeta)
	if options.Beta != nil {
		beta = *options.Beta
	}
	maxTrials := float64(defaultMaxTrials)
	if options.MaxTrials != nil {
		maxTrials = *options.MaxTrials
	}

	if !isFinite(options.P0) || options.P0 <= 0 || options.P0 >= 1 {
		return nil, newRangeError("SPRT p0 must be between zero and one")
	}
	if !isFinite(options.P1) || options.P1 <= options.P0 || options.P1 >= 1 {
		return nil, newRangeError("SPRT p1 must be greater than p0 and less than one")
	}
	if !isFinite(alpha) || alpha <= 0 || alpha >= 1 {
		return nil, newRangeError("SPRT alpha must be between zero and one")
	}
	if !isFinite(beta) || beta <= 0 || beta >= 1 {
		return nil, newRangeError("SPRT beta must be between zero and one")
	}
	if !isInteger(maxTrials) || maxTrials < 1 {
		return nil, newRangeError("SPRT maxTrials must be a positive integer")
	}

	return &Sprt{
		options:       options,
		maxTrials:     maxTrials,
		upperBoundary: math.Log((1 - beta) / alpha),
		lowerBoundary: math.Log(beta / (1 - alpha)),
	}, nil
}

// ForceDecision ports forceDecision. NaN > 0 is false, so a NaN LLR forces
// "pass" — same as JS.
func (s *Sprt) ForceDecision() string {
	if s.currentLlr > 0 {
		return ForcedFail
	}
	return ForcedPass
}

// Observe ports observe. Once terminal, the same verdict is returned forever
// and no LLR/trial accumulation happens.
func (s *Sprt) Observe(passed bool) SprtObservation {
	// TS: `if (terminal) return terminal` — terminal is only ever assigned a
	// non-empty (truthy) string, so "" ⇔ undefined.
	if s.terminal != "" {
		return s.terminal
	}

	var increment float64
	if passed {
		increment = math.Log((1 - s.options.P1) / (1 - s.options.P0))
	} else {
		increment = math.Log(s.options.P1 / s.options.P0)
	}
	s.currentLlr += increment
	s.currentTrials += 1

	if s.currentLlr >= s.upperBoundary {
		s.terminal = ObservationAcceptFail
	} else if s.currentLlr <= s.lowerBoundary {
		s.terminal = ObservationAcceptPass
	} else if s.currentTrials >= s.maxTrials {
		if s.ForceDecision() == ForcedFail {
			s.terminal = ObservationAcceptFail
		} else {
			s.terminal = ObservationAcceptPass
		}
	} else {
		return ObservationContinue
	}
	return s.terminal
}

// Llr ports llr().
func (s *Sprt) Llr() float64 { return s.currentLlr }

// Trials ports trials().
func (s *Sprt) Trials() float64 { return s.currentTrials }

// ---------------------------------------------------------------------------
// Stateless snapshot API (see package header). Adjudicates a whole (successes,
// trials) tally against Wald's bounds in one call — no accumulation state.

// SprtSnapshot is the TS `SprtSnapshot` interface.
type SprtSnapshot struct {
	// Successes counts healthy observations so far.
	Successes float64 `json:"successes"`
	// Trials counts total observations so far.
	Trials float64 `json:"trials"`
	// P0 is the flaky/bad per-run success rate under H0 (e.g. 0.5).
	P0 float64 `json:"p0"`
	// P1 is the healthy per-run success rate under H1 (e.g. 0.95).
	P1 float64 `json:"p1"`
	// Alpha is the type-I error probability (default 0.05).
	Alpha *float64 `json:"alpha"`
	// Beta is the type-II error probability (default 0.05).
	Beta *float64 `json:"beta"`
}

const (
	snapshotDefaultAlpha = 0.05
	snapshotDefaultBeta  = 0.05
)

// Verdicts returned by SprtDecision. The TS union is anonymous.
const (
	VerdictAccept   = "accept"
	VerdictReject   = "reject"
	VerdictContinue = "continue"
)

type snapshotBounds struct {
	llr   float64
	upper float64
	lower float64
}

func validatedSnapshot(input SprtSnapshot) (snapshotBounds, error) {
	alpha := float64(snapshotDefaultAlpha)
	if input.Alpha != nil {
		alpha = *input.Alpha
	}
	beta := float64(snapshotDefaultBeta)
	if input.Beta != nil {
		beta = *input.Beta
	}
	if !isFinite(input.P0) || input.P0 <= 0 || input.P0 >= 1 {
		return snapshotBounds{}, newRangeError("SPRT p0 must be between zero and one")
	}
	if !isFinite(input.P1) || input.P1 <= 0 || input.P1 >= 1 || input.P1 == input.P0 {
		return snapshotBounds{}, newRangeError("SPRT p1 must be between zero and one and differ from p0")
	}
	if !isFinite(alpha) || alpha <= 0 || alpha >= 1 {
		return snapshotBounds{}, newRangeError("SPRT alpha must be between zero and one")
	}
	if !isFinite(beta) || beta <= 0 || beta >= 1 {
		return snapshotBounds{}, newRangeError("SPRT beta must be between zero and one")
	}
	rawTrials := 0.0
	if isFinite(input.Trials) {
		rawTrials = input.Trials
	}
	trials := math.Max(0, math.Floor(rawTrials))
	rawSuccesses := 0.0
	if isFinite(input.Successes) {
		rawSuccesses = input.Successes
	}
	successes := math.Min(trials, math.Max(0, math.Floor(rawSuccesses)))
	failures := trials - successes
	// Log-likelihood ratio of H1 (healthy, p1) over H0 (flaky, p0).
	llr := successes*math.Log(input.P1/input.P0) +
		failures*math.Log((1-input.P1)/(1-input.P0))
	return snapshotBounds{
		llr:   llr,
		upper: math.Log((1 - beta) / alpha),
		lower: math.Log(beta / (1 - alpha)),
	}, nil
}

// SprtDecision adjudicates a (successes, trials) tally with Wald's SPRT
// bounds:
//
//	A = ln((1−beta)/alpha)  (upper — accept H1, healthy)
//	B = ln(beta/(1−alpha))  (lower — accept H0, flaky/broken)
//
// Returns "accept" (healthy), "reject" (flaky), or "continue" (sample again).
func SprtDecision(input SprtSnapshot) (string, error) {
	bounds, err := validatedSnapshot(input)
	if err != nil {
		return "", err
	}
	if bounds.llr >= bounds.upper {
		return VerdictAccept, nil
	}
	if bounds.llr <= bounds.lower {
		return VerdictReject, nil
	}
	return VerdictContinue, nil
}

// NextSampleNeeded reports whether another observation could still change the
// verdict. True iff the snapshot is still "continue": once a boundary is
// crossed the decision is stable under further sampling (the bounds are
// fixed), so no further trial would flip an accept/reject.
func NextSampleNeeded(input SprtSnapshot) (bool, error) {
	decision, err := SprtDecision(input)
	if err != nil {
		return false, err
	}
	return decision == VerdictContinue, nil
}

// SprtPlanKind is the anonymous TS union accepted by sprtPlanFor.
type SprtPlanKind string

const (
	PlanKindDeterministic SprtPlanKind = "deterministic"
	PlanKindBrowser       SprtPlanKind = "browser"
	PlanKindNetwork       SprtPlanKind = "network"
)

// SprtPlanFor ports sprtPlanFor. The TS switch has no default arm, so any
// kind outside the union falls off the end and yields `undefined`; the Go
// port returns nil for that case.
func SprtPlanFor(kind SprtPlanKind) *SprtOptions {
	switch kind {
	case PlanKindDeterministic:
		// Deterministic checks are intentionally decided after their first run.
		one := 1.0
		return &SprtOptions{P0: 0.005, P1: 0.6, MaxTrials: &one}
	case PlanKindBrowser:
		return &SprtOptions{P0: 0.08, P1: 0.5}
	case PlanKindNetwork:
		return &SprtOptions{P0: 0.15, P1: 0.5}
	}
	return nil
}

// ---------------------------------------------------------------------------

// isFinite is Number.isFinite for an already-numeric value.
func isFinite(f float64) bool {
	return !math.IsNaN(f) && !math.IsInf(f, 0)
}

// isInteger is Number.isInteger for an already-numeric value.
func isInteger(f float64) bool {
	return isFinite(f) && math.Floor(f) == f
}
