package harnessx

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Agent-Field/codeaf/internal/praf/appx"

	"github.com/Agent-Field/codeaf/internal/praf/fatal"
)

// --- test fixtures ----------------------------------------------------------

// mockHarness is the HarnessCaller seam the Python tests get by patching
// router.harness. It records what Run passed and returns a scripted result.
type mockHarness struct {
	fn        func(ctx context.Context, prompt string, schema map[string]any, dest any, opts appx.HarnessOptions) (*appx.HarnessResult, error)
	gotOpts   appx.HarnessOptions
	gotSchema map[string]any
	gotPrompt string
}

func (m *mockHarness) Harness(ctx context.Context, prompt string, schema map[string]any, dest any, opts appx.HarnessOptions) (*appx.HarnessResult, error) {
	m.gotOpts = opts
	m.gotSchema = schema
	m.gotPrompt = prompt
	return m.fn(ctx, prompt, schema, dest, opts)
}

// seededResult carries non-zero pydantic-parity defaults via UnmarshalJSON,
// exactly like the real schemas structs (schemas §C.1). Used to prove the
// Parsed==nil path returns seeded defaults, not the Go zero value.
type seededResult struct {
	Complete bool   `json:"complete"`
	Scope    string `json:"estimated_scope"`
}

func (s *seededResult) UnmarshalJSON(b []byte) error {
	*s = seededResult{Complete: true, Scope: "medium"}
	type alias seededResult
	return json.Unmarshal(b, (*alias)(s))
}

// childItem / parentSchema exercise nested $defs, array items, and enum output.
type childItem struct {
	Name string `json:"name"`
}

type parentSchema struct {
	Outcome  string      `json:"outcome" jsonschema:"enum=completed,enum=failed"`
	Children []childItem `json:"children"`
}

// --- schema generation ------------------------------------------------------

// Contract: schema for a nested struct emits $defs / items / enum for the SDK's
// consumption.
func TestSchemaForNestedStructEmitsDefsItemsEnum(t *testing.T) {
	m := schemaFor[parentSchema]()

	props, ok := m["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected top-level properties (ExpandedStruct), got: %v", m)
	}

	// enum on the outcome field.
	outcome, ok := props["outcome"].(map[string]any)
	if !ok {
		t.Fatalf("expected properties.outcome, got: %v", props)
	}
	enum, ok := outcome["enum"].([]any)
	if !ok {
		t.Fatalf("expected properties.outcome.enum, got: %v", outcome)
	}
	if !containsStr(enum, "completed") || !containsStr(enum, "failed") {
		t.Fatalf("enum missing expected values: %v", enum)
	}

	// array items on the children field.
	children, ok := props["children"].(map[string]any)
	if !ok {
		t.Fatalf("expected properties.children, got: %v", props)
	}
	if children["type"] != "array" {
		t.Fatalf("expected children.type=array, got: %v", children["type"])
	}
	if _, ok := children["items"]; !ok {
		t.Fatalf("expected children.items, got: %v", children)
	}

	// $defs for the nested struct type.
	defs, ok := m["$defs"].(map[string]any)
	if !ok || len(defs) == 0 {
		t.Fatalf("expected non-empty $defs for nested type, got: %v", m["$defs"])
	}
}

func TestSchemaForIsCached(t *testing.T) {
	a := schemaFor[parentSchema]()
	b := schemaFor[parentSchema]()
	// Same underlying cached map instance (pointer identity via a mutation probe).
	a["__probe__"] = 1
	if _, ok := b["__probe__"]; !ok {
		t.Fatalf("expected schemaFor to return the cached map instance on repeat calls")
	}
	delete(a, "__probe__")
}

// --- Run: fatal propagation -------------------------------------------------

// Contract: a fatal harness error propagates as *FatalHarnessError via errors.As.
func TestRunFatalErrorPropagates(t *testing.T) {
	mh := &mockHarness{
		fn: func(_ context.Context, _ string, _ map[string]any, _ any, _ appx.HarnessOptions) (*appx.HarnessResult, error) {
			return &appx.HarnessResult{IsError: true, ErrorMessage: "Credit balance is too low"}, nil
		},
	}

	out, res, err := Run[seededResult](context.Background(), mh, "prompt", appx.HarnessOptions{})
	if err == nil {
		t.Fatal("expected an error for a fatal harness result")
	}
	var fe *fatal.FatalHarnessError
	if !errors.As(err, &fe) {
		t.Fatalf("expected *fatal.FatalHarnessError, got %T: %v", err, err)
	}
	if fe.OriginalMessage != "Credit balance is too low" {
		t.Fatalf("expected original message preserved, got %q", fe.OriginalMessage)
	}
	if out != nil {
		t.Fatalf("expected nil value on fatal error, got %v", out)
	}
	if res == nil {
		t.Fatal("expected the *appx.HarnessResult to be returned alongside the fatal error")
	}
}

// --- Run: opts pass-through --------------------------------------------------

// Contract: Run hands the options to the harness unchanged.
func TestRunPassesOptsThrough(t *testing.T) {
	mh := &mockHarness{
		fn: func(_ context.Context, _ string, _ map[string]any, dest any, _ appx.HarnessOptions) (*appx.HarnessResult, error) {
			return &appx.HarnessResult{Parsed: dest}, nil
		},
	}
	base := appx.HarnessOptions{Cwd: "/repo", Label: "review"}
	if _, _, err := Run[seededResult](context.Background(), mh, "prompt", base); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mh.gotOpts != base {
		t.Fatalf("options = %+v, want %+v", mh.gotOpts, base)
	}
}

// --- Run: Parsed==nil fallback path -----------------------------------------

// Contract: on Result.Parsed == nil (schema parse failure), Run returns the
// default-seeded value plus the Result, NOT an error.
func TestRunParsedNilReturnsSeededDefaults(t *testing.T) {
	mh := &mockHarness{
		fn: func(_ context.Context, _ string, _ map[string]any, _ any, _ appx.HarnessOptions) (*appx.HarnessResult, error) {
			// Non-fatal error result with no parsed output.
			return &appx.HarnessResult{IsError: true, ErrorMessage: "schema validation failed after retries", Parsed: nil}, nil
		},
	}

	out, res, err := Run[seededResult](context.Background(), mh, "prompt", appx.HarnessOptions{})
	if err != nil {
		t.Fatalf("expected no error on Parsed==nil, got %v", err)
	}
	if out == nil {
		t.Fatal("expected a seeded default value, got nil")
	}
	if !out.Complete || out.Scope != "medium" {
		t.Fatalf("expected seeded defaults (Complete=true, Scope=medium), got %+v", *out)
	}
	if res == nil || !res.IsError {
		t.Fatal("expected the failing Result returned so the caller can inspect IsError")
	}
}

// --- Run: success path ------------------------------------------------------

func TestRunSuccessReturnsParsed(t *testing.T) {
	mh := &mockHarness{
		fn: func(_ context.Context, _ string, _ map[string]any, dest any, _ appx.HarnessOptions) (*appx.HarnessResult, error) {
			d := dest.(*seededResult)
			d.Complete = false
			d.Scope = "large"
			return &appx.HarnessResult{Parsed: dest}, nil
		},
	}

	out, _, err := Run[seededResult](context.Background(), mh, "prompt", appx.HarnessOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out == nil || out.Complete != false || out.Scope != "large" {
		t.Fatalf("expected parsed value returned, got %+v", out)
	}
}

func TestRunPropagatesTransportError(t *testing.T) {
	sentinel := errors.New("boom")
	mh := &mockHarness{
		fn: func(_ context.Context, _ string, _ map[string]any, _ any, _ appx.HarnessOptions) (*appx.HarnessResult, error) {
			return nil, sentinel
		},
	}
	_, _, err := Run[seededResult](context.Background(), mh, "prompt", appx.HarnessOptions{})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected transport error propagated, got %v", err)
	}
}

// --- helpers ----------------------------------------------------------------

func containsStr(xs []any, want string) bool {
	for _, x := range xs {
		if s, ok := x.(string); ok && s == want {
			return true
		}
	}
	return false
}
