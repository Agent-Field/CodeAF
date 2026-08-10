package retrysched

// Verbatim translation of src/session/retry.test.ts (bun:test). One Go
// subtest per TS `test()`, in source order, with the TS assertions kept
// literally — including the ones that are redundant next to the fixture
// replay, because a hand-written test that disagrees with the fixtures is the
// signal that the fixture corpus drifted.
//
// The TS beforeEach/afterEach pair saves and deletes
// CODEAF_TIMEOUT_RETRY_DELAY_MS around every case; t.Setenv gives the same
// save/restore for free, and unsetEnv does the delete.

import (
	"math"
	"os"
	"strings"
	"testing"
)

// apiError builds the wire-shape APIError object (name + data) that the SDK
// produces via toObject(); isInstance() only checks the `name` field, so a
// plain literal is a faithful stand-in. Mirrors the helper at
// retry.test.ts:16-21, including its `isRetryable: false` default.
func apiError(message string, mutate ...func(*ErrData)) Err {
	isRetryable := false
	d := ErrData{Message: &message, IsRetryable: &isRetryable}
	for _, m := range mutate {
		m(&d)
	}
	return Err{Name: "APIError", Data: d}
}

func withStatus(code float64) func(*ErrData) {
	return func(d *ErrData) { d.StatusCode = &code }
}

func withRetryable(v bool) func(*ErrData) {
	return func(d *ErrData) { d.IsRetryable = &v }
}

func withHeaders(pairs ...string) func(*ErrData) {
	return func(d *ErrData) { d.ResponseHeaders = NewHeaders(pairs...) }
}

// unsetEnv is `delete process.env.CODEAF_TIMEOUT_RETRY_DELAY_MS` with t.Setenv's
// automatic restore.
func unsetEnv(t *testing.T) {
	t.Helper()
	t.Setenv(TimeoutRetryDelayEnv, "")
	os.Unsetenv(TimeoutRetryDelayEnv)
}

// ── describe("isTimeoutError") ───────────────────────────────────────────

func TestIsTimeoutError(t *testing.T) {
	t.Run("matches AbortError by name", func(t *testing.T) {
		unsetEnv(t)
		if got := IsTimeoutError(Err{Name: "AbortError"}); got != true {
			t.Errorf("IsTimeoutError = %v, want true", got)
		}
	})

	t.Run("matches timeout-shaped messages", func(t *testing.T) {
		unsetEnv(t)
		for _, message := range []string{
			"openrouter first-content timeout after 45000ms",
			"openrouter content-idle timeout: no data: chunks",
			"request timed out",
			"context deadline exceeded",
		} {
			if got := IsTimeoutError(apiError(message)); got != true {
				t.Errorf("IsTimeoutError(%q) = %v, want true", message, got)
			}
		}
	})

	t.Run("does not match rate-limit or generic errors", func(t *testing.T) {
		unsetEnv(t)
		if got := IsTimeoutError(apiError("rate limit exceeded")); got != false {
			t.Errorf("IsTimeoutError(rate limit) = %v, want false", got)
		}
		if got := IsTimeoutError(apiError("Internal Server Error")); got != false {
			t.Errorf("IsTimeoutError(500) = %v, want false", got)
		}
	})
}

// ── describe("delay: timeout-class errors use a small constant, not backoff") ──

func TestDelayTimeoutClass(t *testing.T) {
	t.Run("timeout-shaped APIError returns ~1s regardless of attempt", func(t *testing.T) {
		unsetEnv(t)
		err := apiError("openrouter first-content timeout after 45000ms", withRetryable(true))
		if got := Delay(1, &err, false); got != TimeoutRetryDelayDefault {
			t.Errorf("Delay(1) = %v, want %v", got, TimeoutRetryDelayDefault)
		}
		// Attempt 4 would be 2000 * 2^3 = 16000ms under exponential backoff;
		// the timeout class ignores the attempt number entirely.
		if got := Delay(4, &err, false); got != TimeoutRetryDelayDefault {
			t.Errorf("Delay(4) = %v, want %v", got, TimeoutRetryDelayDefault)
		}
		if TimeoutRetryDelayDefault != 1_000 {
			t.Errorf("TimeoutRetryDelayDefault = %v, want 1000", TimeoutRetryDelayDefault)
		}
	})

	t.Run("explicit isTimeout flag (non-APIError timeout) returns the constant", func(t *testing.T) {
		unsetEnv(t)
		if got := Delay(3, nil, true); got != TimeoutRetryDelayDefault {
			t.Errorf("Delay(3, nil, true) = %v, want %v", got, TimeoutRetryDelayDefault)
		}
	})

	t.Run("CODEAF_TIMEOUT_RETRY_DELAY_MS overrides the constant", func(t *testing.T) {
		unsetEnv(t)
		t.Setenv(TimeoutRetryDelayEnv, "2500")
		err := apiError("deadline exceeded", withRetryable(true))
		if got := Delay(1, &err, false); got != 2500 {
			t.Errorf("Delay = %v, want 2500", got)
		}
	})

	t.Run("out-of-range override falls back to default", func(t *testing.T) {
		unsetEnv(t)
		t.Setenv(TimeoutRetryDelayEnv, "0")
		if got := Delay(1, nil, true); got != TimeoutRetryDelayDefault {
			t.Errorf("Delay with env=0 = %v, want %v", got, TimeoutRetryDelayDefault)
		}
		t.Setenv(TimeoutRetryDelayEnv, "not-a-number")
		if got := Delay(1, nil, true); got != TimeoutRetryDelayDefault {
			t.Errorf("Delay with env=not-a-number = %v, want %v", got, TimeoutRetryDelayDefault)
		}
	})
}

// ── describe("delay: rate-limit and 5xx behavior unchanged") ─────────────

func TestDelayRateLimitAnd5xx(t *testing.T) {
	t.Run("retry-after-ms header wins verbatim", func(t *testing.T) {
		unsetEnv(t)
		err := apiError("rate limit", withRetryable(true), withHeaders("retry-after-ms", "5000"))
		if got := Delay(1, &err, false); got != 5000 {
			t.Errorf("Delay = %v, want 5000", got)
		}
	})

	t.Run("retry-after seconds header converts to ms", func(t *testing.T) {
		unsetEnv(t)
		err := apiError("too many requests", withRetryable(true), withHeaders("retry-after", "3"))
		if got := Delay(1, &err, false); got != 3000 {
			t.Errorf("Delay = %v, want 3000", got)
		}
	})

	t.Run("5xx without headers uses exponential backoff", func(t *testing.T) {
		unsetEnv(t)
		err := apiError("Internal Server Error", withStatus(503), withRetryable(false))
		if got := Delay(1, &err, false); got != RetryInitialDelay {
			t.Errorf("Delay(1) = %v, want %v", got, RetryInitialDelay)
		}
		if got := Delay(2, &err, false); got != RetryInitialDelay*2 {
			t.Errorf("Delay(2) = %v, want %v", got, RetryInitialDelay*2)
		}
		if got := Delay(3, &err, false); got != RetryInitialDelay*4 {
			t.Errorf("Delay(3) = %v, want %v", got, RetryInitialDelay*4)
		}
	})

	t.Run("no-error path still uses exponential backoff", func(t *testing.T) {
		unsetEnv(t)
		if got := Delay(1, nil, false); got != RetryInitialDelay {
			t.Errorf("Delay(1) = %v, want %v", got, RetryInitialDelay)
		}
		if got := Delay(2, nil, false); got != RetryInitialDelay*2 {
			t.Errorf("Delay(2) = %v, want %v", got, RetryInitialDelay*2)
		}
	})
}

// ── describe("retryable: timeout class is retryable, others unchanged") ──

func TestRetryableClasses(t *testing.T) {
	t.Run("timeout-shaped APIError is retryable", func(t *testing.T) {
		unsetEnv(t)
		err := apiError("openrouter content-idle timeout", withRetryable(true))
		result := Retryable(err, "openrouter")
		if result == nil {
			t.Fatal("Retryable = nil, want defined")
		}
		if !strings.Contains(result.Message, "failing over") {
			t.Errorf("message %q does not contain %q", result.Message, "failing over")
		}
	})

	t.Run("AbortError is retryable as a timeout", func(t *testing.T) {
		unsetEnv(t)
		result := Retryable(Err{Name: "AbortError"}, "openrouter")
		if result == nil {
			t.Fatal("Retryable = nil, want defined")
		}
		if !strings.Contains(result.Message, "failing over") {
			t.Errorf("message %q does not contain %q", result.Message, "failing over")
		}
	})

	t.Run("plain rate-limit message stays retryable with its own message", func(t *testing.T) {
		unsetEnv(t)
		err := apiError("rate limit exceeded", withRetryable(true))
		result := Retryable(err, "openrouter")
		if result == nil || result.Message != "rate limit exceeded" {
			t.Errorf("Retryable = %+v, want message %q", result, "rate limit exceeded")
		}
	})

	t.Run("non-retryable non-timeout error stays non-retryable", func(t *testing.T) {
		unsetEnv(t)
		err := apiError("Bad Request", withStatus(400), withRetryable(false))
		if result := Retryable(err, "openrouter"); result != nil {
			t.Errorf("Retryable = %+v, want nil", result)
		}
	})
}

// ── beyond retry.test.ts: the port's own invariants ──────────────────────

// TestAbortsNeverRetry pins ENGINE-DESIGN §6.5's "aborts NEVER enter the
// schedule" as executable fact. The real cancellation error is
// MessageAbortedError (message-v2.ts:42), NOT AbortError, and its message
// "Aborted" matches no timeout or rate-limit pattern, so even if the
// interrupt filter at processor.ts:713-716 were removed the schedule would
// still refuse it.
func TestAbortsNeverRetry(t *testing.T) {
	unsetEnv(t)
	msg := "Aborted"
	aborted := Err{Name: "MessageAbortedError", Data: ErrData{Message: &msg}}

	if IsTimeoutError(aborted) {
		t.Error("MessageAbortedError classified as a timeout")
	}
	if got := Retryable(aborted, "openrouter"); got != nil {
		t.Errorf("Retryable(MessageAbortedError) = %+v, want nil", got)
	}
	if got := Step(1, aborted, "openrouter"); got != nil {
		t.Errorf("Step(MessageAbortedError) = %+v, want nil", got)
	}
}

// TestContextOverflowNeverRetries pins retry.ts:112 — the first arm, ahead of
// even the timeout classification, so an overflow whose message happens to
// contain "timeout" still stops.
func TestContextOverflowNeverRetries(t *testing.T) {
	unsetEnv(t)
	msg := "context length exceeded after timeout"
	overflow := Err{Name: "ContextOverflowError", Data: ErrData{Message: &msg}}
	if got := Retryable(overflow, "openrouter"); got != nil {
		t.Errorf("Retryable(ContextOverflowError) = %+v, want nil", got)
	}
	// …while the same message on any other name IS a timeout, proving the
	// ordering is what stopped it.
	other := Err{Name: "UnknownError", Data: ErrData{Message: &msg}}
	if got := Retryable(other, "openrouter"); got == nil {
		t.Error("Retryable(UnknownError with timeout message) = nil, want defined")
	}
}

// TestEmptyHeadersObjectEscapesThe30sCap pins the [BUG-CANDIDATE] asymmetry
// between delay() branch 2d (capped at RETRY_MAX_DELAY, ~24.8 days) and
// branch 3 (capped at 30 s): an empty `{}` is truthy, so merely HAVING a
// responseHeaders property removes the 30 s ceiling.
func TestEmptyHeadersObjectEscapesThe30sCap(t *testing.T) {
	unsetEnv(t)
	withHdrs := apiError("Internal Server Error", withStatus(500), withRetryable(true), withHeaders())
	without := apiError("Internal Server Error", withStatus(500), withRetryable(true))

	if got := Delay(10, &withHdrs, false); got != 2000*math.Pow(2, 9) {
		t.Errorf("Delay with {} headers = %v, want %v", got, 2000*math.Pow(2, 9))
	}
	if got := Delay(10, &without, false); got != RetryMaxDelayNoHeaders {
		t.Errorf("Delay without headers = %v, want %v", got, RetryMaxDelayNoHeaders)
	}
	// …and the only ceiling on the header path is the 32-bit setTimeout limit.
	if got := Delay(100, &withHdrs, false); got != RetryMaxDelay {
		t.Errorf("Delay(100) with {} headers = %v, want %v", got, RetryMaxDelay)
	}
}

// TestStepIsOneIndexed pins ENGINE-DESIGN §6.2: Effect's
// Schedule.fromStepWithMetadata pre-increments from 0, so the FIRST failure is
// attempt 1 and its base delay is 2000 * 2^0.
func TestStepIsOneIndexed(t *testing.T) {
	unsetEnv(t)
	restore := SetNowMSForTesting(func() float64 { return 1_000_000 })
	defer restore()

	err := apiError("Internal Server Error", withStatus(503), withRetryable(false))
	for attempt, want := range map[float64]float64{1: 2000, 2: 4000, 3: 8000, 4: 16000, 5: 30000} {
		d := Step(attempt, err, "openrouter")
		if d == nil {
			t.Fatalf("Step(%v) = nil", attempt)
		}
		if d.Wait != want {
			t.Errorf("Step(%v).Wait = %v, want %v", attempt, d.Wait, want)
		}
		if d.Next != 1_000_000+want {
			t.Errorf("Step(%v).Next = %v, want %v", attempt, d.Next, 1_000_000+want)
		}
		if d.Attempt != attempt {
			t.Errorf("Step(%v).Attempt = %v", attempt, d.Attempt)
		}
	}
}

// TestNoAttemptCap pins the second [BUG-CANDIDATE]: nothing in this package
// ever gives up on a still-retryable error, no matter how many attempts.
func TestNoAttemptCap(t *testing.T) {
	unsetEnv(t)
	err := apiError("Too Many Requests", withStatus(429), withRetryable(true))
	for _, attempt := range []float64{1, 10, 100, 1e6} {
		if d := Step(attempt, err, "openrouter"); d == nil {
			t.Errorf("Step(%v) = nil — an attempt cap appeared", attempt)
		}
	}
}

// TestDelayMissingMessageIsCoercedNotGuarded pins the asymmetry between
// retry.ts:75 (no typeof guard, RegExp.test coerces undefined to "undefined")
// and retry.ts:61 (typeof guard).
func TestDelayMissingMessageIsCoercedNotGuarded(t *testing.T) {
	unsetEnv(t)
	retryable := true
	err := Err{Name: "APIError", Data: ErrData{IsRetryable: &retryable}}

	if IsTimeoutError(err) {
		t.Error("IsTimeoutError with absent message = true, want false (typeof guard)")
	}
	// "undefined" matches none of timeout/timed out/deadline exceeded, so the
	// coercion is observable only as "did not short-circuit".
	if got := Delay(2, &err, false); got != 4000 {
		t.Errorf("Delay = %v, want 4000 (exponential, not the 1s timeout constant)", got)
	}
}

// TestRetryableMissingMessagePanicsLikeTS pins the TypeError at retry.ts:176.
func TestRetryableMissingMessagePanicsLikeTS(t *testing.T) {
	unsetEnv(t)
	retryable := true
	err := Err{Name: "APIError", Data: ErrData{IsRetryable: &retryable}}
	defer func() {
		r := recover()
		want := "TypeError: undefined is not an object (evaluating 'error.data.message.includes')"
		if r != want {
			t.Errorf("panic = %v, want %q", r, want)
		}
	}()
	Retryable(err, "openrouter")
	t.Error("Retryable did not panic")
}

// TestRetryAttemptCapFlagOn verifies CODEAF_GO_FIX_RETRY_ATTEMPT_CAP: after 10
// attempts, Step returns nil even though the error is still retryable.
func TestRetryAttemptCapFlagOn(t *testing.T) {
	t.Setenv("CODEAF_GO_FIX_RETRY_ATTEMPT_CAP", "1")
	unsetEnv(t)
	err := apiError("Too Many Requests", withStatus(429), withRetryable(true))
	// Attempts 1-10 still succeed.
	for _, attempt := range []float64{1, 5, 10} {
		if d := Step(attempt, err, "openrouter"); d == nil {
			t.Errorf("Step(%v) = nil, want non-nil", attempt)
		}
	}
	// Attempt 11 is stopped.
	if d := Step(11, err, "openrouter"); d != nil {
		t.Errorf("Step(11) = %+v, want nil (attempt cap)", d)
	}
}

// TestRetryDelayCapFlagOn verifies CODEAF_GO_FIX_RETRY_DELAY_CAP: an empty
// (non-nil, zero-length) header map takes the 30 s cap, same as absent headers.
func TestRetryDelayCapFlagOn(t *testing.T) {
	t.Setenv("CODEAF_GO_FIX_RETRY_DELAY_CAP", "1")
	unsetEnv(t)
	withEmpty := apiError("Internal Server Error", withStatus(500), withRetryable(true), withHeaders())
	// At a high attempt, delay must be capped at RetryMaxDelayNoHeaders (30s).
	if got := Delay(100, &withEmpty, false); got > RetryMaxDelayNoHeaders {
		t.Errorf("Delay(100) with {} headers = %v, want <= %v", got, RetryMaxDelayNoHeaders)
	}
}

// TestRetryMessageGuardFlagOn verifies CODEAF_GO_FIX_RETRY_MESSAGE_GUARD: a
// missing message is treated as not-retryable instead of panicking.
func TestRetryMessageGuardFlagOn(t *testing.T) {
	t.Setenv("CODEAF_GO_FIX_RETRY_MESSAGE_GUARD", "1")
	unsetEnv(t)
	retryable := true
	err := Err{Name: "APIError", Data: ErrData{IsRetryable: &retryable}}
	// Must not panic; must return nil.
	var result *RetryInfo
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("Retryable panicked with flag on: %v", r)
			}
		}()
		result = Retryable(err, "openrouter")
	}()
	if result != nil {
		t.Errorf("Retryable = %+v, want nil (missing message not retryable)", result)
	}
}
