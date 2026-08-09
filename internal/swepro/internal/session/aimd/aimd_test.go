package aimd

import (
	"math"
	"reflect"
	"testing"
)

// Translation of src/session/aimd.test.ts. Subtest names are verbatim.

func ptr(v float64) *float64 { return &v }

// trace mirrors the helper at the top of aimd.test.ts.
func trace(t *testing.T, controller *AimdController, observations []bool) []float64 {
	t.Helper()
	windows := []float64{controller.Window()}
	for _, mergeOk := range observations {
		controller.Observe(mergeOk)
		windows = append(windows, controller.Window())
	}
	return windows
}

func mustNew(t *testing.T, options *AimdOptions) *AimdController {
	t.Helper()
	controller, err := NewAimdController(options)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return controller
}

func TestAimdController(t *testing.T) {
	t.Run("follows the additive-increase/multiplicative-decrease sawtooth", func(t *testing.T) {
		controller := mustNew(t, &AimdOptions{Cap: ptr(10)})

		got := trace(t, controller, []bool{true, true, true, false, true, true, true, false})
		want := []float64{1, 2, 3, 4, 2, 3, 4, 5, 2}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got %v, want %v", got, want)
		}
	})

	t.Run("never increases past the cap", func(t *testing.T) {
		controller, err := CreateAimdController(&AimdOptions{InitialWindow: ptr(2), Cap: ptr(4)})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		got := trace(t, controller, []bool{true, true, true, true})
		want := []float64{2, 3, 4, 4, 4}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got %v, want %v", got, want)
		}
	})

	t.Run("floors halved windows at one", func(t *testing.T) {
		controller := mustNew(t, &AimdOptions{InitialWindow: ptr(3)})

		got := trace(t, controller, []bool{false, false, false})
		want := []float64{3, 1, 1, 1}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got %v, want %v", got, want)
		}
	})

	t.Run("respects injected increase, decrease, cap, and floor", func(t *testing.T) {
		controller := mustNew(t, &AimdOptions{
			InitialWindow: ptr(2),
			Increase:      ptr(2),
			Decrease:      ptr(0.25),
			Cap:           ptr(6),
			Floor:         ptr(2),
		})

		got := trace(t, controller, []bool{true, true, true, false, false})
		want := []float64{2, 4, 6, 6, 2, 2}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got %v, want %v", got, want)
		}
	})
}

// Not in the TS test file: the constructor's five RangeError branches. TS
// `throw new RangeError(msg)` maps to a *RangeError error return.
func TestAimdControllerRangeErrors(t *testing.T) {
	cases := []struct {
		name    string
		options *AimdOptions
		message string
	}{
		{"floor", &AimdOptions{Floor: ptr(0)}, "AIMD floor must be a positive integer"},
		{"cap", &AimdOptions{Cap: ptr(0)}, "AIMD cap must be an integer greater than or equal to floor"},
		{"increase", &AimdOptions{Increase: ptr(0)}, "AIMD increase must be a positive integer"},
		{"decrease", &AimdOptions{Decrease: ptr(1)}, "AIMD decrease must be between zero and one"},
		{"initialWindow", &AimdOptions{InitialWindow: ptr(math.Inf(1))}, "AIMD initialWindow must be finite"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			controller, err := NewAimdController(tc.options)
			if controller != nil {
				t.Fatalf("expected no controller, got %+v", controller)
			}
			rangeErr, ok := err.(*RangeError)
			if !ok {
				t.Fatalf("expected *RangeError, got %T", err)
			}
			if rangeErr.Name() != "RangeError" {
				t.Fatalf("name = %q, want RangeError", rangeErr.Name())
			}
			if rangeErr.Error() != tc.message {
				t.Fatalf("message = %q, want %q", rangeErr.Error(), tc.message)
			}
		})
	}
}

func TestNilOptionsMatchesTheDefaultArgument(t *testing.T) {
	fromNil := mustNew(t, nil)
	fromEmpty := mustNew(t, &AimdOptions{})
	if fromNil.Window() != fromEmpty.Window() {
		t.Fatalf("nil options window = %v, empty options window = %v", fromNil.Window(), fromEmpty.Window())
	}
	if fromNil.Window() != 1 {
		t.Fatalf("default window = %v, want 1", fromNil.Window())
	}
}
