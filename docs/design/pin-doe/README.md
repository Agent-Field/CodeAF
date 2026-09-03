# Pin semantics after a refusal: the pre-registered experiment (2026-09-02)

This folder is the record of the design-of-experiments run that decided what a
`lane.talk` pin does when the router refuses the pinned lane for the model
(issues #375 and #456; landed as arm B in PR #533). The decision and the fix
shape are on issue #456; this is the evidence behind them.

## What is here

- `prereg.md` — the pre-registration as approved before the first cell, with every
  amendment declared in place and timestamped.
- `report.md`, `report-table.md` — the report and the per-cell table read after the
  sealed map was opened.
- `sealed.json`, `order.txt` — the sealed cell-to-arm map and the run order.
- `baseline-35c1a79e.md` — the unpinned baseline rows the run was compared against.
- `cells/<id>/` — one folder per cell (32: four arms × four briefs × two replicates):
  `arm.json` (arm binary and pin), `cell.json` (the driver's record: wall, cost,
  first-token latency, calls, verdicts), `door.json`, `judge.json` (the grade: the
  issue's fail-to-pass tests and the whole suite), `cell.log`, `screen.txt` (the final
  screen), and `ledger-summary.txt`, one line per row of the run's call ledger
  (`<home>/logs/calls.jsonl`), which is where the `permits only` 404s and the
  re-demands were counted.
- `golden/` — PR #533's acceptance proof on the live router with deepseek-v4-flash:
  the tmux golden frames showing the one-line sentence, and the abridged ledgers
  showing one `permits only` 404 per run (reef-145 through the chat door; attrs-1416,
  a task-spawning brief, one 404 across the whole run including its children).

## What was left out, and where the raw run lived

Each cell also produced `frames.log` (every sampled frame), `f2p.log` and `suite.log`
(pytest output), `entry.json` and `prompt.txt` (the brief, reproducible from the canary
pool by id), and the full `home/` and `work/` directories (the isolated `AFORGE_HOME`
with its transcripts, and the checkout the model edited). Together they were 2.8 GB
and are not in the repository. The raw run lived in the reporting session's scratchpad
on the shared box (`/tmp/claude-1001/-home-santosh-src-aforge-v2/…/scratchpad/pin/run1/`
and `…/scratchpad/e2e-456/`) and does not survive a reboot of that machine. The
arm binaries were built from `exp/pin-yields-line` (arm B, 082a0050) and
`exp/pin-yields-silent` (arm C, e967ad57) on top of PR #368's tip 6b35eb52 (arm A,
the shipped behaviour byte-for-byte).

## Reading the numbers

Arm means (n = 8): A pin absolute 0/8 tests pass, $0.0001, wall 902 s, 2.1 re-demands;
B yields with a line 8/8, $0.0356, 668 s, 3.2; C yields silently 8/8, $0.0271, 598 s,
3.0; N unpinned 8/8, $0.0288, 688 s, 0. Noise band: cost $0.0105, wall 109 s. B and C
tie inside the band on every metric and both tie with N; A is on the front only because
a dead turn is free. The user chose B (say so) over C (silent) by the no-silent-
substitution law. Two findings from the cells shaped the fix: the sentence was never
observed in any frame, and every task child re-paid the 404; both causes are recorded on
#456 and in PR #533's change entry.
