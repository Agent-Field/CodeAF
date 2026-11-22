# DeepSWE

**codeaf** solved the most DeepSWE tasks of ten coding harnesses, 54.9%, at the lowest cost for each one solved.

That row is codeaf run in senior-dev mode. At a shell the same program is `codeaf senior-dev`. The saved rows call the arm `senior-dev`.

| harness | solved | cost per task | cost per solved issue | mean time |
| --- | --- | --- | --- | --- |
| **codeaf** | **62, 54.9%** | **22¢** | **1x** | 54 min |
| mini-swe-agent | 56, 49.6% | 38¢ | 1.9x | 44 min |
| codex | 51, 45.1% | 37¢ | 2.1x | 46 min |
| pi | 42, 37.2% | 35¢ | 2.4x | 52 min |
| omp | 31, 27.4% | 50¢ | 4.5x | 49 min |
| opencode | 30, 26.5% | 50¢ | 4.8x | 48 min |
| kilo | 30, 26.5% | 48¢ | 4.6x | 54 min |
| claude-code | 16, 14.2% | 19¢ | 3.4x | 32 min |
| deepseek-harness | 16, 14.2% | 150¢ | 26.6x | 94 min |
| muse-code | 3, 2.7% | 12¢ | 11.3x | 16 min |

codeaf solved nearly 4× the issues claude-code solved, at about half the cost per solve of the next best harness.

With newer models, the same mode solved 77.9% on DeepSeek V4.1 Flash and 69.0% on Kimi K3.

| | |
| --- | --- |
| Model, every harness | `deepseek/deepseek-v4-flash-0731` through OpenRouter |
| Verifiers | official DeepSWE at `0b9fabb` |
| Budget | 3 h per task |

## Where codeaf placed first

| | codeaf | next best |
| --- | --- | --- |
| all tasks | **62** | mini-swe-agent 56 |
| medium and hard tasks | **36** | mini-swe-agent 30, codex 29 |
| medium band | **24** | mini-swe-agent 20 |
| hard band | **12** | codex 11 |
| Go | **21** | mini-swe-agent and codex 17 |
| Python | **20** | mini-swe-agent 18 |
| mean fail-to-pass | **88.64%** | mini-swe-agent 88.17% |
| cost per solved issue | **39.9¢** | mini-swe-agent 76.9¢ |

Task by task, against each of the nine other harnesses:

| opponent | only codeaf solved | only opponent solved | both | neither |
| --- | --- | --- | --- | --- |
| mini-swe-agent | **29** | 23 | 33 | 28 |
| codex | **25** | 14 | 37 | 37 |
| pi | **33** | 13 | 29 | 38 |
| omp | **44** | 13 | 18 | 38 |
| opencode | **40** | 8 | 22 | 43 |
| kilo | **41** | 9 | 21 | 42 |
| deepseek-harness | **53** | 7 | 9 | 44 |
| claude-code | **51** | 5 | 11 | 46 |
| muse-code | **60** | 1 | 2 | 50 |

## Tested versions

| harness | version | source revision / integration commit | release date (UTC) |
| --- | --- | --- | --- |
| codeaf | first stable with senior-dev mode: [v0.5.0](https://github.com/Agent-Field/CodeAF/releases/tag/v0.5.0) | [`dd0fcc654`](https://github.com/Agent-Field/CodeAF/commit/dd0fcc654a4a77b4cbf6beae27e6af1cfe4e4c28) — [#1488](https://github.com/Agent-Field/CodeAF/pull/1488) | 2026-09-30 |
| mini-swe-agent | 2.4.6 | `a83fcae` | [2026-07-23](https://github.com/SWE-agent/mini-swe-agent/releases/tag/v2.4.6) |
| codex (Codex CLI) | 0.154.0 | `6b9826e` | [2026-09-09](https://github.com/openai/codex/releases/tag/rust-v0.154.0) |
| pi | 0.85.1 | `d981de1` | [2026-09-05](https://github.com/earendil-works/pi/releases/tag/v0.85.1) |
| omp (Oh My Pi) | 18.1.17 | `3b3a6dc` | [2026-09-10](https://github.com/can1357/oh-my-pi/releases/tag/v18.1.17) |
| opencode | 1.18.30 | `3104c14` | [2026-09-09](https://github.com/anomalyco/opencode/releases/tag/v1.18.30) |
| kilo | 7.6.2 | `3d04228` | [2026-09-10](https://github.com/Kilo-Org/kilocode/releases/tag/v7.6.2) |
| claude-code | 2.1.267 | — | [2026-09-09](https://github.com/anthropics/claude-code/releases/tag/v2.1.267) |
| deepseek-harness (`@deepseek-ai/dsh`) | 0.1.5-rc.1 | — | [2026-09-10](https://github.com/deepseek-ai/deepseek-harness/releases/tag/dsh-v0.1.5-rc.1) |
| muse-code | 1.1.1-R2514.1 | — | — |

## How the columns are derived

- **solved**: tasks the official verifier passed.
- **cost per task**: billed OpenRouter spend divided by the number of tasks.
- **cost per solved issue**: spend divided by tasks solved, shown as a multiple of codeaf's (39.9¢).
- **mean time**: mean agent wall time per task.

## The data

[`arms.csv`](arms.csv) has one row per harness and [`versions.csv`](versions.csv)
the versions; the codeaf row is `senior-dev` in both. The
[verifier results ZIP](verifier-results.zip) contains the original scored
`reward.json` files. Its [manifest](verifier-results-manifest.json) lists the
included files, SHA-256 checksums, and ungraded attempts. The per-task records are in
one folder per campaign:

| Folder | What |
| --- | --- |
| [`2026-09-12-harness-comparison-v4-flash/`](2026-09-12-harness-comparison-v4-flash/) | the comparison above, `tasks/<harness>.csv` per harness |
| [`2026-09-15-senior-dev-278a076-v41-flash/`](2026-09-15-senior-dev-278a076-v41-flash/) | senior-dev mode on DeepSeek V4.1 Flash, 77.9% |
| [`2026-09-22-senior-dev-f3b9716-kimi-k3/`](2026-09-22-senior-dev-f3b9716-kimi-k3/) | senior-dev mode on Kimi K3, 69.0% |

The SHAs in those folders belong to senior-dev's own history from before it was
folded into codeaf, not to this repository. The scripts that run and grade
DeepSWE tasks are in [bench/deepswe](../../../bench/deepswe/).

```sh
awk 'FNR>1 || NR==1' 2026-09-12-harness-comparison-v4-flash/tasks/*.csv > all-arms.csv
```
