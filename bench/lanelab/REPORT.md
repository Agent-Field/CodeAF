# lanelab — results and verdict

*Simulated 2026-08-31 against the OpenRouter endpoint sheet for
`deepseek/deepseek-v4-flash`, fetched the same day at 04:00:21Z. 17 lanes,
all with published p50 timing. `python3 sim.py --seed 7 --json result.json`,
10 000 requests per policy per scenario, 0.8 s wall, byte-reproducible.*

**This file is a reference model, not the decision.** `sim.py` is Python and it
re-implements the design; it is here to find arithmetic that cannot work before
anyone writes it in Go. **The SHIP DECISION is taken on the Go simulator in
`bench/lanelab/gosim`, which drives the real registry rather than a
re-implementation of it.** Where the two disagree, the Go run is the one that
counts and this one is the bug report.

## Two corrections since the first run of this lab

Both were found here. C2 is now in the shipped code — `lane.PerceivedSeconds`
in `internal/lane/lane.go` computes exactly the corrected form — and C3 is a
rule about how this lab and the Go one are read. Every number below is from
after both; nothing in this file survives from the earlier objective.

**C2 — the objective is a WAIT, not a duration.** It used to be

```
T = ttft + hidden/rate + visible / min(rate, ReadRate)
```

and that last term counts the time a person spends *reading*, which no router
can remove: 400 visible tokens at 18 tok/s is 22.22 seconds of reading whichever
lane wrote them. It is now, exactly as `lane.PerceivedSeconds` computes it:

```
T = ttft + hidden/rate + visible · max(0, 1/rate − 1/ReadRate)
```

— the wait, and only the wait. The visible term is the *catch-up*: what a lane
costs you by writing slower than you read, zero for any lane at or above the
reading rate. **The correction subtracts exactly 22.222 s from every `talk`
number and changes nothing in `work` or `offpath`, which have no visible
tokens.** It moved no ranking and no lane choice — the removed term is the same
constant for every lane — so the run is directly comparable to the old one, and
the rest of the `talk` block proves it: same modal lanes at the same shares
(Parasail 67%, CoreWeave 99%, DigitalOcean 19%), same TTFT percentiles, same
bills to the tenth of a cent. What the correction moved is every RATIO, because
the old ratios were ratios of mostly reading:

| `talk`, p90 | old objective | corrected |
|---|---:|---:|
| `openrouter-default` | 69.69 s | 47.47 s |
| `strike-ledger` | 24.07 s | 1.85 s |
| `sheet-only` | 28.96 s | 6.74 s |
| `belief+hedge` | 23.96 s | **1.74 s** |
| design vs `strike-ledger` | **+0.5%** | **+5.9%** |
| design vs `openrouter-default` | **+65.6%** | **+96.3%** |

Read the last two rows together and the size of the correction is plain: the
same two policies, the same seed, the same draws, and a win reported as half a
percent is really six, while a win reported as 66% is really 96%.

The constant does its damage by compressing every ratio toward zero, and the
`talk` p50 row is where that is easiest to see. On the old metric the three
competent policies read 22.99, 22.87 and 22.98 seconds — three numbers a
reasonable person would call the same. Their actual waits are 0.770, 0.646 and
0.755 seconds, which differ by 19%. **A ship gate written as a percentage of a
number that is 96% reading is a gate that cannot tell a good router from a bad
one**, in either direction, and that is what C2 removes.

**C3 — the ship gate is stated in the design's own economics.** The old gate was
"p90 improves ≥ 30% at ≤ 3% cost, against both baselines". The 30% is the
design's own number and it stays. **The 3% was a number from nowhere** — nobody
derived it and no scenario means anything by it — and it is gone. In its place,
per scenario, against the mechanism this design proposes to retire:

> **speed** — the scenario's p90 improves by at least 30% against `strike-ledger`;
> for `talk` that p90 is the FIRST TOKEN, because above the reading rate every
> lane is the same speed to a person and the whole prize is the empty line
> before the stream starts.
>
> **money** — `Δ$ per request ≤ Δ(mean wait) / λ`. The design already prices a
> second: λ, in seconds per dollar. A router that spends a dollar and buys more
> than λ seconds has made a good trade by the design's own arithmetic. At
> λ = 0 the right-hand side is zero and the rule degenerates to "it must not
> cost more than the baseline", which is what background work should demand.

The mean and not the p90 is on the left of that division because λ prices the
seconds actually saved across a run, and a percentile is not a quantity you can
divide by a rate and get dollars per request out of.

**The two clauses together flip `work`.** Under the old flat clause the design
failed `work` on cost against both baselines and passed on 1 seed in 8. Under
the λ rule it passes `work` on **8 seeds in 8**: at λ = 90 s/$ the 68 seconds it
saves on the mean request are worth $0.758, and it spends $0.000200. `offpath`
is unchanged at 5 in 8 — λ is zero there, so the money clause is the strict one
and the seed instability decides it. `talk` passes on 0 of 8, as it did before,
now for a reason that is actually about the first token.

---

## Bottom line

**The design passes its corrected gate in 2 of 3 scenarios at seed 7 —
`work` and `offpath` — and fails `talk`. Across eight seeds: `talk` 0/8,
`work` 8/8, `offpath` 5/8.** The old gate passed 2 of its 6 comparisons, both
of them in `offpath`. Almost all of the difference is C3 rather than the
design: the λ clause prices seconds, which the flat 3% clause refused to look
at, and `work` is a scenario where the seconds are worth thousands of times the
dollars.

**Recommendation: still do not ship on this evidence.** Three reasons, none of
which the corrections touched.

1. **The λ money clause barely binds where a person is waiting.** At λ = 90 s/$
   on answers that take a minute, the seconds saved are worth hundreds of times
   the dollars spent — `work`'s money budget is $0.758 per request against
   $0.000200 actually spent. So in `work` the gate is a speed gate with a
   rounding error attached, and the cost blow-up the old clause caught by
   accident (Finding 3: $1.157 to $2.017 per thousand between seeds) now sails
   through. It is caught by the sweep instead, which is where it belongs, but
   the gate alone will not catch it.
2. **`sheet-only` passes `work` too, by a wider margin** (+81.1% p90 against the
   design's +79.8%). The gate therefore does not establish that the belief, the
   prune and the hedge earn their place over the prior alone in `work`. What
   separates them there is the bill and the refusals — $1.157 against $1.652 per
   thousand, 1.7% retried against 4.6% — and neither is a gate clause.
3. **The three findings below are unchanged, and they are in the design's
   constants.** The quality gate cannot be reached from its own prior, it is
   absorbing once it fires, and the hedge budget is denominated in wall clock.
   `work` and `offpath` still converge on three different lanes across eight
   seeds.

The design's robust win is against `openrouter-default`, and it is not subtle:
+96.3% on the p90 wait in `talk`, +84.5% in `work`, +79.3% in `offpath`.
Inverse-square price weighting keeps routing to DigitalOcean, which writes at
6 tok/s. Against the mechanism it actually replaces it wins by 79.8% in `work`,
72.0% in `offpath`, and 5.9% in `talk` — where 30% is required.

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

This is what makes the `work` lane choice a coin flip: whichever fp8 lane clears
0.97 first becomes the only lane, permanently. The corrected gate now passes
`work` on every seed anyway, which means **the instability has stopped being a
verdict and started being a hidden cost** — see the first reason in the bottom
line.

### Finding 3 — the hedge budget is denominated in the wrong currency

The budget is six hedges per minute of wall time. A `work` request takes about
40 seconds, so it refills four tokens of its own budget. The hedge is unbudgeted
for exactly the requests that are expensive to hedge.

The measured effect is bimodal. At seeds 7 and 11 the hedge fires on 1-2% of
`work` requests. At seeds 15 and 17 it fires on **95%**, and the bill goes from
$1.157 to $2.017 per thousand requests to buy 3 seconds of p90 wait (65.35 s to
62.39 s). The switch is which lane lands in second place: `t*` is clamped at a
700 ms floor, and on a primary lane whose own median first token is 2.06 s the
expected remaining wait already exceeds the threshold at that floor, so the
clamp licenses a hedge on nearly every request. λ does not catch it, because at
90 s/$ three seconds of p90 are worth far more than the $0.00086 per request the
hedging costs — and under C3's money clause, correctly stated, that trade
formally passes. **It is a bad trade that the design's own economics endorse**,
which is a defect in the hedge budget rather than in the gate.

The fix is not the clamp. A hedge that saves 1.4 s of first token on a 36-second
answer is a bad trade whatever the deadline says. The budget needs a spend cap
or a "fraction of the answer" test, not a wall-clock bucket.

---

## The run

Pasted verbatim from `python3 sim.py --seed 7 --json result.json` (0.8 s wall);
the raw table is in `result.json`.

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

   policy                  TTFT p50/p90/p99 (ms)    answer p50/p90/p99 (s)       wait p50/p90 (s)     $/1k  hedge%  modal lane
   ---------------------------------------------------------------------------------------------------------------------------
   openrouter-default     1462     4694    49607    13.84   69.22   120.61        1.95      47.47    0.565     0.0  DigitalOcean (19%)
   strike-ledger           767     1773    11819     7.35   13.62    32.22        0.77       1.85    1.148     0.0  Parasail (37%)
   sheet-only              635     6450    27500    10.52   17.29    40.23        0.65       6.74    0.682     0.0  CoreWeave (99%)
   belief+hedge            753     1718     7063     7.22   15.83    22.00        0.75       1.74    0.690    11.7  Parasail (67%)

   14/15 gated lanes write at or above 18 tok/s at their median, and on those the 400 visible tokens add NOTHING to the wait — they are read as they arrive. So this scenario is decided by the first token, which is what its gate reads.
     openrouter-default  refused-and-retried 3.0% of requests   hedge waste $0.0000/1k   lanes used: 15
     strike-ledger       refused-and-retried 2.6% of requests   hedge waste $0.0000/1k   lanes used: 15
     sheet-only          refused-and-retried 1.4% of requests   hedge waste $0.0000/1k   lanes used: 3
     belief+hedge        refused-and-retried 1.3% of requests   hedge waste $0.0052/1k   lanes used: 8

── work  (critical-path tool loop, nobody reads the tokens) ──────────────────────
   lambda=90 s/$   visible=0  hidden=2000  q_need=0.97  tools=yes
   5/17 lanes past the shared capability gate: DigitalOcean, DeepInfra, NextBit, CoreWeave, Phala

   policy                  TTFT p50/p90/p99 (ms)    answer p50/p90/p99 (s)       wait p50/p90 (s)     $/1k  hedge%  modal lane
   ---------------------------------------------------------------------------------------------------------------------------
   openrouter-default     1190     3492   353959    79.55  421.33   655.42       79.55     421.33    0.852     0.0  DigitalOcean (39%)
   strike-ledger           857     4320   155901    61.87  323.95   588.99       61.87     323.95    0.957     0.0  CoreWeave (56%)
   sheet-only             1736     3710    50015    32.67   61.18   105.11       32.67      61.18    1.652     0.0  Phala (95%)
   belief+hedge           2066     4971    37149    38.87   64.37   118.69       39.14      65.35    1.157     2.0  NextBit (96%)

   no visible tokens, so nothing is read as it arrives and the wait is the whole answer
     openrouter-default  refused-and-retried 3.0% of requests   hedge waste $0.0000/1k   lanes used: 5
     strike-ledger       refused-and-retried 2.1% of requests   hedge waste $0.0000/1k   lanes used: 5
     sheet-only          refused-and-retried 4.6% of requests   hedge waste $0.0000/1k   lanes used: 3
     belief+hedge        refused-and-retried 1.7% of requests   hedge waste $0.0063/1k   lanes used: 5

── offpath  (background, nobody is waiting, price wins outright) ─────────────────
   lambda=0 s/$   visible=0  hidden=2000  q_need=0.97  tools=yes
   5/17 lanes past the shared capability gate: DigitalOcean, DeepInfra, NextBit, CoreWeave, Phala

   policy                  TTFT p50/p90/p99 (ms)    answer p50/p90/p99 (s)       wait p50/p90 (s)     $/1k  hedge%  modal lane
   ---------------------------------------------------------------------------------------------------------------------------
   openrouter-default     1198     3617   361511    81.28  433.40   659.89       81.28     433.40    0.853     0.0  DigitalOcean (40%)
   strike-ledger           836     4196   120143    60.73  320.62   574.73       60.73     320.62    0.963     0.0  CoreWeave (58%)
   sheet-only             1769     3924    50394    33.04   61.78   109.88       33.04      61.78    1.660     0.0  Phala (95%)
   belief+hedge            753     1335    65077    72.38   88.69   218.59       72.55      89.76    0.723     0.0  DeepInfra (99%)

   no visible tokens, so nothing is read as it arrives and the wait is the whole answer
     openrouter-default  refused-and-retried 3.4% of requests   hedge waste $0.0000/1k   lanes used: 5
     strike-ledger       refused-and-retried 2.1% of requests   hedge waste $0.0000/1k   lanes used: 5
     sheet-only          refused-and-retried 5.2% of requests   hedge waste $0.0000/1k   lanes used: 3
     belief+hedge        refused-and-retried 1.4% of requests   hedge waste $0.0000/1k   lanes used: 5

── ship gate ── against strike-ledger: the scenario's p90 improves by >= 30%,
   AND the extra dollars per request are no more than the mean seconds saved, priced at lambda

   scenario  policy              speed metric       improve   extra $/req  budget $/req   verdict
   ----------------------------------------------------------------------------------------------
   talk      openrouter-default  p90 first token    -164.8%     -0.000583     -0.122135   FAIL (p90+cost)
   talk      sheet-only          p90 first token    -263.9%     -0.000466     -0.011842   FAIL (p90+cost)
   talk      belief+hedge        p90 first token      +3.1%     -0.000458      0.005415   FAIL (p90)
   work      openrouter-default  p90 wait            -30.1%     -0.000105     -0.781333   FAIL (p90+cost)
   work      sheet-only          p90 wait            +81.1%     +0.000695      0.838522   PASS
   work      belief+hedge        p90 wait            +79.8%     +0.000200      0.758381   PASS
   offpath   openrouter-default  p90 wait            -35.2%     -0.000110      0.000000   FAIL (p90)
   offpath   sheet-only          p90 wait            +80.7%     +0.000696      0.000000   FAIL (cost)
   offpath   belief+hedge        p90 wait            +72.0%     -0.000241      0.000000   PASS

   the design passes in 2/3 scenarios; 3/9 rows pass overall.

   wrote result.json
```

## Reading the run

**`talk`.** 14 of the 15 gated lanes write faster than a person reads, so on all
but one of them the 400 visible tokens add nothing at all to the wait, and the
whole scenario is decided by the first token. That is why the gate reads the p90
TTFT here: gating on the wait would grade the design partly on a term it has no
way to move. `sheet-only` is the instructive arm: it picks CoreWeave 99% of the
time because CoreWeave has the best p50 first token on the whole sheet (587 ms) —
and CoreWeave's p90 is 5247 ms and its p99 is 12.5 s, so `sheet-only` posts a
p90 TTFT of 6450 ms, worse than the strike ledger's 1773 ms. **That is the tail
term earning its keep**: the belief learns CoreWeave's spread and moves to
Parasail, which starts 63 ms later at the median (650 ms against 587 ms) and 3.9
seconds sooner at the ninetieth percentile (1381 ms against 5247 ms).

Against the ledger, though, Parasail is where the ledger also ends up, and the
design's p90 first token is 1718 ms against 1773 ms — **+3.1%, against a
requirement of 30%.** It is a clean loss on the clause and it is not close. What
the design does win in `talk` it wins elsewhere: the p99 answer time is 22.00 s
against the ledger's 32.22 s (that is the hedge, firing on 11.7% of requests),
and the bill is $0.690 per thousand against $1.148 — **40% cheaper**. The gate
does not ask for either.

`belief+hedge`'s hedge waste in `talk` — the tokens the cancelled loser had
already written — is $0.0052 per thousand requests, 0.75% of the bill. **In
`talk` the hedge is cheap and it works.** It is `work` where the same mechanism
goes wrong.

**`work`.** Only 5 of 17 lanes honour a tool call, so the capability gate does
most of the work before any policy has an opinion, and it is given to all four.
Of those five, the two quantized `unknown` lanes (DigitalOcean and Phala) sit at
an assumed 0.95 accept rate and fall below `q_need = 0.97` once they have been
measured. `sheet-only` keeps using Phala anyway — it has no quality axis — and
posts a 4.6% retry rate against `belief+hedge`'s 1.7%, at $1.652 per thousand
against $1.157. **The quality gate is the design's cleanest win in this table**:
it costs about 4 seconds of p90 wait (65.35 s against `sheet-only`'s 61.18 s)
and buys a 30% lower bill and a third fewer refused answers. Both policies pass
the gate; only one of them is cheap.

**`offpath`.** λ is zero, so price decides and time is only the tie-break, and
the money clause is the strict one: cost must not exceed the baseline's. The
design picks DeepInfra — the cheapest lane that survives the quality gate — and
comes out $0.000241 per request cheaper than the strike ledger while also
beating it on p90 wait by 72.0%, because the lane the price weighting actually
loves, DigitalOcean at 6 tok/s, is refused on quality. `sheet-only` beats the
design on p90 here (+80.7%) and fails anyway, on money, which is the clause
doing its job.

## The ship gate, stated plainly

Against `strike-ledger`, the mechanism this design proposes to retire. Every
policy is graded, so the table also shows what today's default and the prior
alone would cost. Nothing is rounded in the design's favour; the figures are
printed by `sim.py` from the raw sums.

| scenario | policy | speed metric | improve | extra $/req | budget $/req | verdict |
|---|---|---|---:|---:|---:|---|
| talk | openrouter-default | p90 first token | -164.8% | -0.000583 | -0.122135 | **FAIL** — p90 + cost |
| talk | sheet-only | p90 first token | -263.9% | -0.000466 | -0.011842 | **FAIL** — p90 + cost |
| talk | belief+hedge | p90 first token | +3.1% | -0.000458 | 0.005415 | **FAIL** — p90 |
| work | openrouter-default | p90 wait | -30.1% | -0.000105 | -0.781333 | **FAIL** — p90 + cost |
| work | sheet-only | p90 wait | +81.1% | +0.000695 | 0.838522 | **PASS** |
| work | belief+hedge | p90 wait | +79.8% | +0.000200 | 0.758381 | **PASS** |
| offpath | openrouter-default | p90 wait | -35.2% | -0.000110 | 0.000000 | **FAIL** — p90 |
| offpath | sheet-only | p90 wait | +80.7% | +0.000696 | 0.000000 | **FAIL** — cost |
| offpath | belief+hedge | p90 wait | +72.0% | -0.000241 | 0.000000 | **PASS** |

Two rows are worth reading slowly.

`talk` / `openrouter-default` fails on **both** clauses while being $0.000583
per request *cheaper* than the ledger. That is the λ clause working: the default
loses 10.99 seconds of mean wait, which at 90 s/$ is worth $0.122, and it hands
back half a tenth of a cent. Under the old flat 3% rule that row would have been
scored a cost win. **A gate that prices seconds cannot be fooled by a cheap slow
lane, and the old one could.**

`work` / `sheet-only` passes. The prior alone, with no belief and no hedge,
clears the gate the design set for itself, with a bigger p90 margin than the
design and inside its money budget. The gate is a floor the design has to clear,
not evidence that its machinery is what cleared it.

## Is the verdict a property of the design or of the seed?

`python3 sim.py --seed 7 --sweep 8` — eight seeds, the same 10 000 requests per
cell, 6.5 s wall.

```
sheet:   sheets/deepseek-deepseek-v4-flash.json
model:   deepseek/deepseek-v4-flash
fetched: 2026-08-31T04:00:21+00:00
lanes:   17 on the sheet, 17 with p50 timing
seed:    7    requests per cell: 10000
prompt:  4000 tokens every request; read rate 18.0 tok/s

── seed sweep ── is the verdict a property of the design or of the seed? ────────

   seed  scenario  design converges on         $/1k   p90 wait  p90 ttft ms     gate
   ------------------------------------------------------------------------------
   7     talk      Parasail (67%)             0.690       1.74         1718     FAIL
   7     work      NextBit (96%)              1.157      65.35         4971     PASS
   7     offpath   DeepInfra (99%)            0.723      89.76         1335     PASS
   9     talk      Parasail (65%)             0.692       1.71         1696     FAIL
   9     work      NextBit (99%)              1.383      63.74         4912     PASS
   9     offpath   DeepInfra (99%)            0.725      89.63         1376     PASS
   11    talk      Parasail (74%)             0.699       1.70         1687     FAIL
   11    work      DeepInfra (98%)            0.741      89.64         1411     PASS
   11    offpath   DeepInfra (99%)            0.722      89.86         1372     PASS
   13    talk      Parasail (65%)             0.688       1.76         1722     FAIL
   13    work      CoreWeave (89%)            1.341      69.76         6054     PASS
   13    offpath   NextBit (99%)              1.116      66.11         4808     FAIL
   15    talk      Parasail (71%)             0.675       1.70         1675     FAIL
   15    work      NextBit (92%)              2.017      62.39         4741     PASS
   15    offpath   DeepInfra (99%)            0.723      89.83         1379     PASS
   17    talk      Parasail (68%)             0.702       1.77         1741     FAIL
   17    work      NextBit (92%)              2.011      62.87         4791     PASS
   17    offpath   NextBit (99%)              1.116      65.47         4914     FAIL
   19    talk      Parasail (68%)             0.693       1.75         1723     FAIL
   19    work      CoreWeave (92%)            1.591      68.27         5010     PASS
   19    offpath   DeepInfra (99%)            0.723      90.09         1353     PASS
   21    talk      Parasail (64%)             0.683       1.75         1711     FAIL
   21    work      NextBit (98%)              1.133      66.01         4889     PASS
   21    offpath   CoreWeave (99%)            1.116      71.73         6797     FAIL

   talk      converged on 1 different lane(s) across 8 seeds: {'Parasail': 8}
   work      converged on 3 different lane(s) across 8 seeds: {'NextBit': 5, 'DeepInfra': 1, 'CoreWeave': 2}
   offpath   converged on 3 different lane(s) across 8 seeds: {'DeepInfra': 5, 'NextBit': 2, 'CoreWeave': 1}

   talk      vs strike-ledger   passes on 0/8 seeds
   work      vs strike-ledger   passes on 8/8 seeds
   offpath   vs strike-ledger   passes on 5/8 seeds
```

`talk` is stable and stably a failure: Parasail on 8 seeds out of 8, p90 wait
between 1.70 s and 1.77 s, p90 first token between 1675 ms and 1741 ms, and the
gate refused on every one.

`work` now passes on 8 seeds of 8 — and the design still converges on three
different lanes across those eight seeds, with the bill running from $0.741 to
$2.017 per thousand. **The gate no longer sees that**, because at λ = 90 the
seconds are worth so much more than the dollars that a 2.7x swing in the bill is
inside the budget every time. The instability is real, it is Finding 2, and the
sweep is now the only thing in this lab that reports it.

`offpath` passes on 5 of 8. λ is zero there, so the money clause is exact, and
the three seeds that fail are the three where the absorbing quality gate locked
the design onto NextBit or CoreWeave at $1.116 per thousand instead of DeepInfra
at $0.723. Which lane it locks onto depends on which one first accumulates
enough clean sightings to clear the quality gate, and because the gate is
absorbing that early race is never revisited. A design that is supposed to have
no penalty box has one, and it is permanent.

## Go simulator (shipped code)

*Run 2026-08-31 from the repository root, against the same sheet fixture:*

```sh
go run ./bench/lanelab/gosim -json bench/lanelab/gosim/result.json
```

*Three scenarios × three policies × eight seeds × 2000 requests = 144 000
requests over a real socket, **57m51s** wall (3470.9 s for the whole process,
compilation included). The raw table, the ship gate and the per-seed verdicts
are in `gosim/result.json`.*

**This is the run the ship decision is taken on.** It drives `lane.Default()`'s
real ledger — primed from this directory's sheet fixture at `lane.SheetWeight` —
the real chooser, the real `lane.Watch` and the real `lane.DefaultBudget()`,
against `internal/lane/lanestub`, with the preference travelling as
`provider.order`/`only`/`ignore` and the serving lane read off the chunks that
come back. Nothing in it re-implements the design.

```
sheet:    bench/lanelab/sheets/deepseek-deepseek-v4-flash.json
model:    deepseek/deepseek-v4-flash
fetched:  2026-08-31T04:00:21+00:00
lanes:    17 on the sheet, 17 with p50 timing
seeds:    [7 9 11 13 15 17 19 21]    requests per cell per seed: 2000    pooled per cell: 16000
prompt:   4000 tokens every request; read rate 18 tok/s
wire:     100× faster than the world; 40 tokens streamed per answer (the rate filter's floor is 32)
script:   the lane the router chose on request 0 goes to a 4s first token at request 500 of every run and recovers at request 1500

   NOTE: the `strike` arm (today's velocity ledger with sort: latency) is NOT RUN:
         `velocityLedger`, its `observe` and its `preferences` are unexported and
         the one process-wide instance has no exported reset, so a bench cannot
         prime it, clear it between seeds, or drive it except through a whole
         provider.Client — and its cooldown reads the wall clock, which a run on
         a divided clock cannot move. Reaching it needs a seam added to
         internal/provider, which this lane may not touch. The gate below is
         therefore taken against `default` and NOT against the mechanism the design
         retires.

── talk  (a person is watching the stream) ───────────────────────────────────────
   lambda=90 s/$   visible=400  hidden=0  q_need=0.9  tools=no
   15/17 lanes past the SHIPPED capability gate: DigitalOcean, StreamLake, DeepInfra, GMICloud, SiliconFlow, Alibaba, Venice, Novita, NextBit, Baidu, CoreWeave, Parasail, Phala, Azure, Cloudflare
   the lane the router chose first, and therefore the lane this scenario breaks: Parasail

   policy                  TTFT p50/p90/p99 (ms)    answer p50/p90/p99 (s)       wait p50/p90 (s)     $/1k  hedge%  modal lane
   ---------------------------------------------------------------------------------------------------------------------------
   default                1573     4791    50951    13.86   70.86   118.88        2.17      49.39    0.558     0.0  DigitalOcean (20%)
   belief                  864     1769    14323    13.48   18.10    33.74        0.87       1.86    0.542     0.0  DeepInfra (57%)
   belief+hedge            870     1767    14079    13.35   17.95    30.34        0.87       1.82    0.593     8.9  DeepInfra (56%)

   14/15 gated lanes write at or above 18 tok/s at their median, and on those the 400 visible tokens add NOTHING to the wait — they are read as they arrive. So this scenario is decided by the first token, which is what its gate reads.
     default             wait p99    97.07 s   $0.000558/request   0.0 hedges per 100   0 streams abandoned   refused-and-retried 2.6%   hedge waste $0.0000/1k   lanes used: 14   socket median 107 ms of world time
     belief              wait p99    18.85 s   $0.000542/request   0.0 hedges per 100   0 streams abandoned   refused-and-retried 1.6%   hedge waste $0.0000/1k   lanes used: 11   socket median 97 ms of world time
     belief+hedge        wait p99    17.46 s   $0.000593/request   8.9 hedges per 100   1277 streams abandoned   refused-and-retried 1.6%   hedge waste $0.0512/1k   lanes used: 10   socket median 98 ms of world time

── work  (critical-path tool loop, nobody reads the tokens) ──────────────────────
   lambda=90 s/$   visible=0  hidden=2000  q_need=0.97  tools=yes
   6/17 lanes past the SHIPPED capability gate: DigitalOcean, DeepInfra, NextBit, CoreWeave, Phala, Azure
   the lane the router chose first, and therefore the lane this scenario breaks: Phala

   policy                  TTFT p50/p90/p99 (ms)    answer p50/p90/p99 (s)       wait p50/p90 (s)     $/1k  hedge%  modal lane
   ---------------------------------------------------------------------------------------------------------------------------
   default                1340     4095   345416    81.19  428.41   656.65       81.19     428.41    0.852     0.0  DigitalOcean (40%)
   belief                 2358     4580    42262    37.49   67.46   127.99       37.49      67.49    1.333     0.0  NextBit (54%)
   belief+hedge           2209     4500    44383    39.01   75.78   153.11       39.01      75.78    1.384    10.0  NextBit (56%)

   no visible tokens, so nothing is read as it arrives and the wait is the whole answer
     default             wait p99   656.65 s   $0.000852/request   0.0 hedges per 100   0 streams abandoned   refused-and-retried 3.1%   hedge waste $0.0000/1k   lanes used: 5   socket median 105 ms of world time
     belief              wait p99   128.63 s   $0.001333/request   0.0 hedges per 100   0 streams abandoned   refused-and-retried 2.7%   hedge waste $0.0000/1k   lanes used: 6   socket median 104 ms of world time
     belief+hedge        wait p99   154.80 s   $0.001384/request   10.0 hedges per 100   1562 streams abandoned   refused-and-retried 2.9%   hedge waste $0.1157/1k   lanes used: 6   socket median 109 ms of world time

── offpath  (background, nobody is waiting, price wins outright) ─────────────────
   lambda=0 s/$   visible=0  hidden=2000  q_need=0.97  tools=yes
   6/17 lanes past the SHIPPED capability gate: DigitalOcean, DeepInfra, NextBit, CoreWeave, Phala, Azure
   the lane the router chose first, and therefore the lane this scenario breaks: DigitalOcean

   policy                  TTFT p50/p90/p99 (ms)    answer p50/p90/p99 (s)       wait p50/p90 (s)     $/1k  hedge%  modal lane
   ---------------------------------------------------------------------------------------------------------------------------
   default                1521     4133   338003    79.13  423.46   650.58       79.13     423.46    0.853     0.0  DigitalOcean (38%)
   belief                 4071     4144   437787   336.84  512.78   796.96      341.91     540.00    0.618     0.0  DigitalOcean (97%)
   belief+hedge           4069     4145   437816   336.28  512.36   792.08      340.97     539.37    0.623     0.7  DigitalOcean (97%)

   no visible tokens, so nothing is read as it arrives and the wait is the whole answer
     default             wait p99   650.58 s   $0.000853/request   0.0 hedges per 100   0 streams abandoned   refused-and-retried 3.1%   hedge waste $0.0000/1k   lanes used: 5   socket median 104 ms of world time
     belief              wait p99   950.46 s   $0.000618/request   0.0 hedges per 100   0 streams abandoned   refused-and-retried 5.1%   hedge waste $0.0000/1k   lanes used: 4   socket median 100 ms of world time
     belief+hedge        wait p99   947.07 s   $0.000623/request   0.7 hedges per 100   105 streams abandoned   refused-and-retried 5.1%   hedge waste $0.0043/1k   lanes used: 4   socket median 100 ms of world time

── ship gate ── against default: the scenario's p90 improves by >= 30%,
   AND the extra dollars per request are no more than the mean seconds saved, priced at lambda

   scenario  policy              speed metric       improve   extra $/req  budget $/req   verdict
   -------------------------------------------------------------------------------------------------
   talk      belief              p90 first token     +63.1%     -0.000017      0.128268   PASS
   talk      belief+hedge        p90 first token     +63.1%     +0.000034      0.129422   PASS
   work      belief              p90 wait            +84.2%     +0.000481      1.600064   PASS
   work      belief+hedge        p90 wait            +82.3%     +0.000532      1.561601   PASS
   offpath   belief              p90 wait            -27.5%     -0.000235      0.000000   FAIL (p90)
   offpath   belief+hedge        p90 wait            -27.4%     -0.000230      0.000000   FAIL (p90)

   the design passes in 2/3 scenarios; 4/6 rows pass overall.

── seed by seed ── is the verdict a property of the design or of the seed? ────────

   offpath  belief              passes on 0/8 seeds
   offpath  belief+hedge        passes on 0/8 seeds
   talk  belief                 passes on 8/8 seeds
   talk  belief+hedge           passes on 8/8 seeds
   work  belief                 passes on 8/8 seeds
   work  belief+hedge           passes on 8/8 seeds

   wall 57m51s

   wrote bench/lanelab/gosim/result.json
```

### What the run says

**The shipped router beats today's default handsomely where somebody is
waiting, and loses to it outright where nobody is.** `talk` and `work` pass on
all eight seeds — the p90 first token is 63.1% better and the p90 work wait
84.2% better than a request with no preference on it — and both are stable, not
a seed. `offpath` fails on all eight, and it does not fail narrowly: **the
router's p90 wait is 27.5% WORSE than sending no preference at all.**

The `offpath` failure has one cause and it is visible in the modal-lane column.
With λ = 0 the score is the money and nothing else, so the router goes to the
cheapest lane that survives the gate, and on this sheet that is DigitalOcean,
which writes at **six tokens a second**. It goes there on 97% of requests. The
capability gate cannot refuse it — DigitalOcean takes a tool call, publishes a
million-token context and a 99.9% five-minute uptime — so the only thing that
could have was the quality gate, and **the quality gate never fires**.

That is arithmetic, not bad luck. `Beta.Toward` forgets each count toward the
prior over a ten-minute half life, so the evidence a lane accumulates
SATURATES: at the rate `offpath` produces outcomes (one per request, and a
request on a 6 tok/s lane takes about 340 s) the belief settles at a mass of
about **twelve** — nine of them the prior's — whatever the lane really does.
And at that mass the 90% upper bound of a Beta near 0.9 sits at or above 1.0:

| accept rate | steady-state belief | mean | `Upper(1.2816)` | refused at `q_need` 0.97? |
|---:|---|---:|---:|---|
| 0.985 | Beta(11.03, 1.05) | 0.913 | **1.000** | no |
| 0.950 | Beta(10.92, 1.15) | 0.905 | **1.000** | no |
| 0.850 | Beta(10.62, 1.46) | 0.879 | **0.995** | no |

**C1 traded an absorbing gate for an inert one.** Finding 2 above says the
reference model's gate on the MEAN can never rise above 0.97 at the saturated
mass; the shipped gate on the 90% UPPER BOUND can never fall below it. Both are
the same saturation, read from opposite ends — and the shipped direction is the
one that ships wrong answers, because a gate that cannot refuse is not a gate.
Worse, it is self-reinforcing: the slower the lane the router settles on, the
longer each request takes, the more the quality evidence decays between
outcomes, the wider the bound, and the less able the gate becomes to refuse the
slow lane. `offpath` is that loop closing. The prior, the half-life, the bound's
`z` and `q_need` still have to be chosen together — Finding 2's conclusion is
unchanged and the correction has not touched it.

**The hedge is a pure cost in this run, in all three scenarios.** In `talk` it
fires on 8.9 requests in a hundred, abandons 1277 streams, adds $0.000051 to
each request, and moves the p90 first token from 1769 ms to 1767 ms. In `work`
it fires on 10 in a hundred and makes the p90 wait **worse** — 67.49 s to
75.78 s — which is not noise across sixteen thousand requests. The reason is
structural rather than a tuning problem: **the race is decided on the first
token and `work`'s wait is nine tenths generation**, so the lane that speaks
first is regularly the lane that then writes slower, and the hedge picks it. A
rescue that chooses on TTFT is the wrong decision procedure for a request whose
prize is the rate.

### Where the Go run and this model disagree

A disagreement between the reference model and the shipped code is the most
valuable thing the second simulator can produce, so all of it is listed and
none of it is smoothed.

First, where they agree, because it is worth knowing the instrument is sound.
Charged through one price table on the same sheet, the two do-nothing
arms — `default` here and `openrouter-default` there — land on top of each other:

| | Python | Go |
|---|---|---|
| `talk` TTFT p50/p90/p99 (ms) | 1462 / 4694 / 49607 | 1573 / 4791 / 50951 |
| `talk` wait p50/p90 (s), $/1k | 1.95 / 47.47, $0.565 | 2.17 / 49.39, $0.558 |
| `work` wait p50/p90 (s), $/1k | 79.55 / 421.33, $0.852 | 81.19 / 428.41, $0.852 |
| `offpath` wait p50/p90 (s), $/1k | 81.28 / 433.40, $0.853 | 79.13 / 423.46, $0.853 |

The residual on the first token is about a tenth of a second, which is the
socket the Go run measures and prints.

**1. The gate is taken against a different, weaker baseline.** `sim.py` grades
against `strike-ledger`. The Go run cannot: `internal/provider`'s
`velocityLedger`, its `observe` and its `preferences` are unexported; the single
`sharedVelocity` has no exported reset, so nothing outside that package can
prime it, clear it between seeds, or drive it except through a whole
`provider.Client`; and its clock is an unexported `now func() time.Time` fixed
to `time.Now`, so a five-minute refusal cooldown cannot be moved by a run on a
divided clock. **The two gate tables are therefore not comparable row for row.**
For an apples-to-apples reading, this model's gate re-taken against
`openrouter-default` is:

| scenario | speed metric | Python `belief+hedge` | Go `belief+hedge` |
|---|---|---:|---:|
| talk | p90 first token | **+63.4% PASS** | **+63.1% PASS** |
| work | p90 wait | **+84.5% PASS** | **+82.3% PASS** |
| offpath | p90 wait | **+79.3% PASS** | **−27.4% FAIL** |

Two of three agree to within half a point. **`offpath` is the disagreement, and
it is a hundred points wide.** The reference model refuses DigitalOcean on
quality and lands on DeepInfra; the shipped code cannot refuse it and lands on
DigitalOcean. The shipped code is what ships, so the Go row is the one that
counts and this file is the bug report.

**2. The two capability gates do not admit the same lanes.** `sim.py` refuses a
lane whose sheet `status` is not zero and admits four-bit weights; the shipped
gate does the opposite on both counts. **`lane.Facts` has no status field and
`decodeSheet` in `internal/lane/sheet.go` never reads `status`**, so a lane the
router itself has marked down is invisible to this build. On this sheet: in
`talk` both admit fifteen lanes, but Python drops Azure (`status -2`) and keeps
AtlasCloud (`fp4`) while the shipped gate does the reverse; in `work` and
`offpath` Python admits five and the shipped gate six, the extra one being
Azure. **The shipped gate will route to an endpoint the router has flagged**, and
it needs the sheet's `status` column to stop.

**3. `lane.Chooses()` is inverted, and the shipped transport therefore never
hedges.**

```go
func Chooses() bool {
	_, empty := Default().Chooser().(*chooser)
	return !empty
}
```

`*chooser` is the package's own chooser and it is the REAL one — no non-test
code anywhere calls `SetChooser`, so `Default().Chooser()` is always a
`*chooser` and `Chooses()` is always **false**. Run, not read:

```
chooser type *lane.chooser
lane.Chooses() = false
```

`internal/provider/hedge.go` opens `raceFor` with `if !lanes.Chooses() { return
nil, false }`, so **no request in a shipped build can be hedged at all**. The
transport's tests pass because they install a `pinChooser`, which is not a
`*chooser` and so makes the check true; nothing tests the shipped case. The
comment describes `*chooser` as "the empty chooser", which it was in the wave
the check was written and has not been since.

The consequence for the table above is direct: **the `belief+hedge` arm measures
a mechanism the shipped binary refuses to run.** Given what that arm costs in
this run, fixing the check without also fixing the hedge would make the build
slower and dearer.

**4. `internal/lane/e2e_test.go` never teaches its ledger a rate.**
`e2eLane.stub()` scripts `Tokens: lanestub.DefaultTokens`, which is 24;
`ratedFloor` in `internal/lane/belief.go` — the shortest answer worth rating —
is 32. Every sighting those five acceptance scenarios fold in is therefore a
first-token measurement with the rate filter switched off, and no claim about
the belief's rate half can be made from that file. `gosim` streams 40 tokens for
exactly this reason.

**5. The wire cannot deliver a rate, and the constant `e2e_test.go` names is not
the one that binds.** That file's note says a hundredfold speedup leaves a
scripted duration "two orders of magnitude above the loopback round trip". On
the machine this ran on the loopback round trip is not what binds — the Go
timer's own floor is:

```
asked     10µs  got 644.163µs  overshoot 634.163µs
asked     33µs  got 505.126µs  overshoot 472.126µs
asked    100µs  got 735.854µs  overshoot 635.854µs
asked    500µs  got 1.055622ms  overshoot 555.622µs
asked      2ms  got 2.108796ms  overshoot 108.796µs
```

Roughly half a millisecond, and it does not shrink with the sleep. A token gap
at thirty tokens a second and a hundredfold speedup is 333 µs, so a rate read
off this socket is a measurement of the scheduler. `gosim` splits the difference
explicitly — **the first token is measured off the wire and the rate is the
world's own draw, carried beside the stream** — and prints what the socket cost
the first token, about 100 ms of world time per cell. That is an addition to
every arm alike, so it DILUTES every improvement ratio in the gate: the design's
margins here are, to that extent, understated.

**6. A first-token fault is a `talk` failure and not a `work` one, and only a
simulator that runs the fault can see it.** Both programs' scenarios differ, but
only the Go one breaks a lane mid-run. In `talk` the shipped chooser moves off
the broken lane on the very next request and stays away until it recovers. In
`work` it stays on the broken lane for thirty requests in a row — and it is
right to: four seconds of first token is a tenth of a thirty-five-second answer,
while the lane it would move to writes six seconds slower over two thousand
hidden tokens. `-trace` shows it request by request. This is a property of the
objective rather than of the router, and it is worth knowing before anybody
writes an acceptance test that expects a work node to flinch.

**7. The shipped ledger re-writes its whole belief file on every sighting and
every outcome.** `Note` and `NoteOutcome` both end in `save()`, which marshals
every belief and writes it through a temporary file and a rename, synchronously,
under the ledger's lock. This run made about a quarter of a million of them. It
is not a law violation — it happens after the answer, never in front of it — but
it is a per-answer file write that nothing in the design asks for.

### What this run does not decide

The `strike` arm is the mechanism the design proposes to retire and it is not in
this table. **Until it is, no row above is the ship decision the design asked
for** — it is the ship decision against today's do-nothing default. Making it
reachable is one exported seam in `internal/provider`: a constructor for the
velocity ledger, or a reset and a clock on the shared one. That is smaller than
anything else on the list at the end of this file, and it should be first.

---

## Where this model can be wrong

Every one of these is a reason to trust the *ordering* of the policies more
than the magnitudes, and to treat the ship-gate percentages as an argument for
running the Go simulator and then the live A/B rather than a substitute for
either.

- **This is Python, and the thing that ships is Go.** `sim.py` re-implements
  the belief, the prune, the scalar and the hedge from the design document. A
  re-implementation can agree with the design and still disagree with the code,
  and the two have already disagreed once — this file carried the old objective
  after `internal/lane` had the corrected one. `gosim` exists because a
  simulator that drives the real registry cannot make that particular mistake.

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
  runs against the design, and `work` / `sheet-only` passing the gate is where
  it shows.

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

- **λ is a constant per scenario, and in life it moves within a turn.** Part II
  section 1 has λ rising as a deadline nears and falling when a node leaves the
  critical path. Here it is 90, 90 and 0 for the whole run. The money clause is
  only as good as the λ handed to it, and a scenario is a coarse stand-in for a
  number the harness is supposed to compute per request.

- **One model, one sheet, one afternoon.** Seventeen lanes of
  `deepseek/deepseek-v4-flash` at 04:00 UTC on 2026-08-31. A model with three
  lanes, or a sheet taken during an incident, is a different experiment. The
  spread that makes lane choice worth anything — 4.9x on first token, 12x on
  throughput at roughly the same price — is a property of *this* sheet.

- **Not implemented, and it would change the numbers.** Part I section 3 bounds
  exploration: "a lane may only be sampled into the top-3 if its prior p50 is
  within 2x of the best lane's". The scope of this lab did not include it, so
  DigitalOcean at 6 tok/s is occasionally explored on the user's time in `work`
  and `talk`. Adding it would improve `belief+hedge`'s p90 slightly, which in
  `talk` is the clause that actually fails — so it is worth doing before the
  `talk` verdict is treated as final.

- **`--sweep 8` is eight seeds, not a confidence interval.** It is enough to
  show that the lane choice is unstable. It is not enough to put a number on
  how unstable.

## What to fix before running this again

1. Choose the quality prior, the Beta half-life and `q_need` together so that a
   good lane can actually clear the gate and a condemned one can come back
   (Findings 1 and 2). Until then the `work` lane choice is a coin flip that the
   gate no longer reports.
2. Budget the hedge in money or as a fraction of the expected answer, not in
   hedges per minute of wall clock (Finding 3). The λ clause endorses the bad
   trade, correctly, so the budget is the only place this can be fixed.
3. Implement the 2x exploration bound from Part I section 3. It is the cheapest
   remaining lever on the `talk` p90 first token, which is the one clause the
   design fails.
4. Measure a real accept rate per lane, even roughly. It is the assumption the
   biggest result rests on.

Then re-run `python3 sim.py --seed 7 --sweep 8` here, and re-run `gosim` — which
is what actually decides. **`gosim` has now been run** and its table is in the
section above: it passes `talk` and `work` on all eight seeds against today's
default and fails `offpath` on all eight, for the reason item 1 names, so item 1
is no longer a caution about a coin flip — it is the fix the shipped router is
waiting on. `live.sh` is worth its money only after both simulators agree. It
has never been run.
