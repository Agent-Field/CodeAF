// Package retrysched is a bug-for-bug port of src/session/retry.ts — the
// attempt-indexed backoff schedule that decides whether a failed streaming
// turn is retried, with what message, and after how long.
//
// It is the ONLY retry in the system: llm.ts:480 passes
// `maxRetries: input.retries ?? 0` to streamText against an SDK default of 2,
// so neither the AI SDK nor the HTTP client re-issues anything. The Go port
// must hold that line too — no http.Client-level retry may be added under it.
//
// TS wraps these three pure functions in an Effect `Schedule`
// (`policy()`, retry.ts:229-256) which processor.ts:717-742 hangs off
// `Effect.retry`. ENGINE-DESIGN §6.6 replaces the Schedule with a plain Go
// loop, so this package exports the three predicates plus Step — one
// evaluation of the schedule, i.e. exactly the body of the metadata function
// `policy()` builds.
//
// ── seams (things TS gets from the runtime that Go has to be handed) ──────
//
//   - Clock.currentTimeMillis (retry.ts:248) and Date.now() (retry.ts:98)
//     are one injectable nowMS (SetNowMSForTesting). TS reads them through
//     two different APIs; Effect's default Clock IS Date.now(), so the port
//     collapses them and the fixture pins both at once.
//   - process.env["CODEAF_TIMEOUT_RETRY_DELAY_MS"] (retry.ts:45) is read with
//     os.LookupEnv on every delay() call, exactly like TS — no caching, so a
//     test can flip it between calls with t.Setenv.
//   - Date.parse of an HTTP-date (retry.ts:98) has no Go equivalent; see
//     jsDateParse in jsutil.go for the covered grammar and the divergences.
//   - There is NO sleeper seam here. Sleeping is the caller's job
//     (ENGINE-DESIGN §6.6 puts the `select { case <-time.After(wait) }` in the
//     processor loop), because TS also only *returns* a Duration from the
//     Schedule and lets Effect do the waiting.
//
// ── sibling ports ─────────────────────────────────────────────────────────
//
// routerval.go is the `error → *adaptive.JSValue` adapter ENGINE-DESIGN F8
// lists as missing: internal/router/adaptive's six IsLikely* classifiers and
// Register() all take a *JSValue, never a Go error. It lives here because
// retry.ts:39-41 already declares itself a *mirror* of adaptive.ts's
// isLikelyTimeout ("Kept as a local mirror rather than a cross-module import
// to respect the W7c file fence"), so this is the one package that has to
// keep the two in step. internal/llm/orclient is the intended second consumer.
// testdata/fixtures.json runs the REAL adaptive.ts over the same corpus as
// the REAL retry.ts, so every place the mirror and the original disagree is
// pinned as data rather than prose.
//
// ── fidelity notes (deliberate, do not "fix") ─────────────────────────────
//
//   - Attempts are 1-INDEXED. Schedule.fromStepWithMetadata's closure is
//     `let n = 0; … attempt: ++n` (node_modules/effect/src/Schedule.ts:358-369),
//     so the FIRST failure is attempt 1 and its base delay is 2000*2^0 = 2000.
//   - delay() branch 2d (retry.ts:103) caps exponential backoff at
//     RETRY_MAX_DELAY (~24.8 days) while branch 3 (:107) caps it at 30 s. An
//     error that carries responseHeaders but no usable retry-after therefore
//     backs off unboundedly. [BUG-CANDIDATE] — kept.
//   - There is no attempt cap and no time budget. The only terminator is
//     Retryable returning nil. [BUG-CANDIDATE] — kept.
//   - `error.name === "AbortError"` (retry.ts:59) is DEAD on the parse path:
//     message-v2.ts:42 names the aborted error "MessageAbortedError", so a
//     real cancellation is not timeout-retryable, and processor.ts:713-716
//     routes interrupts around the schedule before it is ever consulted.
//     Aborts NEVER enter this package. The check is ported anyway because
//     isTimeoutError is exported and retry.test.ts calls it directly.
//   - delay() line 75 does `TIMEOUT_MESSAGE_RE.test(error.data.message)` with
//     NO typeof guard, while isTimeoutError line 61 has one. When
//     data.message is absent, RegExp.test coerces undefined to the STRING
//     "undefined" (no throw) — so delay() sees "undefined" and isTimeoutError
//     sees a non-string. Both behaviours are reproduced; jsRegExpTest carries
//     the coercion.
//   - retryable() reads `error.data.message.includes("Overloaded")` unguarded
//     (retry.ts:176). An APIError with no data.message throws
//     `TypeError: undefined is not an object (evaluating
//     'error.data.message.includes')` in Bun. The Go twin panics with that
//     exact text; the fixture pins the throw.
//   - `retry-after-ms` is returned RAW (no ceil), `retry-after` seconds are
//     ceil'd after ×1000, and the HTTP-date branch is only reached when
//     Number.parseFloat already returned NaN — i.e. only for strings that do
//     not start with a decimal prefix. Same structural bug as
//     openrouter-rate-headers.ts, and it makes the ES5 ISO-8601 branch of
//     Date.parse unreachable here (see internal/router/ratehead/jsdate.go).
//   - An EMPTY responseHeaders object `{}` is truthy in JS, so it selects the
//     24.8-day-capped branch 2d rather than the 30 s-capped branch 3. Headers
//     is therefore a pointer type: nil means absent, non-nil-and-empty means
//     `{}`.
//   - isOpenAiErrorRetryable (error.ts:30-35, 404 counts as retryable) is
//     gated on providerID.startsWith("openai") and is dead for OpenRouter. It
//     is not in this package because it is not in retry.ts; llm/llmerr owns it.
package retrysched

import (
	"math"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/fixflag"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
)

// ── constants (retry.ts:8-42) ─────────────────────────────────────────────

const (
	// GoUpsellMessage / GoUpsellURL — retry.ts:8-9.
	GoUpsellMessage = "Free usage exceeded, subscribe to Go"
	GoUpsellURL     = "https://codeaf.local/go"

	// RetryInitialDelay is the base of the exponential backoff (retry.ts:26).
	RetryInitialDelay float64 = 2000
	// RetryBackoffFactor is the exponent base (retry.ts:27).
	RetryBackoffFactor float64 = 2
	// RetryMaxDelayNoHeaders caps backoff on the header-less path only
	// (retry.ts:28).
	RetryMaxDelayNoHeaders float64 = 30_000
	// RetryMaxDelay is `max 32-bit signed integer for setTimeout`
	// (retry.ts:29) and is the ONLY cap on the header-carrying path.
	RetryMaxDelay float64 = 2_147_483_647
	// TimeoutRetryDelayDefault short-circuits exponential backoff for
	// timeout-class errors (retry.ts:42): the router has already cooled the
	// stalled model, so the next attempt lands on a different provider.
	TimeoutRetryDelayDefault float64 = 1_000

	// TimeoutRetryDelayEnv is the override knob (retry.ts:45).
	TimeoutRetryDelayEnv = "CODEAF_TIMEOUT_RETRY_DELAY_MS"
)

// RetryReason is `"free_tier_limit" | "account_rate_limit" | (string & {})`
// (retry.ts:10) — an OPEN string enum, so it stays a plain string.
type RetryReason = string

const (
	ReasonFreeTierLimit    RetryReason = "free_tier_limit"
	ReasonAccountRateLimit RetryReason = "account_rate_limit"
)

// RetryAction mirrors `Retryable["action"]` (retry.ts:14-21). Link is `link?:`
// — a pointer with omitempty, the same deliberate exception to the repo-wide
// no-omitempty rule that internal/router/ratehead documents, because
// JSON.stringify drops undefined-valued keys.
type RetryAction struct {
	Reason   RetryReason `json:"reason"`
	Provider string      `json:"provider"`
	Title    string      `json:"title"`
	Message  string      `json:"message"`
	Label    string      `json:"label"`
	Link     *string     `json:"link,omitempty"`
}

// RetryInfo is the TS `Retryable` type (retry.ts:12-22). Renamed because Go
// cannot share one identifier between the type and the Retryable function.
type RetryInfo struct {
	Message string       `json:"message"`
	Action  *RetryAction `json:"action,omitempty"`
}

// ── the error wire shape (`Err`, retry.ts:6) ──────────────────────────────

// Err is `ReturnType<NamedError["toObject"]>` — `{ name: string; data: any }`.
// Every producer is MessageV2.fromError (message-v2.ts:1160-1264), whose six
// arms union to the fields below; `isInstance()` only checks `name`, which is
// why retry.test.ts can hand it plain object literals.
//
// Field order is the APIError schema literal order (message-v2.ts:51-58),
// which is also the order fromError's object literals use — that makes
// ErrToRouterValue's JSON.stringify byte-identical to the TS one.
type Err struct {
	Name string  `json:"name"`
	Data ErrData `json:"data"`
}

// ErrData models `error.data` for the keys retry.ts probes. Every field is a
// pointer because `data` is `any`: OutputLengthError's data is `{}`, so even
// `message` can be absent, and retry.ts:61 typeof-guards exactly that.
type ErrData struct {
	Message         *string  `json:"message,omitempty"`
	StatusCode      *float64 `json:"statusCode,omitempty"`
	IsRetryable     *bool    `json:"isRetryable,omitempty"`
	ResponseHeaders *Headers `json:"responseHeaders,omitempty"`
	ResponseBody    *string  `json:"responseBody,omitempty"`
	Metadata        *Headers `json:"metadata,omitempty"`
}

// IsAPIError mirrors `MessageV2.APIError.isInstance(error)` (message-v2.ts:51
// via named-schema-error.ts) — a bare `name` comparison, nothing more.
func (e Err) IsAPIError() bool { return e.Name == "APIError" }

// APIErrorOrNil is the ternary at retry.ts:243, spelled as a method so the
// processor loop reads like the TS.
func (e Err) APIErrorOrNil() *Err {
	if e.IsAPIError() {
		return &e
	}
	return nil
}

// ── isTimeoutError (retry.ts:58-62) ───────────────────────────────────────

// IsTimeoutError reports whether the classified error is timeout-shaped: an
// AbortError by name, or a timeout-shaped `data.message`. Note the typeof
// guard — a MISSING message is not a match here, whereas Delay's own copy of
// the same regex test coerces it to the string "undefined".
func IsTimeoutError(err Err) bool {
	if err.Name == "AbortError" {
		return true
	}
	msg := err.Data.Message
	return msg != nil && timeoutMessageRE.MatchString(*msg)
}

// ── delay (retry.ts:64-108) ───────────────────────────────────────────────

// capMs is `cap(ms) = Math.min(ms, RETRY_MAX_DELAY)` (retry.ts:64-66).
func capMs(ms float64) float64 { return math.Min(ms, RetryMaxDelay) }

// Delay returns the wait in milliseconds before attempt+1. `err` is the TS
// `error?: MessageV2.APIError` — nil for the non-APIError path — and
// isTimeout is the explicit caller flag (default false in TS).
//
// Return value is a raw float64 and can be NaN (attempt NaN) or -0
// (`retry-after-ms: "-0"`); marshal it through jscompat.JSNumber if it has to
// reach JSON.
func Delay(attempt float64, err *Err, isTimeout bool) float64 {
	// Timeout-class errors short-circuit exponential backoff. Detected either
	// from the caller flag or from a timeout-shaped message on the APIError
	// itself, so delay() is self-classifying (retry.ts:70-77).
	if isTimeout || (err != nil && jsRegExpTest(timeoutMessageRE, err.Data.Message)) {
		return capMs(timeoutRetryDelayMs())
	}
	if err != nil {
		headers := err.Data.ResponseHeaders
		if fixflag.Enabled("CODEAF_GO_FIX_RETRY_DELAY_CAP") && headers != nil && len(headers.Entries()) == 0 {
			headers = nil
		}
		if headers != nil {
			// NOTE: `{}` is truthy, so an empty header map lands here.
			if retryAfterMs := headers.Get("retry-after-ms"); retryAfterMs != "" {
				parsedMs := jsParseFloat(retryAfterMs)
				if !math.IsNaN(parsedMs) {
					return capMs(parsedMs)
				}
			}

			if retryAfter := headers.Get("retry-after"); retryAfter != "" {
				parsedSeconds := jsParseFloat(retryAfter)
				if !math.IsNaN(parsedSeconds) {
					// convert seconds to milliseconds
					return capMs(math.Ceil(parsedSeconds * 1000))
				}
				// Try parsing as HTTP date format
				parsed := jsDateParse(retryAfter) - nowMS()
				if !math.IsNaN(parsed) && parsed > 0 {
					return capMs(math.Ceil(parsed))
				}
			}

			return capMs(RetryInitialDelay * math.Pow(RetryBackoffFactor, attempt-1))
		}
	}

	return capMs(math.Min(RetryInitialDelay*math.Pow(RetryBackoffFactor, attempt-1), RetryMaxDelayNoHeaders))
}

// ── retryable (retry.ts:110-206) ──────────────────────────────────────────

// Retryable classifies a parsed error. A nil result means STOP RETRYING; the
// processor then hands the error to halt() (processor.ts:743).
//
// The ordering is load-bearing: the timeout arm sits AHEAD of the APIError arm
// so a gateway-timeout 5xx takes the 1 s fail-over delay instead of 5xx
// backoff (retry.ts:113-116).
func Retryable(err Err, provider string) *RetryInfo {
	// context overflow errors should not be retried
	if err.Name == "ContextOverflowError" {
		return nil
	}
	// Timeout-shaped errors (stalled/aborted streams) are retryable as their
	// own class: the router has already cooled the stalled model, so the retry
	// lands on a different provider.
	if IsTimeoutError(err) {
		return &RetryInfo{Message: "Provider stalled; failing over to another model"}
	}
	// Phase 8.1: ProviderModelNotFoundError is a transient catalog-load race on
	// parallel dispatches.
	if err.Name == "ProviderModelNotFoundError" {
		return &RetryInfo{Message: "Provider model catalog race; retrying"}
	}
	if err.IsAPIError() {
		data := err.Data
		status := data.StatusCode
		// 5xx errors are transient server failures and should always be
		// retried, even when the provider SDK doesn't explicitly mark them as
		// retryable.
		if !(data.IsRetryable != nil && *data.IsRetryable) && !(status != nil && *status >= 500) {
			return nil
		}
		if data.ResponseBody != nil && strings.Contains(*data.ResponseBody, "FreeUsageLimitError") {
			link := GoUpsellURL
			return &RetryInfo{
				Message: GoUpsellMessage,
				Action: &RetryAction{
					Reason:   ReasonFreeTierLimit,
					Provider: provider,
					Title:    "Free limit reached",
					Message:  "Subscribe to codeaf Go for reliable access to the best open-source models, starting at $5/month.",
					Label:    "subscribe",
					Link:     &link,
				},
			}
		}
		if data.ResponseBody != nil && strings.Contains(*data.ResponseBody, "GoUsageLimitError") {
			body := parseJSONString(data.ResponseBody)
			workspace := jsStr(propOf(propOf(body, "metadata"), "workspace"))
			limitName := jsStr(propOf(propOf(body, "metadata"), "limitName"))
			retryAfter := jsNum(data.ResponseHeaders.Get("retry-after"))
			resetIn := func() string {
				if retryAfter == nil {
					return ""
				}
				seconds := math.Max(0, math.Ceil(*retryAfter))
				days := math.Floor(seconds / 86_400)
				hours := math.Floor(math.Mod(seconds, 86_400) / 3_600)
				minutes := math.Ceil(math.Mod(seconds, 3_600) / 60)
				unit := func(value float64, name string) string {
					plural := "s"
					if value == 1 {
						plural = ""
					}
					return jscompat.FormatNumber(value) + " " + name + plural
				}

				if days > 0 {
					if hours > 0 {
						return unit(days, "day") + " " + unit(hours, "hour")
					}
					return unit(days, "day")
				}
				if hours > 0 {
					if minutes > 0 {
						return unit(hours, "hour") + " " + unit(minutes, "minute")
					}
					return unit(hours, "hour")
				}
				if minutes > 0 {
					return unit(minutes, "minute")
				}
				return "less than a minute"
			}()

			prefix := "Usage limit"
			if limitName != "" {
				prefix = limitName + " usage limit"
			}
			message := prefix + " reached. It will reset in " + resetIn +
				". To continue using this model now, enable usage from your available balance"

			link := "https://codeaf.local/workspace/" + workspace + "/go"
			return &RetryInfo{
				Message: message + " - " + link,
				Action: &RetryAction{
					Reason:   ReasonAccountRateLimit,
					Provider: provider,
					Title:    "Go limit reached",
					Message:  message,
					Label:    "open settings",
					Link:     &link,
				},
			}
		}
		if data.Message == nil {
			if fixflag.Enabled("CODEAF_GO_FIX_RETRY_MESSAGE_GUARD") {
				return nil
			}
			// TS: `error.data.message.includes("Overloaded")` on an absent
			// message. Bun 1.2.23 throws this exact TypeError; the fixture
			// pins it.
			panic("TypeError: undefined is not an object (evaluating 'error.data.message.includes')")
		}
		if strings.Contains(*data.Message, "Overloaded") {
			return &RetryInfo{Message: "Provider is overloaded"}
		}
		return &RetryInfo{Message: *data.Message}
	}

	// Check for rate limit patterns in plain text error messages
	msg := err.Data.Message
	if msg != nil {
		lower := jsLowerCase(*msg)
		if strings.Contains(lower, "rate increased too quickly") ||
			strings.Contains(lower, "rate limit") ||
			strings.Contains(lower, "too many requests") {
			// the ORIGINAL message, not the lowercased one
			return &RetryInfo{Message: *msg}
		}
	}

	json := parseJSONString(err.Data.Message)
	if !jsTruthy(json) || !jsIsObject(json) {
		return nil
	}
	code := ""
	if c := propOf(json, "code"); c != nil && c.Kind == jsKindString {
		code = c.Str
	}

	if jsStrictEqStr(propOf(json, "type"), "error") && jsStrictEqStr(propOf(propOf(json, "error"), "type"), "too_many_requests") {
		return &RetryInfo{Message: "Too Many Requests"}
	}
	if strings.Contains(code, "exhausted") || strings.Contains(code, "unavailable") {
		return &RetryInfo{Message: "Provider is overloaded"}
	}
	if errCode := propOf(propOf(json, "error"), "code"); jsStrictEqStr(propOf(json, "type"), "error") &&
		errCode != nil && errCode.Kind == jsKindString && strings.Contains(errCode.Str, "rate_limit") {
		return &RetryInfo{Message: "Rate Limited"}
	}
	return nil
}

// ── policy (retry.ts:229-256) ─────────────────────────────────────────────

// Decision is the object `policy().set(...)` receives (retry.ts:247-252),
// which processor.ts:721-741 turns into a v2 Retried event plus a
// `{type:"retry", …}` session status.
//
// Wait is the Duration the TS Schedule returns alongside the attempt
// (retry.ts:254); it is not part of the `set` payload and so carries no JSON
// tag. Next is ABSOLUTE epoch milliseconds (`now + wait`).
type Decision struct {
	Attempt float64      `json:"attempt"`
	Message string       `json:"message"`
	Action  *RetryAction `json:"action,omitempty"`
	Next    float64      `json:"next"`

	Wait float64 `json:"-"`
}

// Step is one evaluation of the Schedule built by policy(). `attempt` is
// 1-indexed and PRE-incremented by the caller (ENGINE-DESIGN §6.6). A nil
// result is `Cause.done(meta.attempt)` — the retry loop ends and the caller
// halts with the error.
func Step(attempt float64, err Err, provider string) *Decision {
	retry := Retryable(err, provider)
	if retry == nil {
		return nil
	}
	if fixflag.Enabled("CODEAF_GO_FIX_RETRY_ATTEMPT_CAP") && attempt > 10 {
		return nil
	}
	wait := Delay(attempt, err.APIErrorOrNil(), IsTimeoutError(err))
	now := nowMS()
	return &Decision{
		Attempt: attempt,
		Message: retry.Message,
		Action:  retry.Action,
		Next:    now + wait,
		Wait:    wait,
	}
}
