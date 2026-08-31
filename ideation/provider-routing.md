# Lanes — provider choice, the belief that picks them, and the watch that swaps them

*Date: 2026-08-30. Status: ideation → proposal. Extends Decision 25 in
`docs/CHAT-V3.md` (the velocity ledger); does not replace it.*

## Bottom line

A model id is an address; the **lane** (OpenRouter endpoint) is the machine.
Today v3 asks OpenRouter for `sort: latency`, measures what it got, and demotes
a lane after fixed-threshold strikes. That is reactive, blind on the first
call of every process, forgets everything at exit, cannot rescue a request
that is already slow, and the picker shows none of it.

Proposal, in one line: **a prior from the sheet, a belief that forgets, a
choice that samples, a watch that hedges, and a picker that shows the lane.**

- **Prior** — OpenRouter's `/models/{id}/endpoints` sheet (per-lane TTFT and
  throughput p50/p75/p90/p99, uptime, price, quantization, tool support) is
  fetched on a background beat and turned into a log-normal prior per lane.
  Nothing is blind on the first call.
- **Belief** — per `(model, lane)` a two-state Kalman filter in the log domain
  (ln TTFT, ln tok/s) with process noise tuned to a ~10-minute half-life. It
  replaces the 2-strike/3-strike/5-minute constants. Persisted to
  `~/.aforge/v3/lanes.json`.
- **Choice** — capability gate, then Thompson sampling over *time-to-answer*
  with a tail term and a price penalty. Sends `provider.order` = top-3,
  `allow_fallbacks: true`. A pin sends `provider.only`.
- **Watch** — a per-request hedge deadline computed from the belief (not a
  constant), a heartbeat watch, and a CUSUM on inter-token gaps. When the watch
  trips, one hedge request goes to the next-best lane; first to commit wins,
  the loser is cancelled (cancel stops billing on ~20 lanes). Budgeted: ≤1
  concurrent hedge, ≤10% of spend, token bucket per minute.
- **Return** — no penalty box. The posterior drifts back toward the prior on
  its own; demoted lanes are re-measured by the hedges themselves and by the
  sheet's `uptime_last_5m`.
- **Picker** — one row per model gains speed (`▲0.4s · 58 t/s`) and the lane
  in use; `→` unfolds the lanes under a model with live numbers; typing
  filters with a tiny grammar (`@cloudflare`, `<1s`, `>50t/s`, `$<0.3`, `fp8`,
  `tools`, `fast`, `cheap`) that falls back to today's subsequence search.

## The world, measured (2026-08-30, one model, 30-minute window)

`deepseek/deepseek-v4-flash` had 17 lanes. Sorted by first-token p50:

| lane | quant | TTFT p50 | p90 | p99 | tok/s p50 | p90 | up 30m | $/M out | tools | max out |
|---|---|---:|---:|---:|---:|---:|---:|---:|---|---:|
| CoreWeave | fp8 | 430 | 4539 | 12240 | 24 | 48 | 99.6 | 0.28 | ✓ | 943k |
| Parasail | fp8 | 758 | 1852 | 10679 | 41 | 68 | 99.9 | 0.28 | ✗ | 943k |
| DeepInfra | fp8 | 760 | 1345 | 3234 | 27 | 37 | 99.8 | 0.18 | ✓ | 65k |
| Cloudflare | ? | 768 | 1037 | 1749 | 58 | 92 | 100 | 1.32 | ✗ | 345k |
| Alibaba | fp8 | 840 | 1625 | 14568 | 67 | 115 | 99.8 | 0.27 | ✗ | 393k |
| Baidu | fp8 | 844 | 1580 | 5503 | 75 | 115 | 100 | 0.28 | ✗ | 131k |
| … | | | | | | | | | | |
| DigitalOcean | ? | 1504 | 2913 | 64274 | **6** | 9 | 99.8 | 0.17 | ✓ | 943k |
| AtlasCloud | **fp4** | 1793 | 2109 | 3782 | 32 | 80 | 100 | 0.28 | ✗ | 393k |
| GMICloud | fp8 | 3030 | 9268 | 18351 | 30 | 67 | 97.5 | 0.22 | ✗ | 943k |

Four facts drive the design:

1. **Spread is 7× on first token and 12× on throughput for the same model at
   roughly the same price.** Lane choice is a bigger speed lever than model
   choice among peers.
2. **Tails dominate feel.** CoreWeave is fastest at p50 and one of the worst at
   p99. A user remembers the 12-second wait, not the 430 ms one. The objective
   must price the tail.
3. **Lanes differ in capability, not only speed** — fp4 quantization, no tool
   calls, 32k output cap. A "fast" lane that drops the tool call is a wrong
   answer, not a fast one. The gate comes before the race.
4. **The stream tells us who served it on the first chunk** (`provider` is a
   top-level field), and `: OPENROUTER PROCESSING` comments arrive before the
   first token. Both are free signals the watch can use.

Also confirmed live: request accepts `provider.{order, only, ignore, sort,
allow_fallbacks, require_parameters, quantizations, max_price,
preferred_max_latency{p50..p99}, preferred_min_throughput{…}}`; the usage frame
carries exact `cost`; cancelling a stream stops billing on OpenAI, Anthropic,
Fireworks and ~20 more.

## What exists today (so the proposal is a diff, not a rewrite)

- `internal/provider/velocity.go` — `velocityLedger` per `(model, provider)`:
  strikes, `ignoredUntil`, `Sighting{TTFT, Tokens, Elapsed, Rate, Gap, Laggy}`.
  In-memory only. Thresholds `LagTTFT=2s, LagRate=30, LagGap=15s,
  demoteAfter=2, ignoreAfter=3, ignoreCooldown=5m`.
- `providerPreferences` (`velocity.go:299`) already sends `sort`, `order`,
  `ignore`, `allow_fallbacks`, `require_parameters`, `max_price`.
- TTFT and rate are measured on every stream (`client.go:989–1229`); served
  lane is read from `chunk.Provider`.
- `routing` settings row: `latency | price | off`.
- Picker `internal/tui3/palette.go`: fuzzy filter, 12 rows, `ctrl+t` effort;
  row note = `context · price · elo · modalities`. No speed, no lane.
- `bench/routerlab`, `bench/ab-routing`: model-level, wall-clock only. No
  lane-level bench.
- Not present: the endpoints sheet, any persistence, any hedge, `only`,
  `quantizations`, lane UX.

## Design

### 1. The sheet (prior)

`internal/provider/lanesheet.go`. On a background beat (every 5 min while a
session is open, and once at open if the cache is older than 5 min) fetch
`GET /models/{id}/endpoints` for each model in use, decode row-by-row like the
catalog does, write `~/.aforge/v3/lanes/{model}.json`. **Never on the input or
send path** — the send path reads the in-memory copy; a missing sheet means
"no prior, use the belief alone", never a fetch. (The home-lag lesson of
2026-08-30 applies: one reading per beat, re-parsed only on stat change.)

Each lane row becomes a log-normal prior for TTFT and for throughput:

```
μ  = ln p50
σ  = (ln p90 − ln p50) / 1.2816          # z(0.90)
```

plus the gate facts: `tools`, `quantization`, `max_completion_tokens`,
`context_length`, `uptime_last_5m`, `pricing`.

### 2. The belief (Kalman in the log domain)

Per `(model, lane)`, two independent scalar filters — one for `ln TTFT`, one
for `ln tok/s`. State `x`, variance `P`.

```
predict (elapsed Δt since last update):
    P ← P + Q·Δt        with Q = σ₀² · ln2 / τ ,  τ = 10 min half-life
update on a sighting z = ln(observed):
    K ← P / (P + R)
    x ← x + K·(z − x)
    P ← (1 − K)·P
```

- `σ₀²` is the sheet's prior variance for that lane; `R` is the observation
  noise: the sheet's σ² inflated by prompt-size bucket for TTFT (a 60k-token
  prompt has a long prefill that is not the lane's fault) and set to the
  sheet's σ² for rate when `tokens ≥ ratedFloor`, else the rate observation is
  skipped (as today).
- Every sheet refresh is fed as a *pseudo-observation* with `R = σ₀²·k`,
  `k ≈ 4` (worth a quarter of a real sighting) so the public number keeps
  pulling the belief toward reality without drowning our own measurements.
- Persist `{x, P, at}` per filter in `~/.aforge/v3/lanes.json` on every
  update (small, atomic write). A new process starts from yesterday's belief
  aged by `Δt` — which is exactly "mostly the prior, a little memory".

Why Kalman and not the strike table: the strike table has a fixed idea of
slow (2 s) that is wrong for a 100k-token prompt and wrong for a lane whose
normal is 400 ms. The filter's notion of slow is *relative to what this lane
was doing ten minutes ago*, and its innovation `(z − x)/√(P+R)` is a free,
calibrated "how surprising was that" number — that is what a strike wanted to
be.

### 3. The choice (gate, then sample)

Per request, with `N̂` = expected output tokens (from the session's own recent
answers per role; default 400 talk / 2000 work):

1. **Gate** (deterministic, never sampled): drop lanes that lack a needed
   capability — tools when the request carries tools, `max_completion_tokens
   < max_tokens`, context too small, quantization below `fp8` unless the
   settings row allows it, `uptime_last_5m < 95`, price above the ceiling
   (`latencyPriceCeiling` stays, 1.25× the model's list price; a user pin
   overrides it).
2. **Sample** one draw per lane from each posterior:
   `t̃ = exp(x_ttft + √P·ε₁)`, `r̃ = exp(x_rate + √P·ε₂)`.
3. **Score** — time to answer with a tail term and a price term:
   ```
   T̃      = t̃ + N̂ / r̃
   tail   = exp(x_ttft + 1.28·√(P + R))           # posterior p90 first token
   score  = 0.6·T̃ + 0.4·tail + λ·price_out·N̂      # λ: seconds per $, ~20
   ```
   The 0.4·tail term is a CVaR-lite: it is what makes CoreWeave lose to
   Cloudflare despite the better p50.
4. **Send** `provider.order = [best, second, third]`, `allow_fallbacks: true`,
   `require_parameters: true`. `sort` is omitted when `order` is set.
   Exploration is bounded: a lane may only be *sampled into* the top-3 if
   its prior p50 is within 2× of the best lane's — DigitalOcean at 6 tok/s is
   never explored on the user's time.

`routing: price` keeps the same machinery with `λ` large and hedging off.
`routing: off` stays total, as today.

### 4. The watch (adaptive deadline, heartbeat, CUSUM, hedge)

**Deadline is computed per request, not a constant.** For the lane we expect
to serve (top of `order`), with log-normal posterior `(μ, s²)` for TTFT
(`s² = P + R`), the expected remaining wait given we have already waited `t`:

```
P(T > t)             = 1 − Φ((ln t − μ) / s)
E[T·1{T>t}]          = exp(μ + s²/2) · Φ((μ + s² − ln t) / s)
E[T − t | T > t]     = E[T·1{T>t}] / P(T > t) − t
```

For a log-normal this grows with `t` — the longer the wait, the longer the
expected remaining wait. The **hedge time** `t*` is the smallest `t` where

```
E[T − t | T > t]  >  E_alt[T]  +  hedge_overhead  +  c_$ · hedge_cost
```

with `E_alt` from the second-best lane's posterior. `t*` is a number in ms
computed once at send (cheap: a few `Φ` evaluations), clamped to
`[700 ms, 8 s]`. It is per lane and per prompt size — a lane whose normal is
400 ms hedges at ~1.2 s; a lane whose normal is 2 s does not.

**Heartbeat.** OpenRouter emits `: OPENROUTER PROCESSING` before the first
token. A stream with neither a heartbeat nor a byte for `max(2·t*, 3 s)` is a
dead connection, not a slow lane: hedge immediately and do not charge the
lane's belief with it (it is a claim about the path, not the endpoint).

**Mid-stream.** Once tokens flow, run a one-sided CUSUM on
`ln(gap) − (−x_rate)` (log inter-token time against the lane's believed
rate): `S ← max(0, S + (ln gap − expected − k))`, alarm at `h`. A single long
gap is buffered delivery (today's `LagGap`, still demote-worthy); a CUSUM
alarm is a lane that has *become* slow mid-answer.

**Commitment.** After `K` tokens have arrived (K = 64), the current stream is
only abandoned when the CUSUM alarms *and* the expected time to finish on the
current lane exceeds the expected time to redo the whole answer on the
alternative — sunk cost is real here because the hedge starts from zero.

**Continuation hedge (experimental, flagged).** For plain-text answers (no
tools, no JSON schema) the hedge may carry the partial answer as an assistant
prefill so the alternative *continues* rather than restarts. Verified at
runtime: the first 20 characters of the hedge stream must not repeat the
partial; if they do, the hedge is a fresh answer and the partial is dropped.
Not for tool calls: a tool-call JSON split across two lanes is a bug.

**Hedge budget** (`internal/provider/hedge.go`): one concurrent hedge per
request; a token bucket of 6 hedges/min per session; a spend cap of 10% of
the session's last hour; hedges off under `routing: price`. When the loser is
cancelled, the usage frame of the winner is the only cost row; the loser's
partial cost (for lanes that do not honour cancel) is written to the ledger as
`hedge_waste`.

Every hedge is also a **measurement of the alternative lane** — exploration
paid for by a request that needed rescuing anyway.

### 5. The return (no penalty box)

There is no `ignoredUntil`. A slow lane's `x` rose; with `Q` it decays back
toward the prior at the 10-minute half-life, and the next sheet refresh pulls
it too. The lane earns its way back by: a hedge landing on it, a sheet
refresh showing `uptime_last_5m` recovered, or Thompson sampling drawing it
once its `P` has widened enough. `provider.ignore` is still sent, but only
for lanes whose posterior p50 TTFT is > 3× the best *and* `P` is small (we
are sure, not merely unlucky) — and it expires with the belief, not a timer.

### 6. What the user sees

**Picker (`/model`, and the same picker inside settings):**

```
 deepseek-v4-flash      1M · $0.09/$0.18 · elo 1290 · ▲0.8s 58t/s · via Cloudflare
 qwen3.5-9b             128k · $0.02/$0.05 · elo 1180 · ▲0.3s 140t/s · via Groq
 gpt-oss-120b           …
 ─ filter: deep <1s tools ───────────────────────────────────────────────────
   ↑↓ · → lanes · ctrl+t effort · enter switch · esc
```

`→` (or `tab`) on a model unfolds its lanes in place:

```
 deepseek-v4-flash
   ● auto           picks the fastest lane each answer — Cloudflare now   (recommended)
     Cloudflare     0.8s  58 t/s  100%  $1.32   ▁▂▁▃▁▂    no tools
     CoreWeave      0.4s  24 t/s   99%  $0.28   ▁▁▇▁▂▁    tail 12s
     DeepInfra      0.8s  27 t/s   99%  $0.18   —         out ≤ 65k
     Baidu          0.8s  75 t/s  100%  $0.28   —         no tools
   ○ openrouter     let the router balance on price
```

Numbers are the posterior (our belief), not the raw sheet; the sparkline is
our own last eight sightings when we have any. `enter` on a lane pins it;
`enter` on `auto` un-pins. A dim "why" line under the cursor:
`Cloudflare: first token 0.8s, steady 58 t/s, no tail — from the sheet + your
last 12 answers`.

**Filter grammar** (each token narrows; unknown tokens fall back to today's
prefix/substring/subsequence rank, so nothing the user types today breaks):

| token | meaning |
|---|---|
| `@cloudflare` | model has this lane; unfold shows it first |
| `<1s` / `>50t/s` | posterior p50 first-token / throughput bound |
| `$<0.3` | output price per M below |
| `fp8` `bf16` | quantization at least |
| `tools` `sees` `draws` | capability |
| `fast` / `cheap` | sort by score with λ small / large |

**Settings → providers tab**, three rows:

```
 model.talk        deepseek-v4-flash · auto (Cloudflare now)
 lane              auto | pinned: Cloudflare | pinned, borrow when slow | openrouter
 speed guard       on · hedge ≤1 · ≤10% spend        [off under price routing]
```

`pinned, borrow when slow` = `order:[pin]` and the watch may hedge elsewhere;
`pinned` = `only:[pin]` and the watch only reports.

**HUD** (status line, extends today's `via quicksilver · 92 tok/s`):
`via Cloudflare · 0.6s · 61 t/s`; during a hedge `slow · trying CoreWeave…`;
after: `via CoreWeave · rescued`. Honest, and the only time the word "slow"
is shown is when the system is already doing something about it.

**Slash:** `/model @cloudflare` pins the lane on the current model;
`/model auto` un-pins; `/model deepseek <1s` opens the picker pre-filtered.

### 7. Ledger

`usage.jsonl` rows gain `lane`, `ttft_ms`, `tps`, `hedged` (bool),
`hedge_waste_usd`. The session journal already carries `endpoint`. The
call log (`AFORGE_CALL_LOG`) gains `ttft_ms` and `deadline_ms`.

## Algorithm notes, for the reviewer

- **Why Thompson and not UCB or ε-greedy:** the posterior is already there;
  sampling from it is exploration with zero extra state and no schedule to
  tune, and it is quiet when beliefs are sharp (which is the normal state for
  the top lanes).
- **Why log domain:** TTFT and tok/s are multiplicative quantities with heavy
  right tails; `ln` makes the noise near-Gaussian and makes "2× slower"
  the same size event everywhere.
- **Why two scalar filters, not one 2-D:** TTFT and throughput fail for
  different reasons (queueing vs. GPU contention); coupling them buys nothing
  and costs a covariance nobody can explain.
- **Why not race every lane on every request:** cost and load. Hedging only
  when the belief says the wait has become unlikely is the tail-at-scale
  result (Dean & Barroso 2013): ~1–2% extra requests remove most of the p99.
- **Why the sheet is a pseudo-observation, not a hard reset:** the sheet is
  a 30-minute aggregate over everybody's prompts; our own measurement is
  about our prompts and our region. Both are evidence; neither is truth.
- **Where it can be wrong:** prompt caching makes TTFT bimodal (cache hit vs
  miss). v1 inflates `R` for cache-eligible requests; v2 keys the TTFT
  filter by `cache_hit`. Reasoning models produce reasoning deltas — those
  count as tokens for the watch (as today) but not for `N̂`.

## Proof plan

1. **Simulator** (`bench/lanelab/sim.py`): draw lanes from the sheet's
   percentiles (log-normal with the p99 as a mixture tail), replay 10k
   requests under four policies — OpenRouter default (price-weighted), today's
   strike ledger, sheet-only, sheet + belief + hedge. Report p50/p90/p99
   time-to-first-token and time-to-answer, and $ overhead. Ship only if p90
   improves ≥ 30% at ≤ 3% cost.
2. **Live A/B** (`bench/lanelab/live.sh`): two arms on the same prompt set,
   same price table, blind — per the benchmark-honesty law. Metric is the
   diff of the two ledgers, not a count.
3. **Guard tests** (PERF.md): no fetch on the send path; `t*` computed in
   < 50 µs; sheet beat never blocks `Update`; hedge bucket honoured under a
   stall storm.

## Build plan (lanes, each with a law and a structural test first)

| lane | delivers | law |
|---|---|---|
| L1 sheet | `lanesheet.go`, beat, cache, prior fit | never on the send path |
| L2 belief | `laneBelief` replacing strikes; persistence | one notion of slow: the innovation |
| L3 choice | gate + Thompson + `order`/`only` | a gate drop is never a sampled event |
| L4 watch | `t*`, heartbeat, CUSUM, hedge, budget | a hedge is a measurement |
| L5 picker | speed column, lane unfold, grammar | every number shown is the posterior |
| L6 settings + HUD | lane row, speed-guard row, HUD states | "slow" shown only with a remedy |
| L7 ledger + bench | fields, `lanelab` sim + live A/B | ship on the p90 diff, not on a story |

Order: L1→L2→L3 (the router works and is measurable), then L4, then L5–L6
in parallel, L7 throughout. `ARCHITECTURE.md` gains a "lanes" section before
L1 starts.

## What not to do

- No fixed thresholds anywhere new; the four constants in `velocity.go` retire
  with L2.
- No fetch, no exec, no walk on the input or send path.
- No racing of all lanes; no hedge without a budget.
- No lane names in prompts, manuals or laws — aforge names *lanes*, the sheet
  names vendors.
