# DeepSWE

**codeaf** solved 62 of 113 tasks (54.9%) at 22¢ a task — the most issues solved in this comparison, and the lowest cost for each one solved.

That row is codeaf run in senior-dev mode. At a shell the same program is `codeaf senior-dev`. The saved rows call the arm `senior-dev`.

| harness | solved | cost per task | cost per solved issue | mean time |
| --- | --- | --- | --- | --- |
| **codeaf** | **62 of 113, 54.9%** | **22¢** | **1x** | 54 min |
| mini-swe-agent | 56, 49.6% | 38¢ | 1.9x | 44 min |
| codex | 51, 45.1% | 37¢ | 2.1x | 46 min |
| pi | 42, 37.2% | 35¢ | 2.4x | 52 min |
| omp | 31, 27.4% | 50¢ | 4.5x | 49 min |
| opencode | 30, 26.6% | 50¢ | 4.8x | 48 min |
| kilo | 30, 26.6% | 48¢ | 4.6x | 54 min |
| claude-code | 16, 14.2% | 19¢ | 3.4x | 32 min |
| deepseek-harness | 16, 14.2% | 150¢ | 26.6x | 94 min |
| muse-code | 3, 2.7% | 12¢ | 11.3x | 16 min |

codeaf solved nearly 4× the issues claude-code solved, at about half the cost per solve of the next best harness.

On the same 113 tasks, later runs of this mode solved 88 (77.9%, exact 95% CI 69.1% to 85.1%) with DeepSeek V4.1 Flash, and 78 (69.0%, 59.6% to 77.4%) with Kimi K3. Those figures are this mode with those models. They are not a second comparison against the other harnesses.

## The comparison

Every harness ran the full DeepSWE set: 113 tasks, the official verifier, a 3-hour budget on each task, and one model, `deepseek/deepseek-v4-flash-0731`, through OpenRouter. Each harness had its own key and ran as four shards. Each completed one pass of the set, and that pass is the table.

| | |
| --- | --- |
| Benchmark | full DeepSWE set, 113 tasks |
| Model | `deepseek/deepseek-v4-flash-0731` through OpenRouter |
| Verifiers | official DeepSWE at `0b9fabb` |
| Budget | 3 h per task |
| Isolation | four shards per harness, a dedicated OpenRouter key per harness |
| Ran | nine harnesses on 2026-09-10–11 UTC; codeaf on 2026-09-12 UTC |

codeaf used the provider's default sampling. The other harnesses sent temperature 1.0 and top-p 0.95.

[`arms.csv`](arms.csv) has one row per harness: solved, reward rate, valid
grades, invalid outcomes, mean F2P and P2P, OpenRouter spend, cost per task,
mean agent seconds and model. The codeaf row is stored there as `senior-dev`.
The binary's name on the day of the run was an earlier one.

## Tested versions and public references

The nine competing harness versions below were checked against all 113 saved
task records per harness. The codeaf row uses the public integration and first
stable release that contain senior-dev mode, explained below. Release dates come
from the linked upstream releases; they are not installation dates. These
references describe the comparison and its public lineage.

| harness | tested version / public reference | source revision / integration commit | release date (UTC) |
| --- | --- | --- | --- |
| codeaf | First stable: codeaf [v0.5.0](https://github.com/Agent-Field/CodeAF/releases/tag/v0.5.0) | [`dd0fcc654`](https://github.com/Agent-Field/CodeAF/commit/dd0fcc654a4a77b4cbf6beae27e6af1cfe4e4c28) — [#1488](https://github.com/Agent-Field/CodeAF/pull/1488) | 2026-09-30 |
| mini-swe-agent | 2.4.6 | `a83fcae` | [2026-07-23](https://github.com/SWE-agent/mini-swe-agent/releases/tag/v2.4.6) |
| codex (Codex CLI) | 0.154.0 | `6b9826e` | [2026-09-09](https://github.com/openai/codex/releases/tag/rust-v0.154.0) |
| pi | 0.85.1 | `d981de1` | [2026-09-05](https://github.com/earendil-works/pi/releases/tag/v0.85.1) |
| omp (Oh My Pi) | 18.1.17 | `3b3a6dc` | [2026-09-10](https://github.com/can1357/oh-my-pi/releases/tag/v18.1.17) |
| opencode | 1.18.30 | `3104c14` | [2026-09-09](https://github.com/anomalyco/opencode/releases/tag/v1.18.30) |
| kilo | 7.6.2 | `3d04228` | [2026-09-10](https://github.com/Kilo-Org/kilocode/releases/tag/v7.6.2) |
| claude-code | 2.1.267 | — | [2026-09-09](https://github.com/anthropics/claude-code/releases/tag/v2.1.267) |
| deepseek-harness (`@deepseek-ai/dsh`) | 0.1.5-rc.1 | — | [2026-09-10](https://github.com/deepseek-ai/deepseek-harness/releases/tag/dsh-v0.1.5-rc.1) |
| muse-code | 1.1.1-R2514.1 | — | Not independently established |

[`versions.csv`](versions.csv) records the same versions alongside source
revisions where recorded, public release timestamps and source URLs. Its
`harness` values join to `arms.csv`; the codeaf row is `senior-dev` in both
files. Revisions are stored in full where available. Empty revision, release
timestamp, or URL fields mean that information was not established for that row.

For codeaf, `version` names the first stable release containing senior-dev mode,
and `source_revision` names the public integration commit from #1488. That
commit is contained in v0.5.0; it is distinct from the release tag's commit,
`4949397498f775a80de16d3e6429b0f6f933f54c`. The comparison ran before the
integration. This row is where the measured agent became publicly available in
codeaf. It does not describe a new run on v0.5.0.

Muse Code's version comes from the frozen benchmark records. Its historical
publication date and whether a newer build was available at launch were not
independently established.

## Release timing and scope

Most competing harness versions were released within a week of the campaign.
mini-swe-agent 2.4.6 was about seven weeks old, but was still the latest
[published release](https://github.com/SWE-agent/mini-swe-agent/releases)
at launch. The comparison does not establish how newer versions perform.

- **OpenCode was 1.x.** Version 1.18.30 was the latest production 1.x
  release at launch; [1.18.31](https://github.com/anomalyco/opencode/releases/tag/v1.18.31)
  followed on September 14. V2 development already existed, as a
  [contemporaneous upstream issue](https://github.com/anomalyco/opencode/issues/42878)
  shows. This row does not measure OpenCode 2.
- **Claude Code was pinned before a patch release.** Version
  [2.1.268](https://github.com/anthropics/claude-code/releases/tag/v2.1.268)
  was published on September 10 at 20:30:54 UTC, before 112 of this arm's 113
  tasks started. Its release notes include a fix for HTTP 400 responses on
  third-party Anthropic-compatible endpoints. The campaign used such an
  endpoint; whether that fix would change the result has not been established.
  Version [2.1.269](https://github.com/anthropics/claude-code/releases/tag/v2.1.269)
  arrived after all tasks had started, near the end of the arm.
- **DeepSeek Harness had a newer release candidate.** Version
  [0.1.5-rc.2](https://github.com/deepseek-ai/deepseek-harness/releases/tag/dsh-v0.1.5-rc.2)
  was published on September 10 at 15:09:34 UTC, before this arm ran. The
  frozen package lock selected npm's `latest` tag at 0.1.5-rc.1. The rc.2
  release notes describe feedback dialogs and interface layout changes; no
  run on rc.2 is included here.

## How the columns are derived

- **solved**: tasks the verifier passed, out of 113. A task with no verifier
  outcome counts as unsolved. Five tasks produced none: codex 1, pi 1, omp 2,
  opencode 1.
- **cost per task**: billed OpenRouter spend divided by 113, so tasks without a
  verifier result stay in the denominator. omp, opencode, kilo,
  deepseek-harness, claude-code and muse-code did not record per-attempt cost;
  their figure is that harness's total spend.
- **cost per solved issue**: spend divided by tasks solved, shown as a multiple
  of codeaf's (39.9¢).
- **mean time**: mean agent wall time per task.
