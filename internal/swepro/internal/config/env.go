// Package config ports the environment/configuration layer rooted at
// swe-pro/src/config (2163 lines) and the environment switches consumed by
// swe-pro/src/cli/cmd/run.ts at commit 3b25a1a.
package config

import (
	"os"
	"strings"
)

// BoolMode identifies the deliberately asymmetric boolean spellings used by
// the TypeScript source. Do not replace these comparisons with strconv.ParseBool:
// values such as "true", "TRUE", and "junk" are load-bearing test cases.
type BoolMode string

const (
	RawValue      BoolMode = "raw"
	OptInOne      BoolMode = "opt-in-1"
	OptOutZero    BoolMode = "opt-out-0"
	Truthy        BoolMode = "truthy"
	OptOutFalsy   BoolMode = "opt-out-falsy"
	ReviewEnabled BoolMode = "review-enabled"
	Observer      BoolMode = "observer"
)

// VariableNames is the 144-entry environment surface read by the frozen
// TypeScript closure. Insertion order is source/load order and is intentional.
var VariableNames = []string{
	"CODEAF_ADAPTIVE_CUTS",
	"CODEAF_ADMISSIBILITY",
	"CODEAF_ALWAYS_NOTIFY_UPDATE",
	"CODEAF_ARCH_CHECKER_TIMEOUT_MS",
	"CODEAF_ARCH_FIX_CYCLES",
	"CODEAF_ARCH_PANEL",
	"CODEAF_ARCH_REDTEAM",
	"CODEAF_ARTIFACT_REFS",
	"CODEAF_AUDITOR",
	"CODEAF_AUDITOR_MAX_ATTEMPTS",
	"CODEAF_AUDITOR_SPRT",
	"CODEAF_AUDITOR_TIMEOUT_MS",
	"CODEAF_AUTO_HEAP_SNAPSHOT",
	"CODEAF_AUTO_SHARE",
	"CODEAF_CASCADE_HARD",
	"CODEAF_CLIENT",
	"CODEAF_COMPACT_EVIDENCE",
	"CODEAF_COMPACT_TRIGGER_PCT",
	"CODEAF_CONFIG",
	"CODEAF_CONFIG_CONTENT",
	"CODEAF_CONFIG_DIR",
	"CODEAF_CONTRACT",
	"CODEAF_CONTRACT_REVIEW",
	"CODEAF_DB",
	"CODEAF_DELTA_AUDIT",
	"CODEAF_DISABLE_AUTOCOMPACT",
	"CODEAF_DISABLE_AUTOUPDATE",
	"CODEAF_DISABLE_CHANNEL_DB",
	"CODEAF_DISABLE_CLAUDE_CODE",
	"CODEAF_DISABLE_CLAUDE_CODE_PROMPT",
	"CODEAF_DISABLE_CLAUDE_CODE_SKILLS",
	"CODEAF_DISABLE_DEFAULT_PLUGINS",
	"CODEAF_DISABLE_EMBEDDED_WEB_UI",
	"CODEAF_DISABLE_EXTERNAL_SKILLS",
	"CODEAF_DISABLE_LSP_DOWNLOAD",
	"CODEAF_DISABLE_MODELS_FETCH",
	"CODEAF_DISABLE_MOUSE",
	"CODEAF_DISABLE_PROJECT_CONFIG",
	"CODEAF_DISABLE_PRUNE",
	"CODEAF_DISABLE_TERMINAL_TITLE",
	"CODEAF_DISK_FLOOR_GB",
	"CODEAF_DISPATCH_LOOP",
	"CODEAF_DISPATCH_MAX_LOOPS",
	"CODEAF_EAGER_COMMIT",
	"CODEAF_EFFECTIVE_CONTEXT",
	"CODEAF_ENABLE_EXA",
	"CODEAF_ENABLE_EXPERIMENTAL_MODELS",
	"CODEAF_ENABLE_PARALLEL",
	"CODEAF_ENABLE_QUESTION_TOOL",
	"CODEAF_ENV_SIGNALS",
	"CODEAF_EXPERIMENTAL",
	"CODEAF_EXPERIMENTAL_BASH_DEFAULT_TIMEOUT_MS",
	"CODEAF_EXPERIMENTAL_DISABLE_COPY_ON_SELECT",
	"CODEAF_EXPERIMENTAL_EVENT_SYSTEM",
	"CODEAF_EXPERIMENTAL_EXA",
	"CODEAF_EXPERIMENTAL_HTTPAPI",
	"CODEAF_EXPERIMENTAL_ICON_DISCOVERY",
	"CODEAF_EXPERIMENTAL_LSP_TOOL",
	"CODEAF_EXPERIMENTAL_LSP_TY",
	"CODEAF_EXPERIMENTAL_MARKDOWN",
	"CODEAF_EXPERIMENTAL_OUTPUT_TOKEN_MAX",
	"CODEAF_EXPERIMENTAL_OXFMT",
	"CODEAF_EXPERIMENTAL_PARALLEL",
	"CODEAF_EXPERIMENTAL_PLAN_MODE",
	"CODEAF_EXPERIMENTAL_SCOUT",
	"CODEAF_EXPERIMENTAL_WORKSPACES",
	"CODEAF_FAKE_VCS",
	"CODEAF_FIX_GEN_TIMEOUT_MS",
	"CODEAF_FRONTIER",
	"CODEAF_GIT_BASH_PATH",
	"CODEAF_GLOSSARY",
	"CODEAF_HARD",
	"CODEAF_HARD_ESCALATION_REASON",
	"CODEAF_HEFT",
	"CODEAF_HIGH_MODEL",
	"CODEAF_HYGIENE",
	"CODEAF_ISSUE_WRITER_TIMEOUT_MS",
	"CODEAF_KNOB_LEAF_CONTEXT_TRIGGER_TOKENS",
	"CODEAF_LARGE_BAND",
	"CODEAF_LEAF_CONTEXT_TRIGGER",
	"CODEAF_LOW_JUDGE_TIMEOUT_MS",
	"CODEAF_LOW_MODEL",
	"CODEAF_MODELS_PATH",
	"CODEAF_MODELS_URL",
	"CODEAF_MODEL_PRIORITY",
	"CODEAF_NO_SILENCE",
	"CODEAF_OBSERVER",
	"CODEAF_OPENROUTER_CONTENT_IDLE_MS",
	"CODEAF_OPENROUTER_FIRST_CONTENT_MS",
	"CODEAF_OPENROUTER_IDLE_MS",
	"CODEAF_OPENROUTER_TOTAL_REQ_MS",
	"CODEAF_OUTCOME_CACHE",
	"CODEAF_PERMISSION",
	"CODEAF_PLANDB_PERSIST",
	"CODEAF_PLAN_ARB",
	"CODEAF_PLUGIN_META_FILE",
	"CODEAF_PRE_GATES",
	"CODEAF_PURE",
	"CODEAF_REPAIR_AGENT",
	"CODEAF_REPO_CLONE_GITHUB_BASE_URL",
	"CODEAF_REVIEW_AGENT",
	"CODEAF_REVIEW_DRY_RUN",
	"CODEAF_REVIEW_ENABLED",
	"CODEAF_REVIEW_MAX_TOOL_CALLS",
	"CODEAF_REVIEW_REPAIR_CAP",
	"CODEAF_REVIEW_REPAIR_TIER",
	"CODEAF_REVIEW_SKIP_GLOBS",
	"CODEAF_REVIEW_TIMEOUT_MS",
	"CODEAF_ROOT_CAUSE",
	"CODEAF_SCRATCH_MAX_GB",
	"CODEAF_SCRATCH_ROOT",
	"CODEAF_SCRATCH_TTL_H",
	"CODEAF_SERVER_PASSWORD",
	"CODEAF_SERVER_USERNAME",
	"CODEAF_SHARED_BUILD_CACHE",
	"CODEAF_SHOW_TTFD",
	"CODEAF_SKIP_MIGRATIONS",
	"CODEAF_SMALL_MODEL_PRIORITY",
	"CODEAF_SPEC_IDS",
	"CODEAF_STRICT_CONFIG_DEPS",
	"CODEAF_STRUCTURAL_MERGE",
	"CODEAF_SUPERVISED",
	"CODEAF_TAMPER",
	"CODEAF_TASK_AUTOCOMMIT",
	"CODEAF_TEST_HOME",
	"CODEAF_TEST_MANAGED_CONFIG_DIR",
	"CODEAF_TIMEOUT_RETRY_DELAY_MS",
	"CODEAF_TUI_CONFIG",
	"CODEAF_TUI_DEBUG",
	"CODEAF_VALIDITY",
	"CODEAF_WEBSEARCH_PROVIDER",
	"CODEAF_WORKSPACE_ID",
	"CODEAF_WS_DRIFT",
	"CODEAF_AUTORESUME",
	"CODEAF_CHANNEL",
	"CODEAF_ISOLATION",
	"CODEAF_LEAF_MAX_ACTIONS",
	"CODEAF_LEAF_MAX_COST_USD",
	"CODEAF_MAX_COST_USD",
	"CODEAF_MAX_WALL_H",
	"CODEAF_PROCESS_ROLE",
	"CODEAF_RUN_ID",
	"CODEAF_VERSION",
	"CODEAF_PORTFOLIO_SEED",
}

var boolModes = map[string]BoolMode{
	// Exact opt-outs used by the pipeline.
	"CODEAF_ADAPTIVE_CUTS":        OptOutZero,
	"CODEAF_ADMISSIBILITY":        OptOutZero,
	"CODEAF_AUDITOR":              OptOutZero,
	"CODEAF_AUDITOR_SPRT":         OptOutZero,
	"CODEAF_AUTORESUME":           OptOutZero,
	"CODEAF_COMPACT_EVIDENCE":     OptOutZero,
	"CODEAF_CONTRACT":             OptOutZero,
	"CODEAF_CONTRACT_REVIEW":      OptOutZero,
	"CODEAF_DELTA_AUDIT":          OptOutZero,
	"CODEAF_DISPATCH_LOOP":        OptOutZero,
	"CODEAF_EAGER_COMMIT":         OptOutZero,
	"CODEAF_ENV_SIGNALS":          OptOutZero,
	"CODEAF_GLOSSARY":             OptOutZero,
	"CODEAF_HEFT":                 OptOutZero,
	"CODEAF_HYGIENE":              OptOutZero,
	"CODEAF_LEAF_CONTEXT_TRIGGER": OptOutZero,
	"CODEAF_OUTCOME_CACHE":        OptOutZero,
	"CODEAF_PLANDB_PERSIST":       OptOutZero,
	"CODEAF_PLAN_ARB":             OptOutZero,
	"CODEAF_PRE_GATES":            OptOutZero,
	"CODEAF_ROOT_CAUSE":           OptOutZero,
	"CODEAF_SPEC_IDS":             OptOutZero,
	"CODEAF_STRUCTURAL_MERGE":     OptOutZero,
	"CODEAF_TAMPER":               OptOutZero,
	"CODEAF_TASK_AUTOCOMMIT":      OptOutZero,
	"CODEAF_VALIDITY":             OptOutZero,

	// Exact opt-ins used by the pipeline.
	"CODEAF_ARTIFACT_REFS":      OptInOne,
	"CODEAF_CASCADE_HARD":       OptInOne,
	"CODEAF_HARD":               OptInOne,
	"CODEAF_LARGE_BAND":         OptInOne,
	"CODEAF_REVIEW_DRY_RUN":     OptInOne,
	"CODEAF_SHARED_BUILD_CACHE": OptInOne,
	"CODEAF_SUPERVISED":         OptInOne,

	// src/config/review.ts and src/session/observer.ts are the two explicit
	// non-exact exceptions in the pipeline layer.
	"CODEAF_REVIEW_ENABLED": ReviewEnabled,
	"CODEAF_OBSERVER":       Observer,

	// src/core/flag/flag.ts has its older, case-insensitive truthy/falsy
	// compatibility surface. These flags are config plumbing rather than
	// pipeline switches, but remain part of the 144-variable closure.
	"CODEAF_ALWAYS_NOTIFY_UPDATE":                Truthy,
	"CODEAF_AUTO_HEAP_SNAPSHOT":                  Truthy,
	"CODEAF_AUTO_SHARE":                          Truthy,
	"CODEAF_DISABLE_AUTOCOMPACT":                 Truthy,
	"CODEAF_DISABLE_AUTOUPDATE":                  Truthy,
	"CODEAF_DISABLE_CHANNEL_DB":                  Truthy,
	"CODEAF_DISABLE_CLAUDE_CODE":                 Truthy,
	"CODEAF_DISABLE_CLAUDE_CODE_PROMPT":          Truthy,
	"CODEAF_DISABLE_CLAUDE_CODE_SKILLS":          Truthy,
	"CODEAF_DISABLE_DEFAULT_PLUGINS":             Truthy,
	"CODEAF_DISABLE_EMBEDDED_WEB_UI":             Truthy,
	"CODEAF_DISABLE_EXTERNAL_SKILLS":             Truthy,
	"CODEAF_DISABLE_LSP_DOWNLOAD":                Truthy,
	"CODEAF_DISABLE_MODELS_FETCH":                Truthy,
	"CODEAF_DISABLE_MOUSE":                       Truthy,
	"CODEAF_DISABLE_PROJECT_CONFIG":              Truthy,
	"CODEAF_DISABLE_PRUNE":                       Truthy,
	"CODEAF_DISABLE_TERMINAL_TITLE":              Truthy,
	"CODEAF_ENABLE_EXA":                          Truthy,
	"CODEAF_ENABLE_EXPERIMENTAL_MODELS":          Truthy,
	"CODEAF_ENABLE_PARALLEL":                     Truthy,
	"CODEAF_ENABLE_QUESTION_TOOL":                Truthy,
	"CODEAF_EXPERIMENTAL":                        Truthy,
	"CODEAF_EXPERIMENTAL_DISABLE_COPY_ON_SELECT": Truthy,
	"CODEAF_EXPERIMENTAL_EVENT_SYSTEM":           Truthy,
	"CODEAF_EXPERIMENTAL_EXA":                    Truthy,
	"CODEAF_EXPERIMENTAL_HTTPAPI":                Truthy,
	"CODEAF_EXPERIMENTAL_ICON_DISCOVERY":         Truthy,
	"CODEAF_EXPERIMENTAL_LSP_TOOL":               Truthy,
	"CODEAF_EXPERIMENTAL_LSP_TY":                 Truthy,
	"CODEAF_EXPERIMENTAL_OXFMT":                  Truthy,
	"CODEAF_EXPERIMENTAL_PARALLEL":               Truthy,
	"CODEAF_EXPERIMENTAL_PLAN_MODE":              Truthy,
	"CODEAF_EXPERIMENTAL_SCOUT":                  Truthy,
	"CODEAF_EXPERIMENTAL_WORKSPACES":             Truthy,
	"CODEAF_PURE":                                Truthy,
	"CODEAF_SHOW_TTFD":                           Truthy,
	"CODEAF_SKIP_MIGRATIONS":                     Truthy,
	"CODEAF_STRICT_CONFIG_DEPS":                  Truthy,
	"CODEAF_EXPERIMENTAL_MARKDOWN":               OptOutFalsy,
}

// Mode returns the source parsing mode for name. Non-boolean variables retain
// their raw string value.
func Mode(name string) BoolMode {
	if mode, ok := boolModes[name]; ok {
		return mode
	}
	return RawValue
}

// ParseBoolean applies one of the source's exact boolean comparisons. raw=nil
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
	case OptOutFalsy:
		lower := strings.ToLower(value)
		return lower != "false" && lower != "0"
	case ReviewEnabled:
		return value != "0" && value != "false"
	case Observer:
		return value == "1" || strings.ToLower(value) == "true"
	default:
		return false
	}
}

// ParseEnvValue returns the typed projection used by the golden fixture. Raw
// entries preserve absence as nil; boolean entries return bool.
func ParseEnvValue(name string, raw *string) any {
	mode := Mode(name)
	if mode == RawValue {
		if raw == nil {
			return nil
		}
		return *raw
	}
	return ParseBoolean(mode, raw)
}

// Lookup is the minimal environment read boundary used by Config.
type Lookup func(string) (string, bool)

// Env snapshots an environment without mutating the process-global map.
type Env struct {
	values map[string]string
}

// NewEnv snapshots lookup for the 144 declared variables.
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

// Enabled parses name according to its frozen source policy.
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
