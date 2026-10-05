package harnessx

import (
	"context"
	"fmt"
	"io"
	"reflect"

	"github.com/Agent-Field/codeaf/internal/secaf/appx"
	"github.com/Agent-Field/codeaf/internal/secaf/pyfmt"
)

// Diagnostics is where Extract writes the diagnostic block Python printed to
// stdout for a failed or unparseable harness run. It is io.Discard by default:
// inside codeaf stdout is the terminal the person is looking at, so the lines
// go nowhere unless the codeaf side points this at a log. Set it before any
// audit runs; it is read concurrently by every agent.
var Diagnostics io.Writer = io.Discard

// Run is the Go form of
//
//	result = await app.harness(prompt=prompt, schema=Model, cwd=..., project_dir=...)
//
// It resolves Model's schema by Go type name (SchemaFor), allocates the
// destination, and calls app.Harness.
//
// The returned error is NON-NIL ONLY when the run could not be made at all. A
// harness that ran and failed (the agent gave up, unparseable output, schema
// validation exhausted) comes back as a nil error with HarnessResult.IsError
// set, exactly as the Python SDK returned a HarnessResult with is_error=True
// rather than raising; appx.HarnessResult documents that contract for the
// codeaf implementation. Turning that into an error is Extract's job, so the
// diagnostic block is written in one place.
//
// *T is always non-nil so callers can hand it straight to Extract; it holds the
// harness-populated value when HarnessResult.Parsed is set and a
// default-seeded value otherwise.
func Run[T any](ctx context.Context, app appx.Harnesser, prompt string, opts appx.HarnessOptions) (*T, *appx.HarnessResult, error) {
	schema := SchemaFor[T]()
	dest := new(T)
	res, err := app.Harness(ctx, prompt, schema, dest, opts)
	if err != nil {
		return dest, res, err
	}
	return dest, res, nil
}

// Extract ports extract_harness_result from src/sec_af/agents/_utils.py exactly:
//
//	def extract_harness_result(result, schema, agent_name):
//	    is_error = bool(getattr(result, "is_error", False))
//	    if is_error:
//	        ...
//	        print(f"[{agent_name}] HARNESS ERROR: {error_message}\n"
//	              f"  turns={num_turns}, duration_ms={duration_ms}\n"
//	              f"  result_text={str(result_text)[:500] if result_text else None}",
//	              flush=True)
//	        raise RuntimeError(f"{agent_name} harness error: {error_message}")
//	    parsed = getattr(result, "parsed", None)
//	    if isinstance(parsed, schema):
//	        return parsed
//	    debug_message = (...)
//	    if isinstance(parsed, dict):
//	        try: return schema.model_validate(parsed)
//	        except Exception: print(debug_message, flush=True); raise
//	    print(debug_message, flush=True)
//	    raise TypeError(f"{agent_name} did not return a valid {schema.__name__}")
//
// dest is the pointer Run handed to app.Harness. An appx.Harnesser sets
// HarnessResult.Parsed to that same pointer once it holds a schema-valid
// object (the SDK runner this seam used to drive did exactly that), so a
// non-nil Parsed means "*dest is populated and schema-valid" — the exact
// condition `isinstance(parsed, schema)` tests in Python.
//
// Python parity notes:
//
//   - The diagnostics are reproduced verbatim (written to Diagnostics rather
//     than stdout), including the
//     `result_text=None` spelling that Python's `... if result_text else None`
//     produces for an EMPTY result string (falsy, not just missing), and the
//     500-character (not byte) truncation of Python's str slice.
//   - `type(result).__name__` in the debug line is the Python SDK's
//     `HarnessResult`; Go's concrete type is `appx.HarnessResult`, so the line
//     prints "Result". The line is diagnostic only.
//   - The `isinstance(parsed, dict)` branch is Python duck-typing for a SDK that
//     may hand back a raw dict. The Go SDK's Parsed is always the `dest` pointer
//     it was given, so that branch is unreachable here and is deliberately not
//     ported.
func Extract[T any](res *appx.HarnessResult, dest *T, agentName string) (T, error) {
	var zero T
	typeName := reflect.TypeOf((*T)(nil)).Elem().Name()

	if res == nil {
		// Python: getattr(None, "is_error", False) is False and
		// getattr(None, "parsed", None) is None, so a missing result falls
		// straight to the TypeError branch after the debug line.
		fmt.Fprintf(Diagnostics, "[%s] harness result type=%s, is_error=%s, parsed type=%s\n",
			agentName, "NoneType", "False", "NoneType")
		return zero, fmt.Errorf("%s did not return a valid %s", agentName, typeName)
	}

	if res.IsError {
		// Python: `str(result_text)[:500] if result_text else None` — an empty
		// result string is falsy, so it prints the literal "None".
		resultText := "None"
		if res.Result != "" {
			resultText = runeSlice(res.Result, 500)
		}
		fmt.Fprintf(Diagnostics, "[%s] HARNESS ERROR: %s\n  turns=%d, duration_ms=%d\n  result_text=%s\n",
			agentName, res.ErrorMessage, res.NumTurns, res.DurationMS, resultText)
		return zero, fmt.Errorf("%s harness error: %s", agentName, res.ErrorMessage)
	}

	if res.Parsed != nil && dest != nil {
		return *dest, nil
	}

	parsedType := "NoneType"
	if res.Parsed != nil {
		parsedType = reflect.TypeOf(res.Parsed).String()
	}
	fmt.Fprintf(Diagnostics, "[%s] harness result type=%s, is_error=%s, parsed type=%s\n",
		agentName, "Result", pyfmt.Str(res.IsError), parsedType)
	return zero, fmt.Errorf("%s did not return a valid %s", agentName, typeName)
}

// RunExtract is the shape every SEC-AF agent module actually uses:
//
//	result = await app.harness(prompt=prompt, schema=Model, cwd=harness_cwd, project_dir=repo_path)
//	return extract_harness_result(result, Model, "AgentName")
//
// A transport error from app.Harness propagates unchanged (Python would let the
// SDK's exception propagate out of the `await` the same way); everything else
// goes through Extract.
func RunExtract[T any](ctx context.Context, app appx.Harnesser, prompt string, opts appx.HarnessOptions, agentName string) (T, error) {
	// THE AGENT'S NAME RIDES THE OPTIONS, so the session it runs is named by
	// it on the audit's page; it is the name this agent's errors already say.
	if opts.Label == "" {
		opts.Label = agentName
	}
	dest, res, err := Run[T](ctx, app, prompt, opts)
	if err != nil {
		var zero T
		return zero, err
	}
	return Extract[T](res, dest, agentName)
}

// runeSlice reproduces Python's s[:n], which counts Unicode code points, not
// bytes.
func runeSlice(s string, n int) string {
	if n < 0 {
		n = 0
	}
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n])
}
