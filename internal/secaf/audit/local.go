package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Agent-Field/agentfield/sdk/go/ai"

	"github.com/Agent-Field/codeaf/internal/secaf/appx"
	"github.com/Agent-Field/codeaf/internal/secaf/phases"
	"github.com/Agent-Field/codeaf/internal/secaf/reasoners"
)

// localApp is an appx.App whose Call is answered in process.
//
// The audit pipeline is written as a tree of `.call`s — audit calls the four
// `*_phase` reasoners, and those call the leaf hunters, verifiers and
// remediators — which on the AgentField node each travelled through the control
// plane as a tracked child execution. Inside codeaf there is no control plane,
// so localApp looks each target up in a reasoner registry and runs the handler
// directly. Harness, AI and Note are the wrapped App's own.
type localApp struct {
	inner appx.App
	reg   *reasoners.Registry
}

var _ appx.App = (*localApp)(nil)

// WithLocalCalls wraps inner so that every Call resolves to the in-process
// reasoner registry, and returns the wrapper.
//
// The registry is built over the WRAPPER, not over inner: the `*_phase`
// handlers make their nested `.call`s through the App they were registered
// with, and registering them with the wrapper is what keeps those nested calls
// in process too. inner's own Call is never used.
//
// Wrapping an App that is already local returns it unchanged, so a caller that
// wraps defensively does not stack registries.
func WithLocalCalls(inner appx.App) appx.App {
	if l, ok := inner.(*localApp); ok {
		return l
	}
	l := &localApp{inner: inner, reg: reasoners.NewRegistry()}
	reasoners.RegisterAll(l.reg, l)
	return l
}

// Harness delegates to the wrapped App.
func (l *localApp) Harness(ctx context.Context, prompt string, schema map[string]any, dest any, opts appx.HarnessOptions) (*appx.HarnessResult, error) {
	return l.inner.Harness(ctx, prompt, schema, dest, opts)
}

// AI delegates to the wrapped App.
func (l *localApp) AI(ctx context.Context, prompt string, opts ...ai.Option) (*ai.Response, error) {
	return l.inner.AI(ctx, prompt, opts...)
}

// Note delegates to the wrapped App.
func (l *localApp) Note(ctx context.Context, message string, tags ...string) {
	l.inner.Note(ctx, message, tags...)
}

// Call runs the reasoner target names and returns its result the way the
// control-plane hop returned it.
//
// target is `<node>.<reasoner>` (the phases write `sec-af.recon_phase`), or a
// bare reasoner name, which the SDK qualified with its own node id. A node
// other than phases.NodeID() has nothing behind it here and is refused.
//
// THE JSON HOP IS KEPT ON PURPOSE. Over the wire, the input was marshalled,
// decoded by the node into a fresh map[string]any, and the handler's result was
// marshalled and decoded again into a map[string]any for the caller. Callers
// downstream depend on what that does to values — every number arrives as a
// float64, a typed struct arrives as a plain map, a nil result arrives as a nil
// map (which afx.AsMap reports as NoneType), and a non-object result is an
// error — so both directions round-trip through encoding/json exactly as before
// instead of handing Go values across by reference.
//
// A handler's failure comes back as a *CallError carrying the handler's message
// and NOTHING of its type, which is what the caller saw over the wire: a
// validation failure deep inside prove_phase reached audit as an opaque failed
// execution, not as a bad-input error of audit's own. The one exception is
// cancellation, which unwraps to the context's error so a cancelled audit still
// reads as cancelled.
func (l *localApp) Call(ctx context.Context, target string, input map[string]any) (map[string]any, error) {
	name := target
	if node, reasoner, qualified := strings.Cut(target, "."); qualified {
		if node != phases.NodeID() {
			return nil, &CallError{Target: target, Message: fmt.Sprintf("no reasoner %q in this process: node %q is not %q", target, node, phases.NodeID())}
		}
		name = reasoner
	}
	handler, ok := l.reg.Lookup(name)
	if !ok {
		return nil, &CallError{Target: target, Message: fmt.Sprintf("no reasoner %q in this process", target)}
	}

	wireInput, err := roundTripInput(input)
	if err != nil {
		return nil, fmt.Errorf("marshal call payload: %w", err)
	}

	result, err := runHandler(ctx, handler, wireInput)
	if err != nil {
		callErr := &CallError{Target: target, Message: err.Error()}
		if ctxErr := ctx.Err(); ctxErr != nil {
			callErr.cause = ctxErr
		}
		return nil, callErr
	}

	return roundTripResult(target, result)
}

// runHandler runs one handler and turns a panic into an error. Over the wire a
// panicking reasoner failed its own execution and the caller saw a failed call;
// in process the same panic would take codeaf down with it.
func runHandler(ctx context.Context, h reasoners.Handler, input map[string]any) (result any, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("reasoner panicked: %v", r)
		}
	}()
	return h(ctx, input)
}

// roundTripInput is the request half of the hop: marshal, then decode into a
// fresh map[string]any the way the node decoded the request body.
func roundTripInput(input map[string]any) (map[string]any, error) {
	b, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// roundTripResult is the response half: the handler's value is marshalled and
// decoded into a map[string]any, a JSON null becomes a nil map with no error
// (the SDK's `(nil, nil)` for a null result), and anything that is not an
// object is an error, as the SDK's decode of the execution result was.
func roundTripResult(target string, result any) (map[string]any, error) {
	b, err := json.Marshal(result)
	if err != nil {
		return nil, &CallError{Target: target, Message: fmt.Sprintf("encode result of %s: %v", target, err)}
	}
	if string(b) == "null" {
		return nil, nil
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, &CallError{Target: target, Message: fmt.Sprintf("decode result of %s: %v", target, err)}
	}
	return out, nil
}

// CallError is a `.call` that did not succeed. Its text is exactly the
// failing reasoner's own message — the error_message the control plane
// reported — because phases fold that text into fallback findings and notes.
type CallError struct {
	Target  string
	Message string

	// cause is set only when the call failed because the context ended, so
	// errors.Is(err, context.Canceled) holds without exposing the handler's
	// own error types.
	cause error
}

func (e *CallError) Error() string { return e.Message }

// Unwrap exposes the context's error for a cancelled call and nothing
// otherwise.
func (e *CallError) Unwrap() error { return e.cause }
