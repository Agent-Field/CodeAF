//go:build !windows

// Package config is the configuration layer: project config files, the
// SENIOR_DEV_* environment surface and their merge.
package config

import (
	"os"
	"strings"
)

// BoolMode identifies how a boolean environment variable is spelled. The
// spellings are deliberately asymmetric (an opt-out reads "0", an opt-in reads
// "1", a truthy flag reads "true"/"1"), so they are not replaced with
// strconv.ParseBool.
type BoolMode string

const (
	RawValue   BoolMode = "raw"
	OptInOne   BoolMode = "opt-in-1"
	OptOutZero BoolMode = "opt-out-0"
	Truthy     BoolMode = "truthy"
)

// VariableNames is the environment surface the config layer snapshots. A
// variable not listed here is invisible to Env.Get.
var VariableNames = []string{
	"SENIOR_DEV_CLIENT",
	"SENIOR_DEV_CONFIG",
	"SENIOR_DEV_CONFIG_CONTENT",
	"SENIOR_DEV_CONFIG_DIR",
	"SENIOR_DEV_DISABLE_AUTOCOMPACT",
	"SENIOR_DEV_DISABLE_MODELS_FETCH",
	"SENIOR_DEV_DISABLE_PROJECT_CONFIG",
	"SENIOR_DEV_DISABLE_PRUNE",
	"SENIOR_DEV_EAGER_COMMIT",
	"SENIOR_DEV_ENABLE_EXA",
	"SENIOR_DEV_ENABLE_PARALLEL",
	"SENIOR_DEV_ENABLE_QUESTION_TOOL",
	"SENIOR_DEV_ENV_SIGNALS",
	"SENIOR_DEV_EXPERIMENTAL",
	"SENIOR_DEV_EXPERIMENTAL_EXA",
	"SENIOR_DEV_EXPERIMENTAL_OXFMT",
	"SENIOR_DEV_EXPERIMENTAL_PARALLEL",
	"SENIOR_DEV_MODELS_PATH",
	"SENIOR_DEV_MODELS_URL",
	"SENIOR_DEV_OUTPUT_TOKEN_MAX",
	"SENIOR_DEV_PERMISSION",
	"SENIOR_DEV_SCRATCH_MAX_GB",
	"SENIOR_DEV_SCRATCH_ROOT",
	"SENIOR_DEV_SCRATCH_TTL_H",
	"SENIOR_DEV_SHARED_BUILD_CACHE",
	"SENIOR_DEV_WEBSEARCH_PROVIDER",
	"SENIOR_DEV_MAX_COST_USD",
	"SENIOR_DEV_MAX_WALL_H",
}

var boolModes = map[string]BoolMode{
	// Exact opt-outs.
	"SENIOR_DEV_EAGER_COMMIT": OptOutZero,
	"SENIOR_DEV_ENV_SIGNALS":  OptOutZero,

	// Exact opt-ins.
	"SENIOR_DEV_SHARED_BUILD_CACHE": OptInOne,

	// Case-insensitive truthy flags.
	"SENIOR_DEV_DISABLE_AUTOCOMPACT":    Truthy,
	"SENIOR_DEV_DISABLE_MODELS_FETCH":   Truthy,
	"SENIOR_DEV_DISABLE_PROJECT_CONFIG": Truthy,
	"SENIOR_DEV_DISABLE_PRUNE":          Truthy,
	"SENIOR_DEV_ENABLE_EXA":             Truthy,
	"SENIOR_DEV_ENABLE_PARALLEL":        Truthy,
	"SENIOR_DEV_ENABLE_QUESTION_TOOL":   Truthy,
	"SENIOR_DEV_EXPERIMENTAL":           Truthy,
	"SENIOR_DEV_EXPERIMENTAL_EXA":       Truthy,
	"SENIOR_DEV_EXPERIMENTAL_OXFMT":     Truthy,
	"SENIOR_DEV_EXPERIMENTAL_PARALLEL":  Truthy,
}

// Mode returns the parsing mode for name. Non-boolean variables retain their
// raw string value.
func Mode(name string) BoolMode {
	if mode, ok := boolModes[name]; ok {
		return mode
	}
	return RawValue
}

// ParseBoolean applies one of the exact boolean comparisons. raw=nil
// represents an absent environment entry.
func ParseBoolean(mode BoolMode, raw *string) bool {
	value := ""
	if raw != nil {
		value = *raw
	}
	switch mode {
	case OptInOne:
		return value == "1"
	case OptOutZero:
		return value != "0"
	case Truthy:
		lower := strings.ToLower(value)
		return lower == "true" || lower == "1"
	default:
		return false
	}
}

// Lookup is the minimal environment read boundary used by Config.
type Lookup func(string) (string, bool)

// Env snapshots an environment without mutating the process-global map.
type Env struct {
	values map[string]string
}

// NewEnv snapshots lookup for the declared variables.
func NewEnv(lookup Lookup) Env {
	if lookup == nil {
		lookup = os.LookupEnv
	}
	values := make(map[string]string, len(VariableNames))
	for _, name := range VariableNames {
		if value, ok := lookup(name); ok {
			values[name] = value
		}
	}
	return Env{values: values}
}

// Get returns a raw value and preserves absent versus explicitly empty.
func (e Env) Get(name string) (string, bool) {
	value, ok := e.values[name]
	return value, ok
}

// Enabled parses name according to its declared mode.
func (e Env) Enabled(name string) bool {
	value, ok := e.Get(name)
	if !ok {
		return ParseBoolean(Mode(name), nil)
	}
	return ParseBoolean(Mode(name), &value)
}

// All returns a defensive copy.
func (e Env) All() map[string]string {
	out := make(map[string]string, len(e.values))
	for key, value := range e.values {
		out[key] = value
	}
	return out
}
