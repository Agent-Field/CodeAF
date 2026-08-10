// AIMD dispatch window controller — port of src/session/aimd.ts.
//
// W4b: Additive-increase/multiplicative-decrease parallelism controller.
// Standalone and deterministic; scheduler wiring is deferred to integration.
//
// Fidelity notes (deliberate, do not "fix"):
//   - Every knob is a JS number, so the Go fields are float64, NOT int. The TS
//     validators only demand Number.isInteger(), which admits values far past
//     int32/int53 (1e308 is an "integer" to JS), and the decrease path is
//     genuinely fractional. Keeping float64 preserves the exact accumulation
//     shape of `current * decrease` and `current + increase`.
//   - Math.floor on a possibly-negative product maps to math.Floor, never to
//     an int conversion (Go truncates toward zero, JS floors toward -Inf).
//   - Options use *float64 because TS reads them with `??`: only undefined and
//     null fall back to the default. An explicit 0 is kept (and then rejected
//     by the validators), which `||` would not do.
//   - The constructor validates in TS source order — floor, cap, increase,
//     decrease, initialWindow — and the first failure wins. Callers that pass
//     two bad knobs must see the earlier one's message.
//   - `new AimdController()` (no argument) is NewAimdController(nil). TS `null`
//     as the options argument is NOT modeled: TS throws a TypeError on the
//     first property read, Go treats nil as the `= {}` default.
//   - No Date.now()/Math.random() reads in this module, so there is no
//     injectable clock.
package aimd

import "math"

const (
	defaultInitialWindow = 1   // W4-TODO(knobs)
	defaultIncrease      = 1   // W4-TODO(knobs)
	defaultDecrease      = 0.5 // W4-TODO(knobs)
	defaultCap           = 20  // W4-TODO(knobs)
	defaultFloor         = 1   // W4-TODO(knobs)
)

// RangeError mirrors the JS RangeError the constructor throws. The Name method
// exists so callers can assert on the JS error class, not just the message.
type RangeError struct {
	Message string
}

func (e *RangeError) Error() string { return e.Message }

// Name returns the JS error class name, always "RangeError".
func (e *RangeError) Name() string { return "RangeError" }

// AimdOptions is the TS `AimdOptions` interface. Every field is optional; nil
// means "absent" (TS undefined or null) and takes the default.
type AimdOptions struct {
	// InitialWindow is the starting parallelism, clamped to [floor, cap].
	InitialWindow *float64 `json:"initialWindow"`
	// Increase is the slots added after each clean-merge round. Must be a
	// positive integer.
	Increase *float64 `json:"increase"`
	// Decrease is the factor applied after a conflict. Must be in (0, 1).
	Decrease *float64 `json:"decrease"`
	// Cap is the maximum parallelism. Must be an integer >= floor.
	Cap *float64 `json:"cap"`
	// Floor is the minimum parallelism. Must be a positive integer.
	Floor *float64 `json:"floor"`
}

// AimdController is the TS `AimdController` class.
type AimdController struct {
	current  float64
	increase float64
	decrease float64
	cap      float64
	floor    float64
}

// nullish reproduces `value ?? fallback`: only undefined/null (nil here) fall
// back, so an explicit 0 or NaN is kept and reaches the validators.
func nullish(value *float64, fallback float64) float64 {
	if value == nil {
		return fallback
	}
	return *value
}

// isInteger is Number.isInteger: finite and with no fractional part.
func isInteger(v float64) bool {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return false
	}
	return math.Floor(v) == v
}

// isFinite is Number.isFinite.
func isFinite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}

// NewAimdController is `new AimdController(options)`. Pass nil for the TS
// `options: AimdOptions = {}` default.
func NewAimdController(options *AimdOptions) (*AimdController, error) {
	if options == nil {
		options = &AimdOptions{}
	}

	c := &AimdController{
		increase: nullish(options.Increase, defaultIncrease),
		decrease: nullish(options.Decrease, defaultDecrease),
		cap:      nullish(options.Cap, defaultCap),
		floor:    nullish(options.Floor, defaultFloor),
	}
	initialWindow := nullish(options.InitialWindow, defaultInitialWindow)

	if !isInteger(c.floor) || c.floor < 1 {
		return nil, &RangeError{Message: "AIMD floor must be a positive integer"}
	}
	if !isInteger(c.cap) || c.cap < c.floor {
		return nil, &RangeError{Message: "AIMD cap must be an integer greater than or equal to floor"}
	}
	if !isInteger(c.increase) || c.increase < 1 {
		return nil, &RangeError{Message: "AIMD increase must be a positive integer"}
	}
	if !isFinite(c.decrease) || c.decrease <= 0 || c.decrease >= 1 {
		return nil, &RangeError{Message: "AIMD decrease must be between zero and one"}
	}
	if !isFinite(initialWindow) {
		return nil, &RangeError{Message: "AIMD initialWindow must be finite"}
	}

	c.current = math.Min(c.cap, math.Max(c.floor, math.Floor(initialWindow)))
	return c, nil
}

// Observe is `observe(mergeOk)`.
func (c *AimdController) Observe(mergeOk bool) {
	if mergeOk {
		c.current = math.Min(c.cap, c.current+c.increase)
		return
	}
	c.current = math.Max(c.floor, math.Floor(c.current*c.decrease))
}

// Window is `window()`.
func (c *AimdController) Window() float64 {
	return c.current
}

// CreateAimdController is `createAimdController(options)`.
func CreateAimdController(options *AimdOptions) (*AimdController, error) {
	return NewAimdController(options)
}
