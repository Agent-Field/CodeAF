# Idle CPU and RSS of a waiting chat surface, by replayed turns

Measured by `replay.sh` on 2026-09-18 (this repo, `bin/codeaf` built with
`make build` at commit 7cda67c9). One row per K: the pinned profile
`~/src/profiles/fresh-codeaf.tar` was restored FRESH to a scratch dir, the stub
(`~/src/stub.py`, 40 tokens at 5ms) served loopback-only inside
`unshare -rn --mount` with its own tmpfs /tmp, and
`codeaf chat --yolo --no-host --max-cost 5` was driven through K turns in a real
terminal (tmux, private socket). After the K-th turn completed the surface was
left waiting with nothing more sent and nothing in flight, and two consecutive
30s windows were sampled read-only: utime+stime over all threads of
`/proc/<pid>/task/*/stat`, plus VmRSS of the surface and of any engine-daemon
child.

Waiting state held, every point: idle after the last completed turn, nothing
in flight, nothing more sent (the send-nothing-more state, not a held
in-flight request).

| K | threads | idle %core, window 1 | idle %core, window 2 | RSS surface MB | RSS daemon MB |
| --- | --- | --- | --- | --- | --- |
| 5 | 14 | 0.800 | 0.867 | 75.3 | none |
| 50 | 15 | 0.867 | 0.767 | 91.0 | none |
| 200 | 16 | 1.067 | 0.800 | 94.8 | none |

Notes, all from the run artifacts under `runs/` (scratch, gitignored):

- No engine-daemon child existed in any point: with `--no-host` the surface
  process had no child whose cmdline was our binary, so the daemon column is
  "none" rather than a number.
- The stub answered about 5 requests per replayed turn (1001 answers for
  K=200), so the K axis is turns a person sent, not model calls made.
- Idle CPU is flat at about 1 percent of one core from K=5 to K=200; resident
  memory of the surface grows from 75.3 to 94.8 MB over the same range.
- Transcript size could not be read byte-exact from the profile: the session
  store is a preallocated graph WAL that measured the same 1,252,512 bytes at
  every K, and the v3 sessions directory stayed at about 1.4 KB, so a
  transcript-size column is omitted rather than reported wrong.
- A K=1 calibration point (10s windows, same method) measured 0.9 and 0.9
  percent of a core at 62.4 MB, and is not part of the 30s series.

Reproduce: `bench/idle-surface/replay.sh 5 50 200` (about 20 minutes; the table
lands in `bench/idle-surface/runs/`, gitignored). The per-point artifacts
(outer.log, stub.log, final-screen.txt, point.json) stay there too.
