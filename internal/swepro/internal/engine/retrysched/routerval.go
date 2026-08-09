package retrysched

// error → *adaptive.JSValue, the adapter ENGINE-DESIGN §0 F8 lists as missing.
//
// All six router classifiers (adaptive.IsLikelyTimeout, IsLikelyRateLimit,
// IsLikelyProviderIncompatible, IsLikelyStructuredFailure,
// IsLikelyTransientProviderError, IsRetryableRouteError) and
// adaptive.Register(choice, elapsedSeconds, completionTokens, err *JSValue)
// take a *JSValue, never a Go error, because adaptive.ts probes `unknown` with
// `instanceof Error`, bracket property access and JSON.stringify. Go has no
// `unknown`, so somebody has to build the JS value. That somebody is here.
//
// WHY HERE and not in a routerbridge package: retry.ts:39-41 says outright
// that its TIMEOUT_MESSAGE_RE is "a mirror of router adaptive.ts
// isLikelyTimeout … kept as a local mirror rather than a cross-module import
// to respect the W7c file fence". A mirror that nobody diffs rots. This
// package therefore owns both halves and testdata/fixtures.json runs the REAL
// adaptive.ts and the REAL retry.ts over one shared corpus, so every
// disagreement between them is recorded data.
//
// WHAT THE ROUTER ACTUALLY SEES in TS: llm.ts:422 calls
// `registerRoute(0, error?.error ?? error)` from streamText's onError — the
// RAW thrown value (an APICallError instance, or OpenRouter's decoded
// `{code, message, metadata}` JSON body), never the MessageV2.fromError
// output. ToRouterValue models the first, JSONToRouterValue the second.
// ErrToRouterValue models a third thing TS never does (handing the *parsed*
// Err to the router); it exists so the mirror can be diffed and is marked
// clearly as port-provided, not ported.

import (
	"errors"
	"strings"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
	"github.com/Agent-Field/swe-pro-go/internal/router/adaptive"
)

// ── seam interfaces ───────────────────────────────────────────────────────
//
// A Go error carries exactly one observable, Error(); a JS Error carries
// `message`, `name`, `cause` and arbitrary own properties, and the classifiers
// read all four. These interfaces let a producer (internal/llm/orclient, the
// intended second consumer) declare the extra ones without this package
// importing it.

// RouterValuer hands the classifiers a fully-formed JS value. Implement it
// when the thrown thing is not Error-shaped at all — an OpenRouter JSON error
// body, say — and nothing here needs to guess.
type RouterValuer interface {
	RouterValue() *adaptive.JSValue
}

// ErrorNamer supplies `err.name`. Without it the value inherits
// Error.prototype.name, i.e. "Error". Only "AbortError" is behaviourally
// special (adaptive.IsLikelyTimeout probes it by name).
type ErrorNamer interface {
	ErrorName() string
}

// ErrorStatusCoder supplies the numeric `status` own-property that
// adaptive.statusCodeOf reads (it also accepts `statusCode` / `status_code`;
// this adapter always writes `status`, matching adaptive.ErrWithStatus).
type ErrorStatusCoder interface {
	ErrorStatusCode() (float64, bool)
}

// ErrorCauser supplies `err.cause`, which adaptive.errorText appends as
// " cause=<text>". It is DELIBERATELY not errors.Unwrap: Go convention is that
// a wrapped error's Error() already contains the wrapped message ("dial: eof"),
// so unwrapping automatically would double-count text the classifiers
// substring-match. Only an error that explicitly opts in gets a cause chain.
type ErrorCauser interface {
	ErrorCause() error
}

// ── the concrete error type the OpenRouter path throws ───────────────────

// StatusError is the Go twin of `Object.assign(new Error(message), { status })`
// — the shape ENGINE-DESIGN F8 names as "how an OpenRouter HTTP error
// arrives". Name defaults to "Error"; Status nil omits the own-property.
type StatusError struct {
	Message         string
	Name            string
	Status          *float64
	Cause           error
	ResponseHeaders *Headers
	ResponseBody    *string
}

func (e *StatusError) Error() string { return e.Message }

func (e *StatusError) ErrorName() string {
	if e.Name == "" {
		return "Error"
	}
	return e.Name
}

func (e *StatusError) ErrorStatusCode() (float64, bool) {
	if e.Status == nil {
		return 0, false
	}
	return *e.Status, true
}

func (e *StatusError) ErrorCause() error { return e.Cause }

// NewStatusError is the common case: a message and an HTTP status.
func NewStatusError(message string, status float64) *StatusError {
	return &StatusError{Message: message, Status: &status}
}

// NewProviderError carries the APICallError fields consumed by retry.ts in
// addition to the raw Error shape consumed by adaptive.ts.
func NewProviderError(message string, status float64, headers *Headers, body *string) *StatusError {
	return &StatusError{
		Message: message, Status: &status,
		ResponseHeaders: headers, ResponseBody: body,
	}
}

// RetryError is MessageV2.fromError's APIError projection for OpenRouter.
func (e *StatusError) RetryError() Err {
	retryable := false
	if e.Status != nil {
		status := *e.Status
		retryable = status == 408 || status == 409 || status == 429 || status >= 500
	}
	result := Err{Name: "APIError", Data: ErrData{
		Message: &e.Message, StatusCode: e.Status, IsRetryable: &retryable,
		ResponseHeaders: e.ResponseHeaders, ResponseBody: e.ResponseBody,
	}}
	if IsContextOverflow(result) {
		return Err{Name: "ContextOverflowError", Data: ErrData{
			Message: &e.Message, ResponseBody: e.ResponseBody,
		}}
	}
	return result
}

// FromError projects a thrown Go error into retry.ts's parsed Err shape.
func FromError(err error) Err {
	if err == nil {
		return Err{}
	}
	var classified interface{ RetryError() Err }
	if errors.As(err, &classified) {
		return classified.RetryError()
	}
	name := "UnknownError"
	var named ErrorNamer
	if errors.As(err, &named) && named.ErrorName() != "" {
		name = named.ErrorName()
	}
	message := err.Error()
	return Err{Name: name, Data: ErrData{Message: &message}}
}

// HeaderPairs converts net/http-style header values without importing
// net/http. Provider response-header keys are lowercase in the TS SDK.
func HeaderPairs(headers map[string][]string) *Headers {
	if headers == nil {
		return nil
	}
	pairs := make([]string, 0, len(headers)*2)
	for key, values := range headers {
		pairs = append(pairs, strings.ToLower(key), strings.Join(values, ", "))
	}
	return NewHeaders(pairs...)
}

// ── conversions ───────────────────────────────────────────────────────────

// ToRouterValue converts a Go error into the value adaptive's classifiers
// probe. A nil error becomes JS null, which is what llm.ts:425 passes on the
// success path (`registerRoute(usage?.outputTokens ?? 0, null)`).
//
// Mapping:
//   - RouterValuer            → its own value, verbatim
//   - everything else         → an Error instance whose message is err.Error()
//   - ErrorNamer              → sets `name`
//   - ErrorStatusCoder        → adds a numeric `status` own-property
//   - ErrorCauser             → sets `cause`, recursively
func ToRouterValue(err error) *adaptive.JSValue {
	if err == nil {
		return adaptive.Null()
	}
	if rv, ok := err.(RouterValuer); ok {
		if v := rv.RouterValue(); v != nil {
			return v
		}
		return adaptive.Null()
	}
	v := adaptive.Err(err.Error())
	if n, ok := err.(ErrorNamer); ok {
		if name := n.ErrorName(); name != "Error" {
			v.Name = name
		}
	}
	if s, ok := err.(ErrorStatusCoder); ok {
		if code, present := s.ErrorStatusCode(); present {
			v.Props = jscompat.NewOrderedMap[string, *adaptive.JSValue]()
			v.Props.Set("status", adaptive.Num(code))
		}
	}
	if c, ok := err.(ErrorCauser); ok {
		if cause := c.ErrorCause(); cause != nil {
			v.Cause = ToRouterValue(cause)
		}
	}
	return v
}

// JSONToRouterValue decodes an opaque provider error payload — OpenRouter's
// `{"code":520,"message":"Provider returned error","error_type":"unmapped"}`,
// for instance — into the value JSON.parse would have produced.
//
// Key ORDER is preserved, and that is the whole point:
// adaptive.IsLikelyTransientProviderError substring-matches the literal
// `error_type":"unmapped"` against JSON.stringify(err), so a re-serialisation
// that sorted the keys would silently stop classifying the exact payload the
// classifier was written for.
//
// Returns nil (JS undefined) when the payload is not valid JSON; callers on
// the fetch path should fall back to ToRouterValue of the transport error.
func JSONToRouterValue(raw []byte) *adaptive.JSValue {
	return parseJSONText(string(raw))
}

// ErrToRouterValue renders a PARSED Err as the plain `{name, data}` object it
// is, so the router classifiers can be run over the same value retry.ts
// classifies.
//
// PORT-PROVIDED, NOT PORTED: no TS call site does this — llm.ts hands the
// router the raw thrown value while retry.ts gets the fromError output. It
// exists because the two classifiers are documented mirrors of each other
// (retry.ts:39-41) and this is the only way to diff them on one input.
//
// `data`'s key order is the APIError schema literal order, which is also the
// order every fromError arm writes, so JSON.stringify of the result is
// byte-identical to the TS one. Absent (nil) fields are omitted, exactly as
// JSON.stringify drops undefined-valued keys.
func ErrToRouterValue(e Err) *adaptive.JSValue {
	data := adaptive.Obj()
	if e.Data.Message != nil {
		data.Props.Set("message", adaptive.Str(*e.Data.Message))
	}
	if e.Data.StatusCode != nil {
		data.Props.Set("statusCode", adaptive.Num(*e.Data.StatusCode))
	}
	if e.Data.IsRetryable != nil {
		data.Props.Set("isRetryable", adaptive.Bool(*e.Data.IsRetryable))
	}
	if e.Data.ResponseHeaders != nil {
		data.Props.Set("responseHeaders", headersToRouterValue(e.Data.ResponseHeaders))
	}
	if e.Data.ResponseBody != nil {
		data.Props.Set("responseBody", adaptive.Str(*e.Data.ResponseBody))
	}
	if e.Data.Metadata != nil {
		data.Props.Set("metadata", headersToRouterValue(e.Data.Metadata))
	}
	return adaptive.Obj("name", adaptive.Str(e.Name), "data", data)
}

func headersToRouterValue(h *Headers) *adaptive.JSValue {
	obj := adaptive.Obj()
	for _, e := range h.Entries() {
		obj.Props.Set(e.Key, adaptive.Str(e.Val))
	}
	return obj
}

// ── the mirror, made explicit ─────────────────────────────────────────────

// RouterClassification is every verdict adaptive.ts reaches about one value.
// Field order matches the TS export order in src/router/adaptive.ts.
type RouterClassification struct {
	RateLimit            bool `json:"rateLimit"`
	ProviderIncompatible bool `json:"providerIncompatible"`
	Timeout              bool `json:"timeout"`
	StructuredFailure    bool `json:"structuredFailure"`
	TransientProviderErr bool `json:"transientProviderError"`
	RetryableRouteError  bool `json:"retryableRouteError"`
}

// ClassifyForRouter runs all six adaptive classifiers over one value.
func ClassifyForRouter(v *adaptive.JSValue) RouterClassification {
	return RouterClassification{
		RateLimit:            adaptive.IsLikelyRateLimit(v),
		ProviderIncompatible: adaptive.IsLikelyProviderIncompatible(v),
		Timeout:              adaptive.IsLikelyTimeout(v),
		StructuredFailure:    adaptive.IsLikelyStructuredFailure(v),
		TransientProviderErr: adaptive.IsLikelyTransientProviderError(v),
		RetryableRouteError:  adaptive.IsRetryableRouteError(v),
	}
}
