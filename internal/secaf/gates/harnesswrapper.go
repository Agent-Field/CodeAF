package gates

import (
	"context"
	"reflect"
	"strconv"
	"sync"

	"golang.org/x/sync/semaphore"

	"github.com/Agent-Field/codeaf/internal/secaf/appx"
	"github.com/Agent-Field/codeaf/internal/secaf/config"
	"github.com/Agent-Field/codeaf/internal/secaf/harnessx"
)

// maxSchemaRetries ports HarnessWrapper._invoke_with_schema_retry's local
// `max_schema_retries = 3`.
const maxSchemaRetries = 3

// HarnessWrapper ports harness.py HarnessWrapper.
//
// NOT ON THE LIVE PATH. Nothing in the Python tree constructs a HarnessWrapper:
// every agent module calls `app.harness(...)` directly and hands the result to
// extract_harness_result (ported as internal/harnessx). The class is ported
// because the port is 1:1 and because its prompt assembly (prompts.go) is
// worth pinning; the invocation machinery below is kept deliberately compact.
type HarnessWrapper struct {
	// App is the `.harness(...)` seam.
	App appx.Harnesser
	// Config supplies the retry schedule.
	Config config.AIIntegrationConfig
	// Sleep is the backoff sleeper; nil means the real clock.
	Sleep Sleeper

	mu              sync.Mutex
	totalCostUSD    float64
	invocationCount int
}

// NewHarnessWrapper is `HarnessWrapper(app=app, config=config)`; a nil config
// takes config.DefaultAIConfig(), where Python ran AIIntegrationConfig.from_env().
// The error return is always nil now and is kept for the shape NewAIGate shares.
func NewHarnessWrapper(app appx.Harnesser, cfg *config.AIIntegrationConfig) (*HarnessWrapper, error) {
	if cfg != nil {
		return &HarnessWrapper{App: app, Config: *cfg}, nil
	}
	return &HarnessWrapper{App: app, Config: config.DefaultAIConfig()}, nil
}

// TotalCostUSD ports the `total_cost_usd` property.
func (w *HarnessWrapper) TotalCostUSD() float64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.totalCostUSD
}

// InvocationCount ports the `invocation_count` property.
func (w *HarnessWrapper) InvocationCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.invocationCount
}

func (w *HarnessWrapper) registerInvocation() {
	w.mu.Lock()
	w.invocationCount++
	w.mu.Unlock()
}

// registerCost ports _CostTracker.register_cost. Unlike the AI gate's, this one
// is fed a real number: the Python SDK's HarnessResult carries `cost_usd`, and
// appx.HarnessResult carries CostUSD.
func (w *HarnessWrapper) registerCost(costUSD *float64) {
	if costUSD == nil || *costUSD < 0 {
		return
	}
	w.mu.Lock()
	w.totalCostUSD += *costUSD
	w.mu.Unlock()
}

func (w *HarnessWrapper) sleeper() Sleeper {
	if w.Sleep != nil {
		return w.Sleep
	}
	return sleepReal
}

// InvokeRequest is the keyword-argument set of HarnessWrapper.invoke:
//
//	invoke(*, prompt, schema, cwd, project_dir=None, model=None,
//	       max_turns=None, max_budget_usd=None, phase=None)
//
// `schema` is the Go type parameter of HarnessInvoke, not a field. The zero
// value of each remaining field is Python's None.
//
// `model`, `max_turns` and `max_budget_usd` are not carried: they were options
// of the external CLI the harness used to drive, and codeaf's harness owns its
// own model and turn budget, so appx.HarnessOptions has nowhere to put them.
type InvokeRequest struct {
	Prompt     string
	Cwd        string
	ProjectDir string
	Phase      string
}

// harnessOptions builds the appx.HarnessOptions for one invocation — the cwd
// and project_dir keyword arguments HarnessWrapper passed to app.harness. The
// rest of what Python passed (provider, model, max_turns, max_budget_usd, the
// provider environment, schema_max_retries=0, an opencode server) configured an
// external CLI that codeaf does not run, and was dropped with it.
func (w *HarnessWrapper) harnessOptions(req InvokeRequest) appx.HarnessOptions {
	return appx.HarnessOptions{
		Cwd:        req.Cwd,
		ProjectDir: req.ProjectDir,
	}
}

// HarnessInvoke ports HarnessWrapper.invoke.
//
//	self._cost_tracker.register_invocation()
//	enhanced_prompt = f"{_with_phase_guidance(prompt, phase, cwd)}\n\n{_schema_guidance(schema)}"
//	result = await self._run_with_retry(_operation, self.config)
//	self._cost_tracker.register_cost(result.cost_usd)
//	if result.is_error: raise AIIntegrationError(f"Harness failure{f' ({phase})' if phase else ''}: {message}")
//	if isinstance(parsed, schema): return parsed
//	...
//	if not parsed and not result.is_error: return await self._invoke_with_schema_retry(...)
//	raise AIIntegrationError(f"Harness returned invalid payload for schema {schema.__name__}")
//
// Python parity notes:
//
//   - The retry wrapper covers the SDK CALL only. A harness that RAN and failed
//     comes back with is_error set and a nil Go error, so it never reaches
//     _run_with_retry's except clause — it goes straight to the
//     AIIntegrationError raise, transient message or not. That is Python's
//     behavior too.
//   - The phase suffix on the failure message is the empty string when phase is
//     falsy, so an empty phase contributes nothing to the text, not " ()".
//   - The `isinstance(result, schema)` third branch is Python duck-typing for an
//     SDK that returns the model itself; the Go SDK always returns a
//     *appx.HarnessResult, so it is unreachable and not ported.
func HarnessInvoke[T any](ctx context.Context, w *HarnessWrapper, req InvokeRequest) (T, error) {
	w.registerInvocation()
	enhancedPrompt := WithPhaseGuidance(req.Prompt, req.Phase, req.Cwd) + "\n\n" + SchemaGuidance[T]()

	// Python passes prompt=enhanced_prompt down to _invoke_with_schema_retry,
	// which never reads it (it rebuilds the prompt from the schema); carried
	// through anyway so the two call graphs line up.
	retryReq := req
	retryReq.Prompt = enhancedPrompt
	return harnessAttempt[T](ctx, w, enhancedPrompt, retryReq, 0, false)
}

// harnessAttempt is the shared body of invoke and _invoke_with_schema_retry.
// schemaRetry == false runs invoke's operation with the caller's prompt;
// schemaRetry == true runs _invoke_with_schema_retry's, which rebuilds the
// prompt from _build_schema_retry_prompt at each level of the recursion.
func harnessAttempt[T any](ctx context.Context, w *HarnessWrapper, prompt string, req InvokeRequest, retryCount int, schemaRetry bool) (T, error) {
	var zero T
	typeName := reflect.TypeOf((*T)(nil)).Elem().Name()

	if schemaRetry {
		// _invoke_with_schema_retry:
		//   if retry_count >= max_schema_retries: raise AIIntegrationError(...)
		//   error_detail = f"Retry attempt {retry_count + 1}/{max_schema_retries}"
		//   retry_task = _build_schema_retry_prompt(schema, error_detail, cwd)
		//   retry_prompt = f"{_with_phase_guidance(retry_task, phase, cwd)}\n\n{_schema_guidance(schema)}"
		if retryCount >= maxSchemaRetries {
			return zero, newAIIntegrationError(
				"Schema validation failed after " + strconv.Itoa(maxSchemaRetries) + " retries with schema context")
		}
		errorDetail := "Retry attempt " + strconv.Itoa(retryCount+1) + "/" + strconv.Itoa(maxSchemaRetries)
		retryTask := BuildSchemaRetryPrompt[T](errorDetail, req.Cwd)
		prompt = WithPhaseGuidance(retryTask, req.Phase, req.Cwd) + "\n\n" + SchemaGuidance[T]()
	}

	opts := w.harnessOptions(req)

	type attemptResult struct {
		dest *T
		res  *appx.HarnessResult
	}
	operation := func() (attemptResult, error) {
		dest, res, runErr := harnessx.Run[T](ctx, w.App, prompt, opts)
		return attemptResult{dest: dest, res: res}, runErr
	}

	out, err := runWithRetry(ctx, w.Config, w.sleeper(), operation)
	if err != nil {
		return zero, err
	}
	if out.res != nil {
		w.registerCost(out.res.CostUSD)
	}

	if out.res != nil && out.res.IsError {
		message := out.res.ErrorMessage
		if message == "" {
			message = "unknown harness error"
		}
		suffix := ""
		if req.Phase != "" {
			suffix = " (" + req.Phase + ")"
		}
		return zero, newAIIntegrationError("Harness failure" + suffix + ": " + message)
	}

	if out.res != nil && out.res.Parsed != nil {
		if out.dest != nil {
			return *out.dest, nil
		}
		// Python's trailing `raise AIIntegrationError(f"Harness returned
		// invalid payload for schema {schema.__name__}")`: reached when parsed
		// is truthy but is neither the schema nor a dict. The Go SDK always
		// stores the dest pointer it was handed, so this is unreachable here;
		// it is kept so the error surface matches.
		return zero, newAIIntegrationError("Harness returned invalid payload for schema " + typeName)
	}

	// `if not parsed` — the SDK produced no schema-valid object, so recurse with
	// the schema spelled out in the prompt.
	//
	// Python parity: invoke's fall-through calls _invoke_with_schema_retry with
	// its DEFAULT retry_count=0, and only the schema-retry path increments. So
	// a run that never produces a parsed object makes 1 + max_schema_retries ==
	// 4 harness calls, and the error details read "Retry attempt 1/3", "2/3",
	// "3/3" in that order.
	next := 0
	if schemaRetry {
		next = retryCount + 1
	}
	return harnessAttempt[T](ctx, w, prompt, req, next, true)
}

// BatchResult is one slot of `asyncio.gather(..., return_exceptions=True)`:
// either the value or the exception, never both.
type BatchResult[T any] struct {
	Value T
	Err   error
}

// HarnessInvokeBatch ports HarnessWrapper.invoke_batch:
//
//	semaphore = asyncio.Semaphore(max_concurrent) if max_concurrent else None
//	tasks = [_run_request(r) for r in requests]
//	return await asyncio.gather(*tasks, return_exceptions=True)
//
// Python parity notes:
//
//   - RESULT ORDER matches REQUEST order (gather preserves it), so results are
//     written into a pre-indexed slice rather than collected as they finish.
//   - `return_exceptions=True` means one failure does NOT cancel the others;
//     every request runs to completion and failures land in their own slot.
//     There is no errgroup here for exactly that reason.
//   - `max_concurrent=None` (spelled as a non-positive value in Go) means
//     UNBOUNDED, which is why the semaphore is conditional.
//   - Acquiring the weighted semaphore can fail only if ctx is already done; in
//     that case the slot records the ctx error, which is the closest analogue of
//     asyncio cancelling a queued task.
func HarnessInvokeBatch[T any](ctx context.Context, w *HarnessWrapper, requests []InvokeRequest, maxConcurrent int) []BatchResult[T] {
	results := make([]BatchResult[T], len(requests))
	var sem *semaphore.Weighted
	if maxConcurrent > 0 {
		sem = semaphore.NewWeighted(int64(maxConcurrent))
	}

	var wg sync.WaitGroup
	for i := range requests {
		wg.Add(1)
		go func(idx int, req InvokeRequest) {
			defer wg.Done()
			if sem != nil {
				if err := sem.Acquire(ctx, 1); err != nil {
					results[idx] = BatchResult[T]{Err: err}
					return
				}
				defer sem.Release(1)
			}
			v, err := HarnessInvoke[T](ctx, w, req)
			results[idx] = BatchResult[T]{Value: v, Err: err}
		}(i, requests[i])
	}
	wg.Wait()
	return results
}

// RunReconAnalysis ports HarnessWrapper.run_recon_analysis — invoke with
// phase="recon".
func RunReconAnalysis[T any](ctx context.Context, w *HarnessWrapper, prompt, cwd, projectDir string) (T, error) {
	return HarnessInvoke[T](ctx, w, InvokeRequest{Prompt: prompt, Cwd: cwd, ProjectDir: projectDir, Phase: "recon"})
}

// RunHuntAnalysis ports HarnessWrapper.run_hunt_analysis — invoke with
// phase="hunt".
func RunHuntAnalysis[T any](ctx context.Context, w *HarnessWrapper, prompt, cwd, projectDir string) (T, error) {
	return HarnessInvoke[T](ctx, w, InvokeRequest{Prompt: prompt, Cwd: cwd, ProjectDir: projectDir, Phase: "hunt"})
}

// RunProveAnalysis ports HarnessWrapper.run_prove_analysis — invoke with
// phase="prove".
func RunProveAnalysis[T any](ctx context.Context, w *HarnessWrapper, prompt, cwd, projectDir string) (T, error) {
	return HarnessInvoke[T](ctx, w, InvokeRequest{Prompt: prompt, Cwd: cwd, ProjectDir: projectDir, Phase: "prove"})
}
