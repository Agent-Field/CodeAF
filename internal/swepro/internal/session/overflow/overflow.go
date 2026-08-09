// Package overflow exposes the session-layer port of
// src/session/overflow.ts lines 1-159.
//
// The exact arithmetic and JavaScript environment coercions are shared with
// internal/engine/calc, where the engine already consumes this module's
// decisions. Type aliases keep both call sites on one implementation.
package overflow

import "github.com/Agent-Field/swe-pro-go/internal/engine/calc"

type CompactionConfig = calc.CompactionConfig
type Config = calc.Config
type ModelLimit = calc.ModelLimit
type Model = calc.Model
type TokenCache = calc.TokenCache
type Tokens = calc.Tokens

type UsableInput = calc.UsableInput
type ScanDriftInput = calc.ScanDriftInput
type OverflowInput = calc.OverflowInput

func Usable(input UsableInput) float64 {
	return calc.Usable(input)
}

func ShouldScanDrift(input ScanDriftInput) bool {
	return calc.ShouldScanDrift(input)
}

func IsOverflow(input OverflowInput) bool {
	return calc.IsOverflow(input)
}

// SetEnvForTesting installs the process.env image used by all three exported
// functions. It exists so golden vectors can pin every environment branch.
func SetEnvForTesting(env map[string]string) func() {
	return calc.SetEnvForTesting(env)
}
