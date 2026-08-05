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
[`bench/ab-routing/REPORT.md`](bench/ab-routing/REPORT.md). The recommendation is
not to ship the router in this configuration.

## 4. Caveats

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
