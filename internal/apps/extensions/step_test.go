package extensions_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/extensions"
)

func TestPureStepComputesAndEnforcesLimits(t *testing.T) {
	s := extensions.Step{
		Version: 1, Reason: "No built-in step calculates this value.", Summary: "Compute a date span.",
		Inputs: []extensions.Input{{Name: "from", Type: "date"}, {Name: "to", Type: "date"}},
		Output: "integer", Effects: []string{}, TimeoutMS: 50, MemoryPages: 1,
		Source: `(module (func (export "run") (param i64 i64) (result i64) local.get 1 local.get 0 i64.sub))`,
	}
	got, err := s.Run(context.Background(), map[string]any{"from": "2026-11-26", "to": "2026-12-01"})
	if err != nil || got != int64(5) {
		t.Fatalf("date span = %v, %v", got, err)
	}

	s.Inputs = nil
	s.Source = `(module (func (export "run") (result i64) (loop $again br $again) i64.const 0))`
	if _, err := s.Run(context.Background(), nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("unbounded guest returned %v", err)
	}

	for _, source := range []string{
		`(module (import "host" "action" (func)) (func (export "run") (result i64) i64.const 0))`,
		`(module (memory 2) (func (export "run") (result i64) i64.const 0))`,
	} {
		s.Source = source
		if err := s.Check(); err == nil {
			t.Fatalf("Check accepted %s", source)
		}
	}
}
