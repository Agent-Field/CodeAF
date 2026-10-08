// Package audit is the security audit's entry point inside codeaf: the request,
// the repository it reads, the four-phase pipeline, and the in-process Caller
// that answers the pipeline's `.call`s.
//
// It replaces the sec-af node's wiring layer (its `audit` reasoner, its
// repository resolution and its control-plane registration). What the node did
// over HTTP — route `sec-af.recon_phase` to a reasoner of the same node, which
// in turn called `sec-af.run_architecture_mapper` and the rest — happens here
// in process: Run wraps the App with WithLocalCalls, whose registry holds the
// same 33 reasoners under the same names.
package audit

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/agentsession/appx"
	"github.com/Agent-Field/codeaf/internal/secaf/afx"
	"github.com/Agent-Field/codeaf/internal/secaf/orch"
	"github.com/Agent-Field/codeaf/internal/secaf/phases"
	"github.com/Agent-Field/codeaf/internal/secaf/reasoners"
	"github.com/Agent-Field/codeaf/internal/secaf/schemas"
)

// nowMonotonic is `time.monotonic()`. It is a variable so a test can pin the
// two duration fields the pipeline stamps; production never reassigns it.
//
// Go's time.Now carries a monotonic reading that Sub prefers over the wall
// clock, so a DIFFERENCE has exactly Python's time.monotonic() semantics.
var nowMonotonic = time.Now

// Error is an audit that produced no result. StatusCode keeps the HTTP status
// the node answered with, because it is how a caller tells the three outcomes
// apart:
//
//	422  the request failed the SDK-level input validation (BindRequest)
//	400  the request was refused: it did not bind, it names no usable
//	     directory, or the pipeline raised one of Python's ValueError family
//	     (see IsBadInput) — Message is the raw text
//	500  the audit itself failed — Message carries Python's
//	     "audit execution failed: " prefix when the failure came from inside
//	     the pipeline
//
// Err is the underlying cause, for errors.Is and errors.As.
type Error struct {
	StatusCode int
	Message    string
	Err        error
}

func (e *Error) Error() string { return e.Message }

// Unwrap returns the underlying cause.
func (e *Error) Unwrap() error { return e.Err }

// Run audits one local repository and returns the audit result, running every
// `.call` in the pipeline in process (WithLocalCalls). app supplies Harness,
// AI and Note; its own Call is never used.
//
// A failure is always an *Error.
func Run(ctx context.Context, app appx.App, req AuditRequest) (schemas.SecurityAuditResult, error) {
	return newRunner(WithLocalCalls(app)).run(ctx, req)
}

// RunInput is Run for an untyped keyword map — the body the node's `audit`
// reasoner received — validated and bound by BindRequest first.
func RunInput(ctx context.Context, app appx.App, input map[string]any) (schemas.SecurityAuditResult, error) {
	req, err := BindRequest(input)
	if err != nil {
		return schemas.SecurityAuditResult{}, asError(err, http.StatusBadRequest)
	}
	return Run(ctx, app, req)
}

// runner holds the seams the pipeline is tested through: the App whose Call
// the four phase calls go through, the orchestrator constructor, and the
// repository resolver.
type runner struct {
	app             appx.App
	newOrchestrator func(ctx context.Context, app appx.App, in schemas.AuditInput, repoPath string) (*orch.AuditOrchestrator, error)
	resolveRepo     func(repoURL string) (string, error)
}

func newRunner(app appx.App) *runner {
	return &runner{app: app, newOrchestrator: orch.NewAt, resolveRepo: ResolveRepo}
}

// asError returns err as an *Error, wrapping it with status when it is not one.
func asError(err error, status int) *Error {
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	return &Error{StatusCode: status, Message: err.Error(), Err: err}
}

// run ports the `audit(...)` reasoner (app.py:123-231):
//
//	audit_input = AuditInput(...)                # bind
//	orchestrator = AuditOrchestrator(app, input) # OUTSIDE the try
//	repo_path = _resolve_repo(repo_url)          # OUTSIDE the try
//	orchestrator.repo_path = Path(repo_path)
//	orchestrator.checkpoint_dir = repo_path/".sec-af"
//	try:
//	    <resume branch> | <four-call pipeline>
//	except ValueError as exc:  400 {"error": str(exc)}
//	except Exception as exc:   note("Audit pipeline failed: ..."); 500 {"error": "audit execution failed: ..."}
//	return result.model_dump()
//
// One deliberate change of order: the repository is resolved BEFORE the
// orchestrator is built, and handed to it (orch.NewAt). Python built the
// orchestrator against SEC_AF_REPO_PATH or the working directory first, so its
// PR-mode diff analysis and AuditConfig.repo_path described whatever directory
// the process started in; inside codeaf that is never the right tree.
func (r *runner) run(ctx context.Context, req AuditRequest) (schemas.SecurityAuditResult, error) {
	auditInput := req.ToAuditInput()

	repoPath, err := r.resolveRepo(req.RepoURL)
	if err != nil {
		// The node cloned a URL and fell back to its working directory, and
		// only a failed clone was an error (a 500, raised outside the try).
		// Resolution now refuses instead — a URL, a missing path, a file — and
		// a refusal is about the request, so it is a 400 with the reason.
		return schemas.SecurityAuditResult{}, &Error{StatusCode: http.StatusBadRequest, Message: err.Error(), Err: err}
	}

	orchestrator, err := r.newOrchestrator(ctx, r.app, auditInput, repoPath)
	if err != nil {
		// Python parity: AuditConfig.from_input raises ValueError("'x' is not a
		// valid DepthProfile") OUTSIDE the try, so FastAPI answered with a
		// generic 500 — not the 400 the ValueError branch would give. Kept as a
		// 500, with the message surfaced. See IsBadInput.
		return schemas.SecurityAuditResult{}, &Error{StatusCode: http.StatusInternalServerError, Message: err.Error(), Err: err}
	}

	if dir := strings.TrimSpace(req.CheckpointDir); dir != "" {
		orchestrator.CheckpointDir = dir
	}
	result, err := r.runPipeline(ctx, orchestrator, req, repoPath)
	if err != nil {
		if IsBadInput(err) {
			// `except ValueError` -> 400 with the RAW message, no note.
			return schemas.SecurityAuditResult{}, &Error{StatusCode: http.StatusBadRequest, Message: err.Error(), Err: err}
		}
		// `except Exception` -> note, then 500 with the prefix (app.py:224-230).
		// Python also printed `AUDIT ERROR: ...` to stdout first; inside codeaf
		// stdout is the terminal the person is looking at, and the note carries
		// the same text, so the print is not reproduced.
		r.app.Note(ctx, "Audit pipeline failed: "+err.Error(), "audit", "error")
		return schemas.SecurityAuditResult{}, &Error{
			StatusCode: http.StatusInternalServerError,
			Message:    "audit execution failed: " + err.Error(),
			Err:        err,
		}
	}

	// Python returns `result.model_dump()`; SecurityAuditResult marshals to the
	// identical snake_case key set.
	return result, nil
}

// handle is the node's `audit` reasoner end to end — bind the keyword map,
// then run — over this runner's seams. It is what the tests drive.
func (r *runner) handle(ctx context.Context, input map[string]any) (schemas.SecurityAuditResult, error) {
	req, err := BindRequest(input)
	if err != nil {
		return schemas.SecurityAuditResult{}, asError(err, http.StatusBadRequest)
	}
	return r.run(ctx, req)
}

// runPipeline is the body of audit()'s `try:` block — everything whose failure
// the 400/500 mapping applies to.
func (r *runner) runPipeline(
	ctx context.Context,
	orchestrator *orch.AuditOrchestrator,
	req AuditRequest,
	repoPath string,
) (schemas.SecurityAuditResult, error) {
	// `if isinstance(resume_from_checkpoint, str) and resume_from_checkpoint.strip():`
	// — a whitespace-only value is falsy and takes the full-pipeline branch.
	if req.ResumeFromCheckpoint != nil && strings.TrimSpace(*req.ResumeFromCheckpoint) != "" {
		return orchestrator.RunFromCheckpoint(ctx, *req.ResumeFromCheckpoint)
	}

	app := r.app
	nodeID := phases.NodeID()

	app.Note(ctx, "Starting SEC-AF audit pipeline", "audit", "start")
	started := nowMonotonic()

	// --- recon_phase --------------------------------------------------------
	reconDict, err := callMap(ctx, app, nodeID, reasoners.NameReconPhase, map[string]any{
		"repo_path": repoPath,
		"depth":     req.Depth,
	})
	if err != nil {
		return schemas.SecurityAuditResult{}, err
	}
	recon, err := phases.BindReconResult(reconDict)
	if err != nil {
		return schemas.SecurityAuditResult{}, err
	}
	recon.ReconDurationSeconds = nowMonotonic().Sub(started).Seconds()
	if err := orchestrator.WriteCheckpoint(orch.PhaseRecon, recon); err != nil {
		return schemas.SecurityAuditResult{}, err
	}

	// --- hunt_phase ---------------------------------------------------------
	// Python parity: the kwarg is `recon_context=recon_dict` — the RAW payload
	// map returned by recon_phase, NOT recon.model_dump(). The two differ:
	// recon_duration_seconds was just stamped on the MODEL and is still 0.0 in
	// the dict that is passed on.
	huntDict, err := callMap(ctx, app, nodeID, reasoners.NameHuntPhase, map[string]any{
		"repo_path":     repoPath,
		"recon_context": reconDict,
		"depth":         req.Depth,
	})
	if err != nil {
		return schemas.SecurityAuditResult{}, err
	}
	hunt, err := phases.BindHuntResult(huntDict)
	if err != nil {
		return schemas.SecurityAuditResult{}, err
	}
	// Python: `time.monotonic() - started - recon.recon_duration_seconds`, i.e.
	// the hunt's own share of the elapsed time.
	hunt.HuntDurationSeconds = nowMonotonic().Sub(started).Seconds() - recon.ReconDurationSeconds
	if err := orchestrator.WriteCheckpoint(orch.PhaseHunt, hunt); err != nil {
		return schemas.SecurityAuditResult{}, err
	}

	// --- prove_phase --------------------------------------------------------
	// Python parity: here the kwarg IS `hunt.model_dump()` (the model, with the
	// duration stamped), unlike recon_context above.
	huntDump, err := afx.ToMap(hunt)
	if err != nil {
		return schemas.SecurityAuditResult{}, err
	}
	proveDict, err := callMap(ctx, app, nodeID, reasoners.NameProvePhase, map[string]any{
		"repo_path":   repoPath,
		"hunt_result": huntDump,
		"depth":       req.Depth,
		"max_provers": req.MaxProvers,
	})
	if err != nil {
		return schemas.SecurityAuditResult{}, err
	}

	// Python: `prove_dict["verified"]` — a SUBSCRIPT, so a payload without the
	// key raises KeyError (an Exception, not a ValueError => 500).
	verifiedRaw, ok := proveDict["verified"]
	if !ok {
		return schemas.SecurityAuditResult{}, &missingKeyError{Key: "verified", Source: reasoners.NameProvePhase}
	}
	verified, err := bindVerifiedList(verifiedRaw)
	if err != nil {
		return schemas.SecurityAuditResult{}, err
	}

	orchestrator.FindingsNotVerified = mapInt(proveDict, "not_verified", 0)
	// Python: `prove_dict.get("drop_summary", {"demoted_total": 0, "by_reason": {}, "findings": []})`
	// (app.py:205-208) — a `.get`, so the default fires ONLY when the key is
	// ABSENT. A key that is PRESENT with an odd value (a JSON null, a string, a
	// list, a number) is threaded through verbatim and surfaces in the audit
	// result's `metadata.prove_drop_summary`, which is `dict[str, object]` and
	// accepts anything.
	//
	// afx.WireNumbers restores the int-vs-float split CPython's json.loads
	// makes: the summary is stored UNTYPED and re-serialised into
	// `metadata["prove_drop_summary"]`, where a float64 2 would print "2.0"
	// against Python's "2".
	if summary, present := proveDict["drop_summary"]; present {
		orchestrator.ProveDropSummary = afx.WireNumbers(summary)
	} else {
		orchestrator.ProveDropSummary = orch.NewDropSummary()
	}

	if err := orchestrator.WriteCheckpoint(orch.PhaseProve, verified); err != nil {
		return schemas.SecurityAuditResult{}, err
	}

	// --- remediation_phase --------------------------------------------------
	verifiedDumps := make([]any, 0, len(verified))
	for i := range verified {
		dump, dumpErr := afx.ToMap(verified[i])
		if dumpErr != nil {
			return schemas.SecurityAuditResult{}, dumpErr
		}
		verifiedDumps = append(verifiedDumps, dump)
	}
	remediationDict, err := callMap(ctx, app, nodeID, reasoners.NameRemediationPhase, map[string]any{
		"repo_path":         repoPath,
		"verified_findings": verifiedDumps,
	})
	if err != nil {
		return schemas.SecurityAuditResult{}, err
	}
	remediatedRaw, ok := remediationDict["verified"]
	if !ok {
		return schemas.SecurityAuditResult{}, &missingKeyError{Key: "verified", Source: reasoners.NameRemediationPhase}
	}
	verified, err = bindVerifiedList(remediatedRaw)
	if err != nil {
		return schemas.SecurityAuditResult{}, err
	}

	// Python: total_selected + len(hunt.strategies_run) + 3. The "+3" counts the
	// three phase reasoners that are not per-finding fan-outs (recon, hunt,
	// prove); remediation is not counted, and neither is `audit` itself.
	orchestrator.SetAgentInvocations(mapInt(proveDict, "total_selected", 0) + len(hunt.StrategiesRun) + 3)

	result, err := orchestrator.GenerateOutput(ctx, recon, hunt, verified)
	if err != nil {
		return schemas.SecurityAuditResult{}, err
	}

	app.Note(ctx, "SEC-AF audit complete", "audit", "complete")
	return result, nil
}

// ErrBadInput marks a ValueError-class failure raised INSIDE the pipeline,
// i.e. one Python maps to
//
//	except ValueError as exc: raise HTTPException(400, detail={"error": str(exc)})
//
// Wrap an error with %w against this sentinel to have IsBadInput classify it.
var ErrBadInput = errors.New("bad input")

// IsBadInput reports whether err is one of the ValueError-class failures the
// audit pipeline can raise from inside the mapped region.
//
// THREE families reach it, all of them ValueError subclasses in Python:
//
//  1. `ValueError(f"Unknown checkpoint phase: {phase}")` from
//     orchestrator.run_from_checkpoint -> *orch.UnknownCheckpointPhaseError.
//  2. `Model.model_validate(payload)` / `Model(**payload)` anywhere inside the
//     try — app.py:181 ReconResult, :191 HuntResult, :203 and :217
//     VerifiedFinding, plus orchestrator.py's `_read_checkpoint`. pydantic's
//     ValidationError SUBCLASSES ValueError, so EVERY schema failure inside the
//     try takes the 400 branch, not the 500 one -> *phases.ValidationError.
//  3. `json.loads` on a corrupt checkpoint file: `json.JSONDecodeError` is also
//     a ValueError -> encoding/json's *SyntaxError / *UnmarshalTypeError, which
//     are what orch.ReadCheckpoint / ReadCheckpointList and afx.Bind return for
//     the same input.
//
// A failure INSIDE a phase reasoner does not reach this classification with its
// own type: the in-process Call reports it as an opaque *CallError, exactly as
// the control-plane hop reported a failed child execution, so it is a 500.
func IsBadInput(err error) bool {
	var unknownPhase *orch.UnknownCheckpointPhaseError
	if errors.As(err, &unknownPhase) {
		return true
	}
	var validation *phases.ValidationError
	if errors.As(err, &validation) {
		return true
	}
	// json.JSONDecodeError and pydantic's coercion failures are both ValueError
	// subclasses; encoding/json reports the same two conditions as these types.
	var syntaxErr *json.SyntaxError
	if errors.As(err, &syntaxErr) {
		return true
	}
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) {
		return true
	}
	return errors.Is(err, ErrBadInput)
}

// callMap is app.py's
//
//	raw = await app.call(f"{NODE_ID}.{name}", **kwargs)
//	payload = _as_dict(_unwrap(raw, name), name)
//
// app.py declares its own byte-identical copies of _unwrap/_as_dict alongside
// reasoners/phases.py's; afx owns the single Go implementation.
func callMap(ctx context.Context, app appx.Caller, nodeID, name string, input map[string]any) (map[string]any, error) {
	raw, err := app.Call(ctx, nodeID+"."+name, input)
	if err != nil {
		return nil, err
	}
	payload, err := afx.Unwrap(raw, name)
	if err != nil {
		return nil, err
	}
	return afx.AsMap(payload, name)
}

// bindVerifiedList is `[VerifiedFinding.model_validate(v) for v in payload]`
// (app.py:201 and :213).
//
// The comprehension has TWO distinct failure modes, and Python maps them to
// DIFFERENT audit responses, so they are kept apart:
//
//   - the payload is not ITERABLE -> TypeError, an Exception but not a
//     ValueError, so audit()'s `except Exception` answers 500. str(exc) is
//     `'int' object is not iterable`, which notIterableError reproduces.
//   - the payload iterates but an ELEMENT is not a mapping ->
//     `VerifiedFinding.model_validate(5)` raises a pydantic ValidationError,
//     which SUBCLASSES ValueError, so audit() answers 400 with
//     `Input should be a valid dictionary or instance of VerifiedFinding`.
//     A *phases.ValidationError is what IsBadInput routes to that branch.
//
// Iterability follows Python, not Go: a STRING iterates its characters and a
// DICT iterates its keys, so both reach the element branch (and an empty dict
// or empty string yields an empty list, not an error) — only the scalars and
// None are "not iterable".
func bindVerifiedList(payload any) ([]schemas.VerifiedFinding, error) {
	items, ok := pyIterate(payload)
	if !ok {
		return nil, &notIterableError{Got: afx.PyTypeName(payload)}
	}
	out := make([]schemas.VerifiedFinding, 0, len(items))
	for _, item := range items {
		row, isMap := item.(map[string]any)
		if !isMap {
			return nil, &phases.ValidationError{
				Model:  "VerifiedFinding",
				Errors: []string{"Input should be a valid dictionary or instance of VerifiedFinding"},
			}
		}
		finding, err := phases.BindVerifiedFinding(row)
		if err != nil {
			return nil, err
		}
		out = append(out, finding)
	}
	return out, nil
}

// pyIterate is `list(x)` for the JSON value kinds a `.call` payload can hold:
// a list yields its elements, a string its CHARACTERS, a dict its KEYS, and
// everything else (numbers, booleans, None) is not iterable.
func pyIterate(payload any) ([]any, bool) {
	switch v := payload.(type) {
	case []any:
		return v, true
	case string:
		out := make([]any, 0, len(v))
		for _, r := range v {
			out = append(out, string(r))
		}
		return out, true
	case map[string]any:
		// Python iterates a dict in INSERTION order, which a decoded Go map
		// does not carry; sorting keeps the walk deterministic. The order is
		// unobservable here anyway — every key is a string, so whichever comes
		// first fails the element branch identically.
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		out := make([]any, 0, len(keys))
		for _, key := range keys {
			out = append(out, key)
		}
		return out, true
	}
	return nil, false
}

// mapInt is `payload.get(key, def)` coerced to an int.
//
// A JSON number arrives as float64 after the JSON hop and as an int when a Go
// caller built the map in process, so both are accepted; anything else
// (including an absent key) yields def. Python would happily store a float in
// findings_not_verified / agent_invocations; the Go fields are typed int, and
// every producer of these keys emits an integer.
func mapInt(payload map[string]any, key string, def int) int {
	switch v := payload[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case float32:
		return int(v)
	}
	return def
}

// missingKeyError stands in for Python's KeyError on `prove_dict["verified"]`
// (app.py:201) and `remediation_dict["verified"]` (app.py:213).
//
// The TEXT is `str(exc)`, not `repr(exc)`: app.py:229-230 interpolates the
// exception into `f"Audit pipeline failed: {exc}"` and
// `f"audit execution failed: {exc}"`, and `str(KeyError('verified'))` is
// `'verified'` — the repr of the KEY, with quotes and with no class-name
// prefix. Source is kept as a field because it is useful in tests and logs, but
// it must not appear in Error().
type missingKeyError struct {
	Key    string
	Source string
}

func (e *missingKeyError) Error() string {
	return "'" + e.Key + "'"
}

// notIterableError stands in for the TypeError a non-iterable `verified`
// produces. `str(exc)` is `'int' object is not iterable` — again no class-name
// prefix.
//
// Documented residual, inherited from afx.PyTypeName: Go's encoding/json
// decodes every JSON number to float64, so an integral payload says "float"
// where CPython's json.loads would have said "int".
type notIterableError struct{ Got string }

func (e *notIterableError) Error() string {
	return "'" + e.Got + "' object is not iterable"
}
