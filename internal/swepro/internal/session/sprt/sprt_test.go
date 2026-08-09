// Translation of src/session/sprt.test.ts. Subtest names are verbatim.
package sprt_test

import (
	"math"
	"testing"

	"github.com/Agent-Field/swe-pro-go/internal/session/sprt"
)

func ptr(f float64) *float64 { return &f }

// mustCreate is `createSprt(...)` in a context where the TS test does not
// expect a throw.
func mustCreate(t *testing.T, options sprt.SprtOptions) *sprt.Sprt {
	t.Helper()
	s, err := sprt.CreateSprt(options)
	if err != nil {
		t.Fatalf("createSprt threw: %v", err)
	}
	return s
}

// closeTo is bun/jest `toBeCloseTo(expected, precision)`:
// |expected - actual| < 10**-precision / 2.
func closeTo(t *testing.T, actual, expected float64, precision int) {
	t.Helper()
	if !(math.Abs(expected-actual) < math.Pow(10, -float64(precision))/2) {
		t.Errorf("expected %v to be close to %v (precision %d)", actual, expected, precision)
	}
}

func TestSPRT(t *testing.T) {
	t.Run("deterministic preset accepts one pass in one trial", func(t *testing.T) {
		s := mustCreate(t, *sprt.SprtPlanFor(sprt.PlanKindDeterministic))

		if got := s.Observe(true); got != sprt.ObservationAcceptPass {
			t.Errorf("observe(true) = %q, want %q", got, sprt.ObservationAcceptPass)
		}
		if got := s.Trials(); got != 1 {
			t.Errorf("trials() = %v, want 1", got)
		}
		if got := s.ForceDecision(); got != "pass" {
			t.Errorf("forceDecision() = %q, want %q", got, "pass")
		}
	})

	t.Run("alternating browser observations continue, then terminally fail", func(t *testing.T) {
		s := mustCreate(t, *sprt.SprtPlanFor(sprt.PlanKindBrowser))

		// pass: ln(0.5 / 0.92) = -0.6098
		if got := s.Observe(true); got != sprt.ObservationContinue {
			t.Errorf("observe(true) = %q, want %q", got, sprt.ObservationContinue)
		}
		closeTo(t, s.Llr(), -0.6098, 4)
		// fail: -0.6098 + ln(0.5 / 0.08) = 1.2228
		if got := s.Observe(false); got != sprt.ObservationContinue {
			t.Errorf("observe(false) = %q, want %q", got, sprt.ObservationContinue)
		}
		closeTo(t, s.Llr(), 1.2228, 4)
		if got := s.Observe(true); got != sprt.ObservationContinue { // 0.6130
			t.Errorf("observe(true) = %q, want %q", got, sprt.ObservationContinue)
		}
		if got := s.Observe(false); got != sprt.ObservationContinue { // 2.4456
			t.Errorf("observe(false) = %q, want %q", got, sprt.ObservationContinue)
		}
		if got := s.Observe(true); got != sprt.ObservationContinue { // 1.8359
			t.Errorf("observe(true) = %q, want %q", got, sprt.ObservationContinue)
		}
		if got := s.Observe(false); got != sprt.ObservationContinue { // 3.6684
			t.Errorf("observe(false) = %q, want %q", got, sprt.ObservationContinue)
		}
		if got := s.Observe(true); got != sprt.ObservationContinue { // 3.0587
			t.Errorf("observe(true) = %q, want %q", got, sprt.ObservationContinue)
		}
		// 4.8913 >= ln(0.95 / 0.01) = 4.5539
		if got := s.Observe(false); got != sprt.ObservationAcceptFail {
			t.Errorf("observe(false) = %q, want %q", got, sprt.ObservationAcceptFail)
		}
		if got := s.Trials(); got != 8 {
			t.Errorf("trials() = %v, want 8", got)
		}
	})

	t.Run("all failures accept fail within three trials", func(t *testing.T) {
		s := mustCreate(t, *sprt.SprtPlanFor(sprt.PlanKindBrowser))

		if got := s.Observe(false); got != sprt.ObservationContinue { // ln(0.5 / 0.08) = 1.8326
			t.Errorf("observe(false) = %q, want %q", got, sprt.ObservationContinue)
		}
		if got := s.Observe(false); got != sprt.ObservationContinue { // 3.6652
			t.Errorf("observe(false) = %q, want %q", got, sprt.ObservationContinue)
		}
		if got := s.Observe(false); got != sprt.ObservationAcceptFail { // 5.4977 >= 4.5539
			t.Errorf("observe(false) = %q, want %q", got, sprt.ObservationAcceptFail)
		}
		if got := s.Trials(); got != 3 {
			t.Errorf("trials() = %v, want 3", got)
		}
	})

	t.Run("maxTrials forces the sign-based decision", func(t *testing.T) {
		pass := mustCreate(t, sprt.SprtOptions{P0: 0.1, P1: 0.5, MaxTrials: ptr(2)})
		if got := pass.Observe(true); got != sprt.ObservationContinue {
			t.Errorf("pass.observe(true) = %q, want %q", got, sprt.ObservationContinue)
		}
		if got := pass.Observe(true); got != sprt.ObservationAcceptPass {
			t.Errorf("pass.observe(true) = %q, want %q", got, sprt.ObservationAcceptPass)
		}
		if got := pass.ForceDecision(); got != "pass" {
			t.Errorf("pass.forceDecision() = %q, want %q", got, "pass")
		}

		fail := mustCreate(t, sprt.SprtOptions{P0: 0.1, P1: 0.5, MaxTrials: ptr(2)})
		if got := fail.Observe(false); got != sprt.ObservationContinue {
			t.Errorf("fail.observe(false) = %q, want %q", got, sprt.ObservationContinue)
		}
		if got := fail.Observe(true); got != sprt.ObservationAcceptFail {
			t.Errorf("fail.observe(true) = %q, want %q", got, sprt.ObservationAcceptFail)
		}
		if got := fail.ForceDecision(); got != "fail" {
			t.Errorf("fail.forceDecision() = %q, want %q", got, "fail")
		}
	})

	t.Run("terminal observations are stable and accessors expose state", func(t *testing.T) {
		s := mustCreate(t, sprt.SprtOptions{P0: 0.05, P1: 0.5, MaxTrials: ptr(4)})

		if got := s.Llr(); got != 0 {
			t.Errorf("llr() = %v, want 0", got)
		}
		if got := s.Trials(); got != 0 {
			t.Errorf("trials() = %v, want 0", got)
		}
		if got := s.Observe(false); got != sprt.ObservationContinue {
			t.Errorf("observe(false) = %q, want %q", got, sprt.ObservationContinue)
		}
		if got := s.Trials(); got != 1 {
			t.Errorf("trials() = %v, want 1", got)
		}
		afterFirst := s.Llr()
		if got := s.Observe(true); got != sprt.ObservationContinue {
			t.Errorf("observe(true) = %q, want %q", got, sprt.ObservationContinue)
		}
		if got := s.Trials(); got != 2 {
			t.Errorf("trials() = %v, want 2", got)
		}
		if s.Llr() == afterFirst {
			t.Errorf("llr() = %v, want it to differ from %v", s.Llr(), afterFirst)
		}
	})
}
