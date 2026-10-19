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
| Ran | nine harnesses on 2026-09-10–11 UTC; senior-dev on 2026-09-12 UTC |

[`arms.csv`](arms.csv) has one row per harness: solved, reward rate, valid
grades, invalid outcomes, mean F2P and P2P, OpenRouter spend, cost per task,
mean agent seconds and model.

## Versions tested

These are the versions used for the comparison above, checked against all 113
saved task records per harness. Release dates come from the linked upstream
releases; they are not installation dates. The dates and versions describe this
campaign, not the versions available today.

| harness | version tested | release date (UTC) |
| --- | --- | --- |
| senior-dev | Benchmark build `8ae964c` | Not a release tag |
| mini-swe-agent | 2.4.6 | [2026-07-23](https://github.com/SWE-agent/mini-swe-agent/releases/tag/v2.4.6) |
| codex (Codex CLI) | 0.154.0 | [2026-09-09](https://github.com/openai/codex/releases/tag/rust-v0.154.0) |
| pi | 0.85.1 | [2026-09-05](https://github.com/earendil-works/pi/releases/tag/v0.85.1) |
| omp (Oh My Pi) | 18.1.17 | [2026-09-10](https://github.com/can1357/oh-my-pi/releases/tag/v18.1.17) |
| opencode | 1.18.30 | [2026-09-09](https://github.com/anomalyco/opencode/releases/tag/v1.18.30) |
| kilo | 7.6.2 | [2026-09-10](https://github.com/Kilo-Org/kilocode/releases/tag/v7.6.2) |
| claude-code | 2.1.267 | [2026-09-09](https://github.com/anthropics/claude-code/releases/tag/v2.1.267) |
| deepseek-harness (`@deepseek-ai/dsh`) | 0.1.5-rc.1 | [2026-09-10](https://github.com/deepseek-ai/deepseek-harness/releases/tag/dsh-v0.1.5-rc.1) |
| muse-code | 1.1.1-R2514.1 | Not independently established |

[`versions.csv`](versions.csv) records the same versions alongside source
revisions where recorded, release timestamps and source URLs, the artifact
checksums from frozen provenance records, and the first start and last finish
in each arm's saved task metadata. Its `harness` values join to `arms.csv`.
Those time bounds include setup and grading as recorded; they are not the mean
agent times in `arms.csv`.

The artifact kind states which bytes the checksum identifies: an executable,
release archive, prepared Python payload, or npm package tarball. An archive
checksum is not the checksum of its unpacked executable. The npm checksum is
SHA-1 as recorded in its package lock; the others are SHA-256. Empty source
revision, release timestamp, or URL fields mean that information was not
established for that artifact. A source URL identifies its upstream release or
package, not necessarily a download of the prepared benchmark payload.

senior-dev's build identifier names the measured agent before its integration
into codeaf; it is not a codeaf release tag. Muse Code's frozen lock records the
build selected from [Meta's stable channel](https://api.meta.ai/muse-code/channels/muse-stable)
and its [versioned artifact manifest](https://lookaside.facebook.com/lookaside/muse/download/?channel=muse&version=1.1.1-R2514.1&file=manifest.json).
Its historical publication date and whether a newer build was available at
launch were not independently established.

## Release timing and scope

Most tested versions were released within a week of the campaign.
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
