# Repo-scale overhead, 2026-09-20

What seven agent CLIs cost at idle sitting in a big repository versus a tiny
one. One shared Linux machine (aarch64, 20 cores, 119 GB RAM), one session,
the same rig and script as the 2026-09-17 footprint comparison:
`~/src/benchhead/docs/benchmarks/measure-cli.sh`, its idle phase.

Big workdir: `/home/santosh/src/benchhead`, a real checkout, 3,334 Go files
by `git ls-files | grep -c "\.go$"` (the 2026-09-17 doc said 3,249; the repo
grew). Small workdir: `/tmp/footprint2-mini-repo`, a fresh `git init` with
one committed README and nothing else. No turns were fired and no provider
was called in any run: every cell is launch, idle, measure, tear down.

## Method

1. Every cell is one measure-cli.sh idle run: IDLE_SECONDS=30, SETTLE_SECONDS=4, a 160x48 pane on a private tmux socket, launched under the operator's real HOME inside measure-cli.sh's isolated environment.
2. Three reported reps per cell, taken from up to twenty runs as the first three whose peak load1 (sampled every 2 s across the whole run) stayed at or under 8; runs above the gate, runs parked on a trust dialog, and codeaf runs that caught a second engine daemon were discarded and rerun until three clean reps existed.
3. Inotify was sampled every 0.5 s across the whole pane process tree: fds whose symlink target is anon_inode:inotify, watches summed from the "inotify wd:" lines of each fd's fdinfo; the end-of-window figure is the last sample that still held an inotify fd, because a trailing zero after it is the teardown artifact.
4. codeaf runs `chat --session <transcript>` so a conversation is open; in this build its engine daemon re-parents out of the pane tree within seconds, so the daemon's own tree is sampled alongside every 0.5 s and summed into the cell (two processes, verified in every reported codeaf rep).
5. Idle CPU is percent of one core from utime+stime deltas over the window (perf_event_paranoid blocks perf; measure-cli.sh reads /proc), and the box is shared, so load was recorded with every run.

## Versions

| CLI | version |
| --- | --- |
| codeaf | 4f002ca2 built 2026-09-20 12:08, go1.26.5 linux/arm64 |
| codex | codex-cli 0.154.0 |
| claude | 2.1.278 (self-updated from 2.1.274 during the day) |
| pi | 0.84.3 |
| omp | omp/18.1.13 |
| opencode | 1.18.31 (self-updated from 1.18.22 during the day) |
| cursor-agent | 2026.09.18-9a7762b |

## The table

Medians of the three clean reps per cell. MB figures are binary (kB / 1024).

| CLI | PSS MB big | PSS MB small | delta | RSS MB big | RSS MB small | fds big | fds small | inotify watches big | inotify watches small | idle CPU% big | idle CPU% small |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| codex | 35.1 | 34.9 | +0.2 | 62.4 | 62.2 | 37 | 37 | 0 | 0 | 0.00 | 0.00 |
| claude | 314.5 | 307.7 | +6.8 | 316.4 | 309.6 | 38 | 39 | 3 | 3 | 1.12 | 1.18 |
| pi | 100.5 | 97.3 | +3.1 | 129.5 | 128.9 | 25 | 25 | 1 | 1 | 0.03 | 0.00 |
| omp | 298.0 | 360.8 | -62.8 | 324.5 | 385.7 | 37 | 36 | 0 | 0 | 0.89 | 0.66 |
| opencode | 630.6 | 654.7 | -24.1 | 670.7 | 684.0 | 39 | 39 | 296 | 4 | 2.69 | 2.72 |
| cursor-agent | 193.3 | 182.2 | +11.1 | 234.6 | 227.0 | 41 | 41 | 1 | 1 | 1.08 | 1.05 |
| codeaf | 86.4 | 83.9 | +2.4 | 101.1 | 98.4 | 24 | 23 | 0 | 0 | 0.62 | 0.62 |

Reps used per cell (big / small): codex 2,4,5 / 4,5,6; claude 2,4,7 / 4,5,6;
pi 1,2,4 / 1,2,4; omp 1,2,4 / 1,2,4; opencode 1,2,4 / 1,4,5; cursor-agent
4,5,7 / 4,5,6; codeaf 9,15,16 / 7,8,9. Rep numbers above 3 are reruns after a
discarded first attempt.

## Inotify fd counts

Median across the reported reps; per-rep detail in `raw/cells.tsv`.

| CLI | inotify fds big | inotify fds small |
| --- | --- |
| codex | 0 | 0 |
| claude | 1 | 1 |
| pi | 1 | 1 |
| omp | 0 | 0 |
| opencode | 2 | 2 |
| cursor-agent | 1 | 1 |
| codeaf | 0 | 0 |

## Load

Runs spanned 16:17Z to 17:40Z on 2026-09-20. The box is shared with other
benchmark tasks; the first pass rode a sibling burst (load1 16 falling to 8
at start) and a later stretch spiked to 16-21. Every reported cell's peak
load1 stayed between 1.73 and 7.89 (2 s samples across the whole run,
`max_load=` in each raw log). 25 runs were discarded or superseded: runs
over the load gate (peaks up to 20.89), five claude and cursor-agent
small-repo runs that sat on a folder-trust dialog, and codeaf big-repo runs
that caught a second engine daemon from a concurrent sibling probe in the
same workspace. Discarded runs are kept under `raw/` and `raw/invalid/`;
nothing was deleted.

## Caveats

- codex is repo-blind at idle: identical memory in both workdirs to within
  0.2 MB, zero inotify watches, zero idle CPU.
- opencode is the only CLI whose inotify footprint tracks the repository:
  296 watches on 2 fds in the 3,334-file checkout versus 4 watches in the
  one-README repo. It is also the heaviest at rest (630-655 MB PSS) and the
  busiest (about 2.7% of one core idle, versus 0.6-1.2% for the rest).
- claude's cost is session-shaped, not repo-shaped: about 310 MB PSS either
  way, +6.8 MB big versus small, 3 watches each side. Between-launch spread
  is real, though: 267 to 322 MB PSS across its big-repo reps.
- omp measured lighter in the big repo than in the small one, by 62.8 MB
  PSS, consistently across all six reported reps. The welcome screen is
  identical in both workdirs and the cause was not chased; reported as
  measured.
- codeaf holds zero inotify watches in this build (engine daemon sampled
  for 30 s with a conversation open, every rep). Its split is a ~35 MB PSS
  surface plus a ~50-53 MB PSS engine daemon in both workdirs, so the total
  barely moves with repo size: +2.4 MB PSS big versus small. The daemon is
  per-workspace and shared, so a second chat on the same workspace attaches
  and adds only the surface.
- pi varies about 30 MB PSS between launches (92-126 MB across reps); the
  medians carry it. One inotify watch in both workdirs.
- cursor-agent keeps one inotify fd and one watch in both workdirs, about
  1% of a core idle, +11.1 MB PSS in the big repo.
- Folder-trust dialogs: the first launch of claude and cursor-agent in the
  small repo parks on a trust dialog that measure-cli.sh's auto-answerer
  does not recognize (claude's wording has no "trust" next to a folder word
  on one line; cursor-agent's list uses a marker its detector does not
  know). Both dialogs were answered once by hand to trust the folder, then
  every reported cell measured a real session. Before that, claude's dialog
  state measured 161 MB PSS, roughly half its real session cost of 307.7 MB:
  the footprint README's warning, with numbers.
- The CLAUDE_CODE_OAUTH_TOKEN standing order is honoured by construction:
  measure-cli.sh launches the pane from an empty environment and the
  variable was never named in BENCH_ENV, so claude used its keychain login.
- teardown_survivors=0 in every run and no tmux server, CLI process or
  engine daemon from this work was left running; codeaf's engine daemons
  were killed after each codeaf cell by an exact-args match on the daemon
  command line.

## Files

- `results.md` this file
- `raw/<cli>-<big|small>-rep<N>.log` measure-cli.sh output per run (key=value lines, summary line, per-process lines, teardown proof)
- `raw/<cli>-<tag>-rep<N>.inotify` per-tick pane-tree inotify and tree stats, 0.5 s cadence
- `raw/<cli>-<tag>-rep<N>.daemon` per-tick engine-daemon stats (codeaf cells)
- `raw/<cli>-<tag>-rep<N>.load` per-tick load1, 2 s cadence
- `raw/cells.tsv` daemon-adjusted per-rep table
- `raw/final-parse.txt` the parse output that produced the table above
- `raw/matrix.log` the run ledger with per-cell start and end loads
- `raw/invalid/` runs parked on a trust dialog, moved out of the parse
- `raw/side/` superseded codeaf runs (surface-only reps 1-3 and attach-mode reps 4-6), kept for the record
- `scripts/count-inotify.sh` tree walk: inotify fds, watches, RSS, PSS, jiffies, fds
- `scripts/run-cell.sh` one cell: launch, sample pane tree + daemons + load, teardown
- `scripts/run-matrix.sh` and the rerun passes in the same directory, which produced the reps
- `scripts/parse-matrix.sh` rep selection (load gate, dialog filter, multi-daemon filter) and medians
