# Arm A — the single-model baseline

What today's aforge does on three complex tasks, run end to end through the real
CLI. The design that produced these numbers is in [`DESIGN.md`](DESIGN.md); the
panel the routed arm will use, and the measurements behind it, are in
[`panel.json`](panel.json).

Arm A is not a reconstruction of the default configuration — it *is* the default
configuration. `run-arm.sh` with `ARM=a` sets no environment variables at all,
so the model, the reasoning levels, the spine sampling and the node budget are
whatever `internal/config/config.go` ships.

---

## 1. Calibration rounds

Routerlab's round 1 passed 28 of 36 tasks with every model and carried almost no
information. The protocol here is its fix: run arm A once per candidate task,
and harden anything it aces.

**Two of the three tasks were degenerate on the first try.** Both are recorded
below with their round-1 numbers; the round-1 briefs, answer keys and reference
solutions are kept under `tasks/t*/round1/` rather than deleted, and the cells
are in `calibration-round1.jsonl`.

| task | round 1 | verdict |
|---|---|---|
| t1-logstore | score **1.000**, success, $0.0388, 433 s, 9 turns, 1 node | degenerate — hardened |
| t2-synthesis | score **1.000**, success, $0.0698, 761 s, 47 turns, 5 nodes | degenerate — hardened |
| t3-shiftplan | score **0.714**, fail, $0.1335, 767 s, 143 turns, 11 nodes | calibrated — stands |

### t1-logstore, round 1 → round 2

Arm A wrote a complete and correct `tinylog` **on its first tool call** and
scored 6/6. The diagnosis is not that the model is strong; it is that the brief
specified the record frame byte for byte over a single file, so the task was
transcription. A model that has seen a length-prefixed CRC-checked log before —
and all of them have — reproduces it without deciding anything.

Round 2 keeps the frame, because the format group has to pin something the model
cannot invent around, and adds the parts that cannot be transcribed:

- a **segmented** log that rolls at a `segment_bytes` threshold, with sequential
  zero-padded segment files;
- a **manifest** that recovery is explicitly told not to trust — missing,
  unparseable, or naming a segment that is not there, all fall back to
  discovering the directory, and a segment the manifest forgot must still be
  read;
- **compaction that merges every segment into one** and rewrites the manifest;
- damage **confined to the segment it happened in**, so a torn early segment
  does not discard later ones;
- **100,000 keys**, which forces an index rather than a rescan, and a `scan`
  that is a generator rather than a list.

Re-graded against round 2, the round-1 submission scores **1/9**. The reference
passes 47/47, so the task is hard rather than impossible — which is the only
thing the reference exists to establish.

### t2-synthesis, round 1 → round 2

Arm A scored a perfect 8/8, and *how* is the interesting part. aforge planned
t2 as an **ensemble**: three independent audits of the whole corpus, then a
synthesis node. The three members individually made mistakes — one invented two
contradictions — and the synthesis produced an answer with none of them. Round 1
measured that aforge's ensemble strategy works on a judgment-shaped goal. That
is worth knowing and it is not what this experiment is for.

Round 2 adds two hardenings chosen to punish the merge step specifically:

- **Kafka moved to an account-level volume tier.** Round 1 was flat pricing
  summed per service, which is a loop. The tier is assessed on the *combined*
  volume of every service on the technology, so summing per service never
  reaches the threshold and returns the old flat answer — a wrong number that
  looks arithmetically clean. 257,820 cents becomes 224,820.
- **The 2026-02-27 outage is filed twice**, by two teams, three days apart, with
  differently-measured windows. Counting reports instead of incidents turns 490
  minutes into 675. The re-filing is superseded, it contradicts the earlier
  filing, and it is not a fourth incident; both filings carry explicit
  timestamps so "earlier" is a fact rather than an inference.

### t3-shiftplan — no hardening

6 of 7 defect families repaired, failing only the empty-interval edge case, at
143 turns across an eleven-node graph. `success` requires all seven, so arm A
fails it while getting visibly close. That is the band the brief asked for and
the task stands unchanged.

Worth stating plainly as a limit: **t3's discrimination rests substantially on
one edge case.** `Interval(600, 600).overlaps(...)` is False because an empty
interval shares no minute with anything, which follows from half-open semantics
but is not spelled out in the docstring. A run that reasons it out passes; one
that writes the natural `start < other.end and other.start < self.end` does not.

### A defect in the answer key, found by the run rather than by the checker

The round-1 t2 cell returned a contradiction between the two vendor pricing
schedules. It was marked wrong. **It was right.** Under the brief's own
definition — a disagreement between two documents about the same fact, resolved
by `RULES.md` — doc 07 states a value for a field doc 08 owns, exactly as docs
03 and 09 do for theirs. Round 1 had quietly assumed "superseded" and
"contradicting" were exclusive.

The tell was there before any spend and the selfcheck did not look for it:
`unit_price` sat in the field vocabulary with nothing in the key pointing at it.
Offering a field and then penalising its only correct use is a trap, not a task.
The selfcheck now fails if any field in the vocabulary is unused.

This is the 27th ground-truth defect the three labs have caught between them
(routerlab 10, probelab 16, here 1) and the first caught by a real run.

### Graph shapes, from three briefs of comparable length

| task | nodes | shape the planner chose |
|---|---|---|
| t1-logstore | 1 | a single node, flagged `oversized`, never decomposed |
| t2-synthesis | 5 | ensemble: 1 setup, 3 independent full audits, 1 synthesis |
| t3-shiftplan | 11 | 3 audits → 4 parallel fixes → refactor → exports → synthesis |

Cost tracked node count almost exactly and turns tracked it better: 1 node / 9
turns / $0.039, 5 nodes / 47 turns / $0.070, 11 nodes / 143 turns / $0.134.

**t1 collapsing to one node is a finding, not a configuration mistake.** The
planner marked it `oversized` — it knew the leaf was too big — and still shipped
it as one leaf under the default `AFORGE_MAX_DEPTH=2`. Arm A was not tuned to
avoid this, and arm B must not be either; if the router improves t1, it will be
improving a single leaf's model choice rather than a decomposition.

---

## 2. Results

Nine cells, three replicates per task, round-2 definitions, on current master.
Raw record: [`results-armA.jsonl`](results-armA.jsonl).

| task | success | score (min–max, median) | $ mean | wall median | turns median | stop reasons |
|---|---|---|---|---|---|---|
| t1-logstore | **0 / 3** | 0.67–0.67 (0.67) | $0.1441 | 805 s | 127 | 19 done, **5 budget** |
| t2-synthesis | **3 / 3** | 1.00–1.00 (1.00) | $0.0734 | 343 s | 47 | 36 done |
| t3-shiftplan | **0 / 3** | 0.57–0.86 (0.71) | $0.0748 | 438 s | 79 | 16 done |
| **overall** | **3 / 9 = 0.333** | median 0.714 | | | | |

Total spend **$0.8769**, total wall **99.1 min** across three concurrent
streams. Zero harness crashes; every cell produced a graded result and no cell
needed a rerun.

### t1-logstore — 0/3, and the same three failures every time

Every replicate scored exactly **6 of 9 groups**, and every replicate failed the
**identical three tests**:

```
3x  test_GROUP_4_garbage_only_segment_opens_empty
3x  test_GROUP_6_a_hand_written_segment_is_readable
3x  test_GROUP_7_active_segment_rolls_at_the_threshold
```

That reproducibility is the most informative thing in this table. These are not
stochastic near-misses — they are a stable capability boundary. All three runs
built a working segmented store with a manifest, correct recovery of a torn
tail, correct compaction and an index fast enough for 100,000 keys, and all
three got the same three things wrong:

- **the roll boundary** — segments roll, but not at the threshold the brief
  specifies;
- **reading bytes it did not write** — a hand-written segment, framed exactly as
  the brief specifies, is not readable, which means the implementation's reader
  and writer agree with each other and not with the spec;
- **a segment that is entirely garbage** — specified to open as an empty store,
  and does not.

The common thread is that all three are about **conforming to a contract rather
than being internally consistent**. The parts of the task that only had to agree
with themselves were done correctly in every run.

**The dominant runtime failure mode is the leaf token budget.** Five leaves
across three cells stopped on `budget` rather than `done` — the 300,000-token
per-leaf cap — against zero on t2 and t3. t1's prompt token counts are the
reason: **2.0–2.3M prompt tokens per cell**, of which 1.76–1.90M were cached.
The executor is re-sending an enormous context every turn on this task and one
leaf in three runs out of budget before finishing.

### t2-synthesis — 3/3, and the round-2 hardening did not bite

Arm A scored a **perfect 8/8 on all three replicates**, on the round-2 corpus,
including both hardenings:

- the tiered kafka price: 224,820 cents, correct, not the flat 257,820;
- the duplicated incident filing: 490 minutes, correct, not the 675 that
  counting reports instead of incidents produces;
- all three superseded documents and all five contradictions, exactly.

**This is a task that arm A has saturated and I could not un-saturate in one
hardening round.** It should be read as a finding rather than as a failed task:

- aforge's **ensemble strategy is very strong on judgment-shaped work.** Round 1
  showed individual ensemble members inventing contradictions that the synthesis
  node then removed. Round 2 added two traps aimed specifically at the merge and
  the merge caught both.
- It is **robust to graph shape.** One replicate planned 26 nodes and two
  planned 5, at a 5.4x cost spread ($0.1404 vs $0.0262) — and all three scored
  1.000. The extra 21 nodes bought nothing.

For the A/B this makes t2 a **regression control rather than a discriminator**:
the routed arm cannot improve on 3/3, so its only possible result here is to
stay level or to get worse. Phase A's report warned that routing can lose; this
is the cell that will show it if it does.

**What a round 3 would need.** Nothing incremental. The hardenings that failed
were arithmetic and bookkeeping traps, and the ensemble is good at both because
three readers rarely make the same arithmetic slip. Round 3 would have to attack
the merge itself — a corpus where the *correct* answer requires noticing that
two documents are individually consistent and jointly impossible, so that
agreement between ensemble members is evidence of a shared wrong reading rather
than of correctness. That is a different task, not a harder version of this one,
and it was out of budget here.

### t3-shiftplan — 0/3, with one stable failure and three intermittent ones

| replicate | score | families |
|---|---|---|
| 1 | 0.857 | 6 / 7 |
| 2 | 0.571 | 4 / 7 |
| 3 | 0.714 | 5 / 7 |

```
3x  test_GROUP_1_empty_interval_overlaps_nothing        (every run)
1x  test_GROUP_4_returned_list_cannot_be_corrupted_by_the_caller
1x  test_GROUP_5_same_window_next_day_is_free
1x  test_GROUP_6_bad_json_raises
```

**Group 1 fails in every run**, and it is the one family where the correct
answer is an inference rather than a statement: an empty interval `[600, 600)`
overlaps nothing, because it contains no minute to share. The docstring says the
interval is half-open and says touching intervals do not overlap; it does not
mention emptiness. A run that writes the natural half-open predicate
`start < other.end and other.start < self.end` passes the stated cases and fails
this one. Every replicate wrote exactly that.

The other three failures are **intermittent, one replicate each, and different
each time** — a caching defect in one run, date-scoping in another, error
handling in a third. That is the signature of a run that finds *most* of a
defect set and misses a different member of it each time, which is what an
eleven-defect audit spread across parallel leaves would predict.

The score spread (0.571 to 0.857) is also the widest of the three tasks, which
is worth carrying into the arm-B comparison: **t3 is the noisiest task and n=3
is thin for it.**

### Decomposition varies enormously for the same brief

| task | nodes across the three replicates | turns | cost |
|---|---|---|---|
| t1-logstore | 11, 8, 10 | 127, 113, 161 | $0.104, $0.181, $0.147 |
| t2-synthesis | **26, 5, 5** | 171, 47, 35 | $0.140, $0.054, $0.026 |
| t3-shiftplan | **3, 5, 8** | 37, 79, 95 | $0.042, $0.073, $0.109 |

t2 spanned 5 to 26 nodes and t3 spanned 3 to 8, from a byte-identical brief.
Cost tracks node count closely and outcome does not track it at all: t2's
26-node plan scored the same 1.000 as its 5-node plans for 5.4x the money, and
t3's 3-node plan scored **higher** (0.857) than its 8-node plan (0.714).

This matters for arm B in a way that is easy to miss. **A large part of arm A's
cost variance is the planner choosing a different graph, not the executor
spending differently on the same graph.** A routed arm that changes cost by
±30% has not necessarily done anything: that is inside the spread the planner
produces on its own. Any cost claim for arm B needs to be read against this
spread, and ideally against the node count of the specific plan it drew.

### Where the money goes

| task | prompt tokens (mean) | cached | completion | calls |
|---|---|---|---|---|
| t1-logstore | 2,200,621 | 1,819,883 (83%) | 70,710 | 160 |
| t2-synthesis | 798,548 | 433,173 (54%) | 44,517 | 117 |
| t3-shiftplan | 692,187 | 329,003 (48%) | 63,545 | 88 |

Completion tokens are a rounding error; **this workload is almost entirely
prompt**, and t1 is 3x the others because its leaves accumulate a long tool
transcript. Cache hit rates are high but the uncached remainder still dominates
the bill. A router choosing a cheaper model buys most of its saving on the
*input* price, which is worth noting because the panel's input prices span
0.048 to 0.589 $/M — a 12x range, wider than the intuition "the top model costs
14x" from output prices alone suggests.

### Answering the brief's question directly: how does flash fail?

Not by decomposing badly, and not by weak synthesis.

- **Decomposition is not the problem.** Every graph in all nine cells was
  sensible in shape, and on t2 the synthesis node measurably *improved* on its
  inputs — it removed inventions its own ensemble members had made.
- **Leaf overruns are a real and specific problem, confined to t1**: 5 of 5
  budget stops in the whole experiment, driven by 2.2M prompt tokens per cell.
- **The dominant failure is at the leaf, on contract conformance.** Both failing
  tasks fail on the same shape of item: a requirement stated once, in prose,
  whose violation is invisible to any test the model would write for itself.
  Reading a segment it did not write; a segment of pure garbage; the roll
  threshold; an empty interval. In every case the model produced something
  self-consistent and did not check it against the specification.

That is a useful thing to know before routing, because it says where the
headroom is: **not in planning better, but in whichever leaves carry a
conformance requirement.** If the router can spend more on those leaves and less
elsewhere, t1's three stable failures and t3's group 1 are exactly the items a
stronger rung would be expected to fix — and the fact that they are stable
rather than stochastic means a change in them is a signal rather than noise.

---

## 3. Spend

| stage | cells | spend | wall |
|---|---|---|---|
| panel selection (anchor battery, 11 arms × 10 tasks × 2 reps) | 220 | $0.6739 | 4 min |
| calibration round 1 (3 tasks × 1 arm-A cell, sequential) | 3 | $0.2421 | 32.7 min |
| arm A, n=3 × 3 tasks (3 concurrent streams) | 9 | $0.8769 | 99.1 min |
| **total** | **232** | **$1.7929** | **~2.3 h** |

**$1.79 against the $15 cap — 12% used.** The projection from `BENCHMARKS.md`
(full-pipeline runs at $0.18–$0.41) overestimated: the mean arm-A cell cost
$0.097 and the most expensive was $0.181.

Wall-clock caveat, stated because the number is in the table: the arm-A cells
ran as three concurrent streams, so their wall times carry three-way
self-contention. The **uncontended** per-cell timings are the calibration
round's — 433 s, 761 s, 767 s — and those are the ones to compare a sequential
arm B against. Success, score, cost and token counts are unaffected by
contention.

Cost is the run's own usage accounting, summed by the scheduler from the
provider's per-response figures, for the reason `bench/README.md` gives: the API
key is shared, so an account-level delta around a run measures other traffic
too. It is a different number, not a noisier one.

---

## 4. What the routed arm needs

**Command.** The runner is already parameterised; arm B is one flag:

```sh
export OPENROUTER_API_KEY=...
cd <repo root> && make build

# the learning check: three runs in sequence, one shared ledger
ARM=b bench/ab-routing/run-arm.sh

# the control, without which run-1-vs-run-3 cannot be attributed to learning
ARM=b LEDGER_MODE=fresh JSONL=bench/ab-routing/results-armB-fresh.jsonl \
  bench/ab-routing/run-arm.sh

python3 bench/ab-routing/summarize.py bench/ab-routing/results-armB.jsonl --arm b
python3 bench/ab-routing/learning-diff.py bench/ab-routing/results-armB.jsonl \
  --control bench/ab-routing/results-armB-fresh.jsonl
```

**Config contract.** `run-arm.sh` sets `AFORGE_ROUTER=on` and
`AFORGE_PANEL=bench/ab-routing/panel.json` for arm B. If the router lands under
different names, the `case "$ARM"` block at the top of `run-arm.sh` is the only
edit required — nothing else in the harness knows which arm it is running.

**What the router should record**, so the learning check can read it. In
declining order of usefulness, and any one of these is enough:

1. a per-leaf `model` (or `routed_model`) field on the node in the completed
   graph — `learning-diff.py` reads this first;
2. a routing-events file under the profile directory whose name contains
   `rout`, holding either a list of `{node, model}` records or a
   `{"decisions": {node: model}}` object;
3. nothing extra at all — aforge already writes one profile file per
   `(model, skill)`, so the *set* of files under the ledger is direct evidence
   of which panel members were exercised and the record count in each says how
   much work they were given. `learning-diff.py` reports this regardless.

**Comparisons to make, and the traps in each.**

| comparison | read against |
|---|---|
| t1 success 0/3, and specifically the three stable failures | these are reproducible, so any change is signal rather than noise — this is the cleanest headroom in the experiment |
| t3 success 0/3, group 1 in every run | group 1 is the one item every replicate failed; the rest of t3 is noisy and n=3 is thin |
| t2 success 3/3 | **regression control.** The only possible outcomes are level or worse |
| cost | the planner's own spread: t2 ranged 5.4x and t3 2.6x across replicates of the *same* brief. A ±30% cost change is inside that |
| wall clock | the calibration round's uncontended timings, not the arm-A table |
| leaf budget stops | 5 in the whole of arm A, all on t1. A router that raises this has made things worse |

**Budget.** Arm A used $1.79 of $15. The routed arm needs its own cap; on these
numbers a full arm B plus the fresh-ledger control is about 18 cells, which at
arm A's mean cell cost and the panel's price spread should land under $5.

Cost is the run's own usage accounting, summed by the scheduler from the
provider's per-response figures, for the reason `bench/README.md` gives: the API
key is shared, so an account-level delta around a run measures other traffic
too. It is a different number, not a noisier one.
