// Translation of src/session/sprt-decision.test.ts. Subtest names are verbatim.
package sprt_test

import (
	"errors"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/sprt"
)

// Snapshot API. Convention: success = healthy observation. p0 = flaky rate,
// p1 = healthy rate. Defaults alpha = beta = 0.05, so the bounds are
//
//	A = ln(0.95/0.05) = ln(19)  ≈ +2.9444
//	B = ln(0.05/0.95)           ≈ −2.9444
//
// per-success increment  ln(0.95/0.5) = ln(1.9)  ≈ +0.64185
// per-failure increment  ln(0.05/0.5) = ln(0.1)  ≈ −2.302585
func flaky(successes, trials float64) sprt.SprtSnapshot {
	return sprt.SprtSnapshot{Successes: successes, Trials: trials, P0: 0.5, P1: 0.95}
}

func mustDecision(t *testing.T, input sprt.SprtSnapshot) string {
	t.Helper()
	v, err := sprt.SprtDecision(input)
	if err != nil {
		t.Fatalf("sprtDecision threw: %v", err)
	}
	return v
}

func mustNextSampleNeeded(t *testing.T, input sprt.SprtSnapshot) bool {
	t.Helper()
	v, err := sprt.NextSampleNeeded(input)
	if err != nil {
		t.Fatalf("nextSampleNeeded threw: %v", err)
	}
	return v
}

func TestSPRTSnapshotSprtDecision(t *testing.T) {
	t.Run("a short healthy streak is still inconclusive", func(t *testing.T) {
		// 4 successes: llr = 4·0.64185 = 2.5674 < 2.9444 → continue.
		if got := mustDecision(t, flaky(4, 4)); got != "continue" {
			t.Errorf("= %q, want %q", got, "continue")
		}
	})

	t.Run("enough healthy runs accept H1 (healthy)", func(t *testing.T) {
		// 5 successes: llr = 3.2093 ≥ 2.9444 → accept.
		if got := mustDecision(t, flaky(5, 5)); got != "accept" {
			t.Errorf("= %q, want %q", got, "accept")
		}
	})

	t.Run("two straight failures reject H1 (flaky/broken)", func(t *testing.T) {
		// 2 failures: llr = −4.6052 ≤ −2.9444 → reject.
		if got := mustDecision(t, flaky(0, 2)); got != "reject" {
			t.Errorf("= %q, want %q", got, "reject")
		}
	})

	t.Run("one failure alone stays inconclusive", func(t *testing.T) {
		// 1 failure: llr = −2.3026 > −2.9444 → continue.
		if got := mustDecision(t, flaky(0, 1)); got != "continue" {
			t.Errorf("= %q, want %q", got, "continue")
		}
	})

	t.Run("a lone late failure keeps a strong streak open", func(t *testing.T) {
		// 5 successes + 1 failure: llr = 3.2093 − 2.3026 = 0.9067 → continue.
		if got := mustDecision(t, flaky(5, 6)); got != "continue" {
			t.Errorf("= %q, want %q", got, "continue")
		}
	})

	t.Run("successes are clamped to trials", func(t *testing.T) {
		if got, want := mustDecision(t, flaky(99, 3)), mustDecision(t, flaky(3, 3)); got != want {
			t.Errorf("= %q, want %q", got, want)
		}
	})

	t.Run("tighter error rates demand more evidence", func(t *testing.T) {
		// alpha=beta=0.001 ⇒ A = ln(999) ≈ 6.9, so 5 successes no longer accept.
		input := flaky(5, 5)
		input.Alpha = ptr(0.001)
		input.Beta = ptr(0.001)
		if got := mustDecision(t, input); got != "continue" {
			t.Errorf("= %q, want %q", got, "continue")
		}
	})

	t.Run("invalid probabilities throw RangeError", func(t *testing.T) {
		var re *sprt.RangeError

		_, err := sprt.SprtDecision(sprt.SprtSnapshot{Successes: 1, Trials: 1, P0: 0, P1: 0.9})
		if !errors.As(err, &re) {
			t.Errorf("p0=0: err = %v, want *RangeError", err)
		}

		_, err = sprt.SprtDecision(sprt.SprtSnapshot{Successes: 1, Trials: 1, P0: 0.5, P1: 0.5})
		if !errors.As(err, &re) {
			t.Errorf("p1==p0: err = %v, want *RangeError", err)
		}
	})
}

func TestSPRTSnapshotNextSampleNeeded(t *testing.T) {
	t.Run("true only while the verdict is still open", func(t *testing.T) {
		if got := mustNextSampleNeeded(t, flaky(4, 4)); got != true { // continue
			t.Errorf("(4,4) = %v, want true", got)
		}
		if got := mustNextSampleNeeded(t, flaky(5, 5)); got != false { // accept
			t.Errorf("(5,5) = %v, want false", got)
		}
		if got := mustNextSampleNeeded(t, flaky(0, 2)); got != false { // reject
			t.Errorf("(0,2) = %v, want false", got)
		}
	})
}
