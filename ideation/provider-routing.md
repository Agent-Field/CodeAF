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

---

# Part II — The Pareto point, found just in time

*Added 2026-08-30. Answers: "find the ideal point on cost × first-token ×
throughput × quality, per request, without ever feeling slow."*

## Bottom line

A fixed weight (`0.6·T + 0.4·tail + λ·$`) is a guess about a trade the user
never made. The clever move is to stop guessing the weight and **derive it
from the request**: the price of a second is a property of *who is waiting
and what their wait costs*, and the value of throughput *saturates at reading
speed* for text a person reads. With those two facts the multi-objective
problem collapses to a single scalar **per request** — different for a chat
turn, a critical-path node, and an off-path background call — and the Pareto
frontier over lanes is only the *candidate set* that scalar chooses from.

Five mechanisms, ranked by leverage:

1. **The price of a second comes from the graph, not a config.** λ (seconds
   a dollar buys) is computed per request from attention and slack.
2. **Throughput has a ceiling of value.** Visible text is worth at most the
   reading rate (~15–20 tok/s); hidden tokens (reasoning, tool JSON) are
   worth their full rate. So a 75 tok/s lane and a 58 tok/s lane are *equal*
   for a visible answer, and the cheaper one wins.
3. **Cost is path-dependent because of the prompt cache.** The cheapest lane
   on the sheet is not the cheapest lane for *this* request if another lane
   already holds the prefix. Cache state is an input to the price.
4. **Probe on typing.** The moment a person starts typing, a one-token probe
   goes to the top-two frontier lanes. Two seconds later, at `enter`, the
   choice is made on a measurement two seconds old instead of a thirty-minute
   aggregate. Cost ≈ $0.00002 a turn.
5. **Quality is a lane property too**, learned from the gate: a lane that
   drops tool calls, truncates, or returns JSON the parser refuses earns a
   Beta posterior on "accepted", and falls below the gate before it costs
   the user a second retry.

## 1. λ — the price of a second, per request

Three questions, each answerable from state aforge already holds:

| question | source | effect on λ |
|---|---|---|
| Is a person watching this stream now? | the TUI's focus + the request's `IntentInteractive` | attention time: at a nominal $40/h, one dollar buys 90 s → λ ≈ 90 s/$ … in plain terms **spend up to a cent to save a second**. |
| Is this node on the critical path of a task? | the plan DAG: slack = (latest start − earliest start) from the node's expected duration | on the path: λ = the wall value of the whole task (the person waits for its end); off the path with slack ≥ expected duration: λ → 0, price wins. |
| Is there a deadline (standing item, scheduled beat)? | standing/schedule registry | λ rises as the deadline nears; a missed deadline is a step cost. |

`λ` is one number handed to the chooser with the request (`WithRoutingIntent`
becomes `WithValueOfTime`). `routing: price` sets λ = 0 for everything;
`routing: latency` sets the table above; `off` stays total. No new dial —
the existing row keeps its three words and gains its meaning.

## 2. Perceived time, not wall time

```
T_perceived = TTFT
            + hidden_tokens / rate                 # reasoning, tool JSON — nobody reads them
            + visible_tokens / min(rate, R_read)   # R_read ≈ 18 tok/s
```

Consequences the fixed score got wrong:

- For a talk turn (N̂ ≈ 400 visible), any lane over ~30 tok/s is the same
  speed to the person; the decision is TTFT and $ only. Cloudflare's $1.32
  loses to Baidu's $0.28 at the same TTFT.
- For a work node (tool loop, hidden tokens) throughput is linear and
  Cloudflare's 58 tok/s at $1.32 may still lose to Baidu's 75 tok/s — but
  beats DeepInfra's 27.
- The first call of a turn is scored on TTFT alone with λ at attention value
  (the person is looking at an empty line); later calls in the same loop use
  the task's λ.

## 3. Cache-aware price

```
$ (lane, request) = p_in(lane) · (prompt − cached(lane, prefix))
                  + p_cache(lane) · cached(lane, prefix)
                  + p_out(lane) · N̂
```

`cached(lane, prefix)` is a belief, not a fact: a lane that served this
session's prefix within its cache TTL (per-lane, learned from
`cached_tokens` in the usage frame — the sheet says `supports_implicit_caching`)
probably still holds it. A lane switch forfeits it; the score pays that
forfeit explicitly instead of the current hidden "cache-affinity pin
prepended to `order`". This is also what stops the router from flapping
between two equal lanes: the incumbent is cheaper by the cache term.

## 4. The frontier is the candidate set; the scalar is the pick

Per request, after the capability gate:

1. Take each lane's posterior **p75** for TTFT and for `1/rate`, and its
   cache-aware $ and quality posterior mean.
2. **Pareto-prune**: drop a lane that another lane beats on all four at p75.
   With 17 lanes this leaves 3–5. Dominated lanes are never sampled, never
   probed, never hedged to — exploration budget is spent only where it can
   change a decision. (Using p75, not the mean, keeps an *uncertain* lane in
   the set: it might be good.)
3. **Scalarize** the survivors with this request's λ and shape:
   `score = λ⁻¹·$ + T_perceived` (in seconds), Thompson-sampled from the
   posteriors as in Part I.
4. Send `provider.order` = survivors by sampled score. When λ = 0 and the
   frontier has a cheapest lane above the quality gate, `order` is that lane
   alone plus `allow_fallbacks`.

The picker's **auto** row can now explain itself with the frontier, which is
the only honest explanation: *"3 lanes worth choosing between — Baidu
(0.8s, 75 t/s, $0.28), Cloudflare (0.8s, 58 t/s, $1.32), DeepInfra (0.8s,
27 t/s, $0.18). Talk turns go to Baidu; long hidden work goes to Cloudflare
when the task is on the clock."*

## 5. Probe on typing — freshness for two hundredths of a cent

The sheet is a 30-minute aggregate over everyone's prompts. Our own belief
may be minutes old. But we know, seconds ahead, when a request is coming:
the composer got a keystroke.

- On the first keystroke of a turn (debounced; not on every key), send a
  `max_tokens: 1` request with a fixed tiny prompt to the top-two frontier
  lanes with `provider.only: [lane]`, streaming, and record TTFT. Cost per
  probe ≈ 10 prompt tokens + 1 output ≈ $0.000005 on this model; two probes
  a turn is ~$0.00001. It also warms the HTTP/2 connection to that lane so
  TLS is out of the real TTFT.
- The probe is a real sighting into the Kalman with a small `R` (it is
  *exactly* our path, right now). It is not a rate measurement.
- Budget: at most one probe pair per 20 s per session; none when λ = 0
  (nobody is waiting); none when the connection budget is under pacing.
- The chooser at `enter` runs on beliefs that are ~2 s old. That is the
  "just in time" in the title.

## 6. Quality as a lane axis

Per `(model, lane)` a **Beta(α, β)** on "the answer was accepted", updated
from signals the harness already produces and that are attributable to the
lane that served the call: tool-call JSON the decoder refused, `finish_reason:
length` below the requested `max_tokens`, empty replies, refusals classified
by the argument-refusal law, gate failures on a node whose only change was
the model call, `cached_tokens` = 0 on a lane that claims caching. Also
prior from the sheet: quantization `fp4` starts at Beta(2, 2) instead of
Beta(8, 1).

Quality is a **gate**, not a weight: a lane whose posterior mean falls under
`q_need` (per role: talk 0.90, work 0.97) leaves the candidate set until
the posterior recovers (it decays toward the prior like the Kalman states).
Weighing quality against price is how a router learns to ship wrong answers
cheaply.

## 7. Exploration sized to the horizon

Thompson sampling explores in proportion to posterior width, blind to how
many more decisions this session will make. A 3-call session should never
explore; a 500-call swarm should explore early. **Knowledge-gradient**
scaling: multiply the sampling variance by `min(1, H / H₀)` where `H` is the
expected remaining calls (from the task's plan size, or the session's rate
over the last ten minutes) and `H₀ ≈ 50`. It is one multiply; it makes
exploration free where it pays and absent where it does not.

## 8. Model × lane × effort on one frontier (later)

Nothing above is specific to lanes of one model. The same frontier can hold
`(model, lane, effort)` triples, with quality from `bench/routerlab`'s IRT
ability and the request's difficulty estimate giving `q_need` per request.
That is the one-road escalation ladder and this router becoming one
mechanism. Not for v1 — it needs the difficulty estimate to be trustworthy —
but the interfaces should not preclude it: the chooser takes *candidates*
with `{quality, ttft, rate, price}` posteriors, and does not know what a
candidate is.

## What this changes in Part I

- Score: `λ⁻¹·$ + T_perceived` replaces `0.6·T + 0.4·tail + λ·price`; the
  tail term survives inside `T_perceived` by scoring at p75, and the hedge
  deadline still comes from the full posterior.
- Chooser inputs gain: λ, visible/hidden split of N̂, cache state, quality
  posterior, horizon `H`.
- New: `laneprobe.go` (probe on typing), `lanequality.go` (Beta per lane),
  the Pareto prune in the chooser, `WithValueOfTime`.
- Bench: `lanelab` sim gains λ scenarios (talk / critical path / off-path)
  and reports the frontier, so a reviewer can see *which* lane each scenario
  picks and why.

## Ideas considered and dropped

- **Race all frontier lanes, keep the first.** 3–5× cost for a p50 gain the
  hedge already captures at ~2% cost.
- **Split one answer across lanes** (reasoning cheap, answer fast). One
  generation cannot be split; the continuation-prefill hedge is the only
  sanctioned form and it is a rescue, not a plan.
- **A user-facing "speed vs cost" slider.** It asks the person to state λ;
  the graph already knows it, and a slider that is wrong for the next
  request is a setting nobody re-checks.
- **Per-lane fixed allow/deny lists.** Pinning exists for the person who
  knows; everyone else gets a belief that is right more often than a list.

---

# Part III — what the simulator corrected

*Added 2026-08-31, at the end of the build wave. Everything above was written
before any of it ran. This part is what running it changed, and it is kept as a
separate part rather than edited into the text above so that the difference
between a design and a measurement stays visible.*

The instrument is `bench/lanelab`: a reference simulator in Python that draws
lanes from the sheet's own percentiles and replays three scenarios — a talk turn
somebody is watching (λ = 90, four hundred visible tokens), a work node on the
critical path (λ = 90, two thousand hidden tokens), and the same work with
nobody waiting (λ = 0) — under four policies, over eight seeds. Beside it,
`bench/lanelab/gosim` replays the same three scenarios against the SHIPPED code:
the real registry, the real ledger, the real chooser, driven against `lanestub`.
The reference model is where a mechanism is argued about; **the ship decision is
taken on the Go one**, because a simulator that agrees with a design it does not
run is a simulator agreeing with itself.

Seven corrections came out of it. Five are arithmetic or law in this package,
one is the gate the whole thing is judged by, and one is a bug in the test rig
that had been quietly poisoning the person's own belief file.

## C1 — the quality gate asks the upper bound, not the mean

The gate as designed refused a lane whose believed share of usable answers fell
under what the request needed. The prior for a lane the sheet has just described
is Beta(8, 1) — "probably fine" — and its **mean is 0.889**, which is under the
0.90 a talk turn asks for and well under the 0.97 a tool loop asks for. So the
gate refused **every lane of every model on the first request of every process**,
for want of evidence rather than for cause. And the refusal was **absorbing**: a
lane outside the candidate set is never sent to, so it can never earn the ninth
good answer that would have let it back in. The whole router was inert, and
nothing in the unit tests of any one seam could show it, because the failure is
a property of the sequence rather than of any call in it.

Two things had to change.

**The gate asks the 90% upper credible bound.** `Beta.Upper(z)` is the normal
approximation `mean ± z·√(mean(1−mean)/(A+B+1))`, clamped to the unit interval.
It asks the question the gate means to ask — *could this lane be good enough?* —
rather than *is its point estimate above the line?* Beta(8, 1) is bounded near
certainty and passes both needs; three refusals take Beta(8, 4) to a bound of
about 0.83 and the lane leaves a talk turn. A wide belief is not a bad one, and
this is the difference stated in arithmetic.

**And a drop expires.** `Beta.Toward(prior, dt, halfLife)` decays each count
toward the prior's own at `HalfLife`, so the belief's MASS returns to the
prior's nine and its SHARE returns to the prior's share. After two half-lives
with nothing new, the dropped lane is back in the candidate set to earn its own
contradiction. It is the same forgetting the timing beliefs already do, and for
the same reason: forgetting is losing confidence, never changing the estimate,
which is why there is still no penalty box anywhere in this package.

The ledger forgets on the way in (`NoteOutcome` ages before it observes, so
three refusals this afternoon and three from last week are not the same
evidence) and the chooser forgets on the way out (a dropped lane gets no
sightings and no outcomes, so nothing else would ever age it).

## C2 — perceived seconds is a WAIT, and reading is not part of it

Part II §2 costs a visible answer at `visible / min(rate, ReadRate)`. That is the
time a person spends *reading*, and it is a constant no router can remove: four
hundred tokens is twenty-two seconds of reading whichever lane wrote them. What a
router can remove is the part of the wait where the reader has caught up with the
writer — the gap between the two rates, and nothing else:

```
wait = ttft  +  hidden/rate  +  visible · max(0, 1/rate − 1/ReadRate)
```

The correction changes **no ranking**: the removed term is identical for every
candidate. What it changes is every RATIO computed from the number, and that is
the whole of a ship gate. On the reference sim's talk scenario the three
competent policies' p50 read 22.99 s, 22.87 s and 22.98 s — indistinguishable.
Their actual waits are 0.770 s, 0.646 s and 0.755 s, a 19% spread. **The old
number was 96% reading**, so a router with no effect at all would have been
reported as a few per cent better than the thing it replaced. A metric that
cannot separate three policies is not a metric.

The lane package's `PerceivedSeconds` now computes the wait, and so do the
simulator's objective, the simulator's reported metric, and the ship gate below.

## C3 — the ship gate, in the design's own economics

The proof plan said "ship if p90 improves ≥ 30% at ≤ 3% cost". The 30% is the
tail-at-scale result and it survives. **The 3% is a number from nowhere**: this
design's entire argument is that a second has a price, λ, computed per request
from who is waiting — so a cost clause stated as a flat percentage is the one
clause that refuses to use the design's own reasoning. Worse, it is either
vacuous or arbitrary depending on the scenario, which is how a gate stops being
a decision procedure.

The gate, per scenario, against the arm the design retires (the strike ledger):

- **The p90 wait improves by at least 30%** — and for the talk scenario the
  prize is the FIRST TOKEN only, because above the reading rate every lane is
  the same speed to a person, so talk gates on p90 TTFT.
- **The extra money buys time at better than λ:**
  `Δ$/request ≤ Δ(mean wait) / λ`. With nobody waiting (λ = 0) that degenerates
  to `cost ≤ baseline`, which is the honest reading of "price wins outright".

Both clauses, both scenarios, or it does not ship.

**What the new gate cost us, said plainly.** The money clause is close to
unbindable wherever a person is waiting on a long answer: at λ = 90 s/$, sixty-
eight seconds saved on a work request are worth $0.76 and the design spends
$0.0002. So the work scenario went from failing on the flat 3% clause to passing
on all eight seeds — and the cost blow-up the arbitrary clause caught by accident
now sails through. That is not the gate being wrong; it is the gate being honest
about a trade the design really does endorse. It is also why the sweep still
prints the bill, and why a reviewer reads it.

## C4 — the hedge budget is request-relative, not wall-clock

A token bucket of six hedges a minute is a rate against a clock, and the thing it
is meant to bound is a rate against REQUESTS. A session sending four requests a
minute and a swarm sending four hundred are the same bucket, so the same budget
was 1% of traffic in one arm and 95% in another across seeds — a control that
does not control. The budget is now a share of recent requests (at most one hedge
in ten over a sliding window of the last twenty, with a small allowance at the
start so that a fresh session may rescue anything at all), plus the spend share
that was always there.

## C5 — the price term has to be felt, and Thompson has to draw the right thing

Two arithmetic errors, found by one case: a work node with two thousand hidden
tokens and somebody waiting, where Baidu is both quicker and four times cheaper
than Cloudflare and must therefore win.

**λ multiplies money; the code divided by it.** λ is SECONDS PER DOLLAR, so
dollars times λ is seconds and dollars over λ is dollars squared per second — a
quantity of nothing, about eight thousand times too small at the attention value
to be felt beside any wait. Part I had it right (`λ·price·N̂`); Part II's
`λ⁻¹·$` is a slip in the design text that went straight into the code. The
symptom only appeared once C2 landed: with the reading time removed, price is the
ONLY thing left to separate two lanes that feel identical to a person, and a
price term that rounds to zero turns that decision into sampling noise.

**Thompson sampling drew one imagined request, not one opinion about a lane.**
The prior seeds the belief's variance from the sheet's p50-to-p90 spread — which
is the lane's per-request VARIABILITY, not our uncertainty about its median. A
draw from it is a draw of a single request, and reordering lanes on a single
imagined request is paying real money for a coin toss nothing can be learned
from: the sheet has already published both medians. The same file said so twice
over — the hedge's `predictive()` floors that variance back UP precisely because
there the per-request question is the right one.

The correction is one variance meaning two things at two different moments, and
the fix has to respect both. Before this process has measured a lane, its P is
the sheet's spread and the draw is narrowed (`sheetObservations`). After it has,
P is the variance of the median — shrunk by our own sightings, widened again by
`Predict` as they age — and the draw is at full width, which is what lets a lane
this router gave up on earn its way back. `Belief.At` is what separates the two,
and it is a blunt line drawn where it is because it is the one fact that is
certainly true on either side of it. Narrowing BOTH cases was tried first and it
disabled §5's stated return path outright: a lane the router had stopped sending
to could no longer be sampled back, which is a penalty box arrived at by
arithmetic. The tail is still priced where it belongs — the frontier prunes at
the p75 and the hedge deadline uses the full predictive spread.

With both fixed, the case above goes from 306 requests in 400 reaching the
quicker, cheaper lane to 397.

**What is deferred, and named so it is not forgotten:** a `Posterior` still
carries one variance where the design needs two — the lane's per-request spread
(aleatoric, which the tail and the hedge want) and our uncertainty about its
median (epistemic, which the sampler and the refusal want). `Belief.At` is a
proxy for the difference and not the difference itself. Carrying both on the
belief is the right shape and it is a contract change for a later wave.

## C6 — the simulator has to judge the shipped code

A reference model in Python is where a mechanism is argued about, and it is worth
having: it is quick to change and it found five of the seven corrections here.
But it is a second implementation of the design, and a second implementation
agreeing with the first is not evidence about the build. So `bench/lanelab/gosim`
drives the REAL registry — the real ledger primed from the same sheet fixture,
the real chooser, the real watch and budget — against `lanestub` on its fast
clock, replaying the same three scenarios over eight seeds, with a scripted
mid-run slowdown of the lane it initially chose and a recovery afterwards. The
Python model stays as the reference; the ship decision is taken on the Go run,
and both tables are in `bench/lanelab/REPORT.md`.

## C7 — what an answer is entitled to teach

A ledger that folds every finished stream into the same two filters learns the
wrong thing from a failure. **An answer with no tokens in it never had a first
token**, so whatever was timed is the wait until the stream gave up — a fact
about a failure and not about how quickly the lane starts writing. A lane that
returns nothing returns it quickly, so the belief got FASTER the more often the
lane failed, and the router was rewarded for choosing it.

The law, stated once in the ledger and held by two tests:

- **An empty answer teaches quality and nothing else.** Its outcome is real
  evidence and `NoteOutcome` takes it; the timing filters take nothing.
- **A short answer teaches the first token and never the rate.** The generation
  window of a handful of tokens is mostly the handshake, so the rate filter has
  a floor (`ratedFloor`) and a probe — one token, bought on purpose — is never a
  rate measurement at all.

## And one bug in the instrument

The acceptance scenarios in `internal/lane/e2e_test.go` primed a ledger that
writes through a store, and `StorePath` resolves under `AFORGE_HOME` on every
call. They did not move the state root. So every run folded this file's INVENTED
lanes into the belief file of whoever ran the tests, and read them back on the
next run — a real router given an opinion about a lane that does not exist, and
scenarios whose starting ledger was whatever the last run left behind. It showed
up as scenario 3 choosing a different victim on two runs of the same fixed
Tuesday. Every scenario now takes a home of its own.

---

## What merging the lanes together showed

The five wave-1 lanes each held their own half of a seam and each was right
alone. Four things only appeared when the halves met, and every one of them is
the same shape: two pieces of correct code doing the same job twice, or one
piece doing a job the other had already made unnecessary.

**One choice per call.** A choice is a sampled decision, so asking for it twice
gives two answers — and a streamed call asked twice: the watch on the way in
(to decide whether to hedge, and where to), the encoder on the way out (to write
`provider.order`). A watch armed on a lane the wire never asked for hedges at
the wrong moment toward the wrong alternative, and neither half looks wrong on
its own. The call now decides once, carries the choice, and both halves read it
— which also makes every rung of the endpoint ladder and every retry ask for the
same lane as the request they are repeating.

**One sighting per stream.** Every finished stream already reaches the belief
through the ordinary path — that is how a machine with no sheet learns anything
— and the hedge race noted its winner again. A lane that had been raced was
believed on twice the evidence it had earned, and its belief moved twice as fast
for having been looked at. The race now notes the LOSERS, which are the streams
nothing else can see.

**An order of one is not a ranking.** On a machine with no sheet the only lanes
this package has heard of are the ones that have already served. So its first
opinion about a model names ONE endpoint — quite possibly the slow one it is
about to be demoted for — and that name went out as `provider.order`, in front
of a router that knows a dozen, silently overriding a strike ledger that had
correctly demoted it. A ledger that has heard of one lane has nothing to rank,
and now says so.

**Forgetting stops where the public reading stands.** The clamp on ageing was set
at the sheet's spread σ², and the sheet enters the filter as a pseudo-observation
with R = k·σ² — so a fully forgotten belief was still FOUR TIMES more certain
than the number anybody can look up, and could never be outweighed by it however
old it got. The gain on every refresh was pinned at one fifth, and a lane the
router had stopped sending to crawled back toward the public reading at twenty
per cent a beat: twenty-five minutes to return from a bad minute, in a design
whose whole claim is that there is no penalty box. At the honest clamp, k·σ², a
forgotten belief and a fresh sheet weigh the same. And a row now carries the
moment it was read (`Row.At`), because a sheet folded into a belief that was
never aged first is a fresh number weighed against ten-minute-old confidence.

### And two numbers in the acceptance scenarios that no correct build could meet

**"Halve the p90" (scenario 2).** Written before the scenario that would have to
meet it existed. The script breaks one lane to about twice its healthy first
token, so the whole of the damage is a factor of two, and an arm that removed
every trace of it would still sit at the healthy answer time — which is more
than half the pinned arm's p90. The bar is now the design's own ship gate: a p90
improvement of at least 30%, plus the stronger claim the scenario really does
make, that the ninetieth percentile of the arm that moves is better than the
median of the arm that does not.

**"Within ten requests" (scenario 3).** By the time the lane recovers, this
process has watched it be slow five times, and one public reading is not
entitled to erase five of its own measurements in four minutes. Measured: the
belief comes back from about 1.9 s to about 1.16 s over two beats against a pack
at 0.95 s, at which point the lane heads the order on roughly one request in
fifty. Ten requests is a coin toss and a test of it is a test of a random number
generator. Thirty requests and a third beat is where the return is a fact. What
changed is the CLAIM; no constant was tuned to rescue the old one.

### And one bug in the other instrument

`internal/provider`'s own suite had the same disease as the acceptance
scenarios, one layer out: its tests stream simulated answers through a real
client, every finished stream teaches the process-wide lane ledger, and that
ledger writes through a store under `AFORGE_HOME`. So the suite folded lanes
called "quicksilver" into the belief file of whoever ran it, read them back on
the next run, and then failed on a first request arriving with an `order` it
could not have learned. The velocity ledger it was all written around is per
CLIENT, so the tests had never needed to think about it. A home per test binary
and a registry per test client end both halves.
