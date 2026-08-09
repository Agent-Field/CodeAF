// Package calc is a bug-for-bug port of the pure token/context arithmetic the
// engine layer runs between an OpenRouter response and a persisted assistant
// message: `src/session/overflow.ts` (the compaction trigger calculator) and
// `Session.getUsage` (`src/session/session.ts:353-414`, the token-accounting +
// decimal.js cost function), plus the two upstream normalisations ENGINE-DESIGN
// §3.5 pins as part of the same pipeline — the provider's `computeTokenUsage`
// (`@openrouter/ai-sdk-provider/dist/index.mjs:2560-2581`) and the AI SDK's
// `asLanguageModelUsage` (`ai/dist/index.mjs:2423-2444`).
//
// The pipeline, end to end:
//
//	OpenRouter `usage` JSON → ComputeTokenUsage → AsLanguageModelUsage
//	  → GetUsage(model, usage, metadata) → {Cost, Tokens}
//	  → IsOverflow(cfg, tokens, model, drift?, agent?) → compaction decision
//
// ── seams ────────────────────────────────────────────────────────────────
//
//   - process.env. overflow.ts re-reads `process.env` on EVERY call (the
//     triggerPct / effectiveContextCap / driftThreshold / leafTriggerTokens
//     idiom is deliberate — the TS comment at overflow.ts:113-116 says so), so
//     the env image is a package-level seam, not a parameter.
//     SetEnvForTesting(map) swaps it; the default reads os.Environ() fresh on
//     each call. A map is used rather than os.Getenv because Node
//     distinguishes an absent var from one set to "", and every one of the
//     four helpers branches on `raw === undefined` — CODEAF_EFFECTIVE_CONTEXT=""
//     is Number("") === 0, which means "disable the cap", the exact opposite of
//     "unset".
//   - module-load env. `OUTPUT_TOKEN_MAX` (`transform.ts:20`) is an
//     `export const` evaluated once when the module is first imported, off a
//     `Flag` object that is itself built at module load (`flag.ts:30`). Changing
//     CODEAF_EXPERIMENTAL_OUTPUT_TOKEN_MAX at runtime therefore does NOT move
//     it in TS, and does not here either: it is a package-level var seeded at
//     init. SetModuleEnvForTesting(map) re-runs the module-load evaluation.
//   - no clock, no RNG. Neither TS module reads Date.now() or Math.random(),
//     so — as in internal/session/heft — there is deliberately no injectable
//     clock here.
//
// ── sibling ports ────────────────────────────────────────────────────────
//
//   - internal/session/knobs — calc is the single consumer of a knob in the
//     whole tree (overflow.ts:122 `resolveKnobs({}, process.env).values
//     .LEAF_CONTEXT_TRIGGER_TOKENS`). Only ResolveKnobs + the
//     LEAF_CONTEXT_TRIGGER_TOKENS_DEFAULT constant are used; the clamp to the
//     registry's [20_000, 160_000] comes from the resolver, never re-derived.
//   - internal/jscompat — FormatNumber (the `String(n)` decimal.js constructors
//     go through), JSNumber (JSON.stringify number formatting), ToNumber (JS
//     `Number(string)` coercion in the provider-metadata fallback chain).
//
// ── fidelity notes (deliberate, do not "fix") ────────────────────────────
//
//   - decimal.js is reproduced, not approximated. `session.ts:401-411` builds
//     the cost through decimal.js 10.5.0 at its DEFAULT config — precision 20
//     significant digits, rounding mode 4 (ROUND_HALF_UP: ties away from zero)
//     — and nothing in src/ calls Decimal.set/config. Every mul/add/div
//     finalises to 20 significant digits, so the arithmetic is NOT one exact
//     rational expression: 999999 × 0.12345678901234567 has 23 significant
//     digits and loses the last three. decimal.go performs every finite
//     operation with big.Rat, converts its terminating result back to a decimal
//     coefficient, then applies decimal.js's intermediate rounding.
//   - `new Decimal(x)` for a JS number goes through `x.toString()`
//     (decimal.mjs:4373), i.e. the SHORTEST round-trip decimal, so
//     `new Decimal(0.1)` is exactly 0.1 and not 0.1000000000000000055511…
//     jscompat.FormatNumber is that string.
//   - `getUsage`'s `tokens.total` is `input.usage.totalTokens` passed through
//     WITHOUT `safe()` (`session.ts:385`) — every other field is wrapped. A
//     non-finite or absent totalTokens therefore survives into the result,
//     where `undefined` makes JSON.stringify drop the key entirely. Tokens.Total
//     is a *float64 and Tokens.MarshalJSON omits it when nil.
//   - the two `tokens` shapes have DIFFERENT key orders and both are
//     observable. `getUsage`'s object literal is `cache: {write, read}`
//     (`session.ts:389-392`) while the persisted schema declares
//     `cache: {read, write}` (`message-v2.ts:276-279`). JSON.stringify follows
//     the runtime object, so a step-finish part serialises write-before-read.
//     UsageTokens (produced) and Tokens (consumed by overflow) keep their own
//     orders; they are not merged.
//   - `AsLanguageModelUsage` recomputes `totalTokens` as input+output and
//     DISCARDS OpenRouter's own `total_tokens` (ENGINE-DESIGN §3.5). Kept.
//   - `computeTokenUsage`'s cacheWrite is `?? undefined`, not `?? 0`
//     (index.mjs:2565), so an absent `cache_write_tokens` stays undefined and
//     falls through getUsage's provider-metadata `??` chain. cacheRead and
//     reasoning are `?? 0` and do not.
//   - the four provider-metadata cacheWrite fallbacks (anthropic / vertex /
//     bedrock / venice) are unreachable on the OpenRouter path. Ported anyway
//     per ENGINE-DESIGN §3.5 ("keep them, they are cheap"), including the JS
//     `Number()` coercion wrapped around the whole chain, which turns a string
//     metadata value into a number and an object into NaN → safe() → 0.
//   - JS `||` truthiness on floats is preserved verbatim in three places:
//     `Math.min(limit.output, OUTPUT_TOKEN_MAX) || OUTPUT_TOKEN_MAX`
//     (transform.ts:1281 — a NEGATIVE limit.output is truthy and survives, only
//     0/NaN fall through), `model.limit.input ? … : …` (overflow.ts:53 — an
//     input limit of 0 takes the context branch), and
//     `tokens.total || input+output+cache.read+cache.write` (overflow.ts:97,
//     :143 — a total of 0 or NaN falls through to the sum).
//   - `usable`'s clamp order is cap-then-percentage, not the reverse
//     (overflow.ts:63-67), and the floor is applied only at the very end.
//   - `Config.compaction.auto === false` is a STRICT comparison against false:
//     an absent compaction block, or `auto: undefined`, does not disable
//     compaction. `Auto` is therefore a *bool.
package calc

import (
	"math"
	"os"
	"strings"
)

// ── the env seam ─────────────────────────────────────────────────────────

// envProvider is the image of `process.env`. Returning a map (rather than
// hitting os.Getenv) keeps "absent" and "set to empty string" distinct, which
// three of the four helpers below branch on.
var envProvider = processEnv

// processEnv is the Go image of process.env. Split on the first '=' so a value
// containing '=' survives, matching Node. (Same helper as
// internal/session/knobs; duplicated rather than exported across packages
// because it is four lines and the two modules are otherwise independent.)
func processEnv() map[string]string {
	out := make(map[string]string)
	for _, kv := range os.Environ() {
		if i := strings.IndexByte(kv, '='); i >= 0 {
			out[kv[:i]] = kv[i+1:]
		}
	}
	return out
}

// SetEnvForTesting swaps the process.env image. Returns a restore func.
// A nil map is a process with no environment at all, which is distinct from
// the default (read os.Environ() on every call).
func SetEnvForTesting(env map[string]string) func() {
	previous := envProvider
	envProvider = func() map[string]string { return env }
	return func() { envProvider = previous }
}

// envLookup mirrors `process.env[name]`: ok=false stands in for `undefined`.
func envLookup(name string) (string, bool) {
	value, ok := envProvider()[name]
	return value, ok
}

// ── module-load constants ────────────────────────────────────────────────

// OUTPUT_TOKEN_MAX_DEFAULT is transform.ts:20's literal fallback.
const OUTPUT_TOKEN_MAX_DEFAULT float64 = 32_000

// outputTokenMax is `ProviderTransform.OUTPUT_TOKEN_MAX` (transform.ts:20):
//
//	export const OUTPUT_TOKEN_MAX = Flag.CODEAF_EXPERIMENTAL_OUTPUT_TOKEN_MAX || 32_000
//
// evaluated ONCE at module load, off a Flag object that is also built at module
// load. Runtime env changes do not move it. Seeded here at package init for
// exactly that reason.
var outputTokenMax = evalOutputTokenMax(processEnv())

// evalOutputTokenMax reproduces `Flag.number(key) || 32_000` where
// `number(key)` is flag.ts:18-23:
//
//	const value = process.env[key]
//	if (!value) return undefined
//	const parsed = Number(value)
//	return Number.isInteger(parsed) && parsed > 0 ? parsed : undefined
//
// Note `!value`: an absent var AND an empty-string var both fall back, and so
// does the string "0" (falsy in JS) — it never even reaches Number().
func evalOutputTokenMax(env map[string]string) float64 {
	raw := env["CODEAF_EXPERIMENTAL_OUTPUT_TOKEN_MAX"]
	if raw == "" || raw == "0" {
		return OUTPUT_TOKEN_MAX_DEFAULT
	}
	parsed := jsNumberOfString(raw)
	if isJSInteger(parsed) && parsed > 0 {
		return parsed
	}
	return OUTPUT_TOKEN_MAX_DEFAULT
}

// SetModuleEnvForTesting re-runs the module-load evaluation of OUTPUT_TOKEN_MAX
// against the supplied environment. This is the ONE knob in this package that
// TS freezes at import time; SetEnvForTesting deliberately does not touch it.
// Returns a restore func.
func SetModuleEnvForTesting(env map[string]string) func() {
	previous := outputTokenMax
	outputTokenMax = evalOutputTokenMax(env)
	return func() { outputTokenMax = previous }
}

// isJSInteger is Number.isInteger: finite and with no fractional part.
func isJSInteger(f float64) bool {
	return !math.IsNaN(f) && !math.IsInf(f, 0) && math.Trunc(f) == f
}

// safe is session.ts:354-357 — `if (!Number.isFinite(value)) return 0`.
func safe(value float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0
	}
	return value
}
