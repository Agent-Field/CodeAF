//go:build !windows

// Package overflow is the session-layer view of the compaction budget.
//
// The arithmetic lives in internal/engine/calc, where the engine already
// consumes this module's decisions. Type aliases keep both call sites on one
// implementation.
package overflow

import "github.com/Agent-Field/codeaf/internal/seniordev/engine/calc"

type CompactionConfig = calc.CompactionConfig
type Config = calc.Config
type ModelLimit = calc.ModelLimit
type Model = calc.Model
type TokenCache = calc.TokenCache
type Tokens = calc.Tokens

type UsableInput = calc.UsableInput
type OverflowInput = calc.OverflowInput
type CompactionWatermarks = calc.CompactionWatermarks

const PolicyWindow = calc.PolicyWindow

func ValidatePolicy(cfg Config) error { return calc.ValidatePolicy(cfg) }

func EffectiveInputCapacity(input UsableInput) float64 {
	return calc.EffectiveInputCapacity(input)
}

func Watermarks(input UsableInput) CompactionWatermarks {
	return calc.Watermarks(input)
}

func IsOverflow(input OverflowInput) bool {
	return calc.IsOverflow(input)
}
