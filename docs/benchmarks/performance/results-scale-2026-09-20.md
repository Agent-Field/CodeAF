# Session scale curve: seven CLIs on spark, 2026-09-20

Inputs: one raw sweep log per CLI, scale2-<cli>-20260920T161256Z.log for codeaf, codex, claude, pi, omp, opencode, cursor-agent, plus the run journal driver-20260920T161256Z.log. 35 runs completed and all converged; one earlier launch attempt (codeaf N=2) was gate-skipped and retried; the journal holds no GAVE_UP markers. MB below means kB/1024, taken from each run's phase=summary rss_kb and pss_kb lines.

## Method

- Harness: scale2.sh, a convergence-gated idle measurement: N sessions of one CLI launch into one workspace tree, and memory must settle (under 1 percent RSS change across 3 consecutive samples taken 2 s apart, 180 s timeout) before anything is read.
- WORKSPACES=same: all N sessions share one tree; the run happens in /home/santosh/src/benchhead under the real HOME (HOMEDIR=$HOME).
- IDLE_SECONDS=20: after convergence the harness holds a 20 s idle window, then takes one /proc-based aggregate reading (RSS and PSS summed over the session's process tree).
- No provider calls: each session is launched to its interactive prompt and the harness never sends text, so no session talks to a model API while it is measured.
- Medians are not used: the convergence gate makes the single settled reading the measurement. The driver journals load_before and load_after per run and applies a load gate before launch (90 s retry, GAVE_UP after 4 attempts; this sweep saw one skip and zero give-ups).

## Load story

The sweep began at load1 18.48: the first run (codeaf N=1) launched into it and its measurement window recorded gate load1 19.47. The driver's load gate then skipped codeaf N=2's first attempt (load_before 19.29) and reran it 90 s later at load_before 7.26. Load held roughly 3 to 9 through codex, claude and pi; omp ran at load_before 8.60 to 8.88 with its N=16 measurement window at gate load1 21.46; opencode ran at load_before 20.56 down to 12.45; cursor-agent ran at 15.92 down to 10.11, and its last run ended at load_after 9.60, with the sweep finishing at 2026-09-20T16:45:14Z. The journal's final recorded load is 9.60: the sweep ended near 10, not near 1.

## Curve table

Slope column: least-squares fit of PSS MB against N over the five points N = 1, 2, 4, 8, 16; the value is the marginal PSS MB per added session.

| CLI | procs N=1 | procs N=16 | PSS MB N=1 | PSS MB N=16 | marginal PSS MB per added session | RSS MB N=16 | converged or timed out |
|---|---|---|---|---|---|---|---|
| codeaf | 2 | 17 | 108.0 | 507.0 | 27.3 | 974.1 | converged |
| codex | 1 | 16 | 41.7 | 302.4 | 17.2 | 1126.1 | converged |
| claude | 1 | 16 | 250.5 | 3503.2 | 215.8 | 5051.6 | converged |
| pi | 1 | 16 | 115.9 | 1416.9 | 86.7 | 2052.8 | converged |
| omp | 1 | 16 | 330.2 | 4520.6 | 279.0 | 5533.1 | converged |
| opencode | 1 | 16 | 665.5 | 10466.8 | 653.2 | 11736.7 | converged |
| cursor-agent | 2 | 26 | 315.0 | 3661.4 | 227.6 | 5193.1 | converged |

## Raw per-N table

| CLI | N=1 PSS MB | N=1 RSS MB | N=2 PSS MB | N=2 RSS MB | N=4 PSS MB | N=4 RSS MB | N=8 PSS MB | N=8 RSS MB | N=16 PSS MB | N=16 RSS MB |
|---|---|---|---|---|---|---|---|---|---|---|
| codeaf | 108.0 * | 135.2 * | 131.8 | 186.5 | 239.2 | 370.7 | 405.0 | 641.1 | 507.0 * | 974.1 * |
| codex | 41.7 * | 70.7 * | 65.3 | 141.5 | 104.4 | 282.7 | 174.3 | 565.9 | 302.4 | 1126.1 |
| claude | 250.5 | 316.8 | 502.1 | 642.5 | 907.6 * | 1260.8 * | 1769.1 | 2524.0 | 3503.2 | 5051.6 |
| pi | 115.9 | 131.0 | 208.1 | 261.6 | 386.8 | 527.1 | 747.6 | 1047.6 | 1416.9 | 2052.8 |
| omp | 330.2 | 356.7 | 617.3 | 703.8 | 1191.6 * | 1406.1 * | 2304.9 * | 2783.1 * | 4520.6 * | 5533.1 * |
| opencode | 665.5 * | 668.7 * | 1242.6 * | 1330.9 * | 2464.9 * | 2723.3 * | 4847.3 * | 5441.6 * | 10466.8 * | 11736.7 * |
| cursor-agent | 315.0 * | 375.0 * | 580.2 * | 761.6 * | 721.6 * | 929.1 * | 2029.8 * | 2881.5 * | 3661.4 * | 5193.1 * |

Cells marked * are runs whose driver-log line shows load_before above 8.

Rows used: every scale2 log holds exactly one summary pass per N, so each cell comes from that log's only and final pass; the single retried launch (codeaf N=2) was gate-skipped before it started, so its one pass is the rerun. codeaf runs N chat processes plus 1 shared engine daemon (procs 2, 3, 5, 9, 17); codex, claude, pi, omp and opencode run exactly N processes; cursor-agent spawns helper workers and its counts vary (see caveats).

## Caveats

- codeaf's N=1 cell in this sweep reads 108.0 MB PSS (110,640 kB) versus 66.4 MB in the 2026-09-17 footprint doc and 72.0 MB (73,722 kB) in the 2026-09-18 scale run. Likely cause: the load-18 start, since the run launched at load1 18.48 and measured at gate load1 19.47. The slope, 27.3 MB per added session, not the N=1 cell, is the robust figure.
- codex's N=16 reproduces the 2026-09-18 run within noise: 302.4 MB (309,607 kB) here versus 304.0 MB (311,130 kB) then, a 1,523 kB (0.5 percent) gap.
- Versions as printed by each CLI on 2026-09-20: claude 2.1.278, opencode 1.18.31, pi 0.84.3, cursor-agent 2026.09.18 (2026.09.18-9a7762b), codex 0.154.0 (codex-cli), omp 18.1.13, codeaf 4f002ca2 (built 2026-09-20 12:08, rebuilt from the same rev after the checked-in binary arrived as a broken 9-byte shim).
- The asterisked cells (codeaf N=1 and N=16, codex N=1, claude N=4, omp N=4 through N=16, all opencode, all cursor-agent) ran at load_before above 8; ambient load at that level can shift a PSS reading the same way it likely shifted codeaf's N=1 cell, so treat those cells with the same caution.
- cursor-agent's process count varies run to run (2, 4, 4, 15, 26 at N=1, 2, 4, 8, 16): its N=4 run measured only 4 processes where N=1 and N=2 had worker helpers alive too, so the N=4 cell likely understates steady state and its slope leans on N=8 and N=16.
