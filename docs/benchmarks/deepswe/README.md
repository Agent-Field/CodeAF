# DeepSWE harness comparison

Ten coding harnesses, one model, the same 113 tasks, one attempt each.

| | |
| --- | --- |
| Benchmark | full DeepSWE set, 113 tasks, one seed per harness |
| Model | `deepseek/deepseek-v4-flash-0731` through OpenRouter |
| Verifiers | official DeepSWE at `0b9fabb` |
| Budget | 3 h per task |
| Isolation | four shards per harness, a dedicated OpenRouter key per harness |
| Ran | nine harnesses on 2026-09-11; senior-dev on 2026-09-12 |

[`arms.csv`](arms.csv) has one row per harness: solved, reward rate, valid
grades, invalid outcomes, mean F2P and P2P, OpenRouter spend, cost per task,
mean agent seconds and model.

## How the columns in the README are derived

- **solved**: tasks the verifier passed, out of 113.
- **cost per task**: billed OpenRouter spend divided by 113, so tasks without a
  verifier result stay in the denominator.
- **cost per solved issue**: spend divided by tasks solved, shown as a multiple
  of senior-dev's (39.9¢).
- **mean time**: mean agent wall time per task.

## Limits

- One seed per harness. senior-dev's 62 against mini-swe-agent's 56 is not a
  statistically resolved difference.
- senior-dev departs from the sampling contract: the other nine sent temperature
  1.0 and top-p 0.95, senior-dev sent neither, so provider defaults applied.
- Five tasks produced no verifier outcome: codex 1, pi 1, omp 2, opencode 1.
  They count as unsolved.
- omp, opencode, kilo, deepseek-harness, claude-code and muse-code did not record
  per-attempt cost; their spend is known at harness level only.
- senior-dev ran under an earlier name for the binary; values are rewritten to
  `senior-dev`.
