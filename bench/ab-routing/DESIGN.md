# A/B routing experiment — design

**Question.** aforge sends every call to one model. Phase A showed, offline, that
a cascade over a small panel beats that single model on the cost–quality
frontier — on isolated chat completions, graded one call at a time. Does the
result survive contact with the **real harness**: a full `plan → run` pipeline,
a tool-calling executor, multi-leaf graphs, and tasks that take minutes rather
than seconds?

This document is written before arm A is run. Everything below was fixed in
advance; where the calibration protocol later changed a task, the change is
recorded in §7 with its round number rather than folded in silently.

---

## 1. Arms

| arm | configuration | when |
|---|---|---|
| **A** | today's shipped configuration, untouched: one model for every call, `~deepseek/deepseek-v4-flash-latest`, reasoning off for both planning and execution — exactly what `internal/config/config.go` defaults to. No environment overrides at all. | now |
| **B** | the router on, over the five-model panel in `panel.json` | when the router lands |

`run-arm.sh` is parameterised by `ARM`. Arm A sets **no** environment: the point
of a baseline is that it is the thing already running, not a reconstruction of
it. Arm B sets `AFORGE_ROUTER=on` and `AFORGE_PANEL=panel.json`. That two-line
`case` block is the entire difference between the arms; if the router lands
under different names it is the only edit needed.

## 2. Panel

Five models, selected in `panel.json` from the live catalog with per-model
justification and every exclusion recorded. The measurement behind it is
`panel/anchor_battery.py`: Phase A's 10-task anchor block, 2 replicates, 11
candidate arms, 220 cells, **$0.6739**. Two things about it are worth stating
here because they shaped the panel:

- **The controls reproduced.** kimi-k2.6 20/20, ds-v4-flash 18/20, gemma-3-12b
  11/20, against Phase A's 0.968 / 0.871 / 0.548. The scale did not drift, so
  the new candidates sit on it rather than beside it.
- **The panel came out barbell-shaped.** Every candidate priced between the
  $0.19 base and the $2.48 terminus scored at or below the incumbent —
  qwen3-coder 16/20, glm-5.2 16–17/20, qwen3.7-plus 14/20, minimax-m2.7 13/20.
  Phase A had already found its own middle statistically inseparable. There is
  no mid rung to add, and adding one anyway would buy calls rather than ability.

The re-run Phase A explicitly asked for — glm-5.2 **with reasoning on** — was
run and settles that question negatively: 17/20 on versus 16/20 off, one cell in
twenty for twelve times the spend, with three empty completions. It is excluded
on measurement rather than on the caveat.

One gate is new in Phase B and is applied before ability is even considered:
a panel member must advertise **`tools`** as well as `structured_outputs`,
because aforge's executor is a tool-calling loop. A model the harness cannot
drive has no ability from the harness's point of view.

## 3. Tasks

Three, run end to end through the real CLI — `aforge plan "<goal>" --brief` then
`aforge run graph.json -w <workspace>` — never as isolated calls. Each is graded
by code. **There is no LLM judge anywhere in this experiment.**

| id | shape | deliverable | graded on |
|---|---|---|---|
| **t1-logstore** | multi-component coding, greenfield | a `tinylog` package built from an empty directory | 6 capability groups run by a hidden suite: API and binary safety, durability across reopen, `scan` semantics, crash recovery, compaction, and conformance of the bytes on disk to the specified frame |
| **t2-synthesis** | research and synthesis over a 10-document corpus | `findings.json` against a fixed schema plus `REPORT.md` | 8 checks: schema, five independent cross-document answers, the supersession/contradiction set, and mechanical coverage of `REPORT.md` |
| **t3-shiftplan** | refactor and debug on a small provided codebase | the repaired `shiftplan` package | 7 defect families plus two gates (must import; the shipped test file must be byte-identical) |

### Why these three, and where each is hard

**t1** is hard because the parts interlock and the spec has twists that resist
pattern-matching: the crc covers the payload and not the length prefix, `scan`
is half-open and byte-lexicographic (so `b"\x80"` sorts after `b"z"`), an empty
value is a live record rather than a delete, and recovery must *truncate* a torn
tail off the file rather than skip it in memory. Group 6 checks the on-disk
bytes with a parser written independently of the submission, so a
self-consistent format of the model's own invention fails.

**t2** is hard because no single document answers anything. The queue answer
requires following an ADR supersession chain whose second link *changes which
technologies are deprecated* — reading only the first ADR gives 4 services
instead of 7. The cost answer requires ignoring a superseded pricing table that
looks perfectly authoritative. The incident arithmetic spans three reports, one
stating hours in words, one giving clock times, one crossing midnight UTC. Three
planted contradictions must be resolved by a stated precedence rule rather than
by plausibility. This is the shape that catches weak synthesis: a run can read
every document and still merge them wrongly.

**t3** is hard because the visible failures under-report the damage. Four
shipped tests fail, pointing at two defect families; there are six, plus a
required structural refactor. A run that makes the visible suite green scores
2 of 7. The cheapest wrong answer available — editing the shipped test file —
is caught by a hash gate and zeroes the score.

### Diversity

One task writes code from nothing, one writes prose and structured data from
documents, one repairs code that already exists. They stress different parts of
the pipeline: t1 and t3 lean on the executor's tool loop, t2 leans on
decomposition and on the synthesis node actually merging its inputs.

## 4. Graders, and how they were validated

`selfcheck.py`, run before any money is spent. Both Phase A labs ran an
equivalent and between them it caught **26** ground-truth defects, every one of
which would have surfaced later as "the models are weak". It checks, per task:

- a **reference solution** exists and scores full marks — if the reference
  cannot pass, a model failure says nothing about the model;
- an **empty workspace** scores zero, and does so through a named gate rather
  than by accident;
- **near-miss decoys** are rejected: for every t2 field, a submission correct
  everywhere except that field must lose exactly that field and nothing else;
- for t3, reverting **one module at a time** from the reference back to the
  buggy seed must cost exactly the defect families that module owns — this is
  what proves a family is independently detectable;
- for t2, the answer key is **re-derived from the corpus text** rather than
  trusted: `truth.py` holds transcribed numbers, and the selfcheck re-reads
  every document and fails if a transcription and the document disagree.

It currently runs **70 checks**. Two defects it caught before any spend:

1. **t3 group 3 scored 3/3 against the buggy package.** `shiftplan.assign` holds
   a module-level cache; in a single pytest process the group's first call
   filled the cache and its second was answered from it, so the mutable-default
   defect was invisible. Fixed by running **each group in its own pytest
   process** and namespacing ids per test. This is the same class of error as
   routerlab's round-1 degeneracy: a measurement that looks fine and is
   measuring nothing.
2. **t3 group 5 was not independently detectable.** Reverting `interval.py`
   lost families 1 *and* 5, because a date-scoping test used back-to-back
   windows and so depended on half-open interval semantics. Fixed by making
   those windows disjoint.

## 5. Replicates, ledgers, and what is recorded

**n = 3 per task per arm.** aforge runs are stochastic — the planner samples the
spine three times, leaf order varies, provider latency varies — so a single cell
is an anecdote.

Cells run **in sequence**, not in parallel. Arm B needs it (the ledger has to
carry forward in a defined order); arm A keeps it so that both arms meet the
same provider contention rather than one being measured on a quieter endpoint.

**Ledger policy differs between the arms, deliberately:**

- **Arm A: a fresh profile directory per cell.** aforge already learns across
  runs — `recordAndCalibrate` rewrites the planner's sizing ruler from measured
  leaves — so a shared ledger would make replicate 3 a continuation of
  replicate 1 rather than a repeat of it. The baseline has to measure the model,
  not the harness learning about the model.
- **Arm B: one profile directory shared across the three runs, in sequence.**
  This is the learning check (§6). Arm B's **run 1** is the like-for-like
  comparison against arm A, since both start cold; runs 2 and 3 measure what the
  ledger bought.

Per cell, `collect.py` records: the grade and its per-group breakdown, success,
wall clock split into plan and run phases, both exit codes, and — from the
completed graph, which is aforge's own accounting rather than an account-level
credit delta — calls, prompt/completion/cached tokens, cost, total turns, node
and leaf counts, per-leaf state, **per-leaf stop reason**, per-leaf turns and
tokens, and each leaf's failure text. The whole profile directory is snapshotted
after every cell.

Cost comes from the run's own usage accounting for the reason `bench/README.md`
gives: the API key is shared, and an account-level delta measured around a run
includes other traffic. It is a different number, not a noisier one.

## 6. The learning check (arm B)

The claim to be tested is that the router's decisions **shift between run 1 and
run 3** as graded outcomes accumulate in the ledger. The router does not exist
yet, so the measurement is designed to be format-agnostic: `collect.py`
snapshots every file under the profile directory after each cell, parsing JSON
where it can and recording line counts and a text prefix where it cannot.

The comparison, once the router lands:

1. **Decision diff.** For each (task, leaf) the router chose a model for, compare
   run 1's choice to run 3's. Report the number of leaves whose assigned model
   changed, and in which direction (up-rung or down-rung).
2. **Ledger delta.** Diff the routing-event records themselves between the
   snapshot after run 1 and after run 3 — new models seen, ability estimates
   moved, per-model observation counts.
3. **Escalation rate.** Fraction of leaves that needed a second rung, run 1 vs
   run 3. Phase A's cascade fired its second rung about a third of the time; if
   learning is working, that fraction should fall on tasks the panel has already
   seen.
4. **Cost and quality at constant task.** Score and cost for the *same* task in
   run 1 vs run 3. Learning that does not move either is bookkeeping.

A control matters here and is cheap: arm B should also be run once with a
**fresh** ledger (`LEDGER_MODE=fresh`) so that a difference between run 1 and
run 3 can be attributed to the ledger rather than to run-to-run variance. Three
sequential runs with a shared ledger, on their own, cannot separate the two.

## 7. Difficulty calibration protocol

Routerlab's round-1 lesson was that 28 of 36 tasks were passed by every model,
carrying zero information. The protocol here mirrors its fix:

1. Run arm A once on each candidate task (`REPS=1`).
2. If arm A passes a task cleanly, **harden it and record the round.** Target:
   arm A succeeds on at most half of its cells, or produces a visibly partial
   result. A task both arms ace measures nothing.
3. Re-validate with `selfcheck.py` after every hardening round — a hardened task
   with a stale answer key is worse than an easy one.
4. Keep the round-1 definitions and results; nothing is deleted.

Rounds are recorded in `BASELINE.md`.

## 8. Budget and backstops

**≤ $15 for the baseline arm.** Prior: `BENCHMARKS.md` records full-pipeline
runs between $0.18 and $0.41. Nine cells at the top of that range is ~$3.70, so
the cap is roughly 4x headroom.

Spent so far: **$0.6739** on panel selection (220 anchor cells).

Backstops, none of which is a work limit:

| guard | value | why |
|---|---|---|
| `PLAN_TIMEOUT` | 12m | a planner that has not produced a graph by here is wedged |
| `RUN_TIMEOUT` | 30m | `bench/run.sh` uses 40m; the tasks here are smaller |
| `RUN_TOKEN_BUDGET` | 2,000,000 prompt+completion | bounds a single cell to roughly $0.30 at the incumbent's prices |
| `LEAF_TOKEN_BUDGET` | 300,000 | `BENCHMARKS.md` records a leaf overrunning 500k to 748k because the landing reserve is uncapped; this keeps one leaf from eating a cell |

A cell that crashes for **harness** reasons is rerun and both attempts are kept.
Harness bugs are findings and are reported, but they are not evidence about arm
A. A cell that fails for **model** reasons stands.

## 9. What this design cannot establish

- Three tasks, one repository-free workspace each, one attempt at $15. This is
  not a benchmark, it is a controlled comparison of two configurations on three
  tasks.
- All three tasks have a cheap deterministic verifier by construction, which is
  exactly the condition Phase A said the cascade's economics depend on. That
  makes this a **favourable** setting for routing, not a neutral one, and the
  result should be read as an upper bound on the benefit for work of this shape.
- The anchor battery is single-turn; nothing in the panel selection proves a
  model can hold a long tool loop. If arm B shows a panel member failing to
  drive the executor, that is a finding about the panel, not about routing.
- Arm A's ledger is fresh per cell and arm B's is shared. The two arms are
  strictly comparable at run 1 only; §6's fresh-ledger control is what makes the
  rest comparable.

## 10. Files

| file | what it is |
|---|---|
| `panel.json` | the five models, each with a one-line data-backed justification, plus every exclusion and the open risks |
| `panel/anchor_battery.py` | the live-catalog gate and the on-scale ability measurement behind it |
| `panel/anchor-results.jsonl` | 220 raw cells |
| `tasks/t*/GOAL.md` | the brief each arm receives, byte-identical between arms |
| `tasks/t*/grade.py` | the graders; deterministic, no judge |
| `tasks/t1-logstore/reference/`, `tasks/t3-shiftplan/reference/` | reference solutions, used only by the selfcheck |
| `tasks/t2-synthesis/truth.py` | the t2 answer key, derived rather than asserted |
| `selfcheck.py` | 70 checks that must pass before any spend |
| `run-arm.sh` | the runner, parameterised by `ARM` |
| `collect.py`, `summarize.py` | one cell to one JSONL row; rows to a summary |
| `results-armA.jsonl` | every arm-A cell |
| `BASELINE.md` | what arm A did, and how it failed |

---

## 11. Phase B2 — the re-run against the fixed router

Written before the fixed router merged, so that what counts as success is fixed
in advance rather than chosen once the numbers are in.

Phase B produced a clean negative: routing went 3/9 to 2/9, the top rung served
zero calls, and the ledger's only measurable effect was to demote the panel's
best model and collapse two working tasks to zero. `REPORT.md` §5 lists six
defects and §8 orders the fixes. B2 asks whether fixing them turns the negative
into a positive, and it adds the assertions that would have caught the collapse
on the first run rather than the ninth.

### 11.1 What changed in the suite first

**t4-pathmatch is new, and it exists because of a flaw in Phase B's design.**
t1 and t3 both scored 0/3 in *both* arms on the *same* items. That is a stable
capability boundary, which is good for detecting a change — but with no cell in
either arm ever moving from fail to pass, Phase B could only ever have detected
harm. A routing experiment whose suite cannot express improvement is not a fair
test of routing.

t4 is built on the axis arm A's own failure data identified: leaf-level
conformance to a specification stated once in prose. Where t1 had three
all-or-nothing groups, t4 has **twelve small independent rules** spanning an
easy-to-hard range, so a submission can land anywhere on a gradient instead of
at one of two points. Four of the rules deliberately diverge from `.gitignore`,
and the grader reports **baseline groups (8) and divergence groups (4)
separately** — which is what lets "read the spec" be told apart from "wrote a
glob from memory".

### 11.2 Arms

| arm | ledger | runs | purpose |
|---|---|---|---|
| **B2** | shared, sequential | 4 tasks x n=3 | the headline; the learning check |
| **B2-fresh** | fresh per cell | 4 tasks x n=3 | the control that separates learning from variance |
| **B2-warm** | the B2 ledger, as it stands afterwards | t2 x1 | does the graded-observation gate stop the t2 regression recurring? |

Arm A's numbers for t1-t3 are already recorded and are **not** re-run; t4 gets
its own arm-A baseline from the calibration in §11.6, which is why that
calibration is n=3 rather than n=1.

**B2-warm is the cheapest test in the programme and the most direct.** Phase B's
single worst outcome was t2 going 1.000 to 0.000 in run 3 once five budget stops
from t1 had poisoned the global `exec.leaf` rating. Defect 1's fix — gating a
learned rating behind a minimum count of graded observations — should make that
impossible. Starting from the ledger state that produced the collapse and
running t2 once more asks exactly that question for the price of one cell.

### 11.3 Assertions that must hold — the checks Phase B lacked

These are pass/fail properties of the run, not of the model, and each one is a
defect from `REPORT.md` §5 turned into something the harness can check. They are
implemented in `assert-b2.py` and run over the results and event logs.

| # | assertion | the defect it guards |
|---|---|---|
| A1 | **The terminal rung is never weaker than rung 0.** For every recorded `candidates` list, the last entry's ledger rating must not be below the first's. | 3 — success demoting the top rung |
| A2 | **kimi-k2.6 appears in at least one escalation chain** on a hard-task (t1, t3, t4) cell that failed. If the strong model is never reached when the cheap one demonstrably failed, the cascade is decorative. | 4, 6 |
| A3 | **No escalation lands on a model rated below the one it escalated from.** Escalating to something weaker cannot help by construction. | 4 |
| A4 | **Every panel member has at least one observation** by the end of the shared arm, or the run is reported as exploration-starved. | 6 |
| A5 | **No rating with fewer than the gate's minimum graded observations changes a rung order.** Compare each run's `candidates` against the ledger's observation counts. | 1 |
| A6 | **`exec.leaf` ratings are conditioned**, i.e. more than one leaf key exists in the ledger once tasks of different sizes have run. | 2 |
| A7 | **A leaf escalation records its chain** — `escalation` is non-empty whenever `rung > 0`. | 5 |

**The assertions are validated against Phase B's own data**, which is the only
way to know they are not vacuous: run over `results-armB.jsonl` and its ledger,
**all seven fail**, and each failure names the defect it was written for.

```
FAIL A1  plan.bind: kimi-k2.6 (+1.00) terminal under ds-v4-flash (+1.74)
FAIL A2  models reached above rung 0 on failed hard cells: ds-v4-flash, gemma-3-12b
FAIL A3  t1-logstore: ds-v4-flash (-0.98) -> gemma-3-12b (-1.00)
FAIL A4  unobserved: gemma-3-12b, glm-4.7-flash, kimi-k2.6, qwen3-30b-a3b
FAIL A5  exec.leaf: gemma-3-12b n=0; exec.leaf: kimi-k2.6 n=0
FAIL A6  leaf classes in the ledger: ['exec.leaf']
FAIL A7  23 attempts above rung 0 with an empty `escalation`
```

Writing them was not enough on its own. On the first attempt **A1 and A3 passed
on the very run they were written to catch**: both compared ledger ratings, an
unmeasured model has no ledger row, so the comparison was skipped — and an
unmeasured model is exactly what the router was ordering against. They now fall
back to the cold-start prior the router itself uses, which is what makes them
bite in the case that matters. A5 had the same shape of hole: it asked whether
any rating was thin, when the question is whether a *reordered class* contains a
thin model.

A2 is the one that matters most: its failure in Phase B was invisible until the
whole arm had been analysed.

### 11.4 Capture

Unchanged from Phase B except that it is now enforced rather than hoped for:

- `aforge models` after **every cell**, into `models-after.txt` — already done,
  and additionally copied per replicate into the committed ledger directory.
- `router-events.jsonl` sliced per cell by line position, into
  `events-armB2.jsonl` beside the results.
- The full ledger directory after each replicate.
- **A run token** (§11.5) on every row.

### 11.5 Two harness guards, added after Phase B's incidents

Both incidents cost real analysis time and neither was a model failure.

- **Case-collision guard.** `run-arm.sh` refuses to start if the results path
  differs only by case from a file already in that directory. On this
  filesystem `results-armB.jsonl` and `results-armb.jsonl` are one file, and an
  `rm` of either name deletes both — which is how Phase B's arm-B results were
  destroyed and had to be rebuilt from the cell directories.
- **Run tokens.** Every invocation generates a token, stamps it on every row,
  and appends it to `.run-tokens` in the ledger directory. A shared ledger
  written by two overlapping passes now says so on sight, instead of having to
  be reconstructed from file ordering afterwards.

### 11.6 t4 calibration protocol

Same as Phase B's, with a sharper target learned from t1. It is not enough for
arm A to fail:

- **arm A must score at most 1 of 3 successes**, and
- **its scores must vary**. t1's arm-A cells scored exactly 0.667 three times
  out of three; a task that fails identically every time is another harm-only
  detector wearing a gradient's clothes. A spread of at least two distinct
  scores across three replicates is the bar.
- **The floor is not acceptable either.** All-zero means nothing above it can be
  measured.

If arm A aces it, harden. If arm A floors it, soften — and the softening lever
is named in advance so it is not chosen to flatter a result: **drop the hardest
divergence group (11, first-match-wins) from `success` and report it as a
stretch group**, rather than rewriting the task.

### 11.7 Budget and stopping

| item | cells | budget |
|---|---|---|
| t4 arm-A calibration | 3 | ≤ $3 |
| B2 shared | 12 | |
| B2-fresh | 12 | |
| B2-warm | 1 | |
| **B2 total** | **25** | **≤ $8** |

Phase B's 21 cells cost $1.40, so 25 cells at ≤$8 is roughly 4x headroom.

**Do not start B2 until the router fixes are merged**, and re-run
`selfcheck.py` and `go test ./...` first: a suite that has drifted since the
baseline would make the comparison meaningless in a way no amount of replicates
would reveal.

### 11.8 What B2 can and cannot conclude

It can conclude that the six defects are fixed, or that they are not. It can
conclude whether routing beats the single model **on a suite containing one task
where improvement is expressible** — which Phase B's did not.

It still cannot conclude anything about work without a cheap verifier, because
all four tasks have one by construction. And with n=3 on four tasks it remains a
controlled comparison of two configurations, not a benchmark.
