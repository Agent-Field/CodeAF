# Pin semantics after a refusal: the pre-registered experiment (2026-09-02)

This folder is the record of the design-of-experiments run that decided what a
`lane.talk` pin does when the router refuses the pinned lane for the model
(issues #375 and #456; landed as arm B in PR #533). The decision and the fix
shape are on issue #456; this is the evidence behind them.

## What is here

- `prereg.md` — the approved pre-registration.
- `report.md` and `report-table.md` — the summarized result and per-cell table.
- `order.txt` — the seeded run order.
- `baseline-35c1a79e.md` — the comparison baseline.

The raw cell folders, sealed map, and golden captures were removed after these
retained reports recorded their findings.

## What was left out, and where the raw run lived

Each cell also produced `frames.log` (every sampled frame), `f2p.log` and `suite.log`
(pytest output), `entry.json` and `prompt.txt` (the brief, reproducible from the canary
pool by id), and the full `home/` and `work/` directories (the isolated `CODEAF_HOME`
with its transcripts, and the checkout the model edited). Together they were 2.8 GB
and are not in the repository. The raw run lived in a temporary scratchpad on the
shared runner and does not survive a reboot of that machine. The
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
