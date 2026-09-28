# DeepSWE harness comparison

Ten coding harnesses, one model, the same 113 tasks, one attempt each.

| harness | solved | cost per task | cost per solved issue | mean time |
| --- | --- | --- | --- | --- |
| **senior-dev** | **62 of 113, 54.9%** | **22¢** | **1x** | 54 min |
| mini-swe-agent | 56, 49.6% | 38¢ | 1.9x | 44 min |
| codex | 51, 45.1% | 37¢ | 2.1x | 46 min |
| pi | 42, 37.2% | 35¢ | 2.4x | 52 min |
| omp | 31, 27.4% | 50¢ | 4.5x | 49 min |
| opencode | 30, 26.6% | 50¢ | 4.8x | 48 min |
| kilo | 30, 26.6% | 48¢ | 4.6x | 54 min |
| claude-code | 16, 14.2% | 19¢ | 3.4x | 32 min |
| deepseek-harness | 16, 14.2% | 150¢ | 26.6x | 94 min |
| muse-code | 3, 2.7% | 12¢ | 11.3x | 16 min |

senior-dev solved the most issues and paid the least for each one it solved:
nearly 4x the issues claude-code solved, at about half the cost per solve of the
next best harness.

Since then, on the same 113 tasks: 88 solved (77.9%, exact 95% CI 69.1% to 85.1%)
with DeepSeek V4.1 Flash, and 78 (69.0%, 59.6% to 77.4%) with Kimi K3. Those runs
are senior-dev alone, not a comparison.

## Setup

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
