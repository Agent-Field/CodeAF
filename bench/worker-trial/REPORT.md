# aforge-as-worker trial

Can a Claude session hand its lane work to the aforge CLI (`aforge do`, default crew,
plain prompt) instead of an Opus subagent or codex? Three queued issues, handed off
verbatim, judged as a reviewer would judge them. Nothing merges: the WIP freeze is on
and every PR opens as a draft.

Binary: `~/af-trial/bin/aforge`, built from `origin/dev` at `b95f8c29f`.
Crew and profile: the owner's own `~/.aforge/config.json` — work `deepseek/deepseek-v4-pro`,
plan `moonshotai/kimi-k3:high`. One scratch worktree per item, cut from `origin/dev`.

## The table

| item | complexity | quality | cost | wall | calls / rounds | exit | hand-work still needed | DX friction | Opus lane est. |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| #510 | — | — | — | — | — | — | — | — | — |
| #515 | — | — | — | — | — | — | — | — | — |
| #520 | — | — | — | — | — | — | — | — | — |

### How the Opus-lane column is estimated

A comparable Claude Opus 5 subagent lane on this repository — one that reads the seam,
edits three to five files, writes a named test, runs the package tests two to four
times and writes the change entry — is priced from the token counts this harness
reports for such a lane, at the published Opus 5 rates: **$5.00 / MTok input,
$25.00 / MTok output**, with cache reads at a tenth of the input rate. The estimate
is stated per item with the token figures it was built from, so the owner can
disagree with the figures rather than with the arithmetic.

## The runs

