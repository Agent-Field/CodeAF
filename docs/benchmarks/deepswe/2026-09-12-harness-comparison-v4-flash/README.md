# Harness comparison — DeepSWE 113, DeepSeek V4 Flash

Ten coding harnesses, same model, same 113 tasks, one attempt each.
**senior-dev finished first on reward and on F2P.**

| Harness | Solved | Reward | Mean F2P | Mean P2P | Spend | Per task | Mean time |
|---|---|---|---|---|---|---|---|
| [**senior-dev**](tasks/senior-dev.csv) | **62/113** | 54.87% | 88.64% | 99.42% | $24.73 | $0.22 | 54 min |
| [mini-swe-agent](tasks/mini-swe-agent.csv) | 56/113 | 49.56% | 88.17% | 99.66% | $43.09 | $0.38 | 44 min |
| [codex](tasks/codex.csv) | 51/113 | 45.13% | 86.81% | 98.76% | $42.23 | $0.37 | 45 min |
| [pi](tasks/pi.csv) | 42/113 | 37.17% | 71.63% | 89.09% | $39.61 | $0.35 | 51 min |
| [omp](tasks/omp.csv) | 31/113 | 27.43% | 71.91% | 85.26% | $55.98 | $0.50 | 48 min |
| [opencode](tasks/opencode.csv) | 30/113 | 26.55% | 71.13% | 86.79% | $56.96 | $0.50 | 47 min |
| [kilo](tasks/kilo.csv) | 30/113 | 26.55% | 69.03% | 90.51% | $54.65 | $0.48 | 53 min |
| [deepseek-harness](tasks/deepseek-harness.csv) | 16/113 | 14.16% | 53.39% | 91.45% | $169.47 | $1.50 | 93 min |
| [claude-code](tasks/claude-code.csv) | 16/113 | 14.16% | 39.72% | 91.60% | $21.72 | $0.19 | 31 min |
| [muse-code](tasks/muse-code.csv) | 3/113 | 2.65% | 5.14% | 99.01% | $13.54 | $0.12 | 15 min |

Reward is the binary verifier result. F2P and P2P are macro-averages over all 113
scheduled tasks — the five invalid outcomes contribute zero rather than being dropped.
Per task is spend ÷ 113, so invalid outcomes stay in the denominator too.

## Setup

| | |
|---|---|
| Benchmark | full DeepSWE set, 113 tasks, one seed per harness |
| Model | `deepseek/deepseek-v4-flash-0731` via OpenRouter |
| Verifiers | official DeepSWE at `0b9fabb` |
| Budget | 3 h per task |
| Isolation | four GCP shards per harness, dedicated OpenRouter key per harness |
| Ran | nine arms 2026-09-11; senior-dev arm 2026-09-12 03:10Z–08:20Z |
| Total spend | ≈ $521.98 |

**The senior-dev arm departs from the frozen sampling contract.** The other nine sent
temperature 1.0 and top-p 0.95; senior-dev sent neither, so OpenRouter and provider
defaults applied. Everything else matched. senior-dev was deliberately excluded from
the nine-harness campaign and run separately a day later.

## Invalid outcomes

Five tasks across four arms produced no usable verifier outcome. They have an empty
`reward` in `tasks.csv`, stay in the 113-task denominator and contribute zero.

| Harness | Task | Why |
|---|---|---|
| [codex](tasks/codex.csv) | `valibot-recursive-schema-composition` | patch not accepted |
| [pi](tasks/pi.csv) | `langchain-request-coalescing` | agent and verifier both timed out |
| [omp](tasks/omp.csv) | `actionlint-action-pinning-lint` | agent killed, exit 137, no verifier result |
| [omp](tasks/omp.csv) | `kombu-virtual-queue-dead-lettering` | verifier timeout, exit 124 |
| [opencode](tasks/opencode.csv) | `pwntools-tube-multiplexing` | verifier timeout, exit 124 |

A non-zero harness exit is not an invalid result — a task can time out and still
produce a patch the verifier grades. Only tasks with no verifier outcome count here.

## The files

`arms.csv` — one row per harness: solved, reward rate, valid grades, invalid count,
mean F2P/P2P, OpenRouter spend, cost per task, mean agent seconds, model.

`tasks/<harness>.csv` — one file per harness, 113 rows each, ordered by difficulty
rank. All ten share a column layout, so `cat` gives the whole campaign back:

```
awk 'FNR>1 || NR==1' tasks/*.csv > all-arms.csv
```

Columns:

| Column | Meaning |
|---|---|
| `harness` `task` `band` `difficulty_rank` `language` `dev_set` | which arm, which task, and how the task is classified |
| `reward` | 1 pass, 0 fail, empty = no verifier outcome |
| `f2p` `p2p` | fraction of issue tests now passing / pre-existing tests still passing |
| `f2p_passed` `f2p_total` `p2p_passed` `p2p_total` | the underlying test counts |
| `seconds` `started` `finished` | agent wall time and timestamps |
| `cost_usd` `model_calls` | per-attempt model spend and call count |
| `exit` `timed_out` | harness exit status |
| `model` `harness_version` `attempt_id` | what produced it |

**`cost_usd` and `model_calls` are empty for six arms** — omp, opencode, kilo,
deepseek-harness, claude-code and muse-code did not record per-attempt usage. Their
spend is known only at arm level, in `arms.csv`.

## Source

Published as [Harness Comparison — DeepSWE](https://app.notion.com/p/Harness-Comparison-DeepSWE-3d603ed71f1080a5bcf3fddf8d7b2925).

The per-attempt artifacts behind these rows — `events.ndjson`, `run.log`, traces,
verifier stdout, patches — are about 19 GB and stay on the machines that produced
them. These campaigns ran while the binary was still called `codeaf`; the values
here are rewritten to `senior-dev`.
