# Engine layer design — session runner, step loop, OpenRouter streaming client

*Target: `swe-pro` @ `3b25a1a`. Scope: the layer that replaces Effect-TS orchestration
(`src/effect/runner.ts`, `src/session/run-state.ts`, `src/session/prompt.ts:runLoop`,
`src/session/processor.ts`) and the Vercel AI SDK (`streamText` from `ai@6.0.168` +
`@openrouter/ai-sdk-provider@2.8.1`). See `PORT-ASSESSMENT.md` §2 for why this is a
rewrite rather than a transliteration.*

*This document is a plan. No Go code is written by it.*

---

## 0. Settled facts and standing constraints

These are load-bearing; every later section assumes them.

**F1 — one `streamText()` call is one HTTP request.** `ai@6.0.168` defaults
`stopWhen = stepCountIs(1)` for both `streamText` (`node_modules/ai/dist/index.mjs:6381`)
and `generateText` (`:4035`); `stepCountIs` is `({steps}) => steps.length === stepCount`
(`:3842-3844`). `llm.ts:417-506` passes no `stopWhen`/`maxSteps`. The multi-turn tool cycle
is owned entirely by `prompt.ts:1478-1866` (`runLoop`). The Go LLM layer is therefore a
**single-request streaming client**, not a loop engine.
`docs/architecture/loop-and-provider.md` is stale on this point.

Two refinements from reading the recursion (`streamStep` defined `:7064`, continuation
predicate `:7524-7564`): there is no `continueSteps`/`shouldContinue` in v6 (that is the v4
API — zero hits in `dist/`). The predicate is
`(clientToolCalls.length > 0 && clientToolOutputs.length === clientToolCalls.length
  || pendingDeferredToolCalls.size > 0) && !await isStopConditionMet(...)`,
and the `&&` **short-circuits**: with no client tool calls, `isStopConditionMet` is never
even evaluated, so a tool-free response is unconditionally one request. Second: SDK-level
retry (`maxRetries` default **2**, `:2580`, `:2665`) is the only other source of extra HTTP
requests — and codeaf disables it with `maxRetries: input.retries ?? 0` (`llm.ts:480`),
owning retry itself in `retry.ts`. So on the codeaf path: **one turn = one HTTP request,
full stop.**

**F2 — `finish` is a unified string, never `"unknown"`, for OpenRouter.**
The provider produces `{unified, raw}` (`@openrouter/ai-sdk-provider/dist/index.mjs:2615-2623`)
and the SDK splits it into `finishReason` (string) + `rawFinishReason`
(`ai/dist/index.mjs:6186-6191`, type at `ai/dist/index.d.ts:2663-2668`). The unified
domain is `'stop' | 'length' | 'content-filter' | 'tool-calls' | 'error' | 'other'`
(`@ai-sdk/provider/dist/index.d.ts:1771`), and OpenRouter's `mapToUnified`
(`.../index.mjs:2600-2614`) maps everything unrecognised to `"other"`. So
`ctx.assistantMessage.finish` (`processor.ts:474`) is always one of those six strings.
Consequence: the `"unknown"` arm of `prompt.ts:1834` is **dead on the OpenRouter path**
and the exit test at `prompt.ts:1617` reduces to `finish != "tool-calls"`. Port both
literally anyway (bug-for-bug), but the truth table only needs six values.

Three sharp edges on the mapping:
- **`finish_reason: "error"` maps to `other`, not `error`** — it falls through `mapToUnified`'s
  `default`. The only sources of `unified: "error"` are the zod-parse failure
  (`.../internal/index.mjs:3780`), a top-level error payload (`:3786`), and a reader error
  (`:4098`), all with `raw: undefined`.
- **Two synthetic promotions to `tool-calls`** (`.../internal/index.mjs:4101-4109`, stream
  path; `:3668-3674`, generate path): `hasToolCalls && hasEncryptedReasoning && unified=="stop"`
  → `tool-calls`, and `hasToolCalls && unified=="other"` → `tool-calls`. `raw` is preserved.
  This is what keeps the §2.2 exit condition consistent for models that report `stop`
  alongside tool calls — but it is *provider-side* logic, so the Go client must reproduce it
  or conjunct 2 of the exit test will diverge from conjunct 3.
- `stepFinishReason` is initialised to the **string** `"other"` (`ai/dist/index.mjs:7246`),
  and an `error` stream part forces it to `"error"` (`:7402`) while still emitting a
  `finish-step`.

**F3 — zero third-party Go dependencies today.** `go.mod` declares only
`module github.com/Agent-Field/swe-pro-go` / `go 1.23`; there is no `require` block, no
`go.sum`, and `go list -deps ./...` shows nothing outside stdlib. Every package shipped so
far is stdlib-only, including the arbitrary-precision work in
`internal/router/adaptive/mathcr.go` (`crPrec = 200`, `reducePrec = 1400`).
`go build ./... && go vet ./... && go test ./...` is currently green across 35 packages.
The engine layer must hold this line: SSE parsing hand-rolled (`bufio`), JSON via
`internal/jscompat.Stringify`, `decimal.js` parity via `math/big.Float`/`math/big.Rat`,
HTTP via `net/http`. Introducing the first dependency is a decision that needs sign-off,
not a side effect of this layer.

**F4 — bug-for-bug is policy.** `BUGS-KEPT.md` header, user decision 2026-07-27. Every
suspected TS bug found while porting gets an entry there, not a fix. Sections below flag
new candidates explicitly as **[BUG-CANDIDATE]**.

**F5 — house test convention.** Each package gets `testdata/fixtures.json`, generated by
`tools/fixtures/gen-<pkg>.ts` running the *real* TS source, and replayed by a
`fixtures_test.go` that asserts **byte equality** between `jscompat.Stringify(goResult)`
and the recorded `JSON.stringify`. Injection seams are `SetXForTesting(f) func()` closures
returning a restore func (`orfetch.SetTimerFactoryForTesting`, `orfetch.SetDoerForTesting`,
`orfetch.SetLimiterProvider`, `adaptive.SetClockForTesting`, `SetRandomForTesting`,
`SetSleeperForTesting`). The engine layer follows this exactly.

**F6 — the fetch seam already exists.**
`internal/router/orfetch/orfetch.go:417` —
`func OpenRouterAimdFetch(req *http.Request) (*http.Response, error)`.
The TS two-arg signature collapses into `*http.Request`: URL/method/headers/body is the
request, `init.signal` is `req.Context()` (`orfetch.go:409-416`). Errors are returned as
the *abort reason* (the manufactured timeout error), not a `context.Canceled` wrapper
(`orfetch.go:457-467`), which is exactly what the router's `IsLikelyTimeout` needs.
It hands back **raw bytes** (`*http.Response` whose `Body` is an unexported `*wrappedBody`),
not events — SSE framing is the engine's job.

**F7 — house style is documented in the package comment, not the code.** Every ported
package opens with a 20-60 line block comment structured as: (1) `Package X is a bug-for-bug
port of src/…/y.ts — <what>`; (2) `── seams ──` listing every runtime capability TS gets free
that Go must be handed; (3) `── sibling ports ──` naming the Go packages consumed and the
narrow interface used; (4) `── fidelity notes (deliberate, do not "fix") ──` listing every
place the Go looks wrong on purpose, citing TS `file:line`. Other conventions to match:
`errors.New` with **exact TS message text** wherever the string is a behavioural contract;
custom error types are structs with an exported `Message string` field; errors are **not**
wrapped with `%w` and there is no `errors.Is` sentinel convention; every JS `number` is
`float64`, never `int`; optional TS fields read with `??` become **pointers**, not
zero-value sentinels; `context.Context` appears in exactly one package today (`orfetch`) —
`adaptive` uses its own `AbortSignal` instead, so the engine must choose deliberately rather
than sprinkling `ctx` params.

**F8 — two integration gaps the engine layer inherits.**
- `internal/router/state.GetRouter()` currently returns an inert `placeholderRouter`;
  `state.SetRouterFactory` is never called outside tests, and nothing outside
  `internal/router` imports any router package. **Wiring
  `state.SetRouterFactory(… adaptive.NewAdaptiveModelRouter(…))` plus an
  `adaptive.AdaptiveRouteEvent → state.RouteEvent → state.EmitRouteEvent` bridge is
  unclaimed work that belongs to this layer's bootstrap.** The two event structs are
  field-for-field identical apart from `Tier` (`ModelTier` vs `string`) and `Error`
  (`ErrorText` vs `string`).
- All six router error classifiers (`adaptive.IsLikelyTimeout`, `IsLikelyRateLimit`,
  `IsLikelyProviderIncompatible`, `IsLikelyStructuredFailure`,
  `IsLikelyTransientProviderError`, `IsRetryableRouteError`) and
  `adaptive.Register(choice, elapsedSeconds, completionTokens, err *JSValue)` take a
  **`*adaptive.JSValue`, not a Go `error`**. The engine needs an `error → *JSValue` adapter;
  `adaptive.ErrWithStatus(msg, status)` (`jsvalue.go:78`) is the shape an OpenRouter HTTP
  error arrives in.

---

## 1. Package layout

Two trees. `internal/llm/*` is "everything the AI SDK + provider package used to do";
`internal/engine/*` is "everything Effect-TS used to do". Both are new; nothing existing
is modified except that `internal/router/state.GetRouter()` gains a concrete type at the
call site.

```
internal/llm/
  wire/         OpenRouter HTTP DTOs + the SSE frame decoder
  orclient/     one request → normalized stream parts (replaces @openrouter/ai-sdk-provider)
  step/         one step (replaces streamText): tool-call assembly, repair, tool exec, event union
  modelmsg/     ModelMessage / UIMessage structs + convertToModelMessages port
  transform/    ProviderTransform, OpenRouter-reachable subset only
  catalog/      models.dev fetch/cache + Provider.Model
  llmerr/       error taxonomy: APICallError analogue, parseAPICallError, parseStreamError
  usage/        getUsage + cost (math/big)

  overflow/     usable / shouldScanDrift / isOverflow (overflow.ts) — reads knobs
  routerbridge/ error→*adaptive.JSValue adapter; state.SetRouterFactory wiring;
                AdaptiveRouteEvent→state.RouteEvent bridge (see F8)

internal/engine/
  message/      MessageV2 structs, toModelMessages, filterCompacted
  processor/    stream-event → message/part state machine (processor.ts)
  retry/        retry.ts port (policy / delay / retryable / isTimeoutError)
  fault/        interrupt-vs-failure-vs-defect discrimination (the Effect Cause analogue)
  degrade/      the shared timeout+catch-all collapse helper
  runner/       the 4-state runner (runner.ts)
  runstate/     per-session runner registry + BusyError (run-state.ts)
  loop/         runLoop (prompt.ts:1478-1866)
```

None of these exist yet. There is no `internal/engine`, `internal/llm`, `internal/provider`,
`internal/tool` or `internal/config` in the repo, and the `//go:embed` of the 48 prompt/text
assets called for by PORT-ASSESSMENT §6 step 2 has not happened either — there is no embedded
asset anywhere. Confirmed absent by name-grep: no `Message`, `Part`, `Role`, `ToolCall`,
`Usage`, `Cost`, or `Overflow` type exists in Go today. The only trace of overflow is the
knob `LEAF_CONTEXT_TRIGGER_TOKENS` (`internal/session/knobs/knobs.go`, default 60 000, range
20 000–160 000) — `llm/overflow` must read it via `knobs.ResolveKnobs(nil, env)` rather than
re-deriving the constant, and must reproduce `overflow.ts`'s separate
`CODEAF_LEAF_CONTEXT_TRIGGER == "0"` kill switch that returns `+Infinity`.

### Responsibilities

| package | owns | replaces | key TS source |
|---|---|---|---|
| `llm/wire` | request body struct, SSE `data:` frame splitter, chunk DTOs, `error` payload shape | `createEventSourceResponseHandler` + zod schemas | `@openrouter/.../index.mjs:3300-3440`, `:3402-3455` |
| `llm/orclient` | body assembly, `OpenRouterAimdFetch` call, chunk→part translation, tool-call delta accumulation, usage/finish accumulation | `OpenRouterChatLanguageModel.doStream` | `@openrouter/.../index.mjs:3506-3592` (`getArgs`), `:3788-3812` (`doStream`), `:3833-4160` (transform) |
| `llm/step` | the `fullStream` event union, `experimental_repairToolCall`, tool-arg validation, in-process tool `execute`, `start`/`start-step`/`finish-step`/`finish` synthesis, `activeTools` filtering | `streamText` (one step) | `ai/dist/index.mjs:6381+`, `llm.ts:417-506` |
| `llm/modelmsg` | `ModelMessage`/`UIMessage` structs, the `step-start`-splits-blocks algorithm, `createToolModelOutput` | `convertToModelMessages` | `ai/dist/index.mjs:8311-8551`, `:1655-1674` |
| `llm/transform` | `sanitizeSurrogates`, `unsupportedParts`, `normalizeMessages` (2 reachable branches), `options`, `providerOptions`, `temperature`/`topP`/`topK`/`maxOutputTokens`, `schema` (kimi branch) | `ProviderTransform` | `transform.ts:22-24, 393-476, 1043-1185, 1230-1282, 1284-1398` |
| `llm/catalog` | models.dev fetch, 5-min freshness, flock, 60-min refresh, `Provider.Model`, catalog post-processing | `src/provider/models.ts`, `provider.ts:1388-1417` | — |
| `llm/llmerr` | `APIError` analogue with `StatusCode/ResponseBody/ResponseHeaders/IsRetryable/URL`, 19 overflow regexes, `parseAPICallError`, `parseStreamError` | `src/provider/error.ts` | `error.ts:8-28, 118-163, 181-202` |
| `llm/usage` | `getUsage` incl. `experimentalOver200K` switch and reasoning-billed-at-output | `Session.getUsage` | `session.ts:353-414` |
| `llm/overflow` | `usable`, `shouldScanDrift`, `isOverflow`; `EFFECTIVE_CONTEXT_CAP=160_000`, `TRIGGER_PCT=0.6`, drift `0.7` | `src/session/overflow.ts` | consumed at `processor.ts:517-525` |
| `llm/routerbridge` | `error → *adaptive.JSValue`; `state.SetRouterFactory`; route-event bridge | nothing (new wiring) | see F8 |
| `engine/message` | the 12 `Part` variants, 4 `ToolState` variants, `User`/`Assistant`, `fromError`, `toModelMessages` | `message-v2.ts` | `message-v2.ts:86-592, 729-1011, 1159-1264` |
| `engine/processor` | the 18-case event switch, doom-loop guard, tool-call settlement, `cleanup`, `halt`, compaction trigger | `processor.ts` | `processor.ts:223-751` |
| `engine/retry` | attempt-indexed backoff schedule | `retry.ts` | `retry.ts:68-257` |
| `engine/fault` | `Interrupt`/`Defect`/`Failure` classification, `IsInterruptOnly`, `HasInterrupts`, `HasDefects`, `Squash` | `effect/Cause` | `runner.ts:59-65, 159-170`, `processor.ts:713-716` |
| `engine/degrade` | one generic `Degrade[T]` collapsing timeout+failure+defect (+interrupt) into a fallback | ~115 `catchCause` sites | see §4.5 |
| `engine/runner` | 4-state machine over one session's work | `runner.ts` | `runner.ts:32-220` |
| `engine/runstate` | `map[SessionID]*Runner` + `BusyError` + status transitions | `run-state.ts` | `run-state.ts:27-106` |
| `engine/loop` | the outer step loop, exit condition, exit guard, scheduler pump seam, reminder insertion | `prompt.ts:runLoop` | `prompt.ts:1478-1866` |

### Dependency direction

`engine/loop → engine/processor → llm/step → llm/orclient → llm/wire → router/orfetch`.
`engine/message` is depended on by `processor` and `loop`; `llm/modelmsg` is depended on
by `engine/message` (it produces `[]ModelMessage`). `llm/transform` is invoked from
`llm/step` at the point the `wrapLanguageModel` middleware fires (`llm.ts:482-496`), i.e.
*after* the step has assembled its params and *before* `orclient` builds the body.
`engine/fault` and `engine/degrade` have no dependencies and are imported everywhere.

Nothing in `internal/llm` may import `internal/engine` — the LLM layer must be testable
against an httptest server with no session state.

---

## 2. The Go step-loop state machine

### 2.1 Shape

`runLoop` (`prompt.ts:1478-1866`) is a `while(true)` with three exits: the natural exit
(`:1684`), a compaction `"stop"` (`:1712`), and the inner `outcome === "break"` (`:1858`).
Everything else `continue`s. Direct Go transliteration:

```
for {
    setStatus(busy)                                    // :1487
    schedulerPump()                                    // :1497-1585   (step>0 && !session.parentID)
    msgs := filterCompacted(sessionID)                 // :1587
    lastUser, lastAssistant, lastFinished, tasks := backScan(msgs)   // :1593-1601
    if lastUser == nil { panic("No user message found in stream.") } // :1603
    if shouldExit(...) {                               // :1615-1620
        if exitGuardTripped() { continue }             // :1626-1682
        break                                          // :1684
    }
    step++                                             // :1687
    if step == 1 { go title(...) }                     // :1688-1694
    model := getModel(lastUser.Model)                  // :1696
    task := tasks.pop()                                // :1697
    if task.subtask   { handleSubtask(); continue }    // :1699-1702
    if task.compaction { if compact()=="stop" {break}; continue }  // :1704-1714
    if overflowOnLastFinished() { createCompaction(); continue }   // :1716-1723
    agent := agents.get(lastUser.Agent)                // :1725-1732 (nil ⇒ publish+throw)
    isLastStep := step >= agent.Steps                  // :1733-1734
    msgs = insertReminders(msgs, agent, session)       // :1735
    assistant := newAssistantStub(...)                 // :1737-1752
    handle := processor.Create(assistant, sessionID, model)  // :1753-1757
    outcome := runOneTurn(...)                         // :1759-1857
    if outcome == "break" { break }
}
prune(); final := lastAssistant(sessionID); completePlanDBRoot(final)   // :1862-1865
```

### 2.2 Exit condition — exact transliteration

`prompt.ts:1612-1620`:

```ts
const hasToolCalls =
  lastAssistantMsg?.parts.some((part) => part.type === "tool" && !part.metadata?.providerExecuted) ?? false

if (
  lastAssistant?.finish &&
  !["tool-calls"].includes(lastAssistant.finish) &&
  !hasToolCalls &&
  lastUser.id < lastAssistant.id
) { ... break }
```

Go, with each conjunct traced to its line:

| # | TS | Go | notes |
|---|---|---|---|
| 1 | `lastAssistant?.finish` (`:1616`) | `lastAssistant != nil && lastAssistant.Finish != ""` | `finish` is `Schema.optional(Schema.String)` (`message-v2.ts:580`). JS truthiness of a string means `""` is falsy — a recorded empty-string finish does **not** satisfy the guard. Model it as `*string` and test `!= nil && *v != ""` so the JS semantics survive an explicit `""`. |
| 2 | `!["tool-calls"].includes(finish)` (`:1617`) | `*lastAssistant.Finish != "tool-calls"` | single-element array; per F2 the reachable domain is the six unified strings. |
| 3 | `!hasToolCalls` (`:1618`) | `!hasToolParts(lastAssistantMsg)` | `lastAssistantMsg` is resolved by `msgs.findLast(m => m.info.role=="assistant" && m.info.id == lastAssistant.id)` (`:1605-1607`) — **not** by identity with `lastAssistant`. If it is not found, `?? false` (`:1613`) makes `hasToolCalls` false. Go: `if m == nil { hasTool = false }`. |
| 4 | `lastUser.id < lastAssistant.id` (`:1619`) | `lastUser.ID < lastAssistant.ID` | string comparison. JS `<` is UTF-16 code-unit order; Go `<` is byte order. `MessageID.ascending()` (`src/id/id.ts`) emits ASCII only, so they agree — assert this with a fixture rather than assuming it. |

`hasToolParts` (`:1613`) is `part.type == "tool" && part.metadata["providerExecuted"] != true`.
Note it tests the *part-level* `metadata` map (`message-v2.ts:365`), not the tool state.
In Go the metadata map is `map[string]any`; the predicate is
`v, ok := p.Metadata["providerExecuted"]; !(ok && v == true)` — a `providerExecuted: false`
entry counts as a tool call, matching JS truthiness.

**Truth table** (only the four inputs matter; this is the fixture target):

| finish set | finish=="tool-calls" | hasToolParts | user<assistant | result |
|---|---|---|---|---|
| no | – | – | – | continue |
| yes | yes | – | – | continue |
| yes | no | yes | – | continue |
| yes | no | no | no | continue |
| yes | no | no | yes | **exit-candidate → exit guard** |

The comment at `prompt.ts:1608-1611` explains conjunct 3: some providers return `stop`
even with tool calls in the message, and provider-executed tool parts were already handled
inside the stream, so they must not force another turn.

**Conjuncts 2 and 3 are coupled through the provider.** The OpenRouter client promotes
`stop`/`other` to `tool-calls` whenever tool calls are present (F2), so in practice the
exit-candidate row is reached only when the model genuinely produced no tool calls. If the Go
client omits those promotions, conjunct 2 stays false while conjunct 3 becomes true and the
loop still iterates — same outcome, different `finish` value persisted on the assistant
message, which then diverges in the decision-ledger and in `prompt.ts:1834`'s `finished`
check. Port the promotions.

### 2.3 Exit guard (`prompt.ts:1626-1682`)

Runs only on the exit-candidate row. Sequence:

1. `planDBInfoFromMessages(msgs)` (`:1626`, parser at `:1460-1476`) — regex-scans **text
   parts of all messages** for `Project:\s+(p-[a-z0-9-]+)` and `Root task:\s+(t-[a-z0-9-]+)`;
   `Database:\s+(.+)` is optional and defaults to `""`. Returns on the first message with
   both. Go: `regexp` handles these (no lookaround). Note `/i` on the first two.
2. If absent → fall through to `break`.
3. `findOpenCapExhaustFailures({workspace, dbPath, rootTaskID})`, wrapped in
   `.catch(() => [])` (`:1628-1634`) — a degrade site.
4. `open.length == 0` → `break`.
5. `lastModel(sessionID)` degraded to `undefined` (`:1636-1638`). **If it fails, the guard
   is skipped and the loop breaks anyway** (`:1639` guards the whole block).
6. Otherwise: create a fresh `MessageV2.User` (`:1657-1664`) with `agent` = the agent of
   the most recent assistant in the last 10 messages, defaulting to `"orchestrator"`
   (`:1648-1656`), and `model` = the last model; persist it; persist one synthetic text
   part carrying `renderRecoveryReminder(open)` (`:1666-1673`).
7. `markFailureNudged(rootTaskID, f.taskID)` for each open failure (`:1674-1676`) — this is
   what makes the guard fire at most once per failure.
8. `continue` (`:1679`) — **without incrementing `step`**, so the next iteration re-runs
   the scheduler pump.

### 2.4 Tool-part settlement

Two independent mechanisms, both in `processor.ts`.

**In-flight registry.** `ctx.toolcalls: Record<string, ToolCall>` where
`ToolCall = {partID, messageID, sessionID, done: Deferred<void>}` (`processor.ts:63-68`).
- `tool-input-start` (`:279-306`) creates the part in `pending` state
  (`{status:"pending", input:{}, raw:""}`) and registers the entry with a fresh Deferred.
  It reuses `ctx.toolcalls[value.id]?.partID` if one already exists (`:291`).
- `tool-call` (`:322-379`) flips the part to `running` with the parsed input and
  `time.start`.
- `tool-result` → `completeToolCall` (`:178-202`): only if `state.status == "running"`
  (`:188`), writes `completed` with `output/metadata/title/time.end/attachments`, then
  `settleToolCall`.
- `tool-error` → `failToolCall` (`:204-221`): only if `running`, writes `error` state; if
  the error is `Permission.RejectedError` or `Question.RejectedError`, sets
  `ctx.blocked = ctx.shouldBreak` (`:216-218`); then settles.
- `settleToolCall` (`:141-145`): deletes the map entry **then** resolves the Deferred.

**Go**: `map[string]*toolCall` guarded by the processor's own mutex (the event loop is
single-goroutine, but `completeToolCall`/`updateToolCall` are exported on the `Handle`
and called from tool implementations on other goroutines — `processor.ts:757-758` exposes
both). `done` becomes a `chan struct{}` closed exactly once (`sync.Once`).

**Cleanup drain** (`processor.ts:602-660`, runs via `Effect.ensuring` at `:744`, i.e. on
every path including error and interrupt):
1. flush a pending snapshot patch (`:603-616`) — no-op in codeaf, `snapshot.track/patch`
   are stubs (`processor.ts:103-106`).
2. close `ctx.currentText` with an end time (`:618-623`).
3. close every open reasoning part with an end time (`:625-632`).
4. **await every outstanding `done` concurrently, each with a 250 ms timeout, ignoring
   failures** (`:634-638`). Go: one goroutine per entry with
   `context.WithTimeout(ctx, 250*time.Millisecond)`, joined by `sync.WaitGroup`.
5. any tool call still registered is force-written to
   `status:"error", error:"Tool execution aborted", metadata:{...existing, interrupted:true}`,
   with `time.start` taken from the existing state or `now` if the state has no `time`
   (`:640-656`) — the `pending` state genuinely has no `time` field
   (`message-v2.ts:287-294`), so this branch is reachable.
6. stamp `assistantMessage.time.completed` and persist (`:658-659`).

The `interrupted: true` metadata written in step 5 is read back on the next turn by
`message-v2.ts:922` — if `metadata.output` is a string it replays as `output-available`,
otherwise as `output-error`. Since step 5 never writes `metadata.output`, the practical
result is `output-error` with `errorText: "Tool execution aborted"`.

### 2.5 Synthetic-user injection points

Seven, and they are not interchangeable — three persist to the session, four are
in-memory-only for the current turn.

| # | site | persisted? | shape |
|---|---|---|---|
| 1 | scheduler cycle summary — `prompt.ts:1565-1581` | **yes** | new `MessageV2.User` (agent = last assistant's agent or `"orchestrator"`, model = `lastModel`) + one `TextPart{synthetic:true, text: cycleSummary}` |
| 2 | exit-guard recovery reminder — `prompt.ts:1657-1673` | **yes** | identical shape, `text: renderRecoveryReminder(open)` |
| 3 | observer reminder drain — `prompt.ts:1313-1324` | **yes** | synthetic `TextPart`s appended to the user message being created in `createUserMessage`; see §2.6 |
| 4 | plan/build-switch reminders — `prompt.ts:241-259` (non-experimental path) | no (in-memory `parts.push`) | `TextPart{synthetic:true, text: PROMPT_PLAN}` or `BUILD_SWITCH` |
| 4b | plan-mode reminders — `prompt.ts:269-277`, `:287-...` (experimental path) | **yes** (`sessions.updatePart`) | same |
| 5 | late-user-text wrapping — `prompt.ts:1785-1801` | no (mutates `p.text` on the loaded copy) | wraps every non-ignored non-synthetic non-blank user text part with `id > lastFinished.id` in `<system-reminder>…</system-reminder>`, only when `step > 1 && lastFinished` |
| 6 | `MAX_STEPS` — `prompt.ts:1821` | no | appended as an **assistant** `ModelMessage` (`{role:"assistant", content: MAX_STEPS}`) when `isLastStep`, i.e. `step >= agent.steps` |
| 7 | tool-result media extraction — `message-v2.ts:976-995` | no (conversion-time) | user `UIMessage` with `SYNTHETIC_ATTACHMENT_PROMPT = "Attached media from tool result:"` (`message-v2.ts:38`) + one file part per extracted attachment |

Site 5 is a mutation of the in-memory `msgs` slice, which is then handed to
`MessageV2.toModelMessagesEffect` at `:1809`. In Go, `msgs` must be a deep-enough copy that
this mutation does not leak into any cache — TS gets away with it because `filterCompacted`
rebuilds from the store each iteration (`prompt.ts:1587`). **Make the Go `filterCompacted`
return freshly-allocated parts**, or site 5 will corrupt the store on the second iteration.
**[BUG-CANDIDATE]** worth an entry either way, since the TS behaviour depends on that
rebuild being a copy.

Site 7 is OpenRouter-relevant: `supportsMediaInToolResult` (`message-v2.ts:745-755`) returns
`false` for every npm except anthropic/openai/bedrock/vertex-anthropic/gemini-3, and
`@openrouter/ai-sdk-provider` is not in the list. For an OpenRouter-only port the function
collapses to a constant `false`, which makes site 7 the **only** path by which tool-result
media reaches the model.

### 2.6 `queueReminder` drain point

The drain is **not** in `runLoop`. `Session.drainReminders` has exactly one production call
site: `prompt.ts:1313`, inside `createUserMessage` — i.e. it fires when a *new user prompt*
is constructed (`prompt.ts:1431-1450` → `createUserMessage` → `loop`), not on each loop
iteration. Consequences the Go port must preserve:

- Reminders queued by the Observer (`observer.ts:543`) during a long agent turn are seen on
  the **next user prompt**, not the next step of the current loop. A single `codeaf run`
  with one user message drains at most once.
- The queue itself (`session.ts:804-846`): `REMINDER_QUEUE_CAP = 8`; `queueReminder` trims,
  drops empty, drops an exact duplicate of an already-queued entry (`:821`) or of
  `lastDrained[sessionID]` (`:822`), and drops with a warning when at cap (`:823-829`).
  `drainReminders` deletes the queue and records `queue[len-1]` as `lastDrained` (`:838-846`).
- Ordering: reminders are appended **after** the PlanDB bootstrap reminder so they are the
  most-recent context (comment at `prompt.ts:1306-1312`).

Go: `map[SessionID][]string` + `map[SessionID]string` under one mutex on the session
service. Insertion order is load-bearing (the drain returns the slice as-is), so a slice,
not a set.

---

## 3. The OpenRouter streaming client

### 3.1 Request assembly — field provenance

The wire body is `getArgs(...)` (`@openrouter/.../index.mjs:3506-3592`) spread with
`restOpenrouterOptions` (everything under `providerOptions.openrouter` except
`cacheControl`) at the **top level**, then `stream: true` and `stream_options`
(`:3788-3806`). Every field, with where the value comes from in codeaf:

| body field | source | line |
|---|---|---|
| `model` | routed `Provider.Model.id` (the part after the first `/`; router picks it) | `llm.ts:105-122`, `provider.ts:1790-1796` |
| `messages` | `convertToOpenRouterChatMessages(prompt)` over the `ModelMessage[]` produced by `MessageV2.toModelMessagesEffect` and then rewritten by the middleware | `llm.ts:481-496`, `message-v2.ts:1002-1010` |
| `max_tokens` | `ProviderTransform.maxOutputTokens(model)` = `min(model.limit.output, 32_000) \|\| 32_000` | `transform.ts:1280-1282`, `llm.ts:256` |
| `temperature` | `agent.temperature ?? ProviderTransform.temperature(model)`, but **only if `model.capabilities.temperature`**, else `undefined` | `llm.ts:251-253` |
| `top_p` | `agent.topP ?? ProviderTransform.topP(model)` | `llm.ts:254` |
| `top_k` | `ProviderTransform.topK(model)` | `llm.ts:255` |
| `tools[]` | `{type:"function", function:{name, description, parameters: inputSchema}}` — `inputSchema` is `ProviderTransform.schema(model, toolSchema)` | `.../index.mjs:3568-3585`, `transform.ts:1284-1398` |
| `tool_choice` | `getChatCompletionToolChoice(toolChoice)`; codeaf sends `"required"` only for `json_schema` output format, else `undefined` | `llm.ts:454`, `prompt.ts:1824` |
| `usage` | `{include: true}` — from `ProviderTransform.options()` OpenRouter branch, lands top-level via the `restOpenrouterOptions` spread | `transform.ts:1071-1077` |
| `prompt_cache_key` | `sessionID` — same path | `transform.ts:1175-1177` |
| `reasoning` | `{effort:"high"}` **only** when `api.id` contains `gemini-3` — never for the 8 default pool models | `transform.ts:1074-1076` |
| `stream` | literal `true` | `.../index.mjs:3801` |
| `stream_options` | `{include_usage: true}` when `compatibility === "strict"`, else `undefined` | `.../index.mjs:3802-3806` |
| `seed`, `stop`, `frequency_penalty`, `presence_penalty`, `response_format`, `models`, `logit_bias`, `logprobs`, `user`, `parallel_tool_calls`, `plugins`, `provider`, `web_search_options`, `debug`, `cache_control`, `include_reasoning` | never set by codeaf | — |

`ProviderTransform.providerOptions(model, options)` (`transform.ts:1230-1278`) wraps the
merged options under `sdkKey("@openrouter/ai-sdk-provider") = "openrouter"`
(`transform.ts:46-47`), which is exactly the namespace `doStream` unwraps. So the Go client
can skip the namespace round-trip entirely and build the top-level fields directly — but
**keep the merge order**, which is
`mergeDeep(mergeDeep(mergeDeep(base, model.options), agent.options), variant)`
(`llm.ts:210-221`) with remeda's `mergeDeep` semantics.

**The namespace is an unvalidated whole-body override, not a sub-object.**
`doStream` does
`const {cacheControl, ...rest} = providerOptions.openrouter ?? {};
 args = {...getArgs(options), ...rest, ...(cacheControl != null && !("cache_control" in rest) ? {cache_control: cacheControl} : {})}`
(`.../internal/index.mjs:3715-3718`; `doGenerate` identically at `:3520-3523`). There is **no
zod validation** of call-level `providerOptions.openrouter` — `OpenRouterProviderOptionsSchema`
(`:2473-2482`) is used only for *message-level* options. So anything codeaf puts under that
key silently overwrites the corresponding top-level body field. That is how `usage` and
`prompt_cache_key` get there, and it means the Go body builder must apply the merged options
**last**, over the computed fields, with the same shallow-spread semantics.

**Two request-side details that are easy to get wrong:**

- **`deterministicStringify`** (`.../internal/index.mjs:2576-2596`) — assistant `tool_calls`
  serialise their input via a **recursive key sort using `localeCompare`** before
  `JSON.stringify`. This exists for Anthropic-through-OpenRouter signature validation. Go
  needs the same recursive sort with a `localeCompare`-equivalent comparator (**not**
  `sort.Strings` byte order — PORT-ASSESSMENT §3 flags `localeCompare` as an ICU hazard).
  Getting it wrong silently breaks signed reasoning replay. **This is the single highest-risk
  request-side item.**
- **`undefined` is not stripped.** `__spreadValues` keeps `undefined`-valued keys; they only
  disappear at `JSON.stringify` (`.../internal/index.mjs:2135`). In Go, use pointers +
  `omitempty` (this is the one place `omitempty` is correct — see §5.1 for why it is banned
  on the session model).

Sampling knob values (`transform.ts:478-513`) for the default pools: qwen `temp 0.55,
topP 1`; glm-4.6/4.7 `temp 1.0`; minimax-m2 `temp 1.0, topP 0.95, topK 40|20`; kimi-k2
`temp 1.0` if thinking/k2./k2p/k2-5 else `0.6`. Note these are *id-substring* matches —
port the substring table, not a model enum.

**Headers.** Three contributors, merged by `combineHeaders(config.headers(), options.headers)`
with the later winning (`.../internal/index.mjs:521-526`):

1. Provider-level, from `createOpenRouter` (`@openrouter/.../dist/index.mjs:5270-5281`):
   `Content-Type: application/json`; `Authorization: Bearer <apiKey ?? $OPENROUTER_API_KEY>`;
   **`X-OpenRouter-Title: <appName>`** (note: *not* `X-Title`); `HTTP-Referer: <appUrl>`;
   `user-agent: ai-sdk/openrouter/2.8.1 ai-sdk/provider-utils/4.0.23 runtime/…`.
2. codeaf's provider config (`provider.ts:427-447`): `HTTP-Referer: https://codeaf.local/`,
   `X-Title: codeaf`.
3. Call-level (`llm.ts:457-479`, non-`codeaf` branch): `x-session-affinity: <sessionID>`,
   optional `x-parent-session-id`, `User-Agent: codeaf/<version>`, unconditional
   `HTTP-Referer: <APP_URL>` and `X-OpenRouter-Title: <APP_NAME>`, then `model.headers`,
   then plugin headers.

So `X-Title` and `X-OpenRouter-Title` are both on the wire, `HTTP-Referer` is set three times
with the call-level value winning, and `User-Agent`/`user-agent` collide case-insensitively.
Reproduce the final merged set, not each contributor in isolation — **fixture the exact
header map** rather than reasoning about precedence. Auth is env-var only, hard-fail if unset
(`run.ts:402-403`, `provider/auth.ts:196-199`).

**`stream_options` depends on a compatibility mode codeaf never sets explicitly.**
`stream_options: {include_usage: true}` is emitted **only** when
`config.compatibility === "strict"` (`.../internal/index.mjs:3728-3730`);
`createOpenRouter` defaults to `"compatible"` (`dist/index.mjs:5269`) while the exported
`openrouter` singleton is `"strict"` (`:5346-5348`). Which one codeaf gets depends on
`provider.ts`'s instantiation path. It matters less than it looks: the top-level
`usage: {include: true}` from `transform.ts:1071-1077` is mode-independent and is what
actually yields `cost`/`cost_details`. **[OPEN]** see R11.

**Middleware.** `wrapLanguageModel` (`llm.ts:482-496`) rewrites `args.params.prompt` via
`ProviderTransform.message(...)` **only when `args.type === "stream"`**. In Go this is not a
middleware — it is a plain call in `llm/step` immediately before handing params to
`orclient`. Keeping it as a named seam (`transform.Message(prompt, model, options)`) matters
because it is where the DeepSeek stub and surrogate sanitisation land.

### 3.2 Reachable transforms (OpenRouter, default pools only)

All eight default pool entries resolve to `providerID = "openrouter"`,
`api.npm = "@openrouter/ai-sdk-provider"`, and `api.id = model.id = "<vendor>/<name>"`
(`provider.ts:1010-1014`, `:1790-1796`). Against `normalizeMessages` (`transform.ts:60-340`)
that leaves exactly **two** live branches:

1. **Surrogate sanitisation** (`transform.ts:80-125`) — unconditional. Applies
   `sanitizeSurrogates` to: `system` string content; `user` string content and `text` parts;
   `assistant` string content, `text` parts and `reasoning` parts; and `tool` messages'
   `tool-result` outputs via `sanitizeToolResultOutput` (`:65-78`, handles
   `text`/`error-text`/`content`). Mutates in place.
2. **DeepSeek empty-reasoning stub** (`transform.ts:287-303`) — guard is
   `model.api.id.toLowerCase().includes("deepseek")`, so it fires for
   `deepseek/deepseek-v4-flash-0731` — **the default HIGH and LOW model**. Every
   assistant message gets `{type:"reasoning", text:""}` **appended at the
   end** of its content (after tool-calls), unless it already has a reasoning part. String
   content is first converted to `[{type:"text", text}]`. Not an early return.

Dead for these models, with the guard that kills each:
- Anthropic empty-content filter — `npm === "@ai-sdk/anthropic"` (`:129`)
- Bedrock empty-content filter — `npm === "@ai-sdk/amazon-bedrock"` (`:157`)
- claude toolCallId scrub — `api.id.includes("claude")` (`:184`)
- Anthropic tool_use reorder — npm in `{anthropic, google-vertex/anthropic}` (`:212`)
- Mistral branch (**early-returns at `:284`**) — `providerID === "mistral"` or `api.id`
  contains `mistral`/`devstral` (`:237-241`). Would fire for a hypothetical
  `openrouter/mistralai/...` pool entry; the early return would then skip DeepSeek and
  interleaved entirely. Port the branch even though it is dead today, because pool contents
  are user-configurable.
- Interleaved-field folding (**early-returns at `:311`**) — explicitly excluded by
  `model.api.npm !== "@openrouter/ai-sdk-provider"` (`:305-309`). Safe to omit for an
  OpenRouter-only build; note the omission in `BUGS-KEPT.md` as a KNOWN DIVERGENCE.
- `applyCaching` (`:342-391`) — the call guard at `:434-446` requires an
  anthropic/claude/alibaba signal; none of the eight match. **[BUG-CANDIDATE]** the
  payload table at `:350-352` contains a live `openrouter: {cacheControl:{type:"ephemeral"}}`
  entry that can never be reached — a plausible missed prompt-cache saving in the TS
  original. Keep it dead; log it.
- `sdkKey` remap in `ProviderTransform.message` step 4 (`:449-473`) — no-op because
  `sdkKey(npm) === "openrouter" === providerID`.

`unsupportedParts` (`transform.ts:393-429`) *does* run, unconditionally, before
`normalizeMessages` (`:432`). It only touches array-content user messages: empty base64
images become an error string (`:401-412`); parts whose modality
(`mimeToModality`, `:12-18`) is not in `model.capabilities.input` become
`ERROR: Cannot read ... (this model does not support ... input). Inform the user.` (`:420-424`).

`sanitizeSurrogates` (`transform.ts:22-24`) uses a **lookbehind**
(`(?<![\uD800-\uDBFF])`). Go's RE2 has none. Implement as a manual UTF-16 scan: decode the
Go string to `[]uint16` (`unicode/utf16`), replace unpaired surrogates with `�`,
re-encode. This must be a UTF-16 scan, not a rune scan — a lone surrogate can only exist in
the Go string if it arrived via a JSON `\uD800` escape, which `encoding/json` maps to
U+FFFD on decode. **[OPEN]** see §8.

`ProviderTransform.schema` (`transform.ts:1284-1398`): the moonshot branch fires on
`providerID === "moonshotai" || api.id.includes("kimi")` — for OpenRouter it matches via the
id substring, so `moonshotai/kimi-k2.6` (default HIGH pool) gets `$ref` collapsing and
tuple-`items` flattening. Port that branch; the google/gemini branch is dead for these pools.

### 3.3 SSE grammar actually emitted

The wire is standard SSE over `text/event-stream`; the client is
`createEventSourceResponseHandler(OpenRouterStreamChatCompletionChunkSchema)`
(`.../index.mjs:3808-3810`). The Go decoder needs: split on `\n\n` (accept `\r\n`), take
lines beginning `data:`, strip one optional leading space, stop on `data: [DONE]`, and
**ignore comment lines** (`: OPENROUTER PROCESSING` keepalives). `orfetch`'s
`chunkHasContent` already uses the same `data:` test (`orfetch.go:246`,
`openrouter-fetch.ts:108-120`).

Chunk schema (`.../index.mjs:3402-3455` for the streaming variant, base at `:3300-3320`):

```
{ id?, model?, provider?,
  usage?: { prompt_tokens, prompt_tokens_details?: {cached_tokens, cache_write_tokens?},
            completion_tokens, completion_tokens_details?: {reasoning_tokens},
            total_tokens, cost?, cost_details?: {upstream_inference_cost?} },
  choices: [ { delta: { role?, content?, reasoning?, reasoning_details?, images?,
                        tool_calls?: [{index?, id?, type?, function:{name?, arguments?}}],
                        annotations? },
               index?, logprobs?, finish_reason? } ] }
```
plus an alternative top-level error payload (`OpenRouterErrorResponseSchema`, handled at
`.../index.mjs:3860-3864` by emitting `{type:"error"}` and setting finishReason to `error`).
Every object is `.passthrough()` — **unknown fields must be preserved, not dropped**, which
argues for decoding into a struct plus a `map[string]json.RawMessage` overflow for the
fields codeaf round-trips.

Translation to the normalized part union, in the order the transform actually tests
(`.../internal/index.mjs:3774-4094`; add 75 for `dist/index.mjs` line numbers):

| # | wire | emitted part(s) | line |
|---|---|---|---|
| 1 | any chunk, `includeRawChunks` on | `raw{rawValue}` — default off (`ai:6392`) | 3776 |
| 2 | zod parse failure | `error` + `finishReason="error"`, **return** | 3779 |
| 3 | `"error" in value` | `error` + `finishReason="error"`, **return** | 3785 |
| 4 | `value.provider` | stashed | 3790 |
| 5 | `value.id` | `response-metadata{id}` — on **every** chunk carrying an id | 3793 |
| 6 | `value.model` | a **separate** `response-metadata{modelId}` | 3800 |
| 7 | `value.usage` | accumulated via `Object.assign` (last usage-bearing chunk wins per field) | 3806 |
| 8 | `choices[0].finish_reason` | `finishReason = mapOpenRouterFinishReason(...)` — captured **before** the delta in the same chunk | 3837 |
| 9 | `choice?.delta == null` | **return** | 3840 |
| 10 | `delta.reasoning_details[]` | accumulate + emit; see below | 3859 |
| 11 | `delta.reasoning` (string), only if `!textStarted` | `emitReasoningChunk` | 3897 |
| 12 | `delta.content` (truthy) | `reasoning-end`, `text-start` once, `text-delta` | 3900 |
| 13 | `delta.annotations[]` `url_citation` | `source{sourceType:"url", id: url, url, title, providerMetadata.openrouter{content,startIndex,endIndex}}` | 3933 |
| 14 | `delta.tool_calls[]` | see below | 3960 |
| 15 | `delta.images[]` | `file` | 4085 |
| — | flush | `finish{finishReason, usage, providerMetadata}` | 4095 |

Non-obvious behaviours that must be reproduced:

- **`if (delta.content)` is a falsy test** (`:3900`) — empty-string content deltas are
  skipped. The SDK skips them again at `ai/dist/index.mjs:7290-7301` (`delta.length === 0`).
- **Reasoning emission is gated on `!textStarted`** (`:3874`, `:3897`): once text has begun,
  reasoning deltas are **silently swallowed but still accumulated**, and surface only in the
  final `reasoning-end` / `providerMetadata`.
- Consecutive `reasoning.text` details are **merged into the previous accumulated entry**
  (`:3862-3869`): text concatenated, `signature`/`format` filled with `||`.
- **`reasoning.encrypted` emits nothing** (`:3881-3883`) — it only accumulates.
- `reasoning-end` carries the **full accumulated array**, deliberately including the empty
  case (`:3905-3915`).
- `text-start`'s id is the **OpenRouter response id** (`gen-…`) when available, else a random
  16-char id (`:3920`). Reasoning ids are always random (`:3846`).
- `source.id` is **the URL itself**, not a generated id (`:3939`). Old-format
  `file_annotation` is parsed and then **ignored entirely**.
- `logprobs` is parsed by the schema (`:3404`) and **never read** — skip it in Go.
- `finish_reason` is `z.string()`, not an enum (`:3404`), so arbitrary strings reach `raw`.
- Mid-stream reader errors are caught by `withStreamErrorHandling` (`:2551-2573`, `:3739-3742`)
  and surface as an `error` **part** in `flush` (`:4097-4100`) — never as a thrown exception.

**Beyond the OpenRouter chunk translation, the SDK adds its own layer** — the Go `llm/step`
package owns this half. `TextStreamPart` (`ai/dist/index.d.ts:2601-2685`) has **22 variants**,
including two the recon did not list: **`tool-output-denied`** (`:2657`) and
**`tool-approval-request`** (`:2658`), plus `source`, `file`, `raw`, and `abort`. `processor.ts`
handles 18 and routes the rest to its `default:` log-and-ignore branch (`:596-598`) — which
means `abort`, `raw`, `source`, `file`, `tool-output-denied` and `tool-approval-request` are
all *silently dropped* by codeaf today. Reproduce that: emit them, ignore them, log them.

Ordering guarantee worth relying on: `runToolsTransformation` **defers the provider `finish`
chunk until every outstanding tool execution resolves** (`ai/dist/index.mjs:6144-6152`
`attemptClose`, `:6360-6362` `flush`). So `tool-result` / `tool-error` parts always precede
`finish-step`. The Go step must hold the finish until its tool goroutines join, or
`processor.ts:456-528`'s usage/compaction handling will run against an incomplete part list.

The SDK also injects `start-step{request, warnings}` on the first chunk of any kind
(`ai:7267-7281`) and `{type:"start"}` when the stream opens (`ai:6844`); the inner
per-request union (`SingleRequestTextStreamPart`, `ai/dist/index.d.ts:4529-4603`) uses
`delta` where the outer union uses `text`, and has `stream-start`/`response-metadata` that
the outer one does not. Keep the two unions distinct in Go — collapsing them loses the
`start-step`/`finish-step` framing `processor.ts` depends on.

**Tool-call accumulation** (`.../index.mjs:4034-4160`) — the exact algorithm to port:

- `index = toolCallDelta.index ?? toolCalls.length - 1` (`:4036`). A missing index targets
  the **last** slot; on the very first delta that is `-1`. **[BUG-CANDIDATE]** worth a
  fixture.
- If `toolCalls[index] == null`: require `type === "function"` (else
  `InvalidResponseDataError`, `:4039-4044`); require `function.name != null` (else same,
  `:4045-4050`); `toolCallId = delta.id ?? ""`, and if empty **or already seen**, mint a
  fresh id (`:4051-4055`) — id uniqueness is enforced by the client, not the server.
- Slot initialised `{id, type:"function", function:{name, arguments: delta.function.arguments ?? ""}, inputStarted:false, sent:false}` (`:4056-4064`).
- If the initial arguments string already parses as JSON (`isParsableJson`, `:4071`), the
  whole `tool-input-start` → `tool-input-delta` → `tool-input-end` → `tool-call` burst is
  emitted immediately from the first delta (`:4072-4090`).
- Otherwise arguments accumulate across deltas (`:4056-4058`); `tool-input-start` (plus a
  buffered-args delta if non-empty) is emitted lazily once (`:4041-4055`); a
  `tool-input-delta` is emitted on **every** subsequent delta, even for null args
  (`:4059-4063`); and `tool-input-end` + `tool-call` fire **as soon as the accumulated
  buffer parses as JSON** (`:4064-4082`) — opportunistically, *not* gated on `finish_reason`.
- **[BUG-CANDIDATE]** the parsable check at `:4064` has **no `sent` guard**, so `tool-call`
  can be emitted **more than once** for the same id when arguments keep arriving after the
  buffer first became parsable. Reproduce it; fixture it.
- At flush (`:4095-4145`), still-unsent tool calls are emitted **only if** the final
  `finishReason.unified === "tool-calls"` (`:4110`) — which is where the two synthetic
  promotions in F2 earn their keep. Unparsable accumulated args are replaced with `"{}"`
  (`:4113`).
- `reasoning_details` is attached to the **first tool call only**, guarded by
  `reasoningDetailsAttachedToToolCall` (`:3763`, `:4019-4025`, `:4074-4080`, `:4135-4141`);
  later calls get `providerMetadata: undefined`.
- A `tool_calls` delta whose `type` is missing (the schema marks it `.optional()`) **throws**
  `InvalidResponseDataError` (`:3964-3969`), as does a first delta without `function.name`
  (`:3970-3975`).

`processor.ts` handles `tool-input-delta` as a **no-op** (`:308-309`), so the Go
implementation may emit it purely for symmetry; it must still emit `tool-input-start`
(which is what creates the pending part, `processor.ts:290-305`) before `tool-call`.

### 3.3a Tool-call validation, repair, and the `invalid` tool

This is `llm/step`'s job and it has a non-obvious three-stage fallback.

**Validation** — `doParseToolCall` (`ai/dist/index.mjs:3723-3760`): unknown tool name →
`NoSuchToolError{toolName, availableTools}` (`:3733-3736`); otherwise
`toolCall.input.trim() === "" ? validate({}, schema) : parseAndValidate(input, schema)`
(`:3737-3739`) — note **an empty input string validates `{}` against the schema**, it is not
an error. Validation failure → `InvalidToolInputError{toolName, toolInput, cause}` (`:3741`).

**Repair** — `parseToolCall` (`:3657-3686`) calls `experimental_repairToolCall` **only** for
those two error types. codeaf's implementation (`llm.ts:427-447`): if the lowercased tool name
differs from the emitted one and a lowercase tool exists, return the call with the name
lowercased; otherwise rewrite to `toolName: "invalid"` with
`input: JSON.stringify({tool, error: failed.error.message})`. Note the repair callback
receives and must return `input` as a **raw JSON string**
(`LanguageModelV3ToolCall.input: string`, `@ai-sdk/provider/dist/index.d.ts:1677-1706`) —
codeaf's `JSON.stringify` there is correct, and a Go port must not hand back a map.
Semantics of the return value: object → re-validated via `doParseToolCall` (a second failure
is **not** re-repaired); `null` → the **original** error is rethrown (`:3682-3684`); a throw →
wrapped in `ToolCallRepairError` (`:3677-3680`).

**Safety net** — and this is the part that must not be missed: if repair is absent, returns
`null`, or the repaired call still fails, `parseToolCall` **does not throw**. It returns a
synthetic call `{type:"tool-call", toolCallId, toolName, input: bestEffortParse ?? rawString,
dynamic: true, invalid: true, error, …}` (`:3687-3702`). Downstream (`:6226-6237`) that is
emitted as a normal `tool-call` part **and immediately** as a `tool-error` part with
`error: getErrorMessage(toolCall.error)`; the tool is **never executed** (`:6236` breaks
before `execute`). Because that counts as a resolved client tool output, the step still
completes normally and — critically for §2.2 — the assistant message ends up with a tool part,
so `hasToolCalls` is true and the outer loop iterates, giving the model a chance to correct
itself.

`activeTools` excludes `"invalid"` (`llm.ts:452`) so the model can never *choose* it, but the
tool map passed to the SDK includes it so repair can *target* it. Tool map ordering is
`Object.entries(tools).toSorted(([a],[b]) => a.localeCompare(b))` (`llm.ts:308`) — another
`localeCompare` site; it changes the serialized request body and therefore prompt-cache hit
rates.

**Finish reasons** (`.../index.mjs:2600-2614`):
`stop→stop`, `length→length`, `content_filter→content-filter`,
`function_call|tool_calls→tool-calls`, everything else (incl. `null`) `→other`.
The pre-stream default is `createFinishReason("other")` (`:3820`), and a top-level `error`
payload forces `error` (`:3861`). The `raw` string is carried alongside and surfaces as
`rawFinishReason`; codeaf stores only the unified value (`processor.ts:474`).

### 3.4 `reasoning_details` round-trip

Three variants (`.../index.mjs:2400-2421`), all sharing
`{id?: string|null, format?: enum|null, index?: number}` (`:2400-2404`):

| type | payload |
|---|---|
| `reasoning.summary` | `summary: string` |
| `reasoning.encrypted` | `data: string` |
| `reasoning.text` | `text?: string\|null`, `signature?: string\|null` |

`ReasoningDetailsWithUnknownSchema` (`:2423-2426`) maps any unrecognised entry to `null`,
and `ReasoningDetailArraySchema` filters nulls (`:2427`) — so a future variant is **dropped
silently, per-entry**, not fatal. `format` enum values at `:2384-2396`, default
`anthropic-claude-v1`.

Outbound: on the request side the provider reads `providerOptions.openrouter.reasoning_details`
off assistant messages (`.../index.mjs:3021`) and writes them back into the wire message
(`:3059`), warning when entries are dropped for missing signatures (`:3041`). Inbound: the
accumulated details are attached to the emitted reasoning parts and to
`providerMetadata.openrouter.reasoning_details` (`:3980-3988`).

codeaf's storage path: `processor.ts:244/252/274` writes the stream part's
`providerMetadata` onto `ReasoningPart.metadata`; `message-v2.ts:967-971` replays it
**verbatim and unmodified** as `providerMetadata` on the reasoning UI part (note: no
`providerMeta()` stripping here, unlike tool parts at `:918`).

**Go rule: `ReasoningPart.Metadata` is `map[string]json.RawMessage` and is never
re-serialised through a typed struct.** Decode-to-`any`-and-re-encode would reorder keys and
change number formatting, both of which are visible to the provider. The same rule applies
to `ToolPart.Metadata`, `TextPart.Metadata`, and `Assistant.structured`.

The one place metadata is destroyed on purpose is `differentModel` (§5.3).

### 3.5 Usage and cost

Provider-side normalisation, `computeTokenUsage` (`.../index.mjs:2560-2581`):
```
cacheRead   = usage.prompt_tokens_details?.cached_tokens ?? 0
cacheWrite  = usage.prompt_tokens_details?.cache_write_tokens ?? undefined
reasoning   = usage.completion_tokens_details?.reasoning_tokens ?? 0
inputTokens  = {total: prompt_tokens, noCache: prompt_tokens - cacheRead, cacheRead, cacheWrite}
outputTokens = {total: completion_tokens, text: completion_tokens - reasoning, reasoning}
```
Note `cacheWrite` is `?? undefined`, **not `?? 0`** (`:2565`) — the distinction survives into
`getUsage`'s nullish chain. The whole raw `usage` object (including `cost` and `is_byok`) is
carried as `raw`.

The SDK then flattens to `LanguageModelUsage` (`ai/dist/index.mjs:2424-2445`, type at
`ai/dist/index.d.ts:267-325`): `inputTokens`,
`inputTokenDetails.{noCacheTokens,cacheReadTokens,cacheWriteTokens}`, `outputTokens`,
`outputTokenDetails.{textTokens,reasoningTokens}`, `totalTokens`, `raw`, plus the deprecated
flat aliases `reasoningTokens`/`cachedInputTokens`.

**`totalTokens` is recomputed as `input + output` (`ai/dist/index.mjs:2437-2440`) —
OpenRouter's own `total_tokens` is discarded at this layer** and survives only inside
`raw` and `providerMetadata.openrouter.usage.totalTokens`. `Session.getUsage` reads
`input.usage.totalTokens` (`session.ts:385`) and stores it as `tokens.total`, so the value
codeaf records is the SDK's recomputation, not the provider's number. Reproduce the
recomputation; do not "fix" it by using the provider's total.

Likewise **OpenRouter's `usage.cost` never enters `LanguageModelV3Usage`** — it lands only in
`providerMetadata.openrouter.usage.cost` (`.../internal/index.mjs:3825-3827`), which codeaf
ignores entirely. All cost in codeaf is recomputed from the models.dev catalog.

`Session.getUsage` (`session.ts:353-414`) then:
- `reasoningTokens = outputTokenDetails.reasoningTokens ?? reasoningTokens ?? 0`
- `cacheRead = inputTokenDetails.cacheReadTokens ?? cachedInputTokens ?? 0`
- `cacheWrite = inputTokenDetails.cacheWriteTokens ?? anthropic.cacheCreationInputTokens ?? vertex.… ?? bedrock.usage.cacheWriteInputTokens ?? venice.usage.cacheCreationInputTokens ?? 0`
  (the four provider-metadata fallbacks are dead for OpenRouter — keep them, they are cheap)
- `tokens.input = inputTokens - cacheRead - cacheWrite` (`:382`, with the AI-SDK-v6 comment
  at `:379-381` explaining why cache tokens are always subtracted)
- `tokens.output = outputTokens - reasoningTokens`
- every value passes `safe()` which maps non-finite to 0 (`:354-357`)
- `costInfo = model.cost.experimentalOver200K` when `tokens.input + tokens.cache.read > 200_000`, else `model.cost` (`:396-399`)
- cost = `decimal.js` sum of `input`, `output`, `cache.read`, `cache.write`, and
  **`reasoning` billed at the `output` rate** (`:401-411`, TODO comment at `:407-409`),
  each `× rate / 1_000_000`.

**Go**: `math/big.Rat` for the five products and the sum, converted to `float64` only at the
end (`Decimal.toNumber()`). `big.Rat` is exact for decimal rates, so the only divergence
risk is the final `Rat → float64` rounding versus `decimal.js`'s. Fixture the whole function
against TS with adversarial rates (`0.0000001`, long decimals) rather than trusting either.
The catalog must be pinned for parity runs, since every rate comes from it.

Nothing in the Go repo counts tokens or multiplies tokens by price today. The price data
*does* exist — `adaptive.ModelCandidate.PromptUSDPerMtok` / `.CompletionUSDPerMtok`, parsed by
`ParseModelList` from `"id@0.325/1.95"` — but it feeds only the router's scoring term.
`leafoutcome.LeafOutcome.CostUsd`, `loopguard.LoopAction.CostUsd` and
`adaptive.Register(choice, elapsedSeconds, completionTokens, err)` all take a caller-supplied
number. **The engine layer is the producer for all three**, and the shape it must produce is
TS's `StepFinishPart`: `cost float64` +
`tokens{total?, input, output, reasoning, cache{read, write}}`.

### 3.6 The four abort layers and the `orfetch` seam

TS stacks four abort mechanisms on every OpenRouter request:

| layer | where | timers |
|---|---|---|
| 1 | codeaf's own `AbortController` in `LLM.stream`, released by the Effect scope | `llm.ts:513-517` |
| 2 | `resolveSDK`'s fetch wrapper: `AbortSignal.timeout(600_000)` combined with a chunk-abort controller via `AbortSignal.any` | `provider.ts:1529-1541`, `DEFAULT_TIMEOUT_MS = 600_000`, `DEFAULT_CHUNK_TIMEOUT_MS = 120_000` |
| 3 | `wrapSSE`'s per-read timer — aborts with `new Error("SSE read timed out")` | `provider.ts:41-87` |
| 4 | `openRouterAimdFetch`'s four timers: byte-idle 60 s, first-content 45 s (0 disables), content-idle 90 s, total-request 900 s | `openrouter-fetch.ts:127-296` |

Layer 4 is **already ported**: `internal/router/orfetch`. Its defaults are
`byteIdleDefaultMs = 60_000` (`orfetch.go:92`), `FIRST_CONTENT_DEFAULT_MS = 45_000` (`:97`),
`CONTENT_IDLE_DEFAULT_MS = 90_000` (`:101`), `totalReqDefaultMs = 15*60_000` (`:103`),
`envMsMax = 1_800_000` (`:106`), and the abort messages are produced by
`byteIdleTimeoutMessage` / `firstContentTimeoutMessage` / `contentIdleTimeoutMessage` /
`totalRequestTimeoutMessage` (`:263-278`) — all containing the substring `timeout`, which is
what `adaptive.IsLikelyTimeout` (`internal/router/adaptive/adaptive.go:656`) and
`retry.isTimeoutError` (`retry.ts:42`, `/timeout|timed out|deadline exceeded/i`) match on.

**The seam.** `orclient` builds an `*http.Request` and calls
`orfetch.OpenRouterAimdFetch(req)`. Everything else composes through `req.Context()`:

```
callerCtx  (engine/loop's per-turn context; cancelled by runner.Cancel)      ← layer 1
  └─ ctxTotal   = context.WithTimeout(callerCtx, 600s)                       ← layer 2 total
       └─ ctxChunk = context.WithCancelCause(ctxTotal)                       ← layers 2 chunk + 3
            └─ req = req.WithContext(ctxChunk)
                 └─ orfetch derives its own WithCancelCause internally       ← layer 4
```

- Layer 2's 600 s becomes `context.WithTimeout`. Its cause must be a manufactured error
  whose message matches the TS `AbortSignal.timeout` shape, or the router will classify it
  wrongly. `AbortSignal.timeout` rejects with a `TimeoutError` DOMException whose message is
  `"The operation timed out."` — **that does not contain the substring `timeout`
  case-insensitively… it does: "timed out" matches `/timed out/i`.** So `IsLikelyTimeout`
  fires. Reproduce the exact string.
- Layers 2-chunk and 3 both express "no SSE read for N ms". In TS they are separate
  (`chunkTimeout` 120 s on the fetch wrapper, `wrapSSE` re-reading with the same ms). In Go
  they collapse into a single reader-side watchdog in `orclient` that calls
  `cancelCause(errors.New("SSE read timed out"))` — the exact TS message
  (`provider.ts:51`). **[BUG-CANDIDATE]** the collapse changes the number of distinct abort
  reasons observable; log it as a KNOWN DIVERGENCE with a fixture pinning the message.
- Layer 4 needs nothing: `orfetch` reads `req.Context()` at `orfetch.go:425` and chains it
  (`:439-449`).
- `orfetch` returns the *reason* rather than `context.Canceled` (`orfetch.go:459-461`), so
  the error reaching `orclient` is already router-classifiable. Do not wrap it at all — the
  house convention is no `%w` wrapping and no `errors.Is` sentinels, because the **message
  bytes are the contract**: `adaptive.IsLikelyTimeout` lowercases and substring-matches
  `timeout` / `timed out` / `deadline exceeded`. The four manufactured messages are built by
  `byteIdleTimeoutMessage` / `firstContentTimeoutMessage` / `contentIdleTimeoutMessage` /
  `totalRequestTimeoutMessage` (`orfetch.go:263-278`) with the ms formatted through
  `jscompat.FormatNumber`.

**Three `orfetch` behaviours the streaming client must design around** (all ported
deliberately from TS):

1. **`Body.Close()` does not close upstream and does not cancel the context.** It only calls
   `clearAll()` on the watchdogs (`orfetch.go:707-720`). The pump goroutine keeps reading; a
   chunk arriving after `Close()` **re-arms** the timers that were just cleared, and only the
   subsequent failed enqueue tears the pump down. If no further chunk arrives, the pump parks
   on the upstream read forever, holding the connection. **Therefore: to abandon a stream
   early — which §2 does on `needsCompaction` — cancel the request context you passed in, not
   just `Close()` the body.** That path works and surfaces your cause.
2. **The pump is eager**, buffering into an unbuffered channel with a 32 KiB read buffer
   (`orfetch.go:554`, `:604-653`). The byte-idle timer tracks *arrival*, not consumption.
3. **The limiter slot is released when headers arrive, not when the body drains** —
   `defer limiter.Release()` at `orfetch.go:423` with the comment at `:420-422` making the
   timing explicit. Any per-stream concurrency limiting is the engine's to add.

Post-`Close()` reads return `io.EOF`, not an error (mirroring a cancelled `ReadableStream`
resolving pending reads with `{done: true}`).

**Abort surfaces as a stream part, not a throw.** `ai/dist/index.mjs:6842-6880`: on abort the
SDK enqueues `{type:"abort", reason?}` and **closes the stream cleanly**; only non-abort read
errors go through `controller.error(...)`. `processor.ts` has **no `abort` case**, so the part
falls into the `default:` log-and-ignore branch (`:596-598`) and the stream simply ends.
Interruption is then observed by `Effect.onInterrupt` (`processor.ts:705-712`), not by the
event switch. Two consequences for Go: (a) emit an `abort` part and close, do not return an
error from the stream drain; (b) the "did we abort" signal lives in the runner/context, which
is exactly where §4.3 puts it. Separately, the SDK's promise accessors (`.text`, `.steps`,
`.finishReason`) reject with `abortSignal.reason` when **zero** steps completed
(`ai:6746-6758`) — codeaf does not read those accessors, so this is informational only.

**Router registration.** `registerRoute(completionTokens, error)` must fire **exactly once**
per `pick()` (`llm.ts:139-146`), from `onFinish` or `onError`. The recon flags that an Effect
interruption can make neither fire, leaking `inflight` and permanently penalising a model via
`pressurePen`. Go gets this right for free with a `defer` — which is a **behaviour change**.
Decision needed: replicate the leak (guard the defer so it does not run on cancellation) or
fix it. Recommendation: **fix it**, log a KNOWN DIVERGENCE, because the leak is unbounded and
its effect on routing is nondeterministic (so parity runs cannot depend on it anyway).
`adaptive.Register(choice, elapsedSeconds, completionTokens, err *JSValue)`
(`internal/router/adaptive/adaptive.go:1154`) takes the error as a `*JSValue`, so `orclient`
must build one via `adaptive.Err(msg)` / `adaptive.ErrWithStatus(msg, status)` (`jsvalue.go:74-83`).

**`router.Pick` can block 5 minutes then error** (`adaptive.go:1091`). In TS that throw
becomes an Effect *defect*. In Go it is a plain `error` — decide explicitly whether the turn
fails or degrades. TS semantics (a defect propagating out of `LLM.run`) = the turn fails and
`halt` records an `UnknownError`. Match that.

---

## 4. The session-runner replacement

### 4.1 The 4 states

`runner.ts:32-36`:
```ts
type State<A,E> =
  | { _tag: "Idle" }
  | { _tag: "Running";      run: RunHandle }
  | { _tag: "Shell";        shell: ShellHandle }
  | { _tag: "ShellThenRun"; shell: ShellHandle; run: PendingHandle }
```

Go:

```
type state uint8
const (stIdle state = iota; stRunning; stShell; stShellThenRun)

type Runner struct {
    mu    sync.Mutex          // replaces SynchronizedRef's serialized modifyEffect
    st    state
    ids   int                 // runner.ts:51 `let ids = 0`, incremented by next()
    run   *runHandle          // Running / ShellThenRun (pending form)
    shell *shellHandle
    onIdle, onBusy func()
    onInterrupt    func() (WithParts, error)
    onBusyPanic    func()     // run-state.ts:63-65 → panic(BusyError)
}

type runHandle struct {
    id     int
    cancel context.CancelCauseFunc
    done   chan struct{}      // closed once
    val    WithParts
    err    error              // nil | fault.Interrupt | real
    work   func(context.Context) (WithParts, error) // only set while pending
}

type shellHandle struct {
    id        int
    cancel    context.CancelCauseFunc
    cancelled atomic.Bool      // the `cancelled: Deferred<void>` at runner.ts:21
    ready     *Latch           // optional, runner.ts:110
    done      chan struct{}
    val       WithParts
    err       error
}
```

`SynchronizedRef.modifyEffect` serialises the *effectful* transition, so the mutex must be
held across "read state → decide → start the goroutine → write new state". It must **not**
be held while awaiting the result — TS returns the awaiting effect *from* `modify` and runs
it after the ref update (`runner.ts:138`, `.pipe(Effect.flatten)`). So: take the lock,
mutate, capture a `<-chan struct{}`, release, then wait.

### 4.2 Transitions

`ensureRunning(work)` (`runner.ts:115-138`):

| state | action | result |
|---|---|---|
| `Running`, `ShellThenRun` | none | await the existing `run.done` (`:122`) — **the caller's `work` is discarded** |
| `Shell` | create a `PendingHandle{id: next(), done, work}`, go to `ShellThenRun` | await the pending done (`:129`) |
| `Idle` | `startRun(work, done)`, go to `Running` | await (`:134`) |

`startShell(work, ready)` (`runner.ts:140-174`):
- **non-Idle → `opts.busy()` then `throw new Error("Runner is busy")`** (`:145-150`). In
  codeaf `opts.busy` itself throws `Session.BusyError` (`run-state.ts:63-65`), so the second
  throw is unreachable. Go: `onBusyPanic()` panics with `*BusyError`; keep the unreachable
  fallback as a `panic(errors.New("Runner is busy"))` so the shape matches.
- Idle → run `onBusy`, fork with `Effect.ensuring(finishShell(id))`, go to `Shell`.

`finishShell(id)` (`runner.ts:93-106`) is the promotion point: if the state is `Shell` with
the matching id → `Idle` + `onIdle`; if `ShellThenRun` with the matching id → **start the
pending run** and go to `Running`; else no change.

`cancel` (`runner.ts:176-207`):
- `Idle` → nothing.
- `Running` → interrupt the fiber, await the done (swallowing its exit), `idleIfCurrent()`; state → `Idle`.
- `Shell` → `stopShell`, `idleIfCurrent()`; state → `Idle`.
- `ShellThenRun` → `stopShell`, **fail the pending run's deferred with `Cancelled`**, `idleIfCurrent()`; state → `Idle`.

`stopShell` (`runner.ts:108-113`): if `ready` is present, await it (ignoring its exit) —
this is a *rendezvous* so the shell has reached a safe point; then mark `cancelled`; then
interrupt. Go: `<-ready.C` with the exit ignored, then `cancelled.Store(true)`, then
`cancel(errCancelled)`, then `<-shell.done`.

`idleIfCurrent` (`runner.ts:67-68`) runs `onIdle` **only if the state is already `Idle`** —
a compare-and-run guard against a concurrent transition. Preserve it literally.

### 4.3 Interrupt-vs-failure discrimination

This is the single hardest construct in the port (PORT-ASSESSMENT §2b). Effect distinguishes
three cause kinds; Go has one `error`. `internal/engine/fault` supplies the trichotomy:

```
KindFailure   — an ordinary typed error
KindInterrupt — cancellation initiated by runner.Cancel / an outer context
KindDefect    — a recovered panic (Effect `die`)
```

- Every goroutine started by the runner wraps its body in `defer recover()` and converts a
  panic into `fault.Defect(v)`.
- Cancellation uses `context.WithCancelCause` with the sentinel `fault.ErrInterrupted`, so
  `fault.IsInterruptOnly(err)` is `errors.Is(err, fault.ErrInterrupted) && !HasDefects(err)`.
- Errors returned by work that merely *observed* a cancelled context (e.g. an HTTP error
  wrapping `context.Canceled`) must be normalised at the boundary, or a real failure that
  happens to mention `context.Canceled` will be misclassified. The boundary is `orclient`:
  it converts `orfetch` timeouts into `KindFailure` (they are *not* interrupts — they must be
  retried) and caller cancellation into `KindInterrupt`.

Three consumers:

1. **`complete()`** (`runner.ts:59-62`): `Exit.isFailure(exit) && Cause.hasInterruptsOnly(cause)`
   → resolve the deferred with `Cancelled`; otherwise pass the exit through.
   Go: `if fault.IsInterruptOnly(err) { h.err = ErrCancelled } else { h.err = err }`.
2. **`awaitDone()`** (`runner.ts:64-65`): on `Cancelled`, run `onInterrupt` if provided,
   else `Effect.die`. In codeaf `onInterrupt` is always
   `lastAssistant(sessionID)` (`prompt.ts:1869`, `:1875`), i.e. **a cancelled turn returns the
   last assistant message instead of erroring**. Preserve exactly.
3. **The shell await path** (`runner.ts:159-170`), the subtlest:
   ```ts
   if (Exit.isSuccess(exit)) return exit.value
   if (Cause.hasInterruptsOnly(exit.cause) ||
       ((yield* Deferred.isDone(cancelled)) && Cause.hasInterrupts(exit.cause) && !Cause.hasDies(exit.cause)))
       → onInterrupt ?? die(Cancelled)
   return Effect.failCause(exit.cause)
   ```
   The second disjunct is "we asked it to stop, it did stop partly by interruption, and it
   did not panic" — a *mixed* cause counts as cancellation only if `cancel` was called.
   Go: `if fault.IsInterruptOnly(err) || (sh.cancelled.Load() && fault.HasInterrupts(err) && !fault.HasDefects(err))`.

`processor.ts:713-716` is the fourth site and the one that keeps aborts out of the retry
schedule:
```ts
Effect.catchCauseIf(cause => !Cause.hasInterruptsOnly(cause), cause => Effect.fail(Cause.squash(cause)))
```
Only non-interrupt-only causes become typed failures; `Effect.retry` retries failures only,
so interrupts bypass the schedule entirely. Go equivalent, placed identically between the
stream drain and the retry loop:
```
if err != nil && !fault.IsInterruptOnly(err) { err = fault.Squash(err) /* retryable */ }
else { return err /* propagate, do not retry */ }
```

### 4.4 `BusyError` and the registry

`Session.BusyError` (`session.ts:416-420`) is `Error` with a `sessionID` field and message
`` `Session ${sessionID} is busy` ``. Go: `type BusyError struct{ SessionID string }` with
`Error() string { return "Session " + e.SessionID + " is busy" }`.

`run-state.ts` is a per-instance `Map<SessionID, Runner>` (`:35`) with:
- `runner(sessionID, onInterrupt)` (`:49-69`) — lazily creates, wiring `onIdle` to delete
  the map entry **and** set status `idle`, `onBusy` to set status `busy`, and `busy` to
  `throw new BusyError`. **The map entry is deleted on idle**, so the next call gets a fresh
  runner with `ids` reset to 0. Preserve — id monotonicity is per-runner, not per-session.
- `assertNotBusy` (`:71-75`) — throws `BusyError` if an existing runner is busy.
- `cancel` (`:77-85`) — if no runner or not busy, just set status `idle` and return.
- A scope finalizer cancels every runner concurrently and clears the map (`:36-44`). Go:
  a `Close(ctx)` on the registry that `errgroup`s over the runners.

Go registry: `sync.Map` is wrong here (the delete-on-idle callback runs under the runner's
own lock); use `struct{ mu sync.Mutex; m map[SessionID]*Runner }` and take care that
`onIdle` does not deadlock — it is invoked from inside `Runner.mu`, so the registry lock
must always be acquired *after* the runner lock, never before. Document the lock order.

### 4.5 `degrade(err)` — the shared collapse helper

The TS idiom is `effect.pipe(Effect.timeout(d), Effect.catchCause(() => fallback))`. The
comment at `plandb-scheduler.ts:2119-2121` states the contract precisely: *"`Effect.timeout`
itself wraps in a Cause when the deadline trips, so a single `catchCause` swallows both
failures and timeouts. Defects also flow through cause-handling, so this is a complete
catch-all."*

Actual counts at `3b25a1a` (non-test files):

| file | `catchCause` | `Effect.timeout` |
|---|---|---|
| `src/cli/cmd/run.ts` | 29 | 2 |
| `src/session/plandb-scheduler.ts` | 22 | 2 |
| `src/session/auditor-gate.ts` | 12 | 3 |
| `src/session/prompt.ts` | 10 | 0 |
| `src/session/agent-json.ts` | 7 | 3 |
| `src/cli/cmd/arch.ts` | 5 | 1 |
| `src/session/review-gate.ts` | 4 | 2 |
| `src/session/pr-ready-phase.ts` | 4 | 2 |
| `src/tool/task.ts` | 3 | 0 |
| `src/session/issue-writer-phase.ts` | 3 | 1 |
| `src/session/product-gate.ts` | 2 | 1 |
| `src/session/processor.ts` | 2 | 1 |
| `src/session/architecture-gate.ts` | 2 | 1 |
| `src/file/watcher.ts` | 2 | 1 |
| `src/file/index.ts` | 2 | 0 |
| 6 others, 1 each | 6 | 0 |
| **total** | **115** | **27** (+3 in `cross-spawn-spawner.ts`, +1 each in `webfetch.ts`, `mcp-websearch.ts`, `instruction.ts`, `models.ts`) |

Of these, **15 are inside the engine boundary** (`processor.ts` 2, `prompt.ts` 10,
`session.ts` 1, `compaction.ts` 1, `observer.ts` 1); the rest belong to the orchestration
and gate layers but use the identical helper.

Three canonical shapes, all collapsible:

```ts
// (a) timeout + fallback value        — review-gate.ts:802-811, 1087-1096
effect.pipe(Effect.timeout(MS), Effect.catchCause(cause => Effect.succeed({__failed:true, ...})))
// (b) timeout + void                  — plandb-scheduler.ts:2126-2128
effect.pipe(Effect.timeout("3 seconds"), Effect.catchCause(() => Effect.void))
// (c) no timeout, fallback value      — prompt.ts:1500, 1527, 1550, 1554, 1636-1638, 1647
effect.pipe(Effect.catchCause(() => Effect.succeed(fallback)))
```

Proposed API in `internal/engine/degrade`:

```
// Value runs fn with an optional deadline and collapses timeout, failure,
// defect (panic) and interruption into `fallback`. `d <= 0` disables the deadline.
func Value[T any](ctx context.Context, d time.Duration, fallback T, fn func(context.Context) (T, error)) T

// Void is the T=struct{} specialisation.
func Void(ctx context.Context, d time.Duration, fn func(context.Context) error)

// Log is Value with the TS log line preserved (several sites log Cause.pretty(cause).slice(0,300)).
func Log[T any](ctx context.Context, d time.Duration, fallback T, msg string, fn func(context.Context) (T, error)) T
```

Two properties that must be replicated even though they are uncomfortable:

- **It swallows interruption too.** `Effect.catchCause` is a complete catch-all, so an
  outer cancellation inside a degrade site produces the fallback and the caller keeps going.
  Go must do the same (do not special-case `context.Canceled`), or shutdown behaviour will
  diverge. **[BUG-CANDIDATE]** — flag it; it is the most likely place a Go port would
  "helpfully" differ.
- **It swallows panics.** `recover()` inside `Value` is mandatory; Effect defects flow
  through `catchCause`.

The `Cause.pretty(cause).slice(0, 300)` truncation appears at
`prompt.ts:1528`, `agent-json.ts:531`, `review-gate.ts:808`, `:1093` — the sliced string is
written into gate results (`review-gate.ts:806-809` puts it in `result.error`), so it is
**model-visible**, not log-only. Those sites need a `fault.Pretty(err)` that produces a
stable string and a rune-safe 300 slice. Fixture them.

---

## 5. Message model

### 5.1 Structs

`Info = User | Assistant`, discriminated on `role` (`message-v2.ts:588-592`); shared base
`{id, sessionID}` (`:373-376`). `Part` is a 12-variant union on `type` (`:405-434`); shared
base `{id, sessionID, messageID}` (`:86-90`).

```
User (message-v2.ts:378-403)
  ID, SessionID string
  Role          "user"
  Time          struct{ Created uint64 }
  Format        *OutputFormat                 // optional, :384
  Summary       *struct{Title,Body *string; Diffs []FileDiff}  // optional, :385-391
  Agent         string
  Model         struct{ ProviderID, ModelID string; Variant *string }  // :393-397
  System        *string
  Tools         map[string]bool               // optional

Assistant (message-v2.ts:546-586)
  ID, SessionID string
  Role          "assistant"
  Time          struct{ Created uint64; Completed *uint64 }
  Error         *AssistantError               // optional, :553
  ParentID      string
  ModelID, ProviderID string
  Mode          string                        // deprecated, :557-560, still required
  Agent         string
  Path          struct{ Cwd, Root string }
  Summary       *bool
  Cost          float64                       // Schema.Finite
  Tokens        struct{Total *uint64; Input, Output, Reasoning uint64; Cache struct{Read, Write uint64}}
  Structured    json.RawMessage               // opaque, :578
  Variant       *string
  Finish        *string                       // :580 — see F2
```

Parts:

| type | fields | line |
|---|---|---|
| `text` | `Text string`, `Synthetic *bool`, `Ignored *bool`, `Time *{Start uint64; End *uint64}`, `Metadata map[string]json.RawMessage` | `:111-127` |
| `reasoning` | `Text string`, `Metadata map[string]json.RawMessage`, `Time {Start uint64; End *uint64}` (**required**, unlike text) | `:129-141` |
| `tool` | `CallID, Tool string`, `State ToolState`, `Metadata map[string]json.RawMessage` | `:359-371` |
| `step-start` | `Snapshot *string` | `:257-264` |
| `step-finish` | `Reason string`, `Snapshot *string`, `Cost float64`, `Tokens {…}` | `:266-285` |
| `file` | `Mime string`, `Filename *string`, `URL string`, `Source *FilePartSource` | `:185-195` |
| `patch` | `Hash string`, `Files []string` | `:101-109` |
| `snapshot` | `Snapshot string` | `:92-99` |
| `agent` | `Name string`, `Source *{Value string; Start, End uint64}` | `:197-211` |
| `subtask` | `Prompt, Description, Agent string`, `Model *{ProviderID, ModelID}`, `Command *string` | `:224-240` |
| `retry` | `Attempt uint64`, `Error APIError` (full struct), `Time {Created uint64}` | `:242-255` |
| `compaction` | `Auto bool`, `Overflow *bool`, `TailStartID *string` (JSON key `tail_start_id`) | `:213-222` |

`ToolState` is a real sum type on `status` (`:346-349`) — the field sets differ, and
`pending` has **no `time` at all**:

| status | fields | line |
|---|---|---|
| `pending` | `Input map[string]any`, `Raw string` | `:287-294` |
| `running` | `Input`, `Title *string`, `Metadata map[string]json.RawMessage`, `Time {Start uint64}` | `:296-307` |
| `completed` | `Input`, `Output string`, `Title string` (**required**), `Metadata` (**required, non-optional**), `Time {Start, End uint64; Compacted *uint64}`, `Attachments []FilePart` | `:309-324` |
| `error` | `Input`, `Error string`, `Metadata *…`, `Time {Start, End uint64}` | `:332-344` |

`AssistantError` union on `name` (`:462-472`): `ProviderAuthError{providerID,message}`,
`UnknownError{message}`, `MessageOutputLengthError{}`, `MessageAbortedError{message}`,
`StructuredOutputError{message,retries}`, `ContextOverflowError{message,responseBody?}`,
`APIError{message,statusCode?,isRetryable,responseHeaders?,responseBody?,metadata?}`.

**JSON encoding rules** (per PORT-ASSESSMENT §3): declare struct fields in TS literal order;
`SetEscapeHTML(false)`; **no `omitempty`** — TS emits explicit `null`s for present-but-null
optionals. Use `internal/jscompat.Stringify`. Numeric fields that can be non-finite use
`jscompat.JSNumber`.

### 5.2 Fields that are opaque JSON

Round-trip verbatim as `json.RawMessage`, never decoded into a typed struct and re-encoded:

- `TextPart.metadata`, `ReasoningPart.metadata`, `ToolPart.metadata`,
  `ToolStateRunning/Completed/Error.metadata` — these carry
  `providerMetadata` from the stream (`processor.ts:244, 252, 274, 545, 553, 588`,
  `:339-351`), which for OpenRouter includes `openrouter.reasoning_details` (see §3.4) and
  for Anthropic includes `signature`/`redactedData`.
- `Assistant.structured` (`:578`) — the `StructuredOutput` tool's payload.
- `ToolState*.input` — `Record<String, Any>`; also the doom-loop key (§5.5).
- `ToolStateCompleted.attachments[].source` — nested `FilePartSource` union.
- `APIError.responseBody` — a string, but matched by substring
  (`retry.ts:130`, `:143`) so it must be byte-preserved.

`ToolPart.metadata` has one *read* path: `providerExecuted` (`processor.ts:298, 334, 348-350`,
`prompt.ts:1613`, `message-v2.ts:917`). Model it as `map[string]json.RawMessage` plus a
helper `ProviderExecuted() bool` that decodes just that key.

### 5.3 `differentModel`

`message-v2.ts:841`:
```ts
const differentModel = `${model.providerID}/${model.id}` !== `${msg.info.providerID}/${msg.info.modelID}`
```
Plain string comparison of `"<providerID>/<modelID>"` between the model *about to be called*
and the model that produced the historical turn. **With the adaptive router rotating over a
5-model HIGH pool this fires on most historical messages of most turns** — it is a hot path,
not an edge case.

When true, five sites omit metadata and one downgrades:

| site | effect | line |
|---|---|---|
| text part | omit `providerMetadata: part.metadata` | `:879` |
| completed tool | omit `callProviderMetadata: providerMeta(part.metadata)` | `:918` |
| error tool with interrupted string output | omit `callProviderMetadata` | `:931` |
| error tool | omit `callProviderMetadata` | `:941` |
| pending/running tool | omit `callProviderMetadata` | `:955` |
| **reasoning** | if `text.trim() != ""` push `{type:"text", text}` **else drop the part entirely**; `continue` | `:958-966` |

`providerMeta` (`:723-727`): nil in → nil out; else strip the `providerExecuted` key and
return the rest, or nil if nothing remains. Note reasoning replay at `:967-971` does **not**
call `providerMeta` — it passes `part.metadata` straight through.

### 5.4 The conversion pipeline

`toModelMessagesEffect` (`:729-1011`) builds AI-SDK `UIMessage[]` and then calls
`convertToModelMessages`. Both halves must be ported.

**Half 1 — `WithParts[] → UIMessage[]`:**
- skip messages with zero parts (`:792`)
- **user** (`:794-838`): keep non-ignored non-empty `text`; keep `file` unless mime is
  `text/plain` or `application/x-directory` (and if `stripMedia && isMedia(mime)`, replace
  with `[Attached <mime>: <filename|file>]`); `compaction` → `"What did we do so far?"`;
  `subtask` → `"The following tool was executed by the user"`; **everything else silently
  dropped**. Push only if non-empty (`:837`).
- **assistant** (`:840-997`): the error-drop predicate (`:844-852`) — drop the whole message
  if it has any error, **unless** the error is `MessageAbortedError` **and** the message has
  at least one part that is neither `step-start` nor `reasoning`. Then per part: text (with
  the empty→space rule, `:875`), `step-start`, tool (four state branches, `:886-957`),
  reasoning (`:958-972`). Push only if non-empty, then emit the synthetic attachment message
  if `media` is non-empty (`:974-996`).
- **tools map** (`:1000`): `{toolName: {toModelOutput}}` for every tool name seen, regardless
  of state, with the same closure.
- **final filter** (`:1004`): drop any UIMessage whose parts are *all* `step-start`.

`toModelOutput` (`:757-789`): string → `{type:"text", value}`; object → `{type:"content",
value:[ …text?, …media ]}` where attachments are filtered to
`url.startsWith("data:") && url.includes(",")` and `data` is the substring after the **first**
comma (whole url if none); otherwise `{type:"json", value}`.

`truncateToolOutput` (`:326-330`): if `!maxChars || len <= maxChars` return as-is; else
`text[:maxChars] + "\n[Tool output truncated for compaction: omitted <n> chars]"`. **`length`
is UTF-16 code units in JS** — use a UTF-16 length and slice, not `len(s)` or `[]rune`.

**Half 2 — `convertToModelMessages`** (`ai/dist/index.mjs:8311-8551`). The algorithm codeaf
depends on:
- **user** (`:8339-8368`): one `ModelMessage{role:"user"}` whose content maps text→`{type:"text"}`
  and file→`{type:"file", mediaType, filename, data: url}`.
- **assistant** (`:8369-8542`): parts accumulate into a `block`; **`step-start` flushes the
  block** (`:8529-8535`). Each flush emits one `{role:"assistant", content:[…]}` and then, if
  the block contained any tool part that is not `providerExecuted`, one `{role:"tool",
  content:[tool-result…]}` immediately after (`:8452-8515`). So a multi-step assistant
  message expands into alternating assistant/tool `ModelMessage`s — this is why codeaf keeps
  `step-start` parts in the UIMessage but filters messages that are *only* step-starts.
- tool-call content is `{type:"tool-call", toolCallId, toolName, input, providerExecuted, providerOptions?}`
  where `input` for an `output-error` part falls back to `rawInput` (`:8404`).
- tool-result output goes through `createToolModelOutput` (`ai/dist/index.mjs:1655-1671`):
  `errorMode "text"` → `{type:"error-text", value: getErrorMessage(output)}`;
  `errorMode "json"` → `{type:"error-json", value}`; else the tool's `toModelOutput`;
  else string→text / other→json. Note the **errorMode differs by path**: `"json"` for
  provider-executed results (`:8425`), `"text"` for the tool-role message (`:8501`).
- reasoning content is `{type:"reasoning", text, providerOptions: part.providerMetadata}`
  (`:8395-8400`) — `providerOptions` is set **unconditionally**, including to `undefined`
  (contrast with text/file, where the key is conditional).
- `input-streaming` tool parts emit **nothing** (`:8403`).
- `getToolName` (`:5257-5262`): dynamic parts use `part.toolName`; static parts use
  `part.type.split("-").slice(1).join("-")` — strips the `tool-` prefix and **preserves
  internal dashes**, which matters because codeaf builds the type as `"tool-" + part.tool`
  (`message-v2.ts:912`) and tool names contain dashes.
- The `role:"tool"` message is pushed **only if its content is non-empty** (`:8509-8514`). A
  block whose tool parts are all `input-available`/`approval-requested` yields an assistant
  message carrying a `tool-call` and **no matching tool message** — a dangling `tool_use`,
  which is exactly what `message-v2.ts:947-956` exists to prevent upstream.
- Provider-executed tool results stay **inside the assistant message** (`:8419-8434`) with
  `errorMode: "json"`, while the tool-role message uses `errorMode: "text"` (`:8501`).
- `system` messages: non-text parts are **silently filtered, no error** (`:8323-8339`); the
  old "system message parts must be text parts" `MessageConversionError` is gone in v6. Text
  is joined with `""` and `providerMetadata` from all text parts is shallow-merged one level.
- User parts that match no branch (reasoning, tool-*, source-*, step-start) become `undefined`
  and are dropped by `.filter(isNonNullable)` (`:8367`) — silently.
- `source-url` / `source-document` parts on an assistant message match no branch either: they
  are ignored **and do not break the block** (`:8519-8526`).
- The only error raised is `MessageConversionError` for an unsupported `role` (`:8531-8537`).

Go: a straight port. Order of content parts within a block follows UIMessage part order.

### 5.5 Doom-loop key

`processor.ts:353-378`: take the last `DOOM_LOOP_THRESHOLD = 3` parts of the *current
assistant message* (`MessageV2.parts(id)`, `:353-354`); trigger only if there are exactly 3
and **all** are `tool` parts with the same `toolName`, `state.status !== "pending"`, and
`JSON.stringify(part.state.input) === JSON.stringify(value.input)` (`:356-364`). Then
`permission.ask({permission:"doom_loop", patterns:[toolName], …})` (`:370-377`).

The trigger is **stringify equality**, so key order in the input object is load-bearing —
`jscompat.Stringify` over the stored `json.RawMessage` preserves it only if the raw bytes are
preserved. Another reason `input` must not round-trip through `map[string]any`.

`internal/session/loopguard` already exists but is a different construct (budget-based
`LoopGuard`); the doom-loop check belongs in `engine/processor`.

---

## 6. The retry schedule

### 6.1 Constants (`retry.ts:8-42`)

```
RETRY_INITIAL_DELAY        = 2000
RETRY_BACKOFF_FACTOR       = 2
RETRY_MAX_DELAY_NO_HEADERS = 30_000
RETRY_MAX_DELAY            = 2_147_483_647
TIMEOUT_RETRY_DELAY_DEFAULT= 1_000
TIMEOUT_MESSAGE_RE         = /timeout|timed out|deadline exceeded/i
GO_UPSELL_MESSAGE          = "Free usage exceeded, subscribe to Go"
GO_UPSELL_URL              = "https://codeaf.local/go"
```
Env: `CODEAF_TIMEOUT_RETRY_DELAY_MS` (`:45`). `timeoutRetryDelayMs()` (`:44-50`): unset or
`""` → default; `Number(raw)`; non-finite or `< 1` or `> 1_800_000` → default. Note `""` is
short-circuited *before* `Number("")` would yield 0. `cap(ms) = min(ms, RETRY_MAX_DELAY)`.

### 6.2 Attempt numbering

**1-indexed.** `Schedule.fromStepWithMetadata` (`retry.ts:235`) wraps a metadata function
whose closure is `let n = 0; … attempt: ++n` (`node_modules/effect/src/Schedule.ts:358-369`,
`:395-405`). The **first** failure is `attempt == 1`, so the base delay is
`2000 * 2^0 = 2000`. Go: an explicit `attempt` counter starting at 0 and pre-incremented.

### 6.3 `delay(attempt, error?, isTimeout=false)` (`retry.ts:68-108`)

1. `isTimeout || (error && TIMEOUT_MESSAGE_RE.test(error.data.message))` → `cap(timeoutRetryDelayMs())`
   — constant ~1000 ms, **`attempt` is ignored** (`:75-77`).
2. `error != nil && error.data.responseHeaders != nil` (`:78-80`):
   - `retry-after-ms` → `ParseFloat`; not-NaN → `cap(v)` **raw ms, no rounding** (`:81-87`)
   - `retry-after` → `ParseFloat`; not-NaN → `cap(ceil(v*1000))` (`:89-95`)
   - `retry-after` as HTTP date → `Date.parse(v) - Date.now()`; not-NaN **and > 0** →
     `cap(ceil(delta))` (`:96-100`)
   - else → `cap(2000 * 2^(attempt-1))` — **capped only at `RETRY_MAX_DELAY` (~24.8 days),
     not at 30 s** (`:103`). This asymmetry with branch 3 is deliberate-looking but
     unbounded in practice. **[BUG-CANDIDATE]**
3. otherwise → `cap(min(2000 * 2^(attempt-1), 30_000))` (`:107`).

Go notes: `Number.parseFloat` accepts leading whitespace and a numeric prefix
(`"3abc"` → 3); `strconv.ParseFloat` does not. Use `jscompat.ToNumber`-adjacent semantics or
a dedicated `jsParseFloat`. `Date.parse` on an HTTP-date must match V8's lenient parser for
the RFC 1123/850/asctime forms — `http.ParseTime` covers the three legal forms; anything
else V8 might accept is a divergence to fixture.

### 6.4 `retryable(error, provider)` (`retry.ts:110-206`)

Ordered; `nil` means **stop retrying**.

1. `ContextOverflowError` → **nil** (`:112`) — never retried.
2. `isTimeoutError(error)` → `{message: "Provider stalled; failing over to another model"}` (`:117`).
   Deliberately ahead of the APIError branch so a gateway-timeout 5xx takes the 1 s
   fail-over delay rather than 5xx backoff (comment `:113-116`).
   `isTimeoutError` (`:58-62`): `error.name == "AbortError"` **or**
   `TIMEOUT_MESSAGE_RE.test(error.data.message)`.
3. `error.name == "ProviderModelNotFoundError"` → `{message: "Provider model catalog race; retrying"}` (`:122-124`).
4. `APIError` (`:125-177`):
   - `!isRetryable && !(statusCode != nil && statusCode >= 500)` → **nil** (`:129`).
     So: SDK-retryable errors retry, **and any 5xx retries regardless of the flag**.
   - `responseBody` contains `"FreeUsageLimitError"` → upsell action (`:130-142`).
   - `responseBody` contains `"GoUsageLimitError"` → parse body, format `resetIn`
     (days+hours / hours+minutes / minutes / `"less than a minute"`, pluralised by `unit()`,
     `:148-159`), build message + link (`:161-163`), return with
     `reason: "account_rate_limit"` (`:164-174`).
   - else `{message: msg.includes("Overloaded") ? "Provider is overloaded" : msg}` (`:176`,
     case-sensitive).
5. plain-text rate-limit match on `message.toLowerCase()` containing
   `"rate increased too quickly"` / `"rate limit"` / `"too many requests"` → `{message: originalMsg}`
   (the **original**, not lowercased) (`:180-190`).
6. `parseJSON(message)`; not an object → **nil** (`:192-193`).
7. `type=="error" && error.type=="too_many_requests"` → `"Too Many Requests"` (`:196-198`).
8. `code.includes("exhausted") || code.includes("unavailable")` → `"Provider is overloaded"` (`:199-201`).
9. `type=="error" && typeof error.code=="string" && error.code.includes("rate_limit")` → `"Rate Limited"` (`:202-204`).
10. **nil** (`:205`).

`isOpenAiErrorRetryable` (`error.ts:30-35`) — 404 counts as retryable — is gated on
`providerID.startsWith("openai")` (`error.ts:197`) and therefore **dead for OpenRouter**.
Port it; keep it dead.

### 6.5 What is *not* retryable, and the missing cap

**The SDK's own retry is disabled.** `maxRetries: input.retries ?? 0` (`llm.ts:480`) against
an SDK default of 2 (`ai/dist/index.mjs:2580`, `:2665`). So `retry.ts` is the *only* retry in
the system, and the Go client must not add an `http.Client`-level retry of its own. For
reference, the SDK's predicate is
`APICallError.isInstance(err) && err.isRetryable === true && tryNumber <= maxRetries`
(`ai:2615`), aborts are rethrown without retry (`ai:2599`), and `isRetryable` defaults to
`statusCode ∈ {408, 409, 429} || statusCode >= 500`
(`@ai-sdk/provider/dist/index.mjs:52-55`) — that default is what `retry.ts:129` reads.

**Aborts never enter the schedule.** The mechanism is at the call site
(`processor.ts:713-716`, §4.3), not in `retry.ts`. Second line of defence: an
`AbortedError` would fall out `nil` anyway — its `name` is `"MessageAbortedError"`
(`message-v2.ts:42`), not `"AbortError"`, its message `"Aborted"` matches no timeout or
rate-limit pattern, and it is not an `APIError`.

**There is no attempt cap and no time budget.** The only terminator in `policy()` is
`Cause.done(meta.attempt)` when `retryable()` returns `undefined` (`retry.ts:239`). Combined
with branch 2d of `delay`, an error carrying `responseHeaders` but no usable `retry-after`
backs off exponentially with a ~24.8-day ceiling, forever. **[BUG-CANDIDATE] — flag it, port
it, and put a kill-switch env var behind a KNOWN DIVERGENCE if operations require one.**

`policy()` also calls `set({attempt, message, action, next})` on every retry (`:247-252`)
where `next = Clock.currentTimeMillis + wait` (absolute epoch ms). The processor's
implementation (`processor.ts:721-741`) emits a v2 `Retried` event and sets session status
`{type:"retry", attempt, message, action, next}`. `action` is
`{reason, provider, title, message, label, link?}` with `reason` an open string enum
(`retry.ts:10, 14-21`).

### 6.6 Go shape

Not a `Schedule`; a plain loop, placed exactly where `Effect.retry` sits in the pipeline
(`processor.ts:717-742`), i.e. **outside** `onInterrupt` and the interrupt filter, **inside**
`catch(halt)` and `ensuring(cleanup)`:

```
attempt := 0
for {
    err := runStreamOnce(ctx)                       // processor.ts:695-704
    if err == nil { break }
    if fault.IsInterruptOnly(err) { onInterrupt(); return err }   // :705-716
    attempt++
    parsed := message.FromError(err, providerID, aborted)
    r := retry.Retryable(parsed, providerID)
    if r == nil { halt(parsed); break }              // :743
    wait := retry.Delay(attempt, apiErrOrNil(parsed), retry.IsTimeoutError(parsed))
    setStatus(retry{attempt, r.Message, r.Action, now()+wait})
    select { case <-time.After(wait): case <-ctx.Done(): return ctx.Err() }
}
cleanup()                                            // :744, always
```

Note `cleanup()` runs once, after the whole retry loop (TS: `Effect.ensuring` wraps the
retried effect), **not** per attempt.

---

## 7. Test strategy

Three tiers, matching what each layer actually is.

### 7.1 Fixture-driven from TS (deterministic, no network)

Follow F5: `tools/fixtures/gen-<pkg>.ts` runs under `bun` from the swe-pro repo root, imports
the **real** TS module by absolute path, pins `Math.random` to mulberry32 and `Date.now` to a
constant, and emits JSONL — one `{name, fn, args_json, out_json}` per line into
`internal/<path>/<pkg>/testdata/fixtures.json`. `fixtures_test.go` scans it with a
`bufio.Scanner` (1–16 MiB buffer), dispatches on `fn`, and asserts **byte equality** of
`jscompat.Stringify(goOut)` against `out_json` — not a tolerance compare. Each test ends with
a floor assertion (`if cases < 30 { t.Fatalf(...) }`) so a truncated corpus fails loudly.
Helper names are consistent across the 31 existing generators: `loadFixtures(t)`,
`stringify(t, v)`, `decodeArgs(t, argsJSON, into)`, `replay(...)`. Tests live **in-package**
(`package orclient`, not `_test`), split into a verbatim translation of the TS `bun:test`
suite (subtest names included) plus Go-only `TestWire*` / `TestKeptTSQuirks`.

Three `jscompat` gaps to plan around before writing the first fixture:
`OrderedMap` has **no `MarshalJSON`** (passing one to `Stringify` yields `{}`) and no
`Range`/`Iterate` — the serialising insertion-ordered map in this repo is `knobs.Record[V]`;
`Stringify` does **not** preserve insertion order for `map[string]any` (Go sorts map keys), so
key order comes from *declaring struct fields in TS object-literal order*; and there is **no
stable-sort helper** — callers use `sort.SliceStable` directly.

| generator | covers | why it works |
|---|---|---|
| `gen-transform.ts` | `sanitizeSurrogates`, `unsupportedParts`, `normalizeMessages`, `options`, `providerOptions`, `temperature/topP/topK/maxOutputTokens`, `schema` | pure functions of `(msgs, model, options)` |
| `gen-messagev2.ts` | `toModelMessagesEffect` end-to-end, `fromError`, `providerMeta`, `truncateToolOutput` | pure given a fabricated `Provider.Model`; `MessageID.ascending()` must be stubbed for the synthetic-attachment message |
| `gen-convertmodelmessages.ts` | `convertToModelMessages` alone, driven from `ai@6.0.168` | isolates the `step-start`-splits-blocks rule from codeaf's half |
| `gen-retry.ts` | `delay` × (attempt, headers, timeout flag), `retryable` over a corpus of error objects, `isTimeoutError` | pure; `Date.now()` pinned for the HTTP-date branch |
| `gen-usage.ts` | `Session.getUsage` over usage/metadata/cost matrices incl. `experimentalOver200K` | pure; the decimal-precision oracle |
| `gen-llmerr.ts` | `parseAPICallError`, `parseStreamError`, the 19 overflow regexes, the `^4(00\|13) (no body)` test | pure |
| `gen-loopexit.ts` | the §2.2 truth table + `hasToolCalls` + `planDBInfoFromMessages` | extract the predicate into a testable TS shim; the four inputs are enumerable |
| `gen-orstream.ts` | OpenRouter chunk → stream-part translation, driven by feeding recorded chunk arrays through the provider's `doStream` transform | the transform is a pure `TransformStream`; drive it with a fake `ReadableStream` |

`gen-orstream.ts` is the highest-value one: it pins the tool-call accumulator
(`index ?? length-1`, id minting on collision, the `isParsableJson` early burst, the
double-emit bug), the `!textStarted` reasoning gate, the two synthetic finish-reason
promotions, and the flush behaviour — all without a network. Add
`gen-deterministic-stringify.ts` as its own tiny generator: the recursive `localeCompare` key
sort (`@openrouter/.../internal/index.mjs:2576-2596`) is the highest-risk single function in
the layer and deserves a dedicated corpus of nested objects with keys that sort differently
under ICU collation than under byte order (`_` vs `-`, digits vs letters, accented
characters, mixed case).

The **exit-condition truth table** (§2.2) and the **retry decision table** (§6.4) are the two
places to write an exhaustive enumeration rather than sampled cases — both have small finite
input domains.

### 7.2 `httptest` SSE servers (Go-only, no TS oracle)

For behaviour that has no TS counterpart because it lives in the Go plumbing:

- **SSE frame decoding**: CRLF vs LF, a `data:` split across two TCP writes, comment-only
  keepalives (`: OPENROUTER PROCESSING`), `data: [DONE]`, a frame with multiple `data:` lines,
  a trailing frame with no terminating blank line.
- **The four `orfetch` timers end-to-end**: reuse `orfetch.SetTimerFactoryForTesting`
  (`orfetch.go:128`) with the existing virtual-clock recorder (`fixtures_test.go` header
  comment describes the pattern) and assert the *abort message text*, because that text is
  what `IsLikelyTimeout` and `retry.isTimeoutError` match on.
- **Layer-2/3 collapse**: a server that sends headers then stalls must produce
  `"SSE read timed out"`, and one that streams slowly past 600 s must produce the
  `AbortSignal.timeout` message shape.
- **Router registration exactly-once**: assert `Register` is called once on success, once on
  error, and once on cancellation (the deliberate divergence from §3.6).
- **AIMD interaction**: a 429 response decreases the cap, a 2xx with low `X-RateLimit-Remaining`
  calls `OnLowRemaining` — `orfetch.SetLimiterProvider` (`orfetch.go:737`) makes this a
  spy assertion.
- **Tool-part settlement**: a stream that opens three tool calls and never results them must,
  after cleanup, leave three `error` parts with `"Tool execution aborted"` and
  `metadata.interrupted == true`, within ~250 ms per call.
- **Runner state machine**: a table over (initial state × operation) asserting the resulting
  state, which handle is joined, and whether `onIdle`/`onBusy`/`BusyError` fired — this is a
  direct transliteration of the tables in §4.2 and should be exhaustive over the 4×3 grid,
  plus the `finishShell` promotion and the `stopShell` ready-latch rendezvous.

### 7.3 Recorded-wire-log replay

Needed because the fixture tier cannot cover *sequences* that only real providers produce.

Record with a tap in the TS harness (a `fetch` wrapper around `openRouterAimdFetch` that
tees the response body to a file) during a batch-4 bench run
(`docs/bench-roster.md`: werkzeug#3179, marked#4011, express#7365, typer#1869, execa#1218).
Store per-request as `{request_body, status, headers, sse_bytes}` and replay through an
`httptest` server that emits `sse_bytes` verbatim.

Two assertions:
1. **Wire-in parity**: Go's emitted stream-part sequence equals the TS `fullStream` sequence
   recorded alongside (dump `processor.ts`'s `handleEvent` inputs).
2. **Wire-out parity**: for the *next* request in the log, Go's assembled request body is
   byte-identical to the recorded one after canonicalising key order. This is the only test
   that catches `reasoning_details` round-trip regressions, the DeepSeek reasoning stub, the
   `deterministicStringify` key sort, and the `differentModel` metadata stripping in
   combination.

The right harness shape already exists: `tools/diffharness/compare.sh` feeds a
`corpus.jsonl` through both `bun run ts-driver.ts` and a Go binary under `cmd/`, with clock
and RNG pinned identically on both sides, `diff -u`s the JSONL, and dumps to `last-failure/`
on divergence — with a `normalize()` filter for accepted engine-specific divergences (its one
current use is the JSON-parse error text). **Clone that pattern as `tools/enginediff/` with a
`cmd/engine-diff` driver**; it is the correct tool for a surface that is a *stream of events*
rather than a pure function, and it is how the loop, the processor and the runner get tested
end to end rather than unit by unit.

Models to record: at minimum `deepseek/deepseek-v4-pro` (the DeepSeek branch),
`moonshotai/kimi-k2.6` (the schema sanitiser branch), and one qwen (neither). Pin the
models.dev catalog JSON alongside the log — `models-snapshot.js` is a stub
(`src/provider/models.ts`), so token limits, costs and `capabilities.interleaved` all come
from a live fetch and will drift.

### 7.4 What is explicitly *not* tested for parity

Per PORT-ASSESSMENT §5: byte-identical transcripts and NDJSON are out of reach (LLM
nondeterminism, the MergeCoordinator settling window). The engine layer's parity gate is
(a) grader pass/fail, (b) decision-ledger traces, (c) DAG shape, (d) gate verdict sequences.

---

## 8. Open questions, ranked by risk

**R0 — Does the Go `deterministicStringify` sort keys the same way `localeCompare` does?**
*(Highest risk in the layer.)* Assistant `tool_calls[].function.arguments` are stringified
with a **recursive key sort using `String.prototype.localeCompare`**
(`@openrouter/ai-sdk-provider/dist/internal/index.mjs:2576-2596`) specifically so Anthropic
signature validation survives a round-trip. `localeCompare` is ICU collation; Go's
`sort.Strings` is byte order, and they disagree on `_` vs `-`, digits vs letters, case, and
anything non-ASCII. Tool argument keys are model-authored, so non-ASCII is reachable. A
mismatch produces a body that is *valid* but whose signatures fail — a failure mode that
looks like a provider bug, not a port bug. PORT-ASSESSMENT §3 already flags `localeCompare`
as an ICU hazard for HEFT tie-breaks; this is a second, hotter site (and `llm.ts:308`'s tool
map sort is a third).
**Re-read**: `@openrouter/ai-sdk-provider/dist/internal/index.mjs:2576-2596` and `:2913-2989`
(where it is applied), plus `src/session/llm.ts:308`. Then decide: implement ICU collation, or
restrict to a byte-order sort and prove via fixtures that the reachable key domain is
ASCII-and-order-identical.

**R1 — Does `convertToModelMessages`' block-splitting produce the message sequence codeaf
assumes for multi-step assistant turns?**
A single assistant `MessageV2` accumulates parts across *several* steps (each `start-step`
adds a `step-start` part, `processor.ts:447-453`), and `convertToModelMessages` flushes a new
`ModelMessage` pair at each one (`ai/dist/index.mjs:8529-8535`). Get this wrong and every
replayed history has the wrong assistant/tool interleaving, which providers reject.
**Re-read**: `ai/dist/index.mjs:8369-8542` (the whole assistant case, especially
`processBlock` at `:8371` and the `step-start` flush at `:8531`), against
`message-v2.ts:882-885` (where `step-start` parts are pushed) and `:1004` (the filter that
drops all-step-start messages).

**R2 — RESOLVED, with a required design change.** `Stream.takeUntil(() => ctx.needsCompaction)`
(`processor.ts:702`) emits the element satisfying the predicate *and then stops*, tearing down
the scope and aborting the request (`llm.ts:513-516`), so the Go equivalent must abandon the
SSE body mid-stream. `orfetch`'s `Body.Close()` **does not** do that: it only clears the
watchdogs, leaves the eager pump goroutine reading, and can be re-armed by a late chunk
(`orfetch.go:707-720`, `:604-653`) — a deliberately kept TS bug. **Therefore `orclient` must
cancel the request context, not merely `Close()` the body, and must treat `Close()` as
best-effort cleanup.** Captured in §3.6. Residual question: whether the pump goroutine is
guaranteed to exit once the context is cancelled *and* the body closed, or whether a leak
window remains.
**Re-read** to close it: `internal/router/orfetch/orfetch.go:583-593` (`settle`,
`newWrappedBody`), `:604-653` (`pump`), `:659-720` (`finalErr`, `Read`, `Close`) — and write a
goroutine-leak test (`runtime.NumGoroutine` before/after) rather than reasoning about it.

**R3 — Does the `degrade` helper's swallowing of interruption deadlock or hang shutdown?**
§4.5 requires reproducing `catchCause`'s complete catch-all, including interrupts. In TS the
event loop still unwinds because the enclosing fiber is separately interrupted; in Go a
goroutine that ignores `ctx.Done()` and returns a fallback will keep running the caller's
subsequent work.
**Re-read**: `src/session/plandb-scheduler.ts:2119-2129` (the comment stating the contract),
`src/session/review-gate.ts:800-811` (the sentinel-object shape), and
`effect`'s `catchCause` semantics for interrupt causes before committing.

**R4 — Exactly which abort message strings must be preserved, and does layer 2's
`AbortSignal.timeout` text match `isLikelyTimeout`?**
The 4-layer stack produces at least six distinct abort messages, and router cooldown class
(120 s timeout vs 60 s transient vs no cooldown) is chosen by regex over that text
(`adaptive.go:656`, `retry.ts:42`). §3.6 argues `"The operation timed out."` matches
`/timed out/i`, but that string is a runtime-specific DOMException message (Bun vs Node may
differ) and was not verified against the running harness.
**Re-read**: `src/provider/provider.ts:1529-1541` (signal composition),
`internal/router/adaptive/adaptive.go:656` (`IsLikelyTimeout`), `src/session/retry.ts:39-62`,
and empirically capture the message a Bun `AbortSignal.timeout` rejection carries.

**R5 — Is `sanitizeSurrogates` reachable at all in Go, and if not is omitting it a
divergence?**
Go strings are UTF-8; `encoding/json` replaces `\uD800`-style lone surrogates with U+FFFD on
decode, so by the time text reaches `transform.Message` the pattern may be unreachable — in
which case the port is a no-op and the fixture corpus cannot express the input domain (the
same situation `BUGS-KEPT.md` calls a KNOWN DIVERGENCE with a bounded, documented input
domain).
**Re-read**: `src/provider/transform.ts:22-24, 65-125`, plus where text originates —
`processor.ts:552` (`ctx.currentText.text += value.text`) and the SSE decode path — to
determine whether a lone surrogate can survive into a `TextPart`.

**R6 — Does `tasks.pop()` in the back-scan pick the intended compaction/subtask part?**
`prompt.ts:1593-1601` scans messages newest-first and pushes each message's
compaction/subtask parts, so `tasks[0]` comes from the newest message and `tasks[len-1]` from
the oldest scanned; `:1697` then pops the **oldest**. Separately, the guard
`if (task && !lastFinished)` at `:1600` tests an array that is always truthy (empty filter
results included), so the condition is effectively `!lastFinished` — a probable
**[BUG-CANDIDATE]** that must be reproduced, not fixed.
**Re-read**: `src/session/prompt.ts:1592-1601` and `:1697-1714`.

**R7 — `router.Pick`'s 5-minute block-then-error: turn failure or degrade?**
`adaptive.ts:549-591` / `internal/router/adaptive/adaptive.go:1091` can block up to 5 minutes
and then return an error. In TS the throw crosses `Effect.promise` (`llm.ts:105`) and becomes
a *defect*, not a typed failure — so it is not retried and surfaces as an `UnknownError`.
§3.6 recommends matching that, but it means a pool-wide cooldown fails the whole turn.
**Re-read**: `src/session/llm.ts:97-122`, `src/router/adaptive.ts:549-591`,
`internal/router/adaptive/adaptive.go:1091-1134`.

**R8 — Should the missing retry cap be ported as-is?**
§6.5: `policy()` has no maximum attempt count and no elapsed-time budget, and branch 2d of
`delay` is capped only at ~24.8 days. Bug-for-bug says port it. Operationally it means a
persistently-5xx model can pin a leaf forever.
**Re-read**: `src/session/retry.ts:103, 230-257`, and confirm no outer bound exists at the
call site — `src/session/processor.ts:717-742`.

**R9 — `applyCaching` never fires for the default OpenRouter pools.**
`transform.ts:434-446` requires an anthropic/claude/alibaba signal; the eight pool models
match none, yet `:350-352` carries a live `openrouter: {cacheControl:{type:"ephemeral"}}`
payload. If this is an unintended miss, prompt-cache economics differ materially between what
the code intends and what it does — and "fixing" it in Go alone would be a parity break.
**Re-read**: `src/provider/transform.ts:342-391` and `:431-446`.

**R10 — Which models.dev snapshot is pinned, and does `capabilities.interleaved` matter?**
The catalog is fetched live with a 5-min TTL and no baked fallback. `model.limit`,
`model.cost`, and `model.capabilities.temperature` all feed request assembly and usage math.
`capabilities.interleaved` gates the folding branch that §3.2 proposes to omit — safe only
while the `@openrouter/ai-sdk-provider` exclusion at `transform.ts:308` holds.
**Re-read**: `src/provider/models.ts:105-107`, `src/provider/provider.ts:1388-1417`,
`:1044`, `:1248-1253`, `src/provider/transform.ts:305-309`.

**R11 — Which `compatibility` mode does codeaf's OpenRouter provider instance get?**
`stream_options: {include_usage: true}` is emitted only in `"strict"` mode
(`@openrouter/.../internal/index.mjs:3728-3730`); `createOpenRouter` defaults to
`"compatible"` (`dist/index.mjs:5269`) while the exported `openrouter` singleton is
`"strict"` (`:5346-5348`). Low impact (the top-level `usage:{include:true}` is what yields
cost) but it changes the request body byte-for-byte, which breaks §7.3's wire-out parity test.
**Re-read**: `src/provider/provider.ts:427-447` and whichever `BUNDLED_PROVIDERS` entry
(`provider.ts:93-118`) constructs the OpenRouter SDK, to see whether it calls
`createOpenRouter` or imports the singleton.

**R12 — Does the duplicate `tool-call` emission actually reach `processor.ts`, and what does
it do there?**
`.../internal/index.mjs:4064` re-emits `tool-input-end` + `tool-call` every time the
accumulated argument buffer parses as JSON, with no `sent` guard — reachable whenever a model
streams arguments that are momentarily valid JSON and then continue (e.g. `{}` followed by
more). In `processor.ts` a second `tool-call` for the same id would re-run `updateToolCall`
(`:339-351`) and re-evaluate the doom-loop guard (`:353-378`) against the *same* three parts,
which could spuriously trigger a `doom_loop` permission request.
**Re-read**: `@openrouter/.../internal/index.mjs:4030-4083` and `src/session/processor.ts:322-379`.

**R13 — Is `state.SetRouterFactory` wiring in scope for this layer, and who owns the
`AdaptiveRouteEvent → RouteEvent` bridge?**
`state.GetRouter()` returns an inert placeholder today and nothing outside `internal/router`
imports it, so the router is functionally unreachable. §3 assumes `llm/step` calls
`router.Pick`/`Register`, which requires the wiring to exist. It may belong to a CLI-bootstrap
package rather than the engine.
**Re-read**: `internal/router/state/state.go:53-110`, `:121-196`, and
`internal/router/adaptive/adaptive.go:360-386` (`AdaptiveRouteEvent`, `ErrorText`) to confirm
the two event structs are convertible without loss.

---

## 9. `BUGS-KEPT.md` obligations

Per F4 and the registry's own policy header, this layer must add sections for
`llm/*` and `engine/*`. The candidates identified while writing this plan, each of which
needs a fixture name once implemented:

| # | tag | finding | cite |
|---|---|---|---|
| 1 | SUSPECTED TS BUG KEPT | `applyCaching` can never fire for the 8 default OpenRouter pool models, yet carries a live `openrouter: {cacheControl:{type:"ephemeral"}}` payload — a missed prompt-cache saving | `transform.ts:350-352` vs `:434-446` |
| 2 | SUSPECTED TS BUG KEPT | `retry.policy()` has no attempt cap and no time budget; `delay` branch 2d is capped only at `RETRY_MAX_DELAY` (~24.8 days) | `retry.ts:103`, `:239` |
| 3 | SUSPECTED TS BUG KEPT | `tasks.push` guard `if (task && !lastFinished)` tests an always-truthy array, so the condition is effectively `!lastFinished` | `prompt.ts:1600` |
| 4 | SUSPECTED TS BUG KEPT | `tasks.pop()` takes the **oldest** scanned compaction/subtask part, not the newest | `prompt.ts:1593-1601` vs `:1697` |
| 5 | UPSTREAM BUG KEPT | OpenRouter tool-call accumulator: `index ?? toolCalls.length - 1` targets index `-1` on a first delta with no `index` | `@openrouter/.../internal/index.mjs:3962` |
| 6 | UPSTREAM BUG KEPT | OpenRouter re-emits `tool-input-end` + `tool-call` every time the arg buffer parses — no `sent` guard | `@openrouter/.../internal/index.mjs:4064` |
| 7 | KEPT BEHAVIOUR (inherited) | `orfetch` `Body.Close()` neither closes upstream nor cancels; a late chunk re-arms cleared watchdogs | `orfetch.go:707-720` |
| 8 | KNOWN DIVERGENCE | abort layers 2-chunk and 3 collapse into one reader-side watchdog, reducing the number of distinct abort messages | `provider.ts:41-87`, `:1529-1541` |
| 9 | KNOWN DIVERGENCE | the interleaved-field folding branch is omitted entirely (unreachable for `@openrouter/ai-sdk-provider`) | `transform.ts:305-309` |
| 10 | KNOWN DIVERGENCE (proposed) | `router.Register` fires from a `defer`, closing the TS in-flight leak on interruption | `llm.ts:139-146` |
| 11 | KEPT BEHAVIOUR | `degrade` swallows interruption and panics, matching `Effect.catchCause`'s complete catch-all | `plandb-scheduler.ts:2119-2121` |
| 12 | DEAD CODE KEPT | `isOpenAiErrorRetryable`'s 404-is-retryable rule, gated on `providerID.startsWith("openai")` | `error.ts:30-35`, `:197` |
| 13 | DEAD CODE KEPT | the `"unknown"` arm of the `finished` check — unreachable for OpenRouter (F2) | `prompt.ts:1834` |
| 14 | TO VERIFY | whether `sanitizeSurrogates` has any reachable input in Go; if not, a bounded KNOWN DIVERGENCE | `transform.ts:22-24` (R5) |

Items 1–6 are behaviour that must be reproduced. Items 8–10 are deliberate differences that
need an explicit, bounded justification and a fixture pinning the observable delta.
