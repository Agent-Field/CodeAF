# senior-dev `278a076` — DeepSWE 113, DeepSeek V4.1 Flash

**88/113 solved — 77.9%**, one attempt per task, official verifiers.

| Metric | Value |
|---|---|
| Solved | **88/113 — 77.9%** (exact 95% CI 69.1%–85.1%) |
| Mean F2P | 0.9549 |
| Mean P2P | 0.9822 |
| Valid grades | 113/113 — no invalid outcomes |
| Model cost | **$56.52 billed** — $0.50 per attempt |
| Mean agent time | 33 min (median 26 min, longest 103 min) |
| Runs that hit the 3 h budget | 0/113 |
| Model calls | 16,951 total, 150 mean |
| First attempt started | 2026-09-14T22:48:36+00:00 |
| Last attempt finished | 2026-09-15T04:53:10+00:00 |

## Setup

| | |
|---|---|
| Binary | senior-dev `278a076`, sha256 `1a2f212f4042f57e3cc86c76338731e75a488ad648850ebf6f9af0664978fa69` |
| Binary, re-authored | the same tree is `712d980` after the branch history was re-authored; other documents cite that SHA for this arm |
| Model | `deepseek/deepseek-v4.1-flash` via OpenRouter |
| Routing | pinned to two fp8 1M-context endpoints, `{"only": ["novita", "gmicloud"]}` |
| Compaction | `window` policy, capacity 500,000 |
| Effort | max, 131,072 output-token cap, `--variant max` |
| Budget | 3 h per task, hard timeout 10,920 s |
| Verifiers | official DeepSWE at `0b9fabb` |
| Hosts | four `e2-standard-32`, 6 concurrent containers, 2 CPU / 8 GiB, contained egress |

## By difficulty

| Cut | Passes | Rate |
|---|---|---|
| Hard (38 tasks) | 23/38 | 60.5% |
| Medium (38 tasks) | 29/38 | 76.3% |
| Easy (37 tasks) | 36/37 | 97.3% |

## By language

| Language | Passes | Rate |
|---|---|---|
| `go` | 32/34 | 94.1% |
| `javascript` | 5/5 | 100.0% |
| `python` | 21/34 | 61.8% |
| `rust` | 4/5 | 80.0% |
| `typescript` | 26/35 | 74.3% |

Per-language rows are 5–35 tasks wide and separate nothing.

## The tuned split

| Cut | Passes | Rate |
|---|---|---|
| Canonical eight (development set) | 7/8 | 87.5% |
| **Excluding the canonical eight** | **81/105 | 77.1%** |

Removing the eight development tasks moves the headline by 0.7 pp, so the
result is not an artifact of tuning on them. `dev_set` in `tasks.csv` marks which eight.

## The file

`tasks.csv` — one row per task:

| Column | Meaning |
|---|---|
| `task` `band` `difficulty_rank` `language` `dev_set` | which task, and how it is classified |
| `reward` | 1 pass, 0 fail |
| `f2p` `p2p` | fraction of issue tests now passing / pre-existing tests still passing |
| `f2p_passed` `f2p_total` `p2p_passed` `p2p_total` | the underlying test counts |
| `started` `finished` `seconds` | when the attempt ran and how long it took |
| `cost_usd` `model_calls` | per-attempt model spend and call count |
| `input_tokens` `output_tokens` `cache_read_tokens` `reasoning_tokens` | usage |
| `exit` `patch_files` `patch_bytes` | how it ended and what it wrote |
| `model` `harness_version` `attempt_id` | what produced it |

**$56.52 is the billed figure** and the one quoted: the dedicated key's counter
moved $534.28 → $590.80 over the sweep. The per-attempt `cost_usd` column sums to
$34.83 instead, because that telemetry prices tokens at model-level list rates
(models.dev, DeepSeek first-party) rather than at what the two pinned endpoints
actually charge. Treat the column as a relative measure across tasks, not as spend.

## Source

Published as the first row of the DeepSeek V4.1 Flash table in
[Coding Agent Benchmark Baselines](https://app.notion.com/p/Coding-Agent-Benchmark-Baselines-DeepSWE-Terminal-Bench-Aider-3ce03ed71f108147b0bdf596b800a731#50cc22a0368e4931a4139628001678b3).

The per-attempt artifacts behind these rows — `events.ndjson`, `run.log`, traces,
verifier stdout, patches — are about 3.7 GB and stay on the machines that produced
them. This run predates the rename; the values here are rewritten from `codeaf`.
