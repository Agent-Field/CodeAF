# lanelab — results and verdict

*Simulated 2026-08-31 against the OpenRouter endpoint sheet for
`deepseek/deepseek-v4-flash`, fetched the same day at 04:00:21Z. 17 lanes,
all with published p50 timing. `python3 sim.py --seed 7`, 10 000 requests per
policy per scenario, byte-reproducible.*

## Bottom line

**The design does not pass its own ship gate. Two of six comparisons pass at
seed 7, and the two that pass are the two where nobody is waiting.** The gate
the design set itself is "p90 perceived improves by at least 30% at no more
than 3% extra cost, against both baselines". It is met only in `offpath`, and
there it is met because lambda is zero and the design is buying the cheapest
acceptable lane rather than a fast one.

Three things stop it, and none of them is a close call.

1. **In `talk` a 30% improvement is arithmetically impossible.** 400 visible
   tokens at 18 tok/s is 22.22 seconds of reading that no router can remove.
   The best any policy achieves is a p90 of 23.96 s; the strike ledger already
   gets 24.07 s. The entire prize in `talk` is 1.7 seconds, and the design wins
   it — by 0.5%.
2. **In `work` the design beats both baselines on p90 by about 80% and then
   loses on cost, by between 20% and 138%.** With lambda at 90 s/$ and
   per-request prices near $0.001, the price term in the score is worth about 8
   microseconds. The scalar is pure perceived time, so the router always buys
   speed. A 3% cost clause and a lambda of 90 cannot both be satisfied.
3. **The `work` verdict is not stable across seeds.** Over eight seeds the
   design converges on three different lanes and its cost against
   `openrouter-default` swings from -12.6% to +138.5%. Which lane it locks onto
   is decided by a warm-up race in the quality gate, and the gate is absorbing,
   so the coin flip is permanent.

The design's robust win is against `openrouter-default` on p90 — 65.6% in
`talk`, 84.5% in `work`, 79.3% in `offpath` — and that win is not subtle, because
inverse-square price weighting keeps routing to DigitalOcean, which writes at
6 tok/s. Against the mechanism it actually replaces, the strike ledger, it wins
by 80% in `work` and 72% in `offpath` and ties in `talk`.

**Recommendation: do not ship on this evidence.** The three defects below are in
the design's constants, not in this simulator, and all three are cheap to fix.
Re-run this lab after they are fixed; if `work` then passes on 8 of 8 seeds, the
live A/B in `live.sh` is worth the money.

---

## What the autopsy found, before any number here was quoted

### Finding 1 — the quality gate cannot be reached from its own prior

Part II section 6 sets the quality prior at `Beta(8, 1)` and `q_need` at 0.90
for talk, 0.97 for work. `Beta(8, 1)` has mean 0.889. **On a fresh process every
lane's quality posterior is below every scenario's requirement**, so the gate
empties the candidate set before a single token has been drawn. That is
arithmetic and has nothing to do with any lane.

It takes about 47 clean sightings for `(8+n)/(9+n)` to first clear 0.97. The
simulator therefore applies the quality gate only once a lane has 50 outcomes of
its own, on the package's own law that a gate may not refuse a lane on a number
nobody measured (`internal/lane/belief.go`). **That threshold is a deviation
from the design as written**, and it is stated here rather than buried: without
it, nothing is routable at all.

### Finding 2 — the quality gate is absorbing, and both ways out of it fail

A lane condemned at the evidence threshold receives no further outcomes, so its
Beta never moves again and it can never be re-measured. In `offpath` at seed 13
this permanently excludes NextBit on a verdict formed from 50 draws.

Part II section 6 says the Beta "decays toward the prior like the Kalman
states", which would fix it. It cannot: at a 10-minute half-life with 40-second
requests the effective sample size saturates near 22, and
`(8 + 0.985*22) / 31 = 0.958` never clears 0.97 either. **Neither setting of that
switch admits a working quality gate at `q_need = 0.97`.** The prior, the
half-life and the requirement have to be chosen together, and at present they
are not.

This is what makes the `work` verdict a coin flip: whichever fp8 lane clears
0.97 first becomes the only lane, permanently.

### Finding 3 — the hedge budget is denominated in the wrong currency

The budget is six hedges per minute of wall time. A `work` request takes about
40 seconds, so it refills four tokens of its own budget. The hedge is unbudgeted
for exactly the requests that are expensive to hedge.

The measured effect is bimodal. At seeds 7 and 11 the hedge fires on 1-2% of
`work` requests. At seeds 15 and 17 it fires on **95%**, and the bill goes from
$1.16 to $2.02 per thousand requests to buy 3 seconds of p90 (65.4 s to 62.4 s).
The switch is which lane lands in second place: `t*` is clamped at a 700 ms
floor, and on a primary lane whose own median first token is 2.06 s the expected
remaining wait already exceeds the threshold at that floor, so the clamp
licenses a hedge on nearly every request. Lambda does not catch it, because at
90 s/$ the money is worth 8 microseconds.

The fix is not the clamp. A hedge that saves 1.4 s of first token on a 36-second
answer is a bad trade whatever the deadline says. The budget needs a spend cap
or a "fraction of the answer" test, not a wall-clock bucket.

---

## The run

Pasted verbatim from `python3 sim.py --seed 7`; the raw table is in
`result.json`.

```
sheet:   sheets/deepseek-deepseek-v4-flash.json
model:   deepseek/deepseek-v4-flash
fetched: 2026-08-31T04:00:21+00:00
lanes:   17 on the sheet, 17 with p50 timing
seed:    7    requests per cell: 10000
prompt:  4000 tokens every request; read rate 18.0 tok/s

── talk  (a person is watching the stream) ───────────────────────────────────────
   lambda=90 s/$   visible=400  hidden=0  q_need=0.9  tools=no
   15/17 lanes past the shared capability gate: DigitalOcean, StreamLake, DeepInfra, GMICloud, SiliconFlow, Alibaba, Venice, Novita, NextBit, AtlasCloud, Baidu, CoreWeave, Parasail, Phala, Cloudflare

   policy                  TTFT p50/p90/p99 (ms)    answer p50/p90/p99 (s)  perceived p50/p90 (s)     $/1k  hedge%  modal lane
   ---------------------------------------------------------------------------------------------------------------------------
   openrouter-default     1462     4694    49607    13.84   69.22   120.61       24.17      69.69    0.565     0.0  DigitalOcean (19%)
   strike-ledger           767     1773    11819     7.35   13.62    32.22       22.99      24.07    1.148     0.0  Parasail (37%)
   sheet-only              635     6450    27500    10.52   17.29    40.23       22.87      28.96    0.682     0.0  CoreWeave (99%)
   belief+hedge            753     1718     7063     7.22   15.83    22.00       22.98      23.96    0.690    11.7  Parasail (67%)

   floor on perceived time here is 22.22 s (400 visible tokens at 18 tok/s), so any baseline already at 31.75 s p90 or better puts a 30% improvement out of reach
     openrouter-default  refused-and-retried 3.0% of requests   hedge waste $0.0000/1k   lanes used: 15
     strike-ledger       refused-and-retried 2.6% of requests   hedge waste $0.0000/1k   lanes used: 15
     sheet-only          refused-and-retried 1.4% of requests   hedge waste $0.0000/1k   lanes used: 3
     belief+hedge        refused-and-retried 1.3% of requests   hedge waste $0.0052/1k   lanes used: 8

── work  (critical-path tool loop, nobody reads the tokens) ──────────────────────
   lambda=90 s/$   visible=0  hidden=2000  q_need=0.97  tools=yes
   5/17 lanes past the shared capability gate: DigitalOcean, DeepInfra, NextBit, CoreWeave, Phala

   policy                  TTFT p50/p90/p99 (ms)    answer p50/p90/p99 (s)  perceived p50/p90 (s)     $/1k  hedge%  modal lane
   ---------------------------------------------------------------------------------------------------------------------------
   openrouter-default     1190     3492   353959    79.55  421.33   655.42       79.55     421.33    0.852     0.0  DigitalOcean (39%)
   strike-ledger           857     4320   155901    61.87  323.95   588.99       61.87     323.95    0.957     0.0  CoreWeave (56%)
   sheet-only             1736     3710    50015    32.67   61.18   105.11       32.67      61.18    1.652     0.0  Phala (95%)
   belief+hedge           2066     4971    37149    38.87   64.37   118.69       39.14      65.35    1.157     2.0  NextBit (96%)

   no visible tokens, so the reading-rate ceiling does not bind and perceived time is wall time
     openrouter-default  refused-and-retried 3.0% of requests   hedge waste $0.0000/1k   lanes used: 5
     strike-ledger       refused-and-retried 2.1% of requests   hedge waste $0.0000/1k   lanes used: 5
     sheet-only          refused-and-retried 4.6% of requests   hedge waste $0.0000/1k   lanes used: 3
     belief+hedge        refused-and-retried 1.7% of requests   hedge waste $0.0063/1k   lanes used: 5

── offpath  (background, nobody is waiting, price wins outright) ─────────────────
   lambda=0 s/$   visible=0  hidden=2000  q_need=0.97  tools=yes
   5/17 lanes past the shared capability gate: DigitalOcean, DeepInfra, NextBit, CoreWeave, Phala

   policy                  TTFT p50/p90/p99 (ms)    answer p50/p90/p99 (s)  perceived p50/p90 (s)     $/1k  hedge%  modal lane
   ---------------------------------------------------------------------------------------------------------------------------
   openrouter-default     1198     3617   361511    81.28  433.40   659.89       81.28     433.40    0.853     0.0  DigitalOcean (40%)
   strike-ledger           836     4196   120143    60.73  320.62   574.73       60.73     320.62    0.963     0.0  CoreWeave (58%)
   sheet-only             1769     3924    50394    33.04   61.78   109.88       33.04      61.78    1.660     0.0  Phala (95%)
   belief+hedge            753     1335    65077    72.38   88.69   218.59       72.55      89.76    0.723     0.0  DeepInfra (99%)

   no visible tokens, so the reading-rate ceiling does not bind and perceived time is wall time
     openrouter-default  refused-and-retried 3.4% of requests   hedge waste $0.0000/1k   lanes used: 5
     strike-ledger       refused-and-retried 2.1% of requests   hedge waste $0.0000/1k   lanes used: 5
     sheet-only          refused-and-retried 5.2% of requests   hedge waste $0.0000/1k   lanes used: 3
     belief+hedge        refused-and-retried 1.4% of requests   hedge waste $0.0000/1k   lanes used: 5

── ship gate ── p90 perceived improves >= 30% at <= 3% cost, against BOTH baselines

   scenario  vs baseline             p90 perceived      $ per 1k   verdict
   -------------------------------------------------------------------------
   talk      openrouter-default             +65.6%        +22.1%   FAIL (cost)
   talk      strike-ledger                   +0.5%        -39.9%   FAIL (p90)
   work      openrouter-default             +84.5%        +35.9%   FAIL (cost)
   work      strike-ledger                  +79.8%        +20.9%   FAIL (cost)
   offpath   openrouter-default             +79.3%        -15.3%   PASS
   offpath   strike-ledger                  +72.0%        -25.0%   PASS

   2/6 comparisons pass the gate as written.

   wrote result.json
```

## Reading the run

**`talk`.** Perceived time is 22.22 s of reading plus the first token, for every
policy, because every lane the design or the ledger will choose writes faster
than 18 tok/s. The four policies separate on the p90 of the wait before the
first token and on nothing else. `sheet-only` is the instructive one: it picks
CoreWeave 99% of the time because CoreWeave has the best p50 first token on the
whole sheet (587 ms) — and CoreWeave's p90 is 5247 ms and its p99 is 12.5 s, so
`sheet-only` posts the worst p90 perceived of the three competent policies
(28.96 s). **That is the tail term earning its keep**: the belief learns
CoreWeave's spread and moves to Parasail, which starts 63 ms later at the median
(650 ms against 587 ms) and 3.9 seconds sooner at the ninetieth percentile
(1381 ms against 5247 ms).

`belief+hedge` also has the best p99 answer time in `talk` by a wide margin
(22.00 s against 32.22 s for the strike ledger) and that is the hedge, firing on
11.7% of requests. Its cost is the alternative's request price plus the tokens
the cancelled loser had already written; the second of those, the part the
ledger will record as `hedge_waste`, is $0.0052 per thousand requests, or 0.75%
of the bill. The whole `talk` bill lands at $0.690 per thousand against
`sheet-only`'s $0.682 with no hedging at all. **In `talk` the hedge is cheap and
it works.** It is `work` where the same mechanism goes wrong.

**`work`.** Only 5 of 17 lanes honour a tool call, so the capability gate does
most of the work before any policy has an opinion, and it is given to all four.
Of those five, the two quantized `unknown` lanes (DigitalOcean and Phala) sit at
an assumed 0.95 accept rate and fall below `q_need = 0.97` once they have been
measured. `sheet-only` keeps using Phala anyway — it has no quality axis — and
posts a 4.6% retry rate against `belief+hedge`'s 1.7%, at $1.65 per thousand
against $1.16. **The quality gate is the design's cleanest win in this table**:
it costs about 4 seconds of p90 (65.4 s against `sheet-only`'s 61.2 s) and buys
a 30% lower bill and a third fewer refused answers.

**`offpath`.** Lambda is zero, so price decides and time is only the tie-break.
The design picks DeepInfra — the cheapest lane that survives the quality gate —
and comes out 15% cheaper than `openrouter-default` and 25% cheaper than the
strike ledger while also beating both on p90, because the lane the price
weighting actually loves, DigitalOcean at 6 tok/s, is refused on quality. This
is the only scenario where the ship gate passes, and it passes on both clauses
at once.

## The ship gate, stated plainly

| scenario | vs | p90 perceived | $ per 1k | verdict |
|---|---|---:|---:|---|
| talk | openrouter-default | +65.6% | +22.1% | **FAIL** — cost |
| talk | strike-ledger | +0.5% | -39.9% | **FAIL** — p90 |
| work | openrouter-default | +84.5% | +35.9% | **FAIL** — cost |
| work | strike-ledger | +79.8% | +20.9% | **FAIL** — cost |
| offpath | openrouter-default | +79.3% | -15.3% | **PASS** |
| offpath | strike-ledger | +72.0% | -25.0% | **PASS** |

Two of six. Nothing here is rounded in the design's favour; the percentages are
printed by `sim.py` from the raw sums.

The two `talk` rows are worth reading together, because they say something the
gate as written cannot express. Against `openrouter-default` the design is 65.6%
faster at the p90 and 22% more expensive. Against the strike ledger it is 0.5%
faster and **40% cheaper**. Those are both good outcomes and the gate calls both
of them failures. A gate that scores p90 and cost as two independent thresholds
will keep doing this; the design's own scalar — seconds and dollars on one axis
through lambda — is the honest way to score it, and the ship gate should be
written in those terms before it is used to decide anything.

## Is the verdict a property of the design or of the seed?

`python3 sim.py --seed 7 --sweep 8` — eight seeds, the same 10 000 requests per
cell.

```
sheet:   sheets/deepseek-deepseek-v4-flash.json
model:   deepseek/deepseek-v4-flash
fetched: 2026-08-31T04:00:21+00:00
lanes:   17 on the sheet, 17 with p50 timing
seed:    7    requests per cell: 10000
prompt:  4000 tokens every request; read rate 18.0 tok/s

── seed sweep ── is the verdict a property of the design or of the seed? ────────

   seed  scenario  design converges on         $/1k  p90 perceived  vs or-default $
   -----------------------------------------------------------------------------
   7     talk      Parasail (67%)             0.690          23.96           +22.1%
   7     work      NextBit (96%)              1.157          65.35           +35.9%
   7     offpath   DeepInfra (99%)            0.723          89.76           -15.3%
   9     talk      Parasail (65%)             0.692          23.94           +22.1%
   9     work      NextBit (99%)              1.383          63.74           +61.8%
   9     offpath   DeepInfra (99%)            0.725          89.63           -15.2%
   11    talk      Parasail (74%)             0.699          23.93           +23.9%
   11    work      DeepInfra (98%)            0.741          89.64           -12.6%
   11    offpath   DeepInfra (99%)            0.722          89.86           -14.9%
   13    talk      Parasail (65%)             0.688          23.98           +22.1%
   13    work      CoreWeave (89%)            1.341          69.76           +57.2%
   13    offpath   NextBit (99%)              1.116          66.11           +31.1%
   15    talk      Parasail (71%)             0.675          23.93           +19.3%
   15    work      NextBit (92%)              2.017          62.39          +137.3%
   15    offpath   DeepInfra (99%)            0.723          89.83           -15.2%
   17    talk      Parasail (68%)             0.702          24.00           +24.5%
   17    work      NextBit (92%)              2.011          62.87          +138.5%
   17    offpath   NextBit (99%)              1.116          65.47           +31.1%
   19    talk      Parasail (68%)             0.693          23.97           +22.7%
   19    work      CoreWeave (92%)            1.591          68.27           +86.9%
   19    offpath   DeepInfra (99%)            0.723          90.09           -14.9%
   21    talk      Parasail (64%)             0.683          23.97           +21.0%
   21    work      NextBit (98%)              1.133          66.01           +32.3%
   21    offpath   CoreWeave (99%)            1.116          71.73           +31.5%

   talk      converged on 1 different lane(s) across 8 seeds: {'Parasail': 8}
   work      converged on 3 different lane(s) across 8 seeds: {'NextBit': 5, 'DeepInfra': 1, 'CoreWeave': 2}
   offpath   converged on 3 different lane(s) across 8 seeds: {'DeepInfra': 5, 'NextBit': 2, 'CoreWeave': 1}

   talk      vs openrouter-default  passes on 0/8 seeds
   talk      vs strike-ledger       passes on 0/8 seeds
   work      vs openrouter-default  passes on 1/8 seeds
   work      vs strike-ledger       passes on 1/8 seeds
   offpath   vs openrouter-default  passes on 5/8 seeds
   offpath   vs strike-ledger       passes on 5/8 seeds
```

`talk` is stable: Parasail on 8 seeds out of 8, p90 between 23.90 s and 24.02 s,
cost between +19.3% and +25.5%. The `talk` verdict can be trusted.

`work` and `offpath` are not. The design converges on three different lanes in
each, and in `work` the cost against `openrouter-default` runs from **-12.6% to
+138.5%** depending on nothing but the seed. **`work` passes the gate on 1 seed
in 8; `offpath` on 5 in 8.** Reporting seed 7's `work` row as the result would
have been a number chosen after the fact.

The instability has one cause and it is Finding 2. The design commits to a
single lane — 89% to 99% of requests — and which lane that is depends on which
one first accumulates enough clean sightings to clear the quality gate. Because
the gate is absorbing, that early race is never revisited. A design that is
supposed to have no penalty box has one, and it is permanent.

---

## Where this model can be wrong

Every one of these is a reason to trust the *ordering* of the policies more
than the magnitudes, and to treat the ship-gate percentages as an argument for
running the live A/B rather than a substitute for it.

- **The 1% weight on the mixture tail is a choice, not a measurement.** The
  sheet publishes p50, p75, p90 and p99 and the simulator fits a log-normal
  body to p50/p90 and hangs a second component off p99. How much mass belongs
  in that component is not in the sheet. One percent reproduces the published
  p99 by construction; it is a fit to a single quantile, not evidence about the
  shape between p90 and p99. Every p99 figure in the run inherits that choice,
  and the hedge's value is most sensitive to it.

- **Lane speeds are drawn independently; real overloads correlate.** When a
  popular model is busy, several endpoints slow down together, and that is
  precisely when a hedge to the second-best lane is worth least. This simulator
  never has a bad afternoon. It therefore **overstates the hedge**, and the
  overstatement is largest exactly where the hedge looks best.

- **The sheet is a 30-minute aggregate over everybody's prompts, and the
  simulator treats it as ground truth for ours.** It is not. It mixes prompt
  sizes we do not send, regions we do not sit in, and concurrency we do not
  generate. `internal/lane/lane.go` states this as a law — "the sheet is the
  prior rather than the belief" — and the simulator quietly violates it: the
  belief policy learns from draws that came out of the sheet's own
  distribution, so it is learning a distribution it was already primed with.
  **The belief policy's advantage over `sheet-only` is understated as a result,
  because in this world the sheet is never wrong.** That is the one bias that
  runs against the design.

- **Quality accept rates are assumed from quantization, not measured.** fp4 at
  0.85, unknown at 0.95, everything else at 0.985. Nobody has measured these
  numbers on any lane. They are a plausible ordering dressed as data, and the
  quality gate — the design's cleanest win in `work`, and the cause of its
  instability — rests entirely on them. **If the true accept rates are all
  equal, the quality gate does nothing and both the win and the instability
  disappear.** This is the single assumption most worth replacing with a
  measurement before this design ships.

- **The simulator cannot see prompt-cache effects.** `request_price` carries the
  cache-aware term from Part II section 3, and it never fires: exactly one lane
  on this sheet advertises `supports_implicit_caching` (Azure) and it is refused
  by the status gate. So the whole argument that cache state makes price
  path-dependent, and that the incumbent lane is cheaper by the cache term and
  therefore the router does not flap, **is untested here**. On a model whose
  lanes do cache, the results could differ substantially.

- **A refused answer costs exactly one retry on the next lane, at most three
  attempts.** Real refusals are messier: a truncated answer may be usable, a
  malformed tool call may be repaired in-loop, and some failures repeat on the
  next lane. The retry model is uniform across all four policies, so it should
  not bias the comparison, but it does set the absolute cost of quality.

- **One model, one sheet, one afternoon.** Seventeen lanes of
  `deepseek/deepseek-v4-flash` at 04:00 UTC on 2026-08-31. A model with three
  lanes, or a sheet taken during an incident, is a different experiment. The
  spread that makes lane choice worth anything — 4.9x on first token, 12x on
  throughput at roughly the same price — is a property of *this* sheet.

- **Not implemented, and it would change the numbers.** Part I section 3 bounds
  exploration: "a lane may only be sampled into the top-3 if its prior p50 is
  within 2x of the best lane's". The scope of this lab did not include it, so
  DigitalOcean at 6 tok/s is occasionally explored on the user's time in `work`
  and `talk`. Adding it would improve `belief+hedge`'s p90 slightly and would
  not touch the cost clause, which is what actually fails.

- **`--sweep 8` is eight seeds, not a confidence interval.** It is enough to
  show that the `work` verdict is unstable. It is not enough to put a number on
  how unstable.

## What to fix before running this again

1. Choose the quality prior, the Beta half-life and `q_need` together so that a
   good lane can actually clear the gate and a condemned one can come back
   (Findings 1 and 2). Until then the `work` result is a coin flip.
2. Budget the hedge in money or as a fraction of the expected answer, not in
   hedges per minute of wall clock (Finding 3).
3. Restate the ship gate on the design's own scalar — seconds and dollars on one
   axis through lambda — instead of two independent thresholds that call a 40%
   cost saving a failure.
4. Measure a real accept rate per lane, even roughly. It is the assumption the
   biggest result rests on.

Then re-run `python3 sim.py --seed 7 --sweep 8`. If `work` passes on 8 seeds of
8, `live.sh` is worth the money. It has never been run.
