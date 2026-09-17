# The bash belt, measured — REPORT

The experiment set out to answer one question: does a task worker that runs the
planner's loop — **one bash, coordination through PlanDB, no auditor** — do the
work better or worse than the belt codeaf ships? The answer, with the numbers it
comes from.

**Verdict: the belt is cheaper and faster almost everywhere and loses a whole
capability. It does not pass the design's own bar and should not replace the
shipped belt as it stands.**

## The measurement

Six cells, two arms, three runs each, one model (`deepseek/deepseek-v4-flash`),
graded by code (the fixture's own suite, its public surface, its report's
sections). Arm A is the shipped belt. Arm B is CODEAF_TASK_BELT=bash. Both arms
run from one binary, so every row difference is the belt.

| cell | A pass | B pass | A med steps / $ / wall | B med steps / $ / wall |
|---|---|---|---|---|
| c1 fix a bug | 3/3 | 3/3 | 6 · $0.0066 · 41.7s | 8 · **$0.0057** · **22.1s** |
| c2 add a feature | 3/3 | 2/3 | 13 · $0.0160 · 124.0s | 10 · **$0.0098** · **61.2s** |
| c3 refactor 3 files | 3/3 | 3/3 | 8 · $0.0099 · 63.7s | 12 · **$0.0078** · **38.5s** |
| c4 write a report | 2/3 | 2/3 | 12.5 · $0.0132 · 95.7s | 10 · **$0.0073** · **39.5s** |
| c5 fan out a wide job | 3/3 | **0/3** | 26 · $0.0396 · 163.0s | 0 · 0 · 0 |
| c6 make an image | 3/3 | 3/3 | 26 · $0.0150 · 112.3s | 10 · **$0.0084** · **54.3s** |

**Arm A: 17 of 18 (94%). Arm B: 13 of 18 (72%).**

Where the belt works it is 44% cheaper and 52% faster (c6), 39% cheaper and 51%
faster (c2), and it matches the shipped belt's pass rate on four of the six
cells. It loses **one cell outright**: the wide job that wants a split. With the
graph verbs gone the belt does not fan out at all — 0 of 3, against the shipped
belt's 4-of-4 children every time.

Two numbers are friction, not failure: arm B takes **1-17 one-action rejections**
per run (the model still occasionally sends a batch the envelope refuses), and a
few `sed` idiom flags. Neither stopped a passing row.

## Against the design's own bar

The design named three conditions. The belt meets two.

- **No capability-regression cell: FAILED.** c5 is a regression, not a tie.
- **A ≥20% median improvement in steps or cost: MET** on five of six cells (cost
  14-45% lower, wall 47-52% lower where it passes).
- **Zero safety incidents: MET.** No write-scope or outside-ground incident in any
  row; truncation fired zero times.

By its own gate, this belt is **not a replacement**. It is a promising second
option with one hole in it.

## What the run cost, and what it taught on the way

The comparison only became readable after two porting faults were found and
fixed (docs/design/bash-task-loop/INVESTIGATION.md): the page told the model to
**batch** the calls its envelope refuses, and the auditor verified a **staged
diff** a shell worker never produces, so it refuted correct work on every row.
Before those fixes arm B passed 0 of the graded cells. They are the reason the
numbers above exist.

Coordination was then moved onto a ported **PlanDB CLI** (plancode's store, its
verbs, the worker page teaching them), so arm B is the loop the person asked
for: one bash, the store, no auditor.

## Boundaries this report records

- **The `do` door cannot measure this belt, by design.** `codeaf do` runs
  subharness leaves, and the design keeps every subharness leaf byte-identical
  (DESIGN.md:304). A `do`-door run shows both arms calling the same executor
  tools and the belt nowhere — the bench withheld its grid on exactly that
  finding (bench/bashloop/BASHBELT-DO-DOOR.md). The DeepSWE rig drives that same
  `do` door, which is why its sweep could not separate the arms either.
- **The task door is the measurement.** Every number here comes from the engine's
  own `StartTask` path, the one the TUI's task start uses.
- **A DeepSWE reading needs a task-door driver** for that rig, not `do`.

## What would change the verdict

1. Give the belt a split that the store can dispatch — the c5 hole is the whole
   gap between 13/18 and 17/18, and the PlanDB store already knows how to hold a
   subtree; the belt simply does not ask it to.
2. Teach the one-action envelope harder (or accept the retries as the cost of a
   single tool) — 1-17 rejections a run is real money at scale.
3. Re-run on the task door after either change; the bench is in
   bench/bashloop and runs the six cells in about forty minutes.

The two arms' code was also read side by side: on c1 the fixes were
byte-identical, on c3 the belt wrote the tighter signature (`taxRate(discounted
bool)` against `taxRate(i Item)`), and on c4 the shipped belt framed its report
better. The belt is not worse at the work; it is worse at dividing it.
