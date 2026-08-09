package auditconvergence

import (
	"math"
	"regexp"
	"strings"
	"testing"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

// Translation of src/session/audit-convergence.test.ts — verbatim describe/test
// names, same assertion semantics.

func strp(s string) *string                   { return &s }
func nump(f float64) *float64                 { return &f }
func sevp(s BlockerSeverity) *BlockerSeverity { return &s }

func details(bs []*SeverityBlocker) []string {
	out := make([]string, len(bs))
	for i, b := range bs {
		out[i] = b.Detail
	}
	return out
}

func eqStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ── classifyBlockerSeverity ──────────────────────────────────────────────

func TestClassifyBlockerSeverity(t *testing.T) {
	t.Run("defaults to correctness on ambiguous / generic text", func(t *testing.T) {
		for _, in := range []string{
			"the function returns the wrong value",
			"something is off here",
			"",
			// Requirement / failing-check language is correctness, not polish.
			"missing requirement: pagination not implemented",
			"test suite fails with 3 errors",
		} {
			if got := ClassifyBlockerSeverity(in); got != "correctness" {
				t.Errorf("ClassifyBlockerSeverity(%q) = %q, want %q", in, got, "correctness")
			}
		}
	})

	t.Run("recognizes hygiene surface patterns", func(t *testing.T) {
		for _, in := range []string{
			"leftover scratch.py file in repo root",
			"remove the temporary probe added during debugging",
			"config.bak left in place",
			"dead code in the old handler",
		} {
			if got := ClassifyBlockerSeverity(in); got != "hygiene" {
				t.Errorf("ClassifyBlockerSeverity(%q) = %q, want %q", in, got, "hygiene")
			}
		}
	})

	t.Run("recognizes polish surface patterns", func(t *testing.T) {
		for _, in := range []string{
			"variable naming is inconsistent",
			"typo in the docstring",
			"formatting/style nit in the header comment",
		} {
			if got := ClassifyBlockerSeverity(in); got != "polish" {
				t.Errorf("ClassifyBlockerSeverity(%q) = %q, want %q", in, got, "polish")
			}
		}
	})

	t.Run("hygiene wins over polish when both could match", func(t *testing.T) {
		// "dead code" (hygiene) alongside "naming" (polish) -> hygiene.
		if got := ClassifyBlockerSeverity("dead code with poor naming"); got != "hygiene" {
			t.Errorf("got %q, want %q", got, "hygiene")
		}
	})
}

// ── effectiveSeverity ────────────────────────────────────────────────────

func TestEffectiveSeverity(t *testing.T) {
	t.Run("explicit well-formed tag wins over the classifier", func(t *testing.T) {
		// Detail text screams hygiene, but the explicit tag says correctness.
		b := &SeverityBlocker{Detail: "leftover scratch file", Severity: sevp("correctness")}
		if got := EffectiveSeverity(b); got != "correctness" {
			t.Errorf("got %q, want %q", got, "correctness")
		}
		// Detail text is neutral, explicit tag downgrades to polish.
		c := &SeverityBlocker{Detail: "adjust the header", Severity: sevp("polish")}
		if got := EffectiveSeverity(c); got != "polish" {
			t.Errorf("got %q, want %q", got, "polish")
		}
	})

	t.Run("falls back to the classifier when the tag is absent", func(t *testing.T) {
		if got := EffectiveSeverity(&SeverityBlocker{Detail: "returns the wrong value"}); got != "correctness" {
			t.Errorf("got %q, want %q", got, "correctness")
		}
		if got := EffectiveSeverity(&SeverityBlocker{Detail: "leftover backup file config.bak"}); got != "hygiene" {
			t.Errorf("got %q, want %q", got, "hygiene")
		}
	})

	t.Run("an unknown/malformed tag is not trusted — falls through to classifier", func(t *testing.T) {
		b := &SeverityBlocker{Detail: "leftover scratch file", Severity: sevp("cosmetic")}
		if got := EffectiveSeverity(b); got != "hygiene" {
			t.Errorf("got %q, want %q", got, "hygiene")
		}
		// Malformed tag on ambiguous text defaults to correctness (quality floor).
		c := &SeverityBlocker{Detail: "something odd", Severity: sevp("")}
		if got := EffectiveSeverity(c); got != "correctness" {
			t.Errorf("got %q, want %q", got, "correctness")
		}
	})
}

// ── partitionBlockers ────────────────────────────────────────────────────

func TestPartitionBlockers(t *testing.T) {
	t.Run("splits a mixed list into the three tiers", func(t *testing.T) {
		blockers := []*SeverityBlocker{
			{Detail: "wrong output for empty input"},                   // correctness (default)
			{Detail: "leftover scratch.py", Severity: sevp("hygiene")}, // hygiene
			{Detail: "typo in comment", Severity: sevp("polish")},      // polish
			{Detail: "naming is inconsistent"},                         // polish (classifier)
			{Detail: "explicit correctness", Severity: sevp("correctness")},
		}
		p := PartitionBlockers(blockers)
		if want := []string{"wrong output for empty input", "explicit correctness"}; !eqStrings(details(p.Correctness), want) {
			t.Errorf("correctness = %v, want %v", details(p.Correctness), want)
		}
		if want := []string{"leftover scratch.py"}; !eqStrings(details(p.Hygiene), want) {
			t.Errorf("hygiene = %v, want %v", details(p.Hygiene), want)
		}
		if want := []string{"typo in comment", "naming is inconsistent"}; !eqStrings(details(p.Polish), want) {
			t.Errorf("polish = %v, want %v", details(p.Polish), want)
		}
	})

	t.Run("empty input yields three empty tiers", func(t *testing.T) {
		p := PartitionBlockers([]*SeverityBlocker{})
		if len(p.Correctness) != 0 || len(p.Hygiene) != 0 || len(p.Polish) != 0 {
			t.Errorf("expected three empty tiers, got %+v", p)
		}
		// JSON.stringify must still see [] and not null.
		b, err := jscompat.Stringify(p)
		if err != nil {
			t.Fatal(err)
		}
		if string(b) != `{"correctness":[],"hygiene":[],"polish":[]}` {
			t.Errorf("got %s", b)
		}
	})
}

// ── blockerKey ───────────────────────────────────────────────────────────

func TestBlockerKey(t *testing.T) {
	t.Run("stable identity across file/line/detail", func(t *testing.T) {
		if got := BlockerKey(&SeverityBlocker{File: strp("a.ts"), Line: nump(3), Detail: "x"}); got != "a.ts:3:x" {
			t.Errorf("got %q, want %q", got, "a.ts:3:x")
		}
		if got := BlockerKey(&SeverityBlocker{Detail: "  spaced  "}); got != "::spaced" {
			t.Errorf("got %q, want %q", got, "::spaced")
		}
		// Same logical blocker -> same key.
		a := BlockerKey(&SeverityBlocker{File: strp("a.ts"), Line: nump(3), Detail: "x"})
		b := BlockerKey(&SeverityBlocker{File: strp("a.ts"), Line: nump(3), Detail: "x"})
		if a != b {
			t.Errorf("%q != %q", a, b)
		}
	})
}

// ── assessConvergence — full matrix ──────────────────────────────────────

type cycPartial struct {
	correctnessCount float64
	hygieneCount     float64
	polishCount      float64
	blockerKeys      []string
}

func cyc(p cycPartial) *ConvergenceCycle {
	keys := p.blockerKeys
	if keys == nil {
		keys = []string{}
	}
	return &ConvergenceCycle{
		CorrectnessCount: p.correctnessCount,
		HygieneCount:     p.hygieneCount,
		PolishCount:      p.polishCount,
		BlockerKeys:      keys,
	}
}

func TestAssessConvergence(t *testing.T) {
	t.Run("default cap constant is 1", func(t *testing.T) {
		if AUDIT_CLEANUP_MAX_CYCLES_DEFAULT != 1 {
			t.Errorf("got %v, want 1", AUDIT_CLEANUP_MAX_CYCLES_DEFAULT)
		}
	})

	t.Run("empty history -> continue, n/a reason", func(t *testing.T) {
		r := AssessConvergence(AssessConvergenceInput{Cycles: []*ConvergenceCycle{}, MaxCleanupCycles: 1})
		if r.Action != "continue" {
			t.Errorf("action = %q, want continue", r.Action)
		}
		if !regexp.MustCompile(`(?i)n/a`).MatchString(r.Reason) {
			t.Errorf("reason %q does not match /n\\/a/i", r.Reason)
		}
	})

	t.Run("correctness present -> continue", func(t *testing.T) {
		r := AssessConvergence(AssessConvergenceInput{
			Cycles:           []*ConvergenceCycle{cyc(cycPartial{correctnessCount: 2, hygieneCount: 1, blockerKeys: []string{"k1", "k2", "k3"}})},
			MaxCleanupCycles: 1,
		})
		if r.Action != "continue" {
			t.Errorf("action = %q, want continue", r.Action)
		}
		if !regexp.MustCompile(`(?i)correctness`).MatchString(r.Reason) {
			t.Errorf("reason %q does not match /correctness/i", r.Reason)
		}
	})

	t.Run("hygiene-only, first time -> cleanup", func(t *testing.T) {
		r := AssessConvergence(AssessConvergenceInput{
			Cycles:           []*ConvergenceCycle{cyc(cycPartial{hygieneCount: 1, polishCount: 1, blockerKeys: []string{"h1", "p1"}})},
			MaxCleanupCycles: 1,
		})
		if r.Action != "cleanup" {
			t.Errorf("action = %q, want cleanup", r.Action)
		}
	})

	t.Run("hygiene-only after the cap is reached -> accept", func(t *testing.T) {
		// A prior cleanup-eligible cycle (0 correctness, residual non-correctness),
		// then another with DIFFERENT keys (not stagnant) -> accept once cap hit.
		cycles := []*ConvergenceCycle{
			cyc(cycPartial{hygieneCount: 1, blockerKeys: []string{"h1"}}), // prior cleanup-eligible
			cyc(cycPartial{polishCount: 1, blockerKeys: []string{"p2"}}),  // latest, different keys
		}
		r := AssessConvergence(AssessConvergenceInput{Cycles: cycles, MaxCleanupCycles: 1})
		if r.Action != "accept" {
			t.Errorf("action = %q, want accept", r.Action)
		}
		if !regexp.MustCompile(`(?i)residual`).MatchString(r.Reason) {
			t.Errorf("reason %q does not match /residual/i", r.Reason)
		}
	})

	t.Run("accept only fires once prior cleanup-eligible cycles reach the cap", func(t *testing.T) {
		// With a higher cap the same two-cycle history still cleans up.
		cycles := []*ConvergenceCycle{
			cyc(cycPartial{hygieneCount: 1, blockerKeys: []string{"h1"}}),
			cyc(cycPartial{polishCount: 1, blockerKeys: []string{"p2"}}),
		}
		if got := AssessConvergence(AssessConvergenceInput{Cycles: cycles, MaxCleanupCycles: 2}).Action; got != "cleanup" {
			t.Errorf("action = %q, want cleanup", got)
		}
	})

	t.Run("stagnation: identical non-empty blocker set across last two cycles -> continue/stagnant", func(t *testing.T) {
		keys := []string{"h1", "p1"}
		cycles := []*ConvergenceCycle{
			cyc(cycPartial{hygieneCount: 1, polishCount: 1, blockerKeys: keys}),
			cyc(cycPartial{hygieneCount: 1, polishCount: 1, blockerKeys: append([]string{}, keys...)}),
		}
		r := AssessConvergence(AssessConvergenceInput{Cycles: cycles, MaxCleanupCycles: 1})
		if r.Action != "continue" {
			t.Errorf("action = %q, want continue", r.Action)
		}
		if !regexp.MustCompile(`(?i)stagnant`).MatchString(r.Reason) {
			t.Errorf("reason %q does not match /stagnant/i", r.Reason)
		}
	})

	t.Run("stagnation does not fire on empty key sets", func(t *testing.T) {
		// Two empty-key cycles are NOT treated as stagnant (nothing to be stuck on).
		cycles := []*ConvergenceCycle{
			cyc(cycPartial{hygieneCount: 1, blockerKeys: []string{}}),
			cyc(cycPartial{hygieneCount: 1, blockerKeys: []string{}}),
		}
		r := AssessConvergence(AssessConvergenceInput{Cycles: cycles, MaxCleanupCycles: 1})
		if r.Action == "continue" {
			t.Errorf("action = %q, want not continue", r.Action)
		}
	})

	t.Run("kill-switch semantics: with cap 0 the literal rule yields accept (caller gates)", func(t *testing.T) {
		// assessConvergence follows the literal rule; run.ts enforces the kill switch
		// by not invoking the arms when the knob is 0. Documented here so the rule is
		// pinned: cap 0 -> priorEligible(0) is not < 0 -> accept.
		r := AssessConvergence(AssessConvergenceInput{
			Cycles:           []*ConvergenceCycle{cyc(cycPartial{hygieneCount: 1, blockerKeys: []string{"h1"}})},
			MaxCleanupCycles: 0,
		})
		if r.Action != "accept" {
			t.Errorf("action = %q, want accept", r.Action)
		}
	})
}

// ── W11b: machine-verified fixed-point accept ────────────────────────────

func TestW11bFixedPointAccept(t *testing.T) {
	cyc := func(c float64, keys []string) *ConvergenceCycle {
		return &ConvergenceCycle{CorrectnessCount: c, HygieneCount: 0, PolishCount: 0, BlockerKeys: keys}
	}

	t.Run("accepts with machineOverride when tree unchanged + contract passes (even with correctness blockers)", func(t *testing.T) {
		out := AssessConvergence(AssessConvergenceInput{
			Cycles:           []*ConvergenceCycle{cyc(19, []string{"a"}), cyc(5, []string{"b"})},
			MaxCleanupCycles: 1,
			FixedPoint:       &FixedPointEvidence{TreeUnchanged: true, ContractPassed: true},
		})
		if out.Action != "accept" {
			t.Errorf("action = %q, want accept", out.Action)
		}
		if out.MachineOverride == nil || !*out.MachineOverride {
			t.Errorf("machineOverride = %v, want true", out.MachineOverride)
		}
		if !strings.Contains(out.Reason, "machine-verified fixed point") {
			t.Errorf("reason %q missing 'machine-verified fixed point'", out.Reason)
		}
	})

	t.Run("never fires on the FIRST cycle (needs a prior audit to compare against)", func(t *testing.T) {
		out := AssessConvergence(AssessConvergenceInput{
			Cycles:           []*ConvergenceCycle{cyc(3, []string{"a"})},
			MaxCleanupCycles: 1,
			FixedPoint:       &FixedPointEvidence{TreeUnchanged: true, ContractPassed: true},
		})
		if out.Action != "continue" {
			t.Errorf("action = %q, want continue", out.Action)
		}
	})

	t.Run("requires BOTH signals: tree changed ⇒ no override", func(t *testing.T) {
		out := AssessConvergence(AssessConvergenceInput{
			Cycles:           []*ConvergenceCycle{cyc(3, []string{"a"}), cyc(2, []string{"b"})},
			MaxCleanupCycles: 1,
			FixedPoint:       &FixedPointEvidence{TreeUnchanged: false, ContractPassed: true},
		})
		if out.Action != "continue" {
			t.Errorf("action = %q, want continue", out.Action)
		}
		if out.MachineOverride != nil {
			t.Errorf("machineOverride = %v, want undefined", *out.MachineOverride)
		}
	})

	t.Run("requires BOTH signals: failing contract ⇒ no override (existing paths keep the floor)", func(t *testing.T) {
		out := AssessConvergence(AssessConvergenceInput{
			Cycles:           []*ConvergenceCycle{cyc(3, []string{"a"}), cyc(2, []string{"b"})},
			MaxCleanupCycles: 1,
			FixedPoint:       &FixedPointEvidence{TreeUnchanged: true, ContractPassed: false},
		})
		if out.Action != "continue" {
			t.Errorf("action = %q, want continue", out.Action)
		}
		if out.MachineOverride != nil {
			t.Errorf("machineOverride = %v, want undefined", *out.MachineOverride)
		}
	})

	t.Run("absent fixedPoint input behaves exactly as before (stagnation → continue)", func(t *testing.T) {
		out := AssessConvergence(AssessConvergenceInput{
			Cycles:           []*ConvergenceCycle{cyc(2, []string{"x", "y"}), cyc(2, []string{"x", "y"})},
			MaxCleanupCycles: 1,
		})
		if out.Action != "continue" {
			t.Errorf("action = %q, want continue", out.Action)
		}
		if !strings.Contains(out.Reason, "stagnant") {
			t.Errorf("reason %q missing 'stagnant'", out.Reason)
		}
	})
}

// ── port-specific regressions (no TS counterpart) ────────────────────────

// TestPatternNoUnicodeCaseFold pins the divergence Go's `(?i)` would have
// introduced: a JS non-`u` /…/i regex refuses any case fold that crosses the
// ASCII boundary, so KELVIN SIGN and LATIN SMALL LETTER LONG S must NOT match.
func TestPatternNoUnicodeCaseFold(t *testing.T) {
	for _, s := range []string{"notes.baK", "ſcratch", "ſtyle", "TİPO", "ｔypo"} {
		if HYGIENE_PATTERN.Test(s) {
			t.Errorf("HYGIENE_PATTERN matched %q — Go (?i) unicode fold leaked in", s)
		}
		if POLISH_PATTERN.Test(s) {
			t.Errorf("POLISH_PATTERN matched %q — Go (?i) unicode fold leaked in", s)
		}
		if got := ClassifyBlockerSeverity(s); got != "correctness" {
			t.Errorf("ClassifyBlockerSeverity(%q) = %q, want correctness", s, got)
		}
	}
	// The ASCII twins of the same probes must still match.
	if !HYGIENE_PATTERN.Test("notes.bak") || !POLISH_PATTERN.Test("TYPO") {
		t.Error("ASCII case-insensitivity regressed")
	}
}

func TestPatternStringAndSource(t *testing.T) {
	if !strings.HasPrefix(POLISH_PATTERN.String(), "/") || !strings.HasSuffix(POLISH_PATTERN.String(), "/i") {
		t.Errorf("String() = %q", POLISH_PATTERN.String())
	}
	if HYGIENE_PATTERN.Source() != `\.bak\b|\.orig\b|\.tmp\b|leftover|scratch|debug file|backup file|remove\b[^.\n]*\b(temporary|probe|scratch|debug)\b|stray file|dead code|commented[- ]out|left in place` {
		t.Errorf("HYGIENE_PATTERN.Source() drifted: %q", HYGIENE_PATTERN.Source())
	}
}

// TestNaNMaxCleanupCycles cannot be expressed in the JSON fixture format
// (JSON.stringify turns NaN into null), so it is pinned here: `0 < NaN` is
// false in both languages, so the literal rule falls through to accept, and
// `${NaN}` prints "NaN".
func TestNaNMaxCleanupCycles(t *testing.T) {
	r := AssessConvergence(AssessConvergenceInput{
		Cycles:           []*ConvergenceCycle{cyc(cycPartial{hygieneCount: 1, blockerKeys: []string{"h1"}})},
		MaxCleanupCycles: math.NaN(),
	})
	if r.Action != "accept" {
		t.Errorf("action = %q, want accept", r.Action)
	}
	want := "diminishing returns: 1 residual non-correctness blocker(s) after 0 cleanup cycle(s) — accepted with notes"
	if r.Reason != want {
		t.Errorf("reason = %q, want %q", r.Reason, want)
	}
}

// TestBlockerKeyLineIsStringified pins that `${line}` goes through V8 number
// formatting, not Go's %v.
func TestBlockerKeyLineIsStringified(t *testing.T) {
	cases := []struct {
		line float64
		want string
	}{
		{0, "a.ts:0:x"},
		{math.Copysign(0, -1), "a.ts:0:x"}, // JS String(-0) === "0"
		{-12, "a.ts:-12:x"},
		{3.5, "a.ts:3.5:x"},
		{1e21, "a.ts:1e+21:x"},
		{1e-7, "a.ts:1e-7:x"},
		{math.NaN(), "a.ts:NaN:x"},
		{math.Inf(1), "a.ts:Infinity:x"},
	}
	for _, c := range cases {
		got := BlockerKey(&SeverityBlocker{File: strp("a.ts"), Line: nump(c.line), Detail: "x"})
		if got != c.want {
			t.Errorf("line %v: got %q, want %q", c.line, got, c.want)
		}
	}
}
