# Session resume cost, agent CLIs, 2026-09-20

Rig: spark, Linux aarch64, 20 cores, 119 GB, shared box. Every CLI was driven
against stub.py on 127.0.0.1:8092 (STUB_TOKENS=40, STUB_DELAY_MS=5, sentinel
"Zarquon"). No real provider was contacted at any point, so no model spend
occurred and no row is model-labelled. Working directory /home/santosh/src/benchhead.
Each CLI ran under its own fresh HOME profile (/home/santosh/src/profiles/
footprint2-resume-<cli>); the operator's real config dirs were never touched.
CLAUDE_CODE_OAUTH_TOKEN was unset for every claude launch (standing order).

## Method (five lines)

1. Per CLI: fresh profile, 50 identical turns ("reply with the single word ok")
against the stub; a turn is done when the pane is stable 2 s with no new stub
POST; then a clean exit, and the whole profile is tarred
(profiles-tar/<cli>-50turns.tar).
2. Transcript size is the byte size of the CLI's own session/history store:
codeaf .codeaf/v3/projects/<slug>/<id>/transcript.jsonl; claude
.claude/projects/<slug>/<session-id>.jsonl; omp
.omp/agent/sessions/<slug>/<timestamp>_<uuid>.jsonl; opencode
.local/share/opencode/opencode.db (sqlite session store).
3. Fresh launch: 3 reps, pristine restored profile, NEW session; tree RSS and
PSS (walk of /proc/<pid>/task/*/children) at the first usable prompt and after
30 s idle.
4. Resumed: 5 reps resuming the 50-turn session by the CLI's own documented
mechanism (exact command below); milliseconds from launch to usable, where
usable means history visible: the stub sentinel "Zarquon" or a user line from
the session rendered in the pane (the pane snapshots in raw/<cli>/panes/ show
it). First 3 reps also carry the memory readings.
5. Medians over load-clean reps only: a rep feeds a timing figure if load1 was
at or below 8 through the whole launch-to-usable window; it feeds an
idle-memory figure if load1 was at or below 8 at the idle reading. omp pools
two independent runs; per-figure N is in the caveats.

## Versions

- codeaf 4f002ca2 built 2026-09-20 12:08, go1.26.5 linux/arm64 (/home/santosh/src/benchhead/bin/codeaf, not on PATH)
- claude 2.1.278 (Claude Code)
- omp omp/18.1.13
- opencode 1.18.31
- codex codex-cli 0.154.0 (empty row, undrivable)
- pi 0.84.3 (empty row, undrivable)
- cursor-agent 2026.09.18-9a7762b (empty row, undrivable)

## Load readings

load1 was recorded with every rep (load_before, load_usable, load_idle per
line in the raw drive logs). Every timing rep behind the final medians ran at
load1 at or below 7.5; every idle-memory reading behind the final medians saw
load1 at or below 8 (maximum 7.96). Over-8 readings seen and how each was
handled:

- codeaf resumed rep 1: idle reading at load1 12.44, excluded from the memory
median (its timing window ran at 3.05 and stays in the timing median).
- omp run 1 resumed rep 3: timing window at 8.13, excluded from timing.
- omp rerun fresh rep 2: timing window at 8.58, excluded from timing.
- omp rerun fresh rep 1: idle reading at 8.21, excluded from the fresh memory
pool.

Because of the two omp timing exclusions omp was rerun once end to end (fresh
plus resumed phases against the same tarred profile) and the final omp row
pools the clean reps of both runs.

## Table

CLI | transcript MB | fresh idle PSS MB | resumed idle PSS MB | delta MB | MB per MB transcript | resume-to-usable ms (median) | resume mechanism
--- | --- | --- | --- | --- | --- | --- | ---
codeaf | 0.03 | 67.3 | 68.9 | 1.6 | 57.5 | 145 | codeaf chat --session <transcript.jsonl>
claude | 0.35 | 246.4 | 265.9 | 19.5 | 56.4 | 392 | claude --resume <session-id>
omp | 0.82 | 301.8 | 305.9 | 4.2 | 5.1 | 999 | omp -r <session-uuid>
opencode | 0.85 | 660.5 | 685.9 | 25.5 | 30.0 | 2962 | opencode -s <session-id>
codex | - | - | - | - | - | - | not stub-drivable: uses a WEBSOCKET transport: `codex_api::endpoint::responses_websocket: failed to connect to websocket`. A base URL does not cover it; a stub for codex needs a websocket server speaking its protocol (redirect-table.md)
pi | - | - | - | - | - | - | not stub-drivable: "Connection error." No documented base-URL variable is honoured; it would need its own provider config file (redirect-table.md)
cursor-agent | - | - | - | - | - | - | not stub-drivable: reaches the host, own protocol: `POST /auth/exchange_user_api_key`; redirects at the transport level with `CURSOR_API_ENDPOINT` plus `CURSOR_API_KEY`, but speaks its own auth and streaming protocol rather than OpenAI or Anthropic (redirect-table.md)

## Caveats

- Per-figure N: codeaf fresh idle 3, resumed idle 2 (third rep excluded on
load), resumed timing 5. claude 3, 3, 5. opencode 3, 3, 5. omp 5, 6, 9
(pooled across two runs). The brief's 3/5 rep counts are otherwise met.
- Transcript bytes are for THIS conversation shape: 50 turns of a short user
line and a 40-token stub reply (codeaf 29,938 B; claude 362,080 B; omp
862,527 B; opencode 888,832 B). A session with tool calls or long code grows
the store much faster; the MB-per-MB column is the transferable number.
- The small deltas (codeaf 1.6 MB, omp 4.2 MB) are within a run-to-run spread
of one to two MB: omp measured twice gave per-run deltas of 3.4 and 5.8 MB.
Read those two rows as "no measurable eager-history cost at 50 turns", not as
precise multipliers. The 56 to 57 MB-per-MB ratios for codeaf and claude are
real but dominated by fixed heap overhead against a sub-megabyte transcript.
- opencode's transcript figure is its whole sqlite store, which also carries
non-session tables; if any of those grow per session its true MB-per-MB is
slightly lower than 30.0.
- Timing is launch to history-visible from a warm page cache (the profile tar
was extracted minutes earlier); cold-cache resume of a big session is slower.
- The stub never streamed real model output (40 tokens, 5 ms delay). Long
tool-use sessions may shift how much history a CLI keeps hot.
- PSS is the tree sum (shared pages counted once); RSS is also in the raw
logs. Process counts at the idle reading: codeaf 2 to 4, claude 1, omp 1,
opencode 1.
- codeaf was configured with model id deepseek/deepseek-v4.1-flash from its
own catalog; every request hit the stub, so the open-flash-model-only
restriction never bound. No real model was used for any row.
- Background context: the same drive script was rerun today after a process
tree walk undercount was fixed (the /proc children walk in measure-cli.sh
drops every child; noted there too). Memory numbers before that fix
undercounted multi-process trees.

## Raw material (all under /home/santosh/src/footprint2/resume/)

- raw/drive-codeaf.log, raw/drive-claude.log, raw/drive-opencode.log: the
single B+C run for each (rerun from tar)
- raw/drive-omp.log: omp run 2 (B+C from tar); raw/drive-omp-build.log: omp
run 1 in full (build plus B plus C)
- raw/drive-{codeaf,claude,opencode}-build.log: build-phase archives
- raw/analyse-single.log: analyse.py output (single-log medians, no load
filter); raw/analyse-pooled.log: analyse-pooled.py output (the table above,
load-filtered, omp pooled)
- raw/rerun-all.log, raw/rerun-omp2.log: wrapper logs
- raw/<cli>/panes/: per-rep pane snapshots (resumed-N-usable.txt shows the
rendered history; e.g. omp resumed rep 2 shows "reply with the single word
ok (19m ago)")
- raw/<cli>/stub.log, raw/<cli>/stub.out: stub logs (the B+C-phase stub log;
build-phase POST counts are the posts= lines in the drive logs)
- scripts/drive-one.sh, scripts/rerun-mem.sh, scripts/analyse.py,
scripts/analyse-pooled.py
- profiles-tar/<cli>-50turns.tar: the 50-turn profile per CLI

Teardown: every rep printed "teardown survivors=0"; after the last run no
benchmark process was left (no stub, no drive script, no CLI under the test
profiles) and no tmux server was left on any footprint2-resume-* socket.
