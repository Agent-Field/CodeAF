package reasoners

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Agent-Field/codeaf/internal/secaf/afx"
	"github.com/Agent-Field/codeaf/internal/secaf/appx"
	"github.com/Agent-Field/codeaf/internal/secaf/phases"
)

// register.go builds the reasoner surface: the 33 named handlers Python's
// module-level router carried,
//
//	router = AgentRouter(tags=["security", "audit", "red-team"])
//	@router.reasoner()
//	async def run_x(...): ...
//	# app.py: app.include_router(reasoner_router)
//
// held in an in-process Registry instead of mounted on an SDK router. Inside
// codeaf there is no control plane to route a `.call` through: the audit's
// Caller (internal/secaf/audit) looks the target up here and runs the handler
// in the same process, so the names, the order, the input validation and the
// typed binding are exactly what the node registered — only the transport is
// gone.

// Handler is one reasoner: the untyped keyword map a `.call` carries in, the
// reasoner's result out. It is the shape the SDK's agent.HandlerFunc had, kept
// so every handler below moved across unchanged.
type Handler func(ctx context.Context, input map[string]any) (any, error)

// Registry is the reasoner table: name -> handler, plus the registration order
// and the input schema each name was registered with.
//
// It is built once by RegisterAll and only read afterwards, so it needs no
// lock; a caller that registers concurrently with lookups is misusing it.
type Registry struct {
	names    []string
	handlers map[string]Handler
	schemas  map[string]json.RawMessage
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{
		handlers: map[string]Handler{},
		schemas:  map[string]json.RawMessage{},
	}
}

// Register adds one handler under name.
//
// It PANICS on a name registered twice: the SDK's reasoner table was a map
// where the second registration silently won, and a collision is a wiring bug
// that must fail at construction, not at the first `.call` that reaches the
// wrong handler.
func (r *Registry) Register(name string, h Handler, schema json.RawMessage) {
	if _, dup := r.handlers[name]; dup {
		panic(fmt.Sprintf("reasoners: %q registered twice", name))
	}
	r.names = append(r.names, name)
	r.handlers[name] = h
	r.schemas[name] = schema
}

// Lookup returns the handler registered under name.
func (r *Registry) Lookup(name string) (Handler, bool) {
	h, ok := r.handlers[name]
	return h, ok
}

// Names returns a copy of the registered names, in registration order.
func (r *Registry) Names() []string {
	return append([]string(nil), r.names...)
}

// InputSchema returns a copy of the input schema name was registered with, and
// false for a name the registry does not carry.
func (r *Registry) InputSchema(name string) (json.RawMessage, bool) {
	schema, ok := r.schemas[name]
	if !ok {
		return nil, false
	}
	return append(json.RawMessage(nil), schema...), true
}

// RegisterAll registers every router reasoner on reg, in DESIGN.md §3 order,
// and returns the ordered names it registered.
//
// app is the App every handler closes over — and therefore the App whose Call
// the four `*_phase` handlers make their nested `.call`s through. To keep those
// nested calls in process too, pass the same local-calling App the registry is
// answering for (audit.WithLocalCalls does exactly that).
//
// nodeID is read ONCE, here, from phases.NodeID(): Python's module-level
// `NODE_ID = os.getenv("NODE_ID", "sec-af")`, now a constant. Every `*_phase`
// handler closes over that value, so the `.call` targets are fixed.
func RegisterAll(reg *Registry, app appx.App) []string {
	nodeID := phases.NodeID()
	r := &registrar{reg: reg}

	// --- reasoners/recon.py -------------------------------------------------
	register(r, app, NameRunArchitectureMapper, RunArchitectureMapper)
	register(r, app, NameRunDependencyAuditor, RunDependencyAuditor)
	register(r, app, NameRunConfigScanner, RunConfigScanner)
	register(r, app, NameRunDataFlowMapper, RunDataFlowMapper)
	register(r, app, NameRunSecurityContextProfiler, RunSecurityContextProfiler)

	// --- reasoners/hunt.py --------------------------------------------------
	register(r, app, NameRunInjectionHunter, RunInjectionHunter)
	register(r, app, NameRunDosHunter, RunDosHunter)
	register(r, app, NameRunSSRFHunter, RunSSRFHunter)
	register(r, app, NameRunAuthHunter, RunAuthHunter)
	register(r, app, NameRunXSSHunter, RunXSSHunter)
	register(r, app, NameRunCryptoHunter, RunCryptoHunter)
	register(r, app, NameRunBusinessLogicHunter, RunBusinessLogicHunter)
	register(r, app, NameRunLogicBugsHunter, RunLogicBugsHunter)
	register(r, app, NameRunDataExposureHunter, RunDataExposureHunter)
	register(r, app, NameRunSupplyChainHunter, RunSupplyChainHunter)
	register(r, app, NameRunConfigSecretsHunter, RunConfigSecretsHunter)
	register(r, app, NameRunAPISecurityHunter, RunAPISecurityHunter)
	register(r, app, NameRunDeduplicator, RunDeduplicator)

	// --- reasoners/prove.py -------------------------------------------------
	register(r, app, NameRunDepReachability, RunDepReachability)
	register(r, app, NameRunVerifier, RunVerifier)
	register(r, app, NameRunTracer, RunTracer)
	register(r, app, NameRunSanitizationAnalyzer, RunSanitizationAnalyzer)
	register(r, app, NameRunExploitHypothesizer, RunExploitHypothesizer)
	register(r, app, NameRunVerdictAgent, RunVerdictAgent)
	register(r, app, NameRunRemediation, RunRemediation)
	register(r, app, NameRunRemediationAgent, RunRemediationAgent)
	register(r, app, NameRunDastVerifier, RunDastVerifier)
	register(r, app, NameRunCrossServiceAnalyzer, RunCrossServiceAnalyzer)

	// --- reasoners/phases.py ------------------------------------------------
	register(r, app, NameRunCWEExpansion, RunCWEExpansion)
	registerPhase(r, app, nodeID, NameReconPhase, ReconPhase)
	registerPhase(r, app, nodeID, NameHuntPhase, HuntPhase)
	registerPhase(r, app, nodeID, NameProvePhase, ProvePhase)
	registerPhase(r, app, nodeID, NameRemediationPhase, RemediationPhase)

	return r.names
}

// registrar is the single registration path: everything that reaches the
// registry is recorded here too, so the returned bookkeeping cannot drift from
// what the registry received.
type registrar struct {
	reg   *Registry
	names []string
}

// add registers one reasoner, recording it, wrapping it in the input
// validation Python's SDK performed, and attaching the input schema the Python
// node published for that name.
//
// InputSchema panics on a name the capture does not know, so a reasoner added
// here without regenerating testdata/python_input_schemas.json fails at
// registration rather than running unvalidated. See input_schemas.go.
func (r *registrar) add(name string, h Handler) {
	r.names = append(r.names, name)
	r.reg.Register(name, ValidateHandler(name, h), InputSchema(name))
}

// InputError is a request the reasoner refused before running: the input did
// not bind. StatusCode keeps the HTTP status the node answered with — 422 for
// the SDK-level validation Python's endpoint performs, 400 for the audit
// entry's own bad-input family — because that number is how a caller told
// "your input was wrong" from "the audit failed", and the codeaf side may want
// to keep drawing the same line.
type InputError struct {
	StatusCode int
	Message    string
}

func (e *InputError) Error() string { return e.Message }

// ValidateHandler wraps h with `_validate_handler_input` for the reasoner
// called name — the check the Python SDK ran on every request body BEFORE the
// handler function was entered (agent.py:3120-3134).
//
// Exported because internal/secaf/audit runs the same layer over the audit
// entry's own request.
//
// Python answered a failure with `JSONResponse(422, {"detail": safe_message})`
// from the endpoint, so the handler never ran. Here the check runs as the first
// thing inside the handler and reports the same status through an
// *InputError.
func ValidateHandler(name string, h Handler) Handler {
	// Resolve the spec at REGISTRATION time so an unknown name panics at
	// construction, not on the first request — the same loud-drift contract
	// InputSchema has.
	_ = handlerSpecFor(name)
	return func(ctx context.Context, input map[string]any) (any, error) {
		validated, err := ValidateHandlerInput(name, input)
		if err != nil {
			return nil, HandlerInputStatusError(err)
		}
		return h(ctx, validated)
	}
}

// HandlerInputStatusError maps a *HandlerInputError onto the *InputError that
// carries 422, the status Python's endpoint returned.
func HandlerInputStatusError(err error) error {
	return &InputError{StatusCode: http.StatusUnprocessableEntity, Message: err.Error()}
}

// register adapts a typed reasoner function to a Handler: afx.Bind
// the untyped request map into T (running T's default-seeding UnmarshalJSON, so
// the Python keyword defaults apply to absent keys) and hand it to fn. T is
// inferred from fn.
func register[T any](
	r *registrar,
	app appx.App,
	name string,
	fn func(context.Context, appx.App, T) (map[string]any, error),
) {
	r.add(name, func(ctx context.Context, input map[string]any) (any, error) {
		in, err := afx.Bind[T](input)
		if err != nil {
			return nil, err
		}
		return fn(ctx, app, in)
	})
}

// registerPhase is register for the four `*_phase` reasoners, which additionally
// need the node id their `.call` targets are prefixed with.
func registerPhase[T any](
	r *registrar,
	app appx.App,
	nodeID string,
	name string,
	fn func(context.Context, appx.App, string, T) (map[string]any, error),
) {
	r.add(name, func(ctx context.Context, input map[string]any) (any, error) {
		in, err := afx.Bind[T](input)
		if err != nil {
			return nil, err
		}
		return fn(ctx, app, nodeID, in)
	})
}
