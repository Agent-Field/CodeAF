# Pin semantics after a refusal — result of the pre-registered run (2026-09-02, 15:57–18:14 EDT)

Four arms × four briefs (reef-145, attrs-1416, click-3740, tox-4031) × two replicates = 32 cells, chat door, deepseek-v4-flash on every seat, live OpenRouter, `lane.talk: anthropic` on A/B/C (a lane that never serves the model), N unpinned. Total spend $0.73. Sealed map opened only after every cell had a record. Amendments are in prereg.md (packaging skipped before the first cell; the unclosed-row exclusion withdrawn at scoring; one crashed grade read from its f2p log).

## Arm means (n = 8 each)

| arm | quality (issue's tests pass) | cost $ | wall s | re-demands (404 with `permits only`) | ttft median ms | visible routing sentences |
|---|---|---|---|---|---|---|
| A pin absolute (shipped, #368 tip) | 0/8 | 0.0001 | 902 | 2.1 | 1435 | 1.0 (the router's own 404 text) |
| B pin yields, one line | 8/8 | 0.0356 | 668 | 3.2 | 1488 | 0.8 (see below: none of them is B's line) |
| C pin yields, silent | 8/8 | 0.0271 | 598 | 3.0 | 1213 | 0.6 |
| N unpinned null | 8/8 | 0.0288 | 688 | 0.0 | 1378 | 0.0 |

Noise band (mean absolute replicate difference): cost $0.0105, wall 109 s, quality 0.

## The front

Non-dominated on (cost, wall, 1−quality): A, B, C, N. A is on the front only because doing nothing is free: it fixed no issue, and its "wall" is the driver's 900 s limit reached with a dead turn. On the pre-registered tiebreak (quality, then cost, then wall): **C, N, B, A** — and C and B tie inside the band on every metric (cost differs by $0.0085 against a band of $0.0105; wall by 70 s against 109 s). B and C also tie with N: pinning-then-yielding costs nothing measurable against not pinning at all.

**So: C cannot win by the no-silent-substitution law, B is indistinguishable from C on every number, and A is not a contender — it is a defect (#456).** The line costs nothing. The pick between B and C is the law, not the data, exactly as the pre-registration said it would be if H3 and H4 held.

## What the pre-registered hypotheses did

- H1 (re-demands): A ≥ turns — A paid 2.1 per cell (aux call + the turn), then the turn died, so there were no more turns to re-demand on. B and C were expected at exactly 1 and measured ~3: one on the first aux call, then **one more per task child**, because the retirement lives in the process and a task runs in its own session; each child re-pays a 404 before it, too, yields. Plus the router's other 404 class (`All providers have been ignored`) which is not the pin and not counted.
- H2 (wall): expected A to pay a round trip per turn. Measured: A pays the whole session. On the live wire the turn path does not recover from the pinned refusal at all (#456).
- H3 (quality equal across A/B/C): refuted for A by #456; B = C = N = 8/8.
- H4 (cost equal): B ≈ C ≈ N inside the band; A's $0.0001 is the cost of a dead turn.

## Two findings the numbers do not show

1. **B's line was never observed.** The visible-sentence metric found, in every B and C cell, only #368's own `refused · trying coreweave…` status rows (from the tasks' `All providers have been ignored` refusals), and in no B cell the sentence `anthropic cannot serve this model; routing on auto for this model until you pin again`. The wire-level retirement fired (one 404, then the turn went out on auto), so either the line is drawn for less than the 3 s frame sampling interval or it is not drawn on the live path. Before B ships, its line needs a golden frame from the live walk, not the unit test's rendering.
2. **Retirement does not reach task children** (above). If B ships, the retired pair has to travel to the child, or every task starts by paying the refusal and the person sees the line once per task rather than once.

## Cells

See report-table.md (per-cell rows with id, arm, brief, replicate, verdicts, cost, wall, re-demands, first-token latency, visible sentences, unclosed ledger rows). Baseline rows from the canary on 35c1a79e are in baseline-35c1a79e.md; N's walls agree with them (two briefs run to the wall even unpinned, because the chat keeps working after its task lands).
