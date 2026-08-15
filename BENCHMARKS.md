# Benchmarks

Measured comparisons of aforge against two other coding harnesses, pi and
opencode. The protocol is in [`bench/README.md`](bench/README.md); this file is
the record of what came back.

Everything below is one repository —
[MALIBA-AI/bambara-text-normalization](https://github.com/MALIBA-AI/bambara-text-normalization)
— and one model, `deepseek/deepseek-v4-flash-0731`. Read the caveats at the end
before generalising from any of it.

## 1. Issue implementation

Four real open issues. Each harness got the issue text and nothing else, worked
in a fresh clone, and was judged by the repository's own pytest suite run
afterwards. aforge ran as a one-node graph — one agent, one leaf, no planning
call — because pi and opencode are also single agents and that is the
like-for-like shape.

Test totals are only comparable **within a row**. The suites differ per issue
because the issues touch different parts of the repository and add their own
tests.

### #21 — arithmetic normalisation

| harness  | tests passing | wall clock | cost           |
| -------- | ------------- | ---------- | -------------- |
| aforge   | 529–554       | 5m56s      | $0.10–$0.19    |
| pi       | 548           | 12m15s     | $0.174 *       |
| opencode | 539           | 23m35s     | $0.349 *       |

aforge is quoted as a range across two configurations run as a controlled
ablation: with a generated per-leaf contract it finished at 529 passing, and
without one at 554 — both with zero failures, both in 5m56s. The contract
halved turns (45 vs 81) and cost ($0.104 vs $0.185) at a small test-count
price. It was roughly twice as
fast as pi and four times as fast as opencode, at a comparable or better
outcome, but it did not lead on tests passing — pi's 548 sits inside aforge's
range and above its lower end.

\* Starred cost figures come from account-level readings and are unreliable —
see the caveats.

### #22 — currency normalisation

| harness  | tests passing | wall clock | cost        |
| -------- | ------------- | ---------- | ----------- |
| aforge   | 558           | 9m49s      | ~$0.17–0.24 |
| pi       | 580           | 10m56s     | not measured |
| opencode | DNF           | 40m (cap)  | not measured |

pi produced the better result here: 580 passing against aforge's 558, in
comparable time. opencode did not finish. It hit the 40-minute cap having
changed zero files, while continuing to spend — this cell is the one that made
the shared-key cost problem impossible to ignore.

### #23 — optional CLI dependencies

| harness  | tests passing | wall clock | cost         |
| -------- | ------------- | ---------- | ------------ |
| aforge   | 321           | 1m52s      | $0.014       |
| pi       | 324           | 2m37s      | not measured |
| opencode | 321           | 3m30s      | not measured |

A small, well-specified issue. All three harnesses did it; the spread is three
tests and about ninety seconds. This is the row that shows how little separates
the harnesses when the task is small enough that nothing has to be planned.

### #20 — CI workflow

| harness  | result             | wall clock | cost         |
| -------- | ------------------ | ---------- | ------------ |
| aforge   | workflow added     | 1m25s      | $0.005       |
| pi       | workflow + tests   | 3m0s       | not measured |
| opencode | workflow added     | 1m2s       | not measured |

No test-count column: the issue asks for a CI workflow file, so the suite is not
the judge. pi did the most here — it added tests alongside the workflow, which
the issue did not ask for and which is more than aforge or opencode produced.
opencode was fastest.

## 2. PR review

A different task shape: find the defects in a change rather than write one.

The subject is PR #11, a real merged pull request in the same repository. The
workspace is checked out at the PR's base commit, so the reviewer sees the
change as a reviewer would have. Ground truth is not a judgement call — the
repository later merged PRs #13 and #18 fixing defects in #11, so the defects
that were really there are on record, written by the maintainers, after the
fact.

| harness                   | verified defects | false positives | wall clock | cost         |
| ------------------------- | ---------------- | --------------- | ---------- | ------------ |
| aforge, one node          | 4                | 0               | 10m42s     | $0.18        |
| aforge, parallel pipeline | 8                | —               | 19m28s     | $0.41        |
| aforge, parallel pipeline (2026-08-05 re-run) | **INVALID** | — | 19m43s | — |
| aforge, parallel pipeline (2026-08-05, all fixes) | 8 | 0 observed | ~29m17s¹ | $0.32 |
| pi                        | 0 (timed out)    | —               | 40m (cap)  | not measured |
| opencode                  | 0 (timed out)    | —               | 40m (cap)  | not measured |

¹ The machine slept 12 minutes mid-run (caught and excluded by the clock-jump
detector); several provider calls also stalled for 3–6 minutes each (reported
live by the new stall heartbeat), so this is an honest but provider-degraded
wall clock, not a clean measurement of the harness.

"Verified" means independently confirmed against #13/#18, not self-reported by
the reviewer.

**INVALID — 2026-08-05 post-merge regression re-run.** Plan 72s, run 1183s
(~19m43s), no REVIEW.md produced. The planner's bind pass left the deliverable
owner — the "Write review" node — with no dependencies (`needs: []`), so the
scheduler launched it at t=0 with zero inputs; it spun 33 turns, exhausted 624k
tokens producing nothing, and the run then stopped without a summary (the
runtime's stop-without-summary behaviour is tracked separately). The structural
guard added in this commit — `anchorLateStarts`, which wires any late-stage node
that ends binding with empty needs to the unconsumed frontier of earlier stages
— is the fix for the planner half, and the benchmark will be re-run.

The parallel pipeline found twice as many defects as the single node, which is
the result the graph exists to produce. The 2026-08-05 all-fixes re-run
(anchorLateStarts guard, run landing, lossless decay) confirms the depth is
reproducible: 8 defects again, every one reproduced by executing the code in
the run's own venv, with the default-preset corruption correctly ranked most
severe, at $0.32 (128 calls, 2.74M in / 205k out). The graph shape was correct
this time — only the diff scan started at t=0, and REVIEW.md was written
exactly once by its owner. Defect families match the previously verified set
(#13/#18); a per-defect re-verification against those PRs was not repeated.
Two open issues the run surfaced: one leaf overran its 500k token budget to
748k because the landing reserve is uncapped, and provider stalls — not
harness time — dominated the wall clock.

pi and opencode both hit the 40-minute cap having produced no output at all.
That is a total failure on this task shape rather than a slow result, and it is
the largest gap in either benchmark.

## 3. Model routing — single model against a routed panel

A different question again: not how aforge compares to another harness, but
whether sending every call to one model is leaving anything on the table. The
protocol is in [`bench/ab-routing/DESIGN.md`](bench/ab-routing/DESIGN.md), the
panel and its measurements in `bench/ab-routing/panel.json`, and the full write
up in [`bench/ab-routing/BASELINE.md`](bench/ab-routing/BASELINE.md) and
[`bench/ab-routing/REPORT.md`](bench/ab-routing/REPORT.md). Arm A is today's
shipped configuration with no environment overrides; arm B is the same harness
with `AFORGE_MODELS` naming a five-model panel.

Three tasks, run end to end through the CLI, n=3, every one graded by code with
no LLM judge anywhere.

| task | success | score median | $ mean | turns median |
| ---- | ------- | ------------ | ------ | ------------ |
| t1-logstore (build a segmented KV store) | 0 / 3 | 0.67 | $0.144 | 127 |
| t2-synthesis (audit an 11-document corpus) | 3 / 3 | 1.00 | $0.073 | 47 |
| t3-shiftplan (repair and refactor a package) | 0 / 3 | 0.71 | $0.075 | 79 |

**3 of 9 overall, $0.88, 99 minutes.** Zero harness crashes; no cell rerun.

Two results from the baseline are worth quoting outside that document.

**The failures reproduce exactly.** All three t1 replicates failed the identical
three tests and all three t3 replicates failed the same defect family. These are
capability boundaries rather than unlucky draws, and all of them are about
conforming to a stated contract rather than being internally consistent — the
store reads a segment it wrote and not one the spec describes. Whatever only had
to agree with itself was correct in every run.

**Decomposition was never the problem.** On t2 the synthesis node measurably
improved on its own ensemble members, removing contradictions they had invented.
But the planner drew **5 nodes and 26 nodes for the same brief**, at 5.4x the
cost, for the same perfect score — and on t3 the cheapest 3-node plan scored
*higher* than the 8-node one. A large part of aforge's run-to-run cost variance
is the plan it happens to draw, which is worth knowing before attributing a cost
change to anything else.

Two tasks needed hardening after arm A aced them, which is recorded round by
round; t2 survived its hardening and stays in the suite as a regression control
rather than a discriminator.

### Arm B — the routed panel

The router (`internal/router/`) over the five-model panel, same three tasks, same
n=3, `AFORGE_MODELS` pointing at `bench/ab-routing/panel.json`.

| task | A success | B success | A score | B score | A $ mean | B $ mean |
| ---- | --------- | --------- | ------- | ------- | -------- | -------- |
| t1-logstore | 0/3 | 0/3 | 0.667 | 0.667 | $0.144 | $0.038 |
| t2-synthesis | **3/3** | **2/3** | 1.000 | 1.000 | $0.073 | $0.071 |
| t3-shiftplan | 0/3 | 0/3 | 0.714 | 0.571 | $0.075 | $0.089 |
| **overall** | **3/9** | **2/9** | 0.714 | 0.667 | $0.877 | $0.593 |

**Routing did not help.** The only cell that moved got worse, the 23% cost
saving sits inside the planner's own node-count variance, and
`moonshotai/kimi-k2.6` — the model the panel exists for — **served zero calls**.
Three of five panel members were never called at all.

**The continual-learning check came back positive and harmful.** Against a
fresh-ledger control, eight of eleven call classes reordered between run 1 and
run 3 with a shared ledger and **zero** reordered without one, so the change is
attributable to the ledger. What it learned was to drop its best model: the
terminal rung went from kimi-k2.6 to qwen3-30b-a3b. On the same tasks, run 3
scored 0.000 on t2 and t3 where the control's run 3 scored 1.000 and 0.857.

The mechanism is worth recording here because it is a property of the harness
rather than of the router. Of 108 settled `exec.leaf` verdicts, 103 were
`unverified_success` — a finished leaf is not checked by anything, since the
graders run after `aforge run` exits — so the leaf rating was fitted to the five
that were graded, all of them budget stops from one task. `exec.leaf` is a single
global class, so that lesson was applied to every leaf of every other task.

Six router defects and their evidence are in
[`bench/ab-routing/REPORT.md`](bench/ab-routing/REPORT.md). The recommendation
after that arm was not to ship the router in that configuration.

### Arm B2 — after the six defects were fixed

The same suite plus a fourth task, against the fixed router. Full write-up in
[`bench/ab-routing/REPORT-B2.md`](bench/ab-routing/REPORT-B2.md).

| task | A success | B2 success | A scores | B2 scores |
| ---- | --------- | ---------- | -------- | --------- |
| t1-logstore | 0/3 | 0/3 | 0.667 x3 | 0.889, 0.667, 0.000 |
| t2-synthesis | 3/3 | 2/3 | 1.000 x3 | 1.000, 0.875, 1.000 |
| t3-shiftplan | 0/3 | **2/3** | 0.857, 0.571, 0.714 | 0.857, 1.000, 1.000 |
| t4-pathmatch | 1/3 | **3/3** | 1.000, 0.929, 0.929 | 1.000 x3 |
| **overall** | **4/12** | **7/12** | | |

**Routing works now, on this suite, for +18% cost per cell** ($0.125 to $0.147).
A fresh-ledger control sits at 5/12, so the gain is the routing rather than the
ledger. Every panel member was exercised; `moonshotai/kimi-k2.6` was called 36
times, all of them leaf escalations, all upward.

**The collapse cannot be reproduced.** Replaying t2 against the preserved ledger
that produced 0.000 in arm B now scores 1.000, twice. `aforge models` shows why:
the poisoned rating still reads -0.98 and now carries "under the gate — ordering
uses the prior until n=8".

**Continual learning is inert rather than harmful.** The ledger reordered
nothing across twelve cells: the gate that stops bad evidence driving the order
also means eight graded observations never accumulate at this volume. Arm B's
learning was measurable and harmful; B2's is neither, which is a strict
improvement and is not the same as working.

Two findings stand open. The escalation target accumulates no evidence about
itself — every one of kimi's 36 attempts was an unverifiable leaf — so its
position rests on an operator role hint alone. And t1 did not move: its leaves
carry 2.2M prompt tokens and run out of budget, which a stronger model does not
fix. That is an at-scale failure, not a capability one, and routing is the wrong
instrument for it.

## 4. The subharness grid — 2026-08-09

The first measurement of the swe subharness (docs/SUBHARNESSES.md): the same
four issues, three aforge shapes, every cell pinned to base `6c978ff` — the
last commit with all four issues open, because the repository has since merged
fixes for #23 and #21 and an unpinned clone passes the suite before any
harness arrives. Suite baseline at the pin: 317 passing. The pi and opencode
rows above are the recorded bar, per protocol; they were not re-run.

Two iterations, because the first one measured the integration rather than
the worker, and what it found is part of the record:

**Iteration 1 (strict config)** — the engine's inner flash-tier auditor
refused every finished change over clause-coverage matrices while build,
tests, and lint were green; two runs died at a 30-minute watchdog with their
suites already grown to 572 and 598 passing. Real work — 9 to 13 files
committed per issue — landed as failures. Three knobs came out of it: the
inner auditor yields to aforge's own delivery gate (mechanical verification
stays on), the deadline floor is the hour the anchors promise, and the
choice prior licenses swe for *discovered* work, not every coding issue.

**Iteration 2 (shipping config):**

| issue | linear | swe forced | select (full stack) | pi (recorded) | opencode (recorded) |
| --- | --- | --- | --- | --- | --- |
| #20 | 32s · $0.008 | 3m14s · $0.08 · pass | 4m35s · $0.15 | 3m0s | 1m02s |
| #21 | 43s · $0.018 · 317 | 9m04s · $0.28 · **526** | 30m · $1.23 · **535** | 12m15s · 548 | 23m35s · 539 |
| #22 | 2m07s · $0.027 · 317 | 5m46s · $0.21 · **543** | 36m · $1.29 · **564** | 10m56s · 580 | 40m DNF |
| #23 | 1m37s · $0.009 | 2m57s · $0.11 · pass | 12m24s · $0.81 | 2m37s · 324 | 3m30s · 321 |

Counts are tests passing after the run; every aforge cell finished with zero
failures and its work committed (the engine commits, so `git status` reads
clean — the change accounting is `git diff` against the pin).

What the grid establishes:

- **swe forced beats pi's recorded wall clock on every comparable row** and
  completes the #22 that opencode could not, at self-reported cost between
  eight and twenty-eight cents. On raw counts pi's recorded runs still lead
  the two big issues (548/580 against 526/543) — count measures test-writing
  volume as much as correctness, but it is the recorded table's metric and
  the gap is real at this model tier with a single-model pool.
- **The layered product improves the engine's work.** Select's #22 landed 564
  against forced swe's 543: the delivery gate judged the engine's deliverable
  and bought a revision that added twenty-one green tests. The stack paid for
  it in wall and dollars — the gate's price is real too.
- **Selection chose swe four of four**, including the two issues linear
  settles for a cent. The boundary prior plus one session of measured
  history does not yet route small issues away from the specialist; the
  evidence records that should move it (audit-ceiling notes, cost under the
  generalist's median) are on file and recalibration reads them.
- **The drift control moved.** Today's linear is five to eight times faster
  than its own recorded rows (43s against 5m56s on #21) and writes no tests
  where the recorded run grew the suite — the harness got faster and
  shallower over five days of development, which is exactly what the control
  row exists to catch.

Levers deliberately not pulled, for a future round: the engine's stock
multi-tier model pools (pinned here to the one benchmark model for
like-for-like), hard mode, and further boundary-learning iterations.

## 6. The vs-pi cells and the wave campaign — 2026-08-13 (spark)

A different benchmark from sections 1–4: three hand-authored cells (BUG: fix
the failing suite; FEATURE: add a bulk discount with tests; REPORT: six
fictional vendor briefs evaluated into report.md), three replicates each,
`deepseek/deepseek-v4-flash`, all nine cells of an arm run CONCURRENTLY (the
only serializer is the provider rate limit). pi is 0.82.1 pinned side-by-side.
Costs are `normcost.py` (0423 price sheet) for aforge; pi is `picost.py`
recomputed-at-list in parentheses. Quality: pytest for BUG/FEATURE
(17 / 20 passes is the fixture bar), report.md presence and six-vendor
coverage for REPORT.

Same-day medians, 2026-08-13/14, in campaign order:

| arm | bug | feat | report | total |
|---|---|---|---|---|
| pi 0.82.1 | 26s / $0.0019 (0.0028) | 48s / $0.0032 (0.0043) | 31s / $0.0012 (0.0017) | **105s / $0.0063** |
| w4b baseline (= v3 + structural keepers) | 94s / $0.0187 | 89s / $0.0135 | 89s / $0.0111 | 272s / $0.0433 |
| w5 guard+digest+continuation+spine | 67s / $0.0130 | 86s / $0.0123 | 146s / $0.0225 | 299s / $0.0478 |
| w6 planner amortization prompts | 105s / $0.0141 | 102s / $0.0125 | 148s / $0.0134 | 355s / $0.0400 |
| w7 atomic nodes decline specialists | 62s / $0.0130 | 92s / $0.0157 | 102s / $0.0148 | 256s / $0.0405 |
| w8 + one-sitting chain collapse | 60s / $0.0118 | 92s / $0.0168 | 112s / $0.0148 | 264s / $0.0434 |

What the campaign learned, in the order the evidence forced it:

- **Advisory prompt text does not move a multi-turn coding model on this
  workload.** Wave 4 injected the boring-cwd/tool-economy discipline verbatim
  into every swe coder leaf and measured zero turn-count change (~26 turns
  for a one-line fix, before and after). Reverted. Structural mechanisms are
  the only ones that paid.
- **The waves DID fix real defects** (each verified in isolation): the spill
  tee reopened after filling and pointed "First N bytes" at the last, EMPTY,
  file; a coder could write the harness's own `.codeaf/contract.json` (the v3
  FEATURE regression); a leaf could spin 200 turns and burn its 150k budget
  twice through stateless continuations (no-progress guard + continuation
  state handover now land both); prefix-stability is now property-tested.
- **The residual gap (6.3x cost, 1.9–3.4x wall) is architectural, not a
  missing mechanism.** pi runs a minimal loop: 8–14 calls and ~13.5KB of
  total tool output per cell. aforge pays a planning layer, per-node
  orientation, and engine turns of ~7.7k tokens each. On one-sitting tasks
  that envelope dominates; on the section-1 issues the same machinery is what
  wins (section 4's grid). Routing cannot see it pre-execution: the sizing
  pass judges a bug fix borderline under the baseline ruler because the fix's
  size is unknowable before the work — "atomic" is only ever known in
  retrospect.
- **Found gate hole:** wave-8 report-r3 ended with exit 0 and no report.md —
  the final leaf wrote `ranked-recommendation.md` instead. Nothing compares
  the landed files against the brief's named deliverable. Tracked as the
  first move of the next wave.
- **Quality bar held everywhere else:** BUG 17/17 ×3, FEATURE 19–26 ×3,
  REPORT six-vendor tables ×2, on every wave-5+ arm.

Levers deliberately not pulled, for a future round: generalist-first with
swe-escalation-on-overrun (the doctrine's calibrated answer to the sizing
uncertainty above — needs escalation to carry the generalist's partial state
into the specialist cheaply); observation slimming (pi's reads average ~1KB;
the 2000-line read is the default the leaf reaches for); the engine's
multi-tier model pools; hard mode.

## 5. Caveats

**pi and opencode cost figures are unreliable.** The starred figures in the #21
table are account-level credit readings taken around the runs. The API key is
shared, and the measurement window contained up to $116 of unrelated and runaway
traffic. Those numbers therefore include spend that has nothing to do with the
cell being timed and should not be quoted as the cost of a run. aforge's figures
come from its own per-run usage accounting and are not affected. Neither pi nor
opencode self-reports usage, so getting a real cost for either requires a key
isolated to a single run; that has not been done. Every "not measured" cell in
this document is that gap, not a missing entry.

**One repository, one model, one attempt.** Everything here is
bambara-text-normalization on `deepseek/deepseek-v4-flash-0731`. Nothing
establishes that the ordering holds on another codebase, another language, or a
stronger model. Cells were run once each except where a range is given.

**aforge ran in a specific configuration:** executor reasoning off, per-leaf
contracts on. Both are knobs, both change the results, and neither was swept.

**The test suite is a proxy for correctness, not correctness.** A test-count
delta says a change works against tests that already existed. It does not say
the change is the one a maintainer would have written, and on issues where the
harness also writes tests it partly measures the harness grading itself. The
run.sh prompt forbids weakening tests and the diff is retained so that can be
checked, but no cell here was reviewed line by line.

**Wall clock includes provider-side variance.** Latency on a shared endpoint
moves between runs and between times of day. Differences of a minute or two in
these tables are not meaningful; the 4x on #21 and the timeouts in section 2
are.
