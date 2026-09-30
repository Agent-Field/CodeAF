//go:build !windows

// Package calc holds the pure token and cost arithmetic that runs between an
// OpenRouter response and a persisted assistant message: it normalises the
// provider usage block, prices a call from the model catalog, and derives the
// compaction budget and its watermarks from the model limits and the
// compaction config.
package calc

import (
	"math"
	"os"
	"strconv"
	"strings"
)

// ── process-start constants ──────────────────────────────────────────────

// processEnv is the process environment as a map. Split on the first '=' so
// a value containing '=' survives.
func processEnv() map[string]string {
	out := make(map[string]string)
	for _, kv := range os.Environ() {
		if i := strings.IndexByte(kv, '='); i >= 0 {
			out[kv[:i]] = kv[i+1:]
		}
	}
	return out
}

// OUTPUT_TOKEN_MAX_DEFAULT is the output-token ceiling when
// SENIOR_DEV_OUTPUT_TOKEN_MAX is unset.
const OUTPUT_TOKEN_MAX_DEFAULT float64 = 32_000

// outputTokenMax is the output-token ceiling every request is capped at:
// SENIOR_DEV_OUTPUT_TOKEN_MAX, or OUTPUT_TOKEN_MAX_DEFAULT. It is evaluated ONCE
// at package init; a runtime change to the variable does not move it.
var outputTokenMax = evalOutputTokenMax(processEnv())

// evalOutputTokenMax reads SENIOR_DEV_OUTPUT_TOKEN_MAX: a positive integer
// (decimal or exponent notation) is the ceiling; absent, empty, "0" or
// anything else falls back to the default.
func evalOutputTokenMax(env map[string]string) float64 {
	raw := env["SENIOR_DEV_OUTPUT_TOKEN_MAX"]
	if raw == "" || raw == "0" {
		return OUTPUT_TOKEN_MAX_DEFAULT
	}
	parsed, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err == nil && parsed > 0 && math.Trunc(parsed) == parsed && !math.IsInf(parsed, 0) {
		return parsed
	}
	return OUTPUT_TOKEN_MAX_DEFAULT
}

// SetModuleEnvForTesting re-runs the package-init evaluation of
// OUTPUT_TOKEN_MAX against the supplied environment. Returns a restore func.
func SetModuleEnvForTesting(env map[string]string) func() {
	previous := outputTokenMax
	outputTokenMax = evalOutputTokenMax(env)
	return func() { outputTokenMax = previous }
}

// safe maps a non-finite value to 0.
func safe(value float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0
	}
	return value
}
