package retrysched

// Unit tests for the error → *adaptive.JSValue adapter. The fixture corpus
// already checks the ADAPTER OUTPUT against the real adaptive.ts for 27
// values; these cover the Go-side seams that have no TS counterpart (the four
// interfaces, nil handling, JSON key order) and the mirror-vs-original
// divergences the cross-table exposes.

import (
	"errors"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/router/adaptive"
)

func TestToRouterValueNilIsJSNull(t *testing.T) {
	v := ToRouterValue(nil)
	if v == nil || v.Kind != adaptive.JSNull {
		t.Fatalf("ToRouterValue(nil) = %#v, want JS null", v)
	}
	// null is what llm.ts:425 passes on the success path, and every
	// classifier must be false for it.
	if c := ClassifyForRouter(v); c.RetryableRouteError {
		t.Errorf("null classified as a retryable route error: %+v", c)
	}
}

func TestToRouterValuePlainGoError(t *testing.T) {
	v := ToRouterValue(errors.New("openrouter byte-idle timeout after 60000ms"))
	if v.Kind != adaptive.JSError {
		t.Fatalf("kind = %v, want JSError", v.Kind)
	}
	if v.Message != "openrouter byte-idle timeout after 60000ms" {
		t.Errorf("message = %q", v.Message)
	}
	// An error with no ErrorNamer inherits Error.prototype.name.
	if v.Name != "" {
		t.Errorf("name = %q, want \"\" (inherits \"Error\")", v.Name)
	}
	if !adaptive.IsLikelyTimeout(v) {
		t.Error("orfetch byte-idle message not classified as a timeout")
	}
}

func TestStatusErrorImplementsEverySeam(t *testing.T) {
	inner := &StatusError{Message: "connection reset"}
	e := &StatusError{Message: "upstream failed", Name: "AbortError", Cause: inner}
	status := 429.0
	e.Status = &status

	if e.Error() != "upstream failed" {
		t.Errorf("Error() = %q", e.Error())
	}
	v := ToRouterValue(e)
	if v.Name != "AbortError" {
		t.Errorf("name = %q, want AbortError", v.Name)
	}
	if got, ok := v.Props.Get("status"); !ok || got.Num != 429 {
		t.Errorf("status prop = %#v, ok=%v", got, ok)
	}
	if v.Cause == nil || v.Cause.Message != "connection reset" {
		t.Errorf("cause = %#v", v.Cause)
	}
	// statusCodeOf reads `status`, so 429 alone makes it a rate limit; the
	// AbortError name makes it a timeout; the cause text reaches errorText.
	c := ClassifyForRouter(v)
	if !c.RateLimit || !c.Timeout || !c.RetryableRouteError {
		t.Errorf("classification = %+v", c)
	}
}

func TestNewStatusErrorOmitsNameAndCause(t *testing.T) {
	v := ToRouterValue(NewStatusError("Service Unavailable", 503))
	if v.Name != "" {
		t.Errorf("name = %q, want inherited", v.Name)
	}
	if v.Cause != nil {
		t.Errorf("cause = %#v, want nil", v.Cause)
	}
	if !adaptive.IsLikelyTransientProviderError(v) {
		t.Error("503 not classified as transient")
	}
}

// statusOnlyError has no name and no cause — the minimal producer contract
// internal/llm/orclient can satisfy without importing this package's types.
type statusOnlyError struct{ msg string }

func (e statusOnlyError) Error() string                    { return e.msg }
func (e statusOnlyError) ErrorStatusCode() (float64, bool) { return 502, true }

func TestArbitraryProducerSatisfiesTheInterfaces(t *testing.T) {
	v := ToRouterValue(statusOnlyError{msg: "bad upstream"})
	if got, ok := v.Props.Get("status"); !ok || got.Num != 502 {
		t.Fatalf("status prop = %#v, ok=%v", got, ok)
	}
	if !adaptive.IsLikelyTransientProviderError(v) {
		t.Error("502 producer not classified as transient")
	}
}

// routerValuerError bypasses the Error-shaped path entirely.
type routerValuerError struct{ v *adaptive.JSValue }

func (e routerValuerError) Error() string                  { return "opaque" }
func (e routerValuerError) RouterValue() *adaptive.JSValue { return e.v }

func TestRouterValuerWins(t *testing.T) {
	payload := JSONToRouterValue([]byte(`{"code":520,"message":"Provider returned error","error_type":"unmapped"}`))
	v := ToRouterValue(routerValuerError{v: payload})
	if v != payload {
		t.Fatalf("RouterValuer output was not used verbatim")
	}
	if !adaptive.IsLikelyTransientProviderError(v) {
		t.Error(`the literal 'error_type":"unmapped"' match was lost`)
	}
	// A RouterValuer that returns nil degrades to JS null rather than to a
	// Go nil the classifiers would have to guard.
	if got := ToRouterValue(routerValuerError{v: nil}); got.Kind != adaptive.JSNull {
		t.Errorf("nil RouterValue = %#v, want JS null", got)
	}
}

// TestErrorCauseIsNotErrorsUnwrap pins the deliberate choice documented on
// ErrorCauser: fmt.Errorf("%w") already folds the inner message into Error(),
// so following errors.Unwrap would duplicate the text the classifiers
// substring-match.
func TestErrorCauseIsNotErrorsUnwrap(t *testing.T) {
	wrapped := errors.Join(errors.New("outer"), errors.New("deadline exceeded"))
	v := ToRouterValue(wrapped)
	if v.Cause != nil {
		t.Errorf("cause = %#v, want nil — errors.Unwrap must not be followed", v.Cause)
	}
	// The text is still reachable, because Error() already contains it.
	if !adaptive.IsLikelyTimeout(v) {
		t.Error("joined error text lost")
	}
}

func TestJSONToRouterValuePreservesKeyOrder(t *testing.T) {
	raw := []byte(`{"z":1,"a":2,"m":{"b":3,"a":4}}`)
	v := JSONToRouterValue(raw)
	if got := v.Props.Keys(); len(got) != 3 || got[0] != "z" || got[1] != "a" || got[2] != "m" {
		t.Fatalf("top-level key order = %v, want [z a m]", got)
	}
	inner, _ := v.Props.Get("m")
	if got := inner.Props.Keys(); len(got) != 2 || got[0] != "b" || got[1] != "a" {
		t.Fatalf("nested key order = %v, want [b a]", got)
	}
}

func TestJSONToRouterValueRejectsGarbage(t *testing.T) {
	for _, raw := range []string{"", "{not json", `{"a":1} trailing`, "undefined"} {
		if v := JSONToRouterValue([]byte(raw)); v != nil {
			t.Errorf("JSONToRouterValue(%q) = %#v, want nil (JS undefined)", raw, v)
		}
	}
}

// TestMirrorDivergesFromOriginal is the point of keeping both halves in one
// package. retry.ts:39-41 calls TIMEOUT_MESSAGE_RE "a mirror of router
// adaptive.ts isLikelyTimeout"; it is not an exact one, and these are the two
// shapes where they disagree. Both directions are pinned so a future edit to
// either side shows up as a test change, not as silent drift.
func TestMirrorDivergesFromOriginal(t *testing.T) {
	body := "upstream gateway timeout"
	msg := "ok"
	retryableFlag := false

	// (1) retry.ts only reads data.message; adaptive.ts JSON.stringifies the
	// whole object, so timeout text anywhere else counts.
	inBodyOnly := Err{Name: "APIError", Data: ErrData{
		Message:      &msg,
		IsRetryable:  &retryableFlag,
		ResponseBody: &body,
	}}
	if IsTimeoutError(inBodyOnly) {
		t.Error("retry mirror matched timeout text outside data.message")
	}
	if !adaptive.IsLikelyTimeout(ErrToRouterValue(inBodyOnly)) {
		t.Error("adaptive original missed timeout text in responseBody")
	}

	// (2) retry.ts probes the OUTER name; adaptive.ts probes it too, so
	// "AbortError" agrees — but MessageAbortedError, the name a real
	// cancellation actually carries (message-v2.ts:42), matches NEITHER. This
	// is the second line of defence behind processor.ts:713-716.
	abortMsg := "Aborted"
	realAbort := Err{Name: "MessageAbortedError", Data: ErrData{Message: &abortMsg}}
	if IsTimeoutError(realAbort) {
		t.Error("retry mirror treated a real cancellation as a timeout")
	}
	if adaptive.IsLikelyTimeout(ErrToRouterValue(realAbort)) {
		t.Error("adaptive original treated a real cancellation as a timeout")
	}
}

// TestErrToRouterValueDropsAbsentKeys pins that a nil field is an ABSENT
// property, matching JSON.stringify dropping undefined-valued keys — the
// difference is observable, because an extra `"isRetryable":false` would
// change the text the classifiers substring-match.
func TestErrToRouterValueDropsAbsentKeys(t *testing.T) {
	v := ErrToRouterValue(Err{Name: "MessageOutputLengthError"})
	data, ok := v.Props.Get("data")
	if !ok {
		t.Fatal("no data key")
	}
	if n := data.Props.Len(); n != 0 {
		t.Errorf("data has %d keys, want 0: %v", n, data.Props.Keys())
	}
	if got := v.Props.Keys(); len(got) != 2 || got[0] != "name" || got[1] != "data" {
		t.Errorf("key order = %v, want [name data]", got)
	}
}

func TestHeadersDistinguishAbsentFromEmpty(t *testing.T) {
	var absent *Headers
	empty := NewHeaders()
	if absent.Get("retry-after") != "" || empty.Get("retry-after") != "" {
		t.Fatal("Get on absent/empty headers must be \"\"")
	}
	// The difference shows up in Delay: `{}` is truthy in JS.
	msg := "Internal Server Error"
	flag := true
	withEmpty := Err{Name: "APIError", Data: ErrData{Message: &msg, IsRetryable: &flag, ResponseHeaders: empty}}
	withNone := Err{Name: "APIError", Data: ErrData{Message: &msg, IsRetryable: &flag, ResponseHeaders: absent}}
	if Delay(10, &withEmpty, false) == Delay(10, &withNone, false) {
		t.Error("empty {} headers must escape the 30s cap that the absent case is subject to")
	}
}
