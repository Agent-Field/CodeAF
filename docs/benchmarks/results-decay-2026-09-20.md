# Long-session decay and transcript growth, 2026-09-20

Machine: spark, Linux aarch64, 20 cores, 119 GB, shared with other users.
Working directory for every session: /home/santosh/src/benchhead (a real repo, 3,249 Go files).
All CLIs were resolved as a login shell does. Versions recorded today:

| CLI | version measured in this run | 2026-09-17 doc | note |
| --- | --- | --- | --- |
| codeaf | codeaf 4f002ca2 built 2026-09-20 12:08, go1.26.5 linux/arm64 | n/a | commit matches; build date printed 09-20, the binary at /home/santosh/src/benchhead/bin/codeaf was rebuilt since the 09-18 build |
| claude | 2.1.278 (Claude Code) | 2.1.274 | DRIFT: 2.1.274 was on PATH this morning; the run's fresh profile staged a 2.1.278 self-install, which is what was measured |
| omp | omp/18.1.13 | 18.2.4 | DRIFT, a version lower than the doc; "Update Available 18.2.6" banner in the pane |
| opencode | 1.18.31 | 1.18.31 | package updated on the box midday (binary printed 1.18.22 at 12:10, 1.18.31 by run time); the run measured 1.18.31 |
| codex | codex-cli 0.154.0 | 0.154.0 | matches; not drivable by the stub, empty row |
| pi | 0.84.3 | n/a | not drivable by the stub, empty row |
| cursor-agent | 2026.09.18-9a7762b | n/a | not drivable by the stub, empty row |

## Method, in five lines

1. Each CLI ran one interactive TUI session in its own tmux pane (160x48, TERM=xterm-256color, private socket footprint2-decay-<cli>-$$), cwd /home/santosh/src/benchhead, HOME a fresh profile dir /home/santosh/src/profiles/footprint2-decay-<cli>; every request was redirected to the local stub (STUB_PORT=8091, STUB_TOKENS=40, STUB_DELAY_MS=5), so real API spend was zero and no CLI ever touched a real provider.
2. 100 identical turns: send `reply with the single word ok`, and send the next only after the pane had been unchanged for 2 consecutive seconds having changed since the send.
3. After turns 1, 10, 25, 50, 75 and 100 the whole process tree was sampled: total RSS from VmRSS and total PSS from smaps_rollup, using the /proc walk from benchhead/docs/benchmarks/measure-cli.sh, unioned with any process that had detached from the pane but carried the run's profile HOME (codeaf daemonizes a `codeaf engine --daemon`; the union is what makes its row two processes instead of one).
4. Profile dir bytes were taken with du -sb at turns 0, 50 and 100; load1 was recorded at run start, run end and every turn, and any run whose load1 exceeded 8 was discarded and rerun once, keeping the lower-peak run.
5. Cap 45 minutes per CLI; a stall or death mid-loop is recorded as that CLI's result with pane evidence; codex, pi and cursor-agent cannot be driven by the stub and get empty rows with the probe's reason rather than numbers taken against a real provider.

## Load readings

load1 at run start and end per CLI, from each kept run's finish.txt; max is the peak per-turn load1 in turns.log.

| CLI | load1 start | load1 end | load1 max in run |
| --- | --- | --- | --- |
| codeaf | 3.91 | 6.52 | 10.77 (about 5 of 100 turns above 8, median ~4; first attempt discarded at sustained 23-33) |
| claude | 4.51 | 5.72 | 10.77 (19 turns above 8, turns 61-79; first attempt peak 14.66 discarded, archived at claude-run1/) |
| omp | 3.51 | 7.27 | 10.77 (23 turns above 8; first attempt load1_start 10.86, peak 13.02, discarded) |
| opencode | 3.91 | 6.48 | 10.77 (first attempt peak 14.66 discarded, preserved as *-run1-invalid files) |

The load rule was exercised to its end on every CLI: the shared box carried external bursts (other users' builds, sibling benchmark tasks) that pushed peaks past 8 in both the first run and the single allowed rerun. Per the rule the lower-peak rerun was kept each time, and the memory numbers stand: RSS/PSS of a fixed process tree is insensitive to CPU contention. Treat the absolute timing, not the memory, as carrying a few percent of noise.

## The table

MB = KB/1024. Slope is (turn 100 minus turn 1) RSS, i.e. MB per 100 turns by construction.

| CLI | RSS MB turn 1 | RSS MB turn 100 | delta MB | slope MB per 100 turns | PSS MB at turn 100 | profile bytes at turn 0 / 50 / 100 | turns completed | drive (tui or headless) |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| codeaf | 113.5 | 128.6 | +15.2 | +15.2 | 84.1 | 7,779,377 / 11,455,733 / 11,701,512 | 100 | tui |
| claude | 298.1 | 383.6 | +85.5 | +85.5 | 293.7 | 240,557,986 / 240,923,440 / 241,079,690 | 100 | tui |
| omp | 363.9 | 356.6 | -7.4 | -7.4 | 330.1 | 175,667,152 / 176,692,566 / 177,696,465 | 100 | tui |
| opencode | 888.5 | 965.9 | +77.4 | +77.4 | 962.7 | 124,878,472 / 162,746,395 / 163,441,539 | 100 | tui |
| codex | empty: not drivable by the stub. Probe 2026-09-19: "uses a WEBSOCKET transport: codex_api::endpoint::responses_websocket: failed to connect to websocket. A base URL does not cover it; a stub for codex needs a websocket server speaking its protocol" | | | | | | | |
| pi | empty: not drivable by the stub. Probe 2026-09-19: "Connection error." No documented base-URL variable is honoured; it would need its own provider config file | | | | | | | |
| cursor-agent | empty: not drivable by the stub. Probe 2026-09-19: "redirects at the transport level with CURSOR_API_ENDPOINT plus CURSOR_API_KEY, but speaks its own auth and streaming protocol rather than OpenAI or Anthropic" | | | | | | | |

Headline: omp is flat after 100 turns (RSS -2.0%, PSS -2.1%, a tight 356-372 MB band the whole way), codeaf is near-flat (+13.4%, most growth in the first 25 turns), claude climbs +28.7% in RSS while PSS stays flat (the climb is shared/file-backed pages, not private growth), and opencode holds the highest footprint of the four, +8.7% net with a GC dip at turn 75 (peak +13.4% at turn 75) and PSS near-equal to RSS (963 vs 966 MB: almost everything is private memory).

## Raw logs

Per CLI under /home/santosh/src/footprint2/decay/<cli>/: summary.md (every key verbatim), driver.log, samples.tsv (per-checkpoint process-tree RSS/PSS), du.tsv (profile bytes at turns 0/50/100), turns.log (per-turn load1), finish.txt, teardown.txt (SURVIVORS=N with every killed pid's cmdline). The shared driver: driver-template.sh; each CLI's adapted copy: driver-<cli>.sh. Invalidated first runs are preserved: claude-run1/ (whole run) and opencode's *-run1-invalid files plus driver-both-runs.log. The stub log for the whole session: stub.log (5,063 lines, every request served with 40 canned tokens).

## Caveats

- Load rule: see the load table. Every CLI needed its single allowed rerun; every kept run still peaked at 10.77 from external shared-box bursts. The memory numbers are unaffected; absolute timings are not trustworthy to a few percent.
- Turn-0 profile bytes are dominated by first-run bootstrap, not transcript state: claude staged a 234 MB self-install (.local/share/claude/versions/2.1.278), omp extracted 165 MB of natives (.omp/natives/18.1.13), opencode ran an npm plugin bootstrap of about 157 MB (.npm/_cacache, .config/opencode/node_modules, caught mid-flight at turn 0). Session-state growth proper per 100 turns: codeaf +3.9 MB, claude +0.5 MB, omp +2.0 MB (linear, ~20 KB/turn, one file added, no vacuum observed), opencode +6.5 MB (sqlite 1.5 MB + db-wal 4.2 MB + log + snapshot).
- omp required PI_NO_THINKING_LOOP_GUARD=1 against the stub: its thinking-loop guard reads the stub's cyclic 40-token filler as a stalled model and fails every turn without it (verified headless both ways). Onboarding keys for a fresh HOME: Enter, Escape, Ctrl+C.
- claude's two runs behaved differently on identical fresh launches: the invalid first run staged no self-install, wrote zero profile bytes across 100 turns and held RSS flat at 197-207 MB; the kept run staged the 2.1.278 self-install, wrote a transcript and climbed in RSS. Both are real claude behavior; the variance is unexplained from the driver logs alone and is the one honest hole in this table.
- codeaf's row is two processes (chat TUI plus daemonized `codeaf engine --daemon`) at every checkpoint; claude, omp and opencode were one process throughout.
- No CLI was pointed at a real provider at any point. The stub was the only endpoint; the person's real-model rule (open flash-tier for codeaf, OpenRouter for claude) never had to fire.
- Standing orders honored: CLAUDE_CODE_OAUTH_TOKEN was unset on every claude launch (the template's env -u, reworked into a nested env because GNU env 9.4 on spark rejects -u after assignments). The "watch that PR" order names no PR reachable from this work and asked nothing of it.
- Teardown on every run: SURVIVORS=0 in teardown.txt, the /proc environ audit for each profile HOME found zero processes, and every run's tmux server is gone. A final box-wide audit across all four profile HOMEs printed nothing.
