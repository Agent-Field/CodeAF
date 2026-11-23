package provider

import (
	"context"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"sync"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// THE ACTUAL COMPLETION ATTEMPT: ONE HTTP SEND, SAID TO WHOEVER MAY HAVE TO STOP IT.
//
// A spending cap that counts calls, turns or answers counts the wrong thing. The
// provider charges for what went onto the wire: a retry after a 5xx, a repaired
// or relaxed body, both arms of a hedge and an answer thrown away are each a
// separate paid generation, and none of them is visible one level up. This file
// is the foundation a cap stands on: an OPTIONAL observer that is asked, with
// the attempt's own identity, immediately before EACH completion `Do`, and that
// hands back a token which is then told how that one send ended and what it cost.
//
// THIS FILE IS NOT POLICY. It admits nothing, caps nothing and writes nothing
// durable. The observer decides; the provider only guarantees the grain:
//
//   - One `BeforeAttempt` per `httpClient.Do` in [Client.send] — not per call,
//     not per turn, not once per RouteGate. The ID is the callTrace's existing
//     per-send ID, so it is the same string the model-call log's two rows carry.
//   - The token is captured when the send is admitted and travels with that
//     send. A late receipt, a hedge loser's cut stream and a body that is billed
//     after a later send began all report to the token of THEIR send; nothing is
//     reconstructed from the current trace, a day's delta or a usage bank.
//   - Transport and money are different facts and are reported by different
//     methods. A response arriving is not a price, and a price may arrive
//     seconds after the transport ended ([ActualAttemptToken]).
//   - The absence of a fact is never zero. After a send started, no response and
//     no price is [FinancialUnknown]; only a provider-stated cost, zero included,
//     is [FinancialKnown].
//   - Nobody listening is the ordinary case: one context lookup per send, no map,
//     no goroutine, no allocation, no behaviour change.
//
// SCOPE IS THE COMPLETION ADAPTER ONLY. Probes, media jobs, the document parser
// and the SDK tool loop do not pass the hook, so under an observer they are
// REFUSED before any network use ([RequireActualAttemptCoverage]) rather than
// allowed to spend unobserved. Search, shell, delegation and the Codex adapter
// are other packages' doors and are not claimed here.

// WorkOwner names the unit of work a send was made for — a task, a turn, an
// automation. It is opaque to the provider and copied into every attempt at
// admission. It is separate context metadata and not a second observer.
type WorkOwner struct {
	Kind string
	ID   string
}

func (o WorkOwner) set() bool {
	return strings.TrimSpace(o.Kind) != "" || strings.TrimSpace(o.ID) != ""
}

// ActualAttemptInfo is everything known about one send before it goes out. It
// carries no key, no prompt and no endpoint URL.
type ActualAttemptInfo struct {
	// ID is the existing per-send pairing ID of the model-call log. Every
	// retry, repair, relaxation and hedge arm has its own.
	ID string
	// RequestedModel is the name the caller's request carried (empty when it
	// carried none); SelectedModel is the model this send is addressed to.
	RequestedModel string
	SelectedModel  string
	// Billing is the road this send goes out under — immutable, and including
	// the metered plan overflow when this very send is that overflow.
	Billing BillingFact
	// Owner is the work owner copied from the context, or the zero value.
	Owner WorkOwner
}

// TransportEnd is how one send's HTTP exchange ended, reported exactly once.
type TransportEnd string

const (
	// TransportNotSent: no request bytes left the process. The send cost nothing
	// and any reservation made for it may be released. This is the only ending
	// that implies "no charge".
	TransportNotSent TransportEnd = "not_sent"
	// TransportResponded: `Do` returned a response. The body may still be
	// streaming; the price is a separate fact ([FinancialObservation]).
	TransportResponded TransportEnd = "responded"
	// TransportFailed: the request may have been accepted and `Do` returned an
	// error. Whether the provider charged is not known.
	TransportFailed TransportEnd = "failed"
	// TransportCancelled: the caller's context ended during the exchange — a
	// hedge loser, a stop key, a deadline. Whether the provider charged is not
	// known.
	TransportCancelled TransportEnd = "cancelled"
)

// FinancialState is what is known about one send's price.
type FinancialState string

const (
	// FinancialUnknown: the send may have been charged and nobody can say what.
	// It is never a zero, and it may still become known from a late receipt.
	FinancialUnknown FinancialState = "unknown"
	// FinancialPending: a receipt that will state the price has been queued.
	FinancialPending FinancialState = "pending"
	// FinancialKnown: the provider stated this send's cost. CostUSD is
	// authoritative, an exact zero included, and final.
	FinancialKnown FinancialState = "known"
)

// FinancialObservation is one statement about a send's price.
type FinancialObservation struct {
	State   FinancialState
	CostUSD float64
}

// UnknownCost and PendingCost are the two statements that carry no figure.
func UnknownCost() FinancialObservation { return FinancialObservation{State: FinancialUnknown} }

// PendingCost says a receipt is owed.
func PendingCost() FinancialObservation { return FinancialObservation{State: FinancialPending} }

// KnownCost is the provider-stated cost of one send. A negative or non-finite
// figure is not a price and reads as [FinancialUnknown]; zero is a real price.
func KnownCost(costUSD float64) FinancialObservation {
	if costUSD < 0 || math.IsNaN(costUSD) || math.IsInf(costUSD, 0) {
		return UnknownCost()
	}
	return FinancialObservation{State: FinancialKnown, CostUSD: costUSD}
}

// costFrom reads an optional provider-stated cost: absent is unknown, never 0.
func costFrom(cost *float64) FinancialObservation {
	if cost == nil {
		return UnknownCost()
	}
	return KnownCost(*cost)
}

// ActualAttemptToken is the observer's handle on exactly one send. The provider
// calls EndTransport at most once and never twice with different answers.
// ObserveFinancial may be called several times over a send's life, but only in
// a way that cannot double money: a known cost is delivered once and is final,
// and an identical consecutive statement is not repeated.
//
// Implementations must be safe for use from the goroutine that made the send
// AND from a background receipt worker, and must not call back into the
// provider: the callbacks are made in order, under a per-send lock.
type ActualAttemptToken interface {
	EndTransport(TransportEnd)
	ObserveFinancial(FinancialObservation)
}

// ActualAttemptObserver is asked immediately before each completion send, after
// the request is built and the rate limiter has admitted it. An error means NO
// HTTP is sent and the local resources are released; a nil token with a nil
// error is the same refusal. The observer may persist and admit against a shared
// cap here — the provider implements no policy. A blocking observer holds a
// shared limiter slot until it returns.
type ActualAttemptObserver interface {
	BeforeAttempt(ctx context.Context, info ActualAttemptInfo) (ActualAttemptToken, error)
}

// ErrActualAttemptObserverInstalled is what installing a second, different
// observer — or a second, different work owner — returns. Nothing is replaced
// and nothing is shadowed: a cap authority that was installed first stays the
// only one.
var ErrActualAttemptObserverInstalled = errors.New("provider: an actual-attempt observer or owner is already installed")

// ErrActualAttemptNilObserver refuses a typed-nil observer before installation.
var ErrActualAttemptNilObserver = errors.New("provider: the actual-attempt observer is a typed nil")

// ErrActualAttemptNoToken is the refusal for an observer that admitted a send
// without handing back a token.
var ErrActualAttemptNoToken = errors.New("provider: the attempt observer returned no token")

// AttemptDeniedError is the typed end of a send its observer refused.
type AttemptDeniedError struct{ Cause error }

func (e *AttemptDeniedError) Error() string {
	if e == nil || e.Cause == nil {
		return "provider: the attempt was not admitted"
	}
	return "provider: the attempt was not admitted: " + e.Cause.Error()
}

func (e *AttemptDeniedError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

type attemptObserverKey struct{}
type workOwnerKey struct{}

// WithActualAttemptObserver installs the one observer for everything sent under
// ctx. Installing the SAME comparable observer again is a no-op; installing a
// different one is [ErrActualAttemptObserverInstalled]. A nil observer returns
// ctx untouched. An installed observer also turns every surface this foundation
// does not cover into a typed refusal ([RequireActualAttemptCoverage]).
func WithActualAttemptObserver(ctx context.Context, observer ActualAttemptObserver) (context.Context, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if observer == nil {
		return ctx, nil
	}
	if nilInterfaceValue(observer) {
		return ctx, ErrActualAttemptNilObserver
	}
	if held := actualObserverFrom(ctx); held != nil {
		if sameValue(held, observer) {
			return ctx, nil
		}
		return ctx, ErrActualAttemptObserverInstalled
	}
	return context.WithValue(ctx, attemptObserverKey{}, observer), nil
}

// WithWorkOwner attaches the immutable owner every attempt under ctx is
// admitted with. The same owner again is a no-op; a different one is
// [ErrActualAttemptObserverInstalled].
func WithWorkOwner(ctx context.Context, owner WorkOwner) (context.Context, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if !owner.set() {
		return ctx, nil
	}
	if held, _ := ctx.Value(workOwnerKey{}).(WorkOwner); held.set() {
		if held == owner {
			return ctx, nil
		}
		return ctx, ErrActualAttemptObserverInstalled
	}
	return context.WithValue(ctx, workOwnerKey{}, owner), nil
}

// WorkOwnerFrom reads back the owner [WithWorkOwner] attached, or the zero value.
func WorkOwnerFrom(ctx context.Context) WorkOwner {
	if ctx == nil {
		return WorkOwner{}
	}
	owner, _ := ctx.Value(workOwnerKey{}).(WorkOwner)
	return owner
}

// ActualAttemptObserverFrom reads back the installed observer, or nil.
func ActualAttemptObserverFrom(ctx context.Context) ActualAttemptObserver {
	return actualObserverFrom(ctx)
}

func actualObserverFrom(ctx context.Context) ActualAttemptObserver {
	if ctx == nil {
		return nil
	}
	observer, _ := ctx.Value(attemptObserverKey{}).(ActualAttemptObserver)
	return observer
}

// sameValue is interface equality that cannot panic on an uncomparable dynamic
// type: two uncomparable observers are never "the same".
// nilInterfaceValue also sees nil pointers/functions held in a non-nil
// interface. Admission must validate the handle before it permits HTTP.
func nilInterfaceValue(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}

func sameValue(a, b any) bool {
	ta, tb := reflect.TypeOf(a), reflect.TypeOf(b)
	return ta == tb && ta.Comparable() && a == b
}

// AttemptSurface names a provider entry point for coverage questions.
type AttemptSurface string

const (
	SurfaceCompletion AttemptSurface = "completion"
	SurfaceDocument   AttemptSurface = "document"
	SurfaceProbe      AttemptSurface = "probe"
	SurfaceMedia      AttemptSurface = "media"
	SurfaceToolLoop   AttemptSurface = "sdk_tool_loop"
)

// UnsupportedAttemptSurfaceError is the typed refusal of a surface whose sends
// are not observed. It is returned BEFORE any network use.
type UnsupportedAttemptSurfaceError struct {
	Surface AttemptSurface
	Reason  string
}

func (e *UnsupportedAttemptSurfaceError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("provider: %s sends are not covered by the actual-attempt observer: %s", e.Surface, e.Reason)
}

// RequireActualAttemptCoverage is the preflight for a caller that must not
// spend unobserved: nil for the completion adapter, a typed
// [UnsupportedAttemptSurfaceError] for everything else. It is a statement
// about this package's doors only — other packages' adapters must be
// preflighted by their own owners.
func RequireActualAttemptCoverage(surface AttemptSurface) error {
	switch surface {
	case SurfaceCompletion:
		return nil
	case SurfaceDocument:
		return &UnsupportedAttemptSurfaceError{surface, "the document parser sends without the completion hook"}
	case SurfaceProbe:
		return &UnsupportedAttemptSurfaceError{surface, "lane probes send without the completion hook"}
	case SurfaceMedia:
		return &UnsupportedAttemptSurfaceError{surface, "image, speech and video jobs send without the completion hook"}
	case SurfaceToolLoop:
		return &UnsupportedAttemptSurfaceError{surface, "the SDK tool loop owns its own transport"}
	}
	return &UnsupportedAttemptSurfaceError{surface, "unknown surface"}
}

// RequireActualAttemptCoverage is the same preflight for THIS client's
// completion door, which adds the transport contract: one `Do` is one counted
// attempt only if the transport underneath never re-sends or redirects on its
// own. The shipped transport is accepted; a caller-supplied
// [Config.HTTPClient] must declare [Config.SingleSendTransport] or is refused.
// Redirects are never followed on an observed send whatever the client says.
func (c *Client) RequireActualAttemptCoverage() error {
	if c == nil {
		return &UnsupportedAttemptSurfaceError{SurfaceCompletion, "no client"}
	}
	if c.config.HTTPClient != nil && !c.config.SingleSendTransport {
		return &UnsupportedAttemptSurfaceError{SurfaceCompletion,
			"a custom HTTP client must declare that its transport sends once per Do"}
	}
	return nil
}

// refuseObserved is the guard at every entry point this foundation does not
// cover: nothing happens when nobody observes, a typed refusal when somebody does.
func refuseObserved(ctx context.Context, surface AttemptSurface) error {
	if actualObserverFrom(ctx) == nil {
		return nil
	}
	return RequireActualAttemptCoverage(surface)
}

// ── the per-send handle ─────────────────────────────────────────────────────

// attemptHandle is the provider's side of one admitted send: the observer's
// token behind a lock that makes the transport end happen once and keeps money
// from being reported twice. A nil handle is the unobserved case and every
// method accepts it.
type attemptHandle struct {
	info ActualAttemptInfo
	mu   sync.Mutex
	tok  ActualAttemptToken
	// ended says the transport end was delivered. fin is the last financial
	// state delivered, empty before the first.
	ended bool
	fin   FinancialState
}

func (h *attemptHandle) end(how TransportEnd) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.ended {
		return
	}
	h.ended = true
	h.tok.EndTransport(how)
}

// financial delivers a statement unless it would repeat the last one or follow
// a known cost. Known is final; everything else may be superseded, which is how
// a pending or unknown send becomes known from its own late receipt.
func (h *attemptHandle) financial(o FinancialObservation) {
	if h == nil {
		return
	}
	switch o.State {
	case FinancialUnknown, FinancialPending, FinancialKnown:
	default:
		o = UnknownCost()
	}
	if o.State == FinancialKnown {
		o = KnownCost(o.CostUSD)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.fin == FinancialKnown || h.fin == o.State {
		return
	}
	h.fin = o.State
	h.tok.ObserveFinancial(o)
}

// unknown states "may have been charged, cannot say" for a send nothing has
// spoken for yet.
func (h *attemptHandle) unknown() { h.financial(UnknownCost()) }

// finishAttempt is the call door's unconditional exit: whatever send this trace
// last made and nothing has spoken for is unknown. It runs after the parser and
// the settle, because the terminal row and the bill both come first, and it
// never downgrades a pending receipt or a known cost.
func (t *callTrace) finishAttempt() {
	if t != nil {
		t.attempt.supersede()
	}
}

// supersede is called when a later send begins on the same call: the earlier
// send's answer, if any, was discarded, and if nothing priced it it is unknown.
// It never downgrades a pending receipt or a known cost.
func (h *attemptHandle) supersede() {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.fin != "" {
		return
	}
	h.fin = FinancialUnknown
	h.tok.ObserveFinancial(UnknownCost())
}

// attemptOf is the handle of the send a trace is currently on, or nil.
func attemptOf(traces ...*callTrace) *attemptHandle {
	if len(traces) == 0 || traces[0] == nil {
		return nil
	}
	return traces[0].attempt
}

// admitAttempt asks the observer about the send about to go out and returns the
// handle that will carry its outcome. The trace must be non-nil: its ID is the
// attempt's identity.
func (c *Client) admitAttempt(ctx context.Context, observer ActualAttemptObserver, request *ai.Request, knobs callKnobs, usingOverflow bool) (*attemptHandle, error) {
	trace := knobs.trace
	// The previous send of this call, if it spoke for nothing, is unknown now.
	trace.attempt.supersede()
	info := ActualAttemptInfo{
		ID:             trace.attemptID,
		RequestedModel: strings.TrimSpace(request.Model),
		SelectedModel:  c.modelFor(request),
		Billing:        c.attemptBilling(usingOverflow),
		Owner:          WorkOwnerFrom(ctx),
	}
	token, err := observer.BeforeAttempt(ctx, info)
	if nilInterfaceValue(token) {
		token = nil
	}
	if err == nil && token == nil {
		err = ErrActualAttemptNoToken
	}
	if err != nil {
		trace.attempt = nil
		// A token handed back WITH an error is rolled back, not stranded: no
		// request was built into a Do, so the send is explicitly not sent and
		// authoritatively free, and the observer may release its reservation.
		if token != nil {
			rolled := &attemptHandle{info: info, tok: token}
			rolled.end(TransportNotSent)
			rolled.financial(KnownCost(0))
		}
		return nil, &AttemptDeniedError{Cause: err}
	}
	handle := &attemptHandle{info: info, tok: token}
	trace.attempt = handle
	return handle, nil
}

// afterDo reports how the exchange ended. An error or a cancelled caller after
// Do started is unknown, never zero. A response is only the transport's end —
// its price arrives by bill or receipt.
func (h *attemptHandle) afterDo(ctx context.Context, err error, responded bool) {
	if h == nil {
		return
	}
	// NOTHING AFTER A STARTED Do IS INFERRED AS "NOT SENT". A failed dial looks
	// like proof, but only the dispatcher before Do (a refused admission, a
	// rolled-back token) can prove no send happened; the transport's own error
	// is just a failure whose charge is unknown.
	switch {
	case responded:
		h.end(TransportResponded)
	case ctx.Err() != nil || errors.Is(err, context.Canceled):
		h.end(TransportCancelled)
		h.unknown()
	default:
		h.end(TransportFailed)
		h.unknown()
	}
}

// admissionRefusedFrom reports a send the local admission authority declined:
// an observer's refusal, or a surface that cannot be observed while observation
// is required. It is the one fact the dispatcher's walk, the hedge's walk and
// the taxonomy read to know that no other lane, shape or model may be bought
// past the refusal. The cause stays unwrappable.
func admissionRefusedFrom(err error) bool {
	var denied *AttemptDeniedError
	var unsupported *UnsupportedAttemptSurfaceError
	return errors.As(err, &denied) || errors.As(err, &unsupported)
}
