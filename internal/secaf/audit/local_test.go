package audit

// Tests for the in-process Caller.
//
// Validation contract (behaviour, derived from what the control-plane hop did
// to a `.call` — sdk/go/agent's Call and the node's request decoding):
//
//   - the wrapper's registry carries exactly the 33 reasoner names, in
//     DESIGN.md §3 order, and `audit` is not one of them;
//   - `sec-af.<name>` and a bare `<name>` both resolve; any other node, and an
//     unknown name, is an error that names the target;
//   - input and result each cross a JSON round trip: numbers arrive as float64,
//     typed values arrive as plain maps, a null result is a nil map with no
//     error, and a non-object result is an error;
//   - a handler failure reaches the caller as a *CallError carrying exactly the
//     handler's message and none of its type; a cancelled call still unwraps to
//     the context's error; a panic is an error, not a crash;
//   - Run never uses the wrapped App's own Call: the phase calls and the leaf
//     calls under them all resolve in process.

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/secaf/appx"
	"github.com/Agent-Field/codeaf/internal/secaf/phases"
	"github.com/Agent-Field/codeaf/internal/secaf/reasoners"
)

// localWith returns a local App whose registry holds exactly the given handlers,
// for the transport tests that should not run a real reasoner.
func localWith(t *testing.T, handlers map[string]reasoners.Handler) *localApp {
	t.Helper()
	l := &localApp{inner: &appx.Fake{}, reg: reasoners.NewRegistry()}
	for name, h := range handlers {
		l.reg.Register(name, h, nil)
	}
	return l
}

func TestWithLocalCallsRegistersTheReasonerSurface(t *testing.T) {
	app := WithLocalCalls(&appx.Fake{})
	l, ok := app.(*localApp)
	if !ok {
		t.Fatalf("WithLocalCalls returned %T", app)
	}
	if got := l.reg.Names(); !reflect.DeepEqual(got, reasoners.Names) {
		t.Fatalf("registry = %v, want %v", got, reasoners.Names)
	}
	if len(l.reg.Names()) != 33 {
		t.Errorf("registry holds %d reasoners, want 33", len(l.reg.Names()))
	}
	if _, ok := l.reg.Lookup(reasoners.NameAudit); ok {
		t.Error("audit is the entry point, not a reasoner the pipeline calls")
	}
	if again := WithLocalCalls(app); again != app {
		t.Error("wrapping a local App again must return it unchanged")
	}
}

func TestLocalCallResolvesQualifiedAndBareNames(t *testing.T) {
	l := localWith(t, map[string]reasoners.Handler{
		"run_x": func(context.Context, map[string]any) (any, error) {
			return map[string]any{"ok": true}, nil
		},
	})
	for _, target := range []string{phases.NodeID() + ".run_x", "run_x"} {
		got, err := l.Call(context.Background(), target, nil)
		if err != nil {
			t.Fatalf("Call(%q): %v", target, err)
		}
		if got["ok"] != true {
			t.Errorf("Call(%q) = %v", target, got)
		}
	}

	for _, target := range []string{"other-node.run_x", "sec-af.run_missing", "run_missing"} {
		_, err := l.Call(context.Background(), target, nil)
		var callErr *CallError
		if !errors.As(err, &callErr) {
			t.Fatalf("Call(%q) error = %T %v, want *CallError", target, err, err)
		}
		if !strings.Contains(callErr.Error(), target) {
			t.Errorf("Call(%q) error %q does not name the target", target, callErr.Error())
		}
	}
}

func TestLocalCallRoundTripsThroughJSON(t *testing.T) {
	type typed struct {
		Count int    `json:"count"`
		Name  string `json:"name"`
	}
	var seen map[string]any
	l := localWith(t, map[string]reasoners.Handler{
		"run_echo": func(_ context.Context, in map[string]any) (any, error) {
			seen = in
			return struct {
				Total int   `json:"total"`
				Typed typed `json:"typed"`
			}{Total: 3, Typed: typed{Count: 2, Name: "n"}}, nil
		},
	})

	n := 9
	got, err := l.Call(context.Background(), "sec-af.run_echo", map[string]any{
		"max_provers": &n,
		"none":        (*int)(nil),
		"item":        typed{Count: 1, Name: "a"},
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}

	// The handler sees what the node decoded from the request body.
	if seen["max_provers"] != float64(9) {
		t.Errorf("max_provers = %#v, want float64(9)", seen["max_provers"])
	}
	if v, present := seen["none"]; !present || v != nil {
		t.Errorf("none = %#v (present %v), want a present null", v, present)
	}
	if item, ok := seen["item"].(map[string]any); !ok || item["count"] != float64(1) || item["name"] != "a" {
		t.Errorf("item = %#v, want the decoded object", seen["item"])
	}

	// The caller sees what it decoded from the execution result.
	if got["total"] != float64(3) {
		t.Errorf("total = %#v, want float64(3)", got["total"])
	}
	if typedOut, ok := got["typed"].(map[string]any); !ok || typedOut["count"] != float64(2) {
		t.Errorf("typed = %#v, want a decoded object", got["typed"])
	}
}

func TestLocalCallResultShapes(t *testing.T) {
	l := localWith(t, map[string]reasoners.Handler{
		"run_null": func(context.Context, map[string]any) (any, error) { return nil, nil },
		"run_list": func(context.Context, map[string]any) (any, error) { return []any{1}, nil },
	})

	got, err := l.Call(context.Background(), "sec-af.run_null", nil)
	if err != nil || got != nil {
		t.Errorf("null result = (%#v, %v), want (nil, nil)", got, err)
	}

	if _, err := l.Call(context.Background(), "sec-af.run_list", nil); err == nil {
		t.Error("a non-object result must be an error, as the SDK's decode was")
	}
}

func TestLocalCallFailuresAreOpaque(t *testing.T) {
	l := localWith(t, map[string]reasoners.Handler{
		"run_invalid": func(context.Context, map[string]any) (any, error) {
			return nil, &phases.ValidationError{Model: "ReconResult", Errors: []string{"bad"}}
		},
		"run_panics": func(context.Context, map[string]any) (any, error) {
			panic("boom")
		},
	})

	_, err := l.Call(context.Background(), "sec-af.run_invalid", nil)
	var callErr *CallError
	if !errors.As(err, &callErr) {
		t.Fatalf("error = %T, want *CallError", err)
	}
	want := (&phases.ValidationError{Model: "ReconResult", Errors: []string{"bad"}}).Error()
	if err.Error() != want {
		t.Errorf("message = %q, want the handler's own %q", err.Error(), want)
	}
	var validation *phases.ValidationError
	if errors.As(err, &validation) {
		t.Error("the handler's error type leaked through the call; the wire hop never carried it")
	}
	if IsBadInput(err) {
		t.Error("a failed child call must not classify as the audit's own bad input")
	}

	_, err = l.Call(context.Background(), "sec-af.run_panics", nil)
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Errorf("panic = %v, want an error carrying the panic value", err)
	}
}

func TestLocalCallCancellationUnwraps(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	l := localWith(t, map[string]reasoners.Handler{
		"run_wait": func(ctx context.Context, _ map[string]any) (any, error) {
			cancel()
			<-ctx.Done()
			return nil, ctx.Err()
		},
	})
	_, err := l.Call(ctx, "sec-af.run_wait", nil)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want it to unwrap to context.Canceled", err)
	}
}

func TestLocalCallDelegatesHarnessAINote(t *testing.T) {
	fake := &appx.Fake{
		HarnessFn: func(context.Context, string, map[string]any, any, appx.HarnessOptions) (*appx.HarnessResult, error) {
			return &appx.HarnessResult{Result: "r"}, nil
		},
	}
	app := WithLocalCalls(fake)
	if _, err := app.Harness(context.Background(), "p", nil, nil, appx.HarnessOptions{Cwd: "/c"}); err != nil {
		t.Fatalf("Harness: %v", err)
	}
	app.Note(context.Background(), "hello", "t")
	_, _ = app.AI(context.Background(), "q")
	if len(fake.Harnesses) != 1 || fake.Harnesses[0].Opts.Cwd != "/c" {
		t.Errorf("harness calls = %+v", fake.Harnesses)
	}
	if len(fake.AIs) != 1 || fake.AIs[0].Prompt != "q" {
		t.Errorf("ai calls = %+v", fake.AIs)
	}
	if !reflect.DeepEqual(fake.NoteMessages(), []string{"hello"}) {
		t.Errorf("notes = %v", fake.NoteMessages())
	}
}

// TestRunResolvesEveryCallInProcess drives Run end to end against a repository
// on disk with a harness that fails every run. The audit's outcome does not
// matter here; what does is that the phases and the leaf reasoners under them
// were reached — the harness ran — while the wrapped App's own Call, which
// would have recorded the attempt, was never used.
func TestRunResolvesEveryCallInProcess(t *testing.T) {
	repo := t.TempDir()
	fake := &appx.Fake{
		HarnessFn: func(context.Context, string, map[string]any, any, appx.HarnessOptions) (*appx.HarnessResult, error) {
			return &appx.HarnessResult{IsError: true, ErrorMessage: "no model in this test"}, nil
		},
	}

	_, _ = Run(context.Background(), fake, AuditRequest{
		RepoURL:           repo,
		Depth:             "quick",
		Branch:            "main",
		SeverityThreshold: "low",
	})

	if len(fake.Calls) != 0 {
		t.Errorf("the wrapped App's Call was used: %v", fake.CallTargets())
	}
	if len(fake.Harnesses) == 0 {
		t.Error("no harness ran: the phase calls never reached the leaf reasoners")
	}
	msgs := fake.NoteMessages()
	if len(msgs) == 0 || msgs[0] != "Starting SEC-AF audit pipeline" {
		t.Errorf("notes = %v, want the pipeline to have started", msgs)
	}
}

func TestRunRefusesARepositoryThatIsNotLocal(t *testing.T) {
	fake := &appx.Fake{}
	_, err := Run(context.Background(), fake, AuditRequest{RepoURL: "git@github.com:o/r.git", Depth: "quick"})
	var auditErr *Error
	if !errors.As(err, &auditErr) || auditErr.StatusCode != 400 {
		t.Fatalf("error = %#v, want a 400 *Error", err)
	}
	if !errors.Is(err, ErrRemoteRepository) {
		t.Errorf("error = %v, want ErrRemoteRepository", err)
	}
	if len(fake.Notes) != 0 || len(fake.Harnesses) != 0 {
		t.Error("a refused repository must not start the pipeline")
	}
}

func TestRunInputValidatesTheBody(t *testing.T) {
	_, err := RunInput(context.Background(), &appx.Fake{}, map[string]any{"repo_url": nil})
	var auditErr *Error
	if !errors.As(err, &auditErr) || auditErr.StatusCode != 422 {
		t.Fatalf("error = %#v, want a 422 *Error", err)
	}
	if auditErr.Message != "Field 'repo_url' cannot be None" {
		t.Errorf("message = %q", auditErr.Message)
	}
}
