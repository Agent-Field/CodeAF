// Package orclient is a bug-for-bug port of the OpenRouter streaming client —
// `OpenRouterChatLanguageModel.doStream` from `@openrouter/ai-sdk-provider@2.8.1`
// (`dist/internal/index.mjs:3713-4199`), the reachable half of codeaf's
// `ProviderTransform` (`src/provider/transform.ts`), the request-assembly
// provenance of `src/session/llm.ts:417-506`, and the tool-call
// validation/repair/`invalid` fallback the AI SDK runs on the way out
// (`ai@6.0.168` `dist/index.mjs:3657-3760`, `llm.ts:427-447`).
//
// One `DoStream` call is one HTTP request (ENGINE-DESIGN §0 F1). Nothing here
// loops, retries, executes a tool or touches session state; the caller owns all
// four. What this package owns is: the exact bytes of the request body, the
// exact header set, the SSE grammar, the chunk→stream-part translation
// (including the accumulator bugs upstream ships), the reasoning_details
// round-trip, the usage/finish accumulation, and the four-layer abort stack.
//
// ── seams ────────────────────────────────────────────────────────────────
//
//   - Math.random — `generateId()` (`internal/index.mjs:590-615`) mints the
//     16-char ids carried by `reasoning-start`, `text-start` (when the response
//     has no `id`) and every tool call whose server id is empty or duplicated.
//     SetRandomForTesting swaps it; the draw order per id is one call per
//     character, exactly like the TS, so a seeded differential run stays
//     aligned.
//   - Date.now — only for the router's `elapsed_s`. SetNowForTesting swaps it.
//   - the HTTP round trip — `orfetch.OpenRouterAimdFetch` (ENGINE-DESIGN §0
//     F6). It is called through the package var `fetcher`, swapped by
//     SetFetcherForTesting, so an httptest server can be driven without
//     reaching for orfetch's own seams.
//   - the reader watchdog timer — SetTimerFactoryForTesting, same shape as
//     orfetch.SetTimerFactoryForTesting, so the layer-2/3 collapse (see
//     fidelity notes) can be tested on a virtual clock.
//   - the router — `state.GetRouter()`. Wiring it to the real
//     `adaptive.AdaptiveModelRouter` was the F8 gap; it now lives in
//     internal/router/state/adaptivewire.go and orclient only calls
//     `Register` through the narrow RouterRegistrar interface.
//
// ── sibling ports ────────────────────────────────────────────────────────
//
//   - internal/engine/msgmodel — ModelMessage and the content-part structs are
//     the input to normalizeMessages and convertToOpenRouterChatMessages.
//   - internal/engine/calc — ComputeTokenUsage / the LanguageModelV3Usage
//     shape. §3.5's arithmetic is NOT re-implemented here.
//   - internal/engine/retrysched — ToRouterValue / NewStatusError, the
//     `error → *adaptive.JSValue` adapter the router classifiers need.
//   - internal/router/orfetch — the fetch seam (abort layer 4).
//   - internal/router/state — the process router singleton + route events.
//   - internal/jscompat — LocaleCompare (the deterministicStringify key sort,
//     ENGINE-DESIGN R0 — the highest-risk single function in the layer),
//     Stringify, FormatNumber, JSNumber.
//
// ── fidelity notes (deliberate, do not "fix") ────────────────────────────
//
//   - Tool-call accumulation reproduces two upstream bugs, both fixtured:
//     `index = toolCallDelta.index ?? toolCalls.length - 1` targets slot -1 on
//     a first delta with no index (`:3962`, BUGS-KEPT #5); and the
//     parsable-args check at `:4064` has NO `sent` guard, so `tool-input-end` +
//     `tool-call` are re-emitted for every subsequent delta once the buffer
//     first parses (`:4064`, BUGS-KEPT #6).
//   - Reasoning deltas are swallowed once text has started (`!textStarted`,
//     `:3874`/`:3897`) but still accumulate, surfacing only in `reasoning-end`
//     and `providerMetadata`. `reasoning.encrypted` emits nothing at all.
//   - `if (delta.content)` is a FALSY test: an empty-string content delta
//     produces no `text-start` and no `text-delta`.
//   - `logprobs` is fully schema-validated and then discarded because the
//     provider never reads it after zod parsing.
//   - A zod parse failure and a top-level `error` payload both emit an `error`
//     part, set finishReason to `error` and RETURN — later fields of the same
//     chunk are not looked at.
//   - `source.id` is the URL itself, not a minted id. Old-format
//     `file_annotation` annotations are parsed and then ignored entirely.
//   - Object key order is the TS object-literal order everywhere, because it
//     reaches the wire body and the prompt cache. Every part type therefore
//     has its own MarshalJSON rather than a shared struct with omitempty.
//     Validated reasoning_details retain their zod-normalized bytes as opaque
//     json.RawMessage until the provider's own consecutive-text merge mutates
//     one.
//   - `deterministicStringify` sorts recursively with `localeCompare`, not byte
//     order, and re-serialises numbers through `String(n)` — because the TS
//     sorts a value that has already been through `JSON.parse`. Object key
//     enumeration follows the ES OrdinaryOwnPropertyKeys rule (canonical array
//     indices first, ascending), which is observable when two keys collate
//     equal.
//   - KNOWN DIVERGENCE (BUGS-KEPT #8): abort layers 2-chunk (120 s) and 3
//     (`wrapSSE`) collapse into ONE reader-side watchdog whose cause is
//     `errors.New("SSE read timed out")` — the layer-3 message
//     (`provider.ts:51`). TS could produce either that or the layer-2
//     `AbortSignal.timeout` text; Go produces one. Both still match
//     `IsLikelyTimeout`.
//   - KNOWN DIVERGENCE (BUGS-KEPT #10): `Register` fires from a `defer`, so a
//     cancelled turn still releases the router's in-flight slot. TS leaks it
//     (`llm.ts:139-146`).
//   - Early teardown CANCELS THE REQUEST CONTEXT, it does not merely Close()
//     the body (ENGINE-DESIGN R2): orfetch's Close() clears the watchdogs but
//     leaves the eager pump reading, and a late chunk re-arms them.
package orclient

import (
	"math"
	"sync"
	"time"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

// ── seams ─────────────────────────────────────────────────────────────────

var seamMu sync.Mutex

// random is `Math.random`. `generateId` draws one per character.
var random func() float64 = defaultRandom

// nowMS is `Date.now()`.
var nowMS func() float64 = func() float64 { return float64(time.Now().UnixMilli()) }

// SetRandomForTesting swaps Math.random. Returns a restore func.
func SetRandomForTesting(f func() float64) func() {
	seamMu.Lock()
	prev := random
	random = f
	seamMu.Unlock()
	return func() {
		seamMu.Lock()
		random = prev
		seamMu.Unlock()
	}
}

// SetNowForTesting swaps Date.now. Returns a restore func.
func SetNowForTesting(f func() float64) func() {
	seamMu.Lock()
	prev := nowMS
	nowMS = f
	seamMu.Unlock()
	return func() {
		seamMu.Lock()
		nowMS = prev
		seamMu.Unlock()
	}
}

func currentRandom() func() float64 {
	seamMu.Lock()
	defer seamMu.Unlock()
	return random
}

func currentNow() func() float64 {
	seamMu.Lock()
	defer seamMu.Unlock()
	return nowMS
}

// idAlphabet is createIdGenerator's default (internal/index.mjs:590-593).
const idAlphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// idSize is createIdGenerator's default `size`.
const idSize = 16

// generateId is `generateId()` — createIdGenerator() with no prefix.
//
//	chars[i] = alphabet[Math.random() * alphabetLength | 0]
//
// `| 0` is ToInt32, so a random() that returns exactly 1 (impossible for
// Math.random, reachable for a stubbed one) indexes past the end and yields
// `undefined`, which `join("")` renders as the empty string. Go cannot index
// past the end, so an out-of-range draw contributes nothing — the same
// observable.
func generateId() string {
	draw := currentRandom()
	out := make([]byte, 0, idSize)
	for i := 0; i < idSize; i++ {
		idx := toInt32(draw() * float64(len(idAlphabet)))
		if idx < 0 || idx >= int32(len(idAlphabet)) {
			continue
		}
		out = append(out, idAlphabet[idx])
	}
	return string(out)
}

// toInt32 is the ES ToInt32 abstract operation, which is what `x | 0` performs.
func toInt32(f float64) int32 {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0
	}
	n := math.Trunc(f)
	m := math.Mod(n, 4294967296)
	if m < 0 {
		m += 4294967296
	}
	if m >= 2147483648 {
		m -= 4294967296
	}
	return int32(m)
}

func defaultRandom() float64 {
	// The engine layer never wants unseeded randomness in a parity run, but
	// the default has to be *some* source. jscompat.Mulberry32 seeded off the
	// clock keeps this package dependency-free (F3) while remaining a real
	// PRNG.
	defaultRandomOnce.Do(func() {
		defaultRandomFn = jscompat.Mulberry32(uint32(time.Now().UnixNano()))
	})
	return defaultRandomFn()
}

var (
	defaultRandomOnce sync.Once
	defaultRandomFn   func() float64
)
