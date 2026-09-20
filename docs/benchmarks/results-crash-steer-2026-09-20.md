# Steering latency and crash recovery, 2026-09-20

One Linux machine (aarch64, 20 cores, 119 GB RAM, shared, load 2-4 typical), one repository
(/home/santosh/src/benchhead), one stub endpoint. Every measured turn ran against the local stub,
never a real provider, so no row cost any tokens.

| CLI | version measured |
| --- | --- |
| CodeAF | 4f002ca2 built 2026-09-20 12:08, go1.26.5 linux/arm64, benchhead binary (rebuilt from the same rev after the checked-in binary arrived as a broken 9-byte shim; the earlier stamp was 2026-09-18 08:34) |
| claude | 2.1.278 (Claude Code); the shared installer symlink auto-updated from 2.1.274 to 2.1.278 at 12:10 EDT before the first launch, auto-update disabled during runs |
| omp | omp/18.1.13 |
| opencode | 1.18.31 (the installed binary at /home/santosh/.npm-global/bin/opencode self-reports 1.18.31; the 2026-09-19 probe had seen 1.18.22, the npm package on the box moved between the two dates; recorded verbatim, not on the login PATH) |
| codex | codex-cli 0.154.0 |
| pi | 0.84.3 |
| cursor-agent | 2026.09.18-9a7762b |

## Method

1. One shared stub (python3 /home/santosh/src/stub.py) on 127.0.0.1:8093 with STUB_TOKENS=40 and
   STUB_DELAY_MS=3000, so every answer streams one word every 3 s and a turn stays in flight about
   120 s. No real provider was contacted by any measured row; the stub log is the wire-side record.
2. Steering, per CLI, 5 reps, medians: interactive session in a 160x48 tmux pane on a private
   socket, fresh HOME. Turn started with "write a long poem about the sea"; at streaming start
   plus 2 s, Escape was sent via tmux send-keys. If pane output was still changing 10 s later,
   the rep was ended and Ctrl-C was tried on a fresh rep; the key each CLI honours is a column
   of the table. Output stop = last pane-content change, counted only after 4 s of stillness
   (the 3 s delta period makes 4 s a stop, not a gap). CPU stop = utime+stime over the walked
   process tree, sampled every 50 ms, stopped when a rolling 1 s window stays under 2 jiffies
   (2 percent of one core) for two consecutive windows. Then one more turn checked the session
   still answers.
3. Crash, per CLI: 5 turns of "reply with the single word ok", then turn 6 "write a long poem
   about the sea" killed with kill -9 across the discovered process tree at streaming start
   plus 2 s. Relaunch under the same profile, then check: session listed, turns 1-5 intact,
   partial turn 6 visible, continuable with a new turn against the stub, and any recovery,
   rollback or corruption message quoted.
4. Every kill targeted only pids discovered from the tmux panes of this run, each confirmed
   against the pid own /proc command line first; no pattern kill anywhere. Load1 was recorded
   with every run and every timing figure; any timing rep with load1 above 8 was discarded and
   rerun.
5. The four drivable CLIs were measured by parallel workers, each with its own tmux socket
   and fresh HOME profiles, all against the one stub; codex, pi and cursor-agent cannot be
   stub-driven and get empty rows with the probe reason quoted. A row measured against a real
   model would be labelled with its model id; none was.

## Load readings

- The fan-out window was heavily loaded by other users at times: coordinator readings 7.71 at
  stub start, 2.96 after staging, and the first omp pass saw load1 between 9.75 and 12.27
  during its first two reps, which the load rule discarded. By cleanup time load1 was 1.73.
  Every timing rep with load1 above 8 inside its window was discarded and replaced: codeaf
  opencode lost 1, omp lost its first two completed reps of the first pass. The final omp run
  kept all 5 reps under 8 (maxima 7.02, 6.94, 4.39, 2.27, 2.27).
- Kept steering windows ran at load1 under 8 throughout (codeaf kept reps 3.51 to 8.27 at the
  edges, claude kept maxima 5.05 to 7.69, opencode kept C-c windows 4.67 to 7.47 and
  double-Escape windows max 5.38, omp kept maxima 6.94 and 7.02 so far). Crash build phases
  are not timing figures; their per-turn readings are logged in raw/<cli>/loads.txt.

## Table 1: steering latency

| CLI | key honoured | ms to output stop (median) | ms to CPU stop (median) | session answers after (y/n) |
| --- | --- | --- | --- | --- |
| CodeAF | Escape | 0 | 7016 | y |
| claude | Escape | 86 | 1171 | y |
| omp | Escape | 104 | 1504 | y |
| opencode | Ctrl-C (exits the CLI; a single Escape does nothing, double Escape is the designed interrupt) | 0 | 10 | n (Ctrl-C exits; the CLI is gone) |
| codex | not measured | not measured | not measured | not measured |
| pi | not measured | not measured | not measured | not measured |
| cursor-agent | not measured | not measured | not measured | not measured |

Behind the medians, per rep (output stop ms / CPU stop ms / answered):

- CodeAF (reps 5-9 of 9 attempts; 4 discarded by load): 0/7850, 0/6398, 0/7184, 0/7016,
  0/6605, all answered y. Output stop 0 means the pane never changed again after the Escape
  keypress in any rep: the abort landed inside one 50 ms poll, before the next 3 s delta would
  have painted.
- claude (5 reps; 1 attempt discarded by load): 85/1118, 86/1064, 84/1171, 146/1467, 148/1492,
  all answered y. Output stop is bimodal, about 85 ms and about 147 ms, both well under 150 ms.
- omp (5 kept reps, Escape honoured in all): 107.1/1557.1 answered y in 1113.6 ms,
  103.8/1503.8 answered y in 1101.8 ms, 103.8/1503.8 answered y in 1053.0 ms,
  102.7/2102.7 answered y in 1104.0 ms, 103.2/1503.3 answered y in 1101.9 ms; kept load maxima
  7.02, 6.94, 4.39, 2.27, 2.27. Reps 2 and 3 share numbers because both landed on the same 50 ms
  polling grid slots; the raw timelines confirm independent runs. The discarded first pass
  measured 105.8/1655.8 and 103.5/1903.5 but at load1 9.75 to 12.27; kept as evidence only.
- opencode, spec keys: a single Escape does nothing mid-stream (output kept changing past 10 s;
  the pane footer itself advertises "esc interrupt" and after one press changes to "esc again
  to interrupt"). Ctrl-C stops the turn by EXITING the CLI: the pane died 9 to 237 ms after the
  keypress in all 5 reps (0/9, 66/237, 0/9, 0/10, 0/10), process tree gone, so no follow-up turn
  is possible without relaunching: answered n, 0 of 5.
- opencode, designed interrupt, double Escape (5 fresh supplementary reps, timed from the second
  press): 127/1838, 70/4609, 126/3844, 66/3837, 68/3951; medians 70 ms output stop and 3844 ms
  CPU stop; the CLI stays alive and the follow-up turn streams (first new token about 3.1 s
  after send, answered y 5 of 5).
- opencode streaming start note: the first pane change at least 1 s after the prompt send is
  the TUI pending animation at +1040 to +1049 ms in every rep, not the first token (the first
  word renders about 3.6 s after the send). The keypress therefore lands about 0.3 s before
  the first token renders, the same interrupt state as the claude reps.

Empty-row reasons, quoted from /home/santosh/src/profiles/redirect-table.md (probe of 2026-09-19):

- codex: "uses a WEBSOCKET transport: codex_api::endpoint::responses_websocket: failed to connect
  to websocket. A base URL does not cover it; a stub for codex needs a websocket server speaking
  its protocol"
- pi: "Connection error. No documented base-URL variable is honoured; it would need its own
  provider config file"
- cursor-agent: "reaches the host, own protocol" via POST /auth/exchange_user_api_key; "speaks its
  own auth and streaming protocol rather than OpenAI or Anthropic"

## Table 2: crash recovery (kill -9 at turn 6, streaming)

| CLI | session listed after kill -9 (y/n) | turns 1-5 intact (y/n) | partial turn visible (y/n) | continuable (y/n) | recovery message |
| --- | --- | --- | --- | --- | --- |
| CodeAF | y | y | y (prompt only) | y | none |
| claude | y | y | y (prompt only) | y | "Session model stub-1 could not be restored (not a model this version of Claude Code recognizes) — using the default model instead." |
| omp | y | y | y (prompt only) | y | none |
| opencode | y (via the ctrl+x l switcher; no auto-resume on relaunch) | y | y (prompt only) | y | none |
| codex | not measured | not measured | not measured | not measured | not measured |
| pi | not measured | not measured | not measured | not measured | not measured |
| cursor-agent | not measured | not measured | not measured | not measured | not measured |

Detail behind the two measured rows:

- CodeAF: the relaunched chat auto-reopened the interrupted conversation itself and it is also
  listed in the /resume picker ("Reply with the Single Word Ok   write a long poem about the sea
  · now"). All five ok turns are present with their full streamed answers. Turn 6 shows its
  prompt with no answer and no cut-off partial text (the kill landed about 2 s after the first
  streamed word). One more turn streamed new content within about 1 s and the stub gained the
  POST. No recovery, rollback or corruption message is printed; the only trace is a status chip
  line "· resumed · reply with the single word ok". The kill set was the pane zsh, the chat
  client and the codeaf engine session host daemon, each confirmed first.
- omp: the resume picker lists the crashed session as "reply with the single word ok, just
  now, 12.5KB, pending" and the -c welcome sidebar shows it under Recent sessions. After resume
  all five ok turns render with their streamed answers, and turn 6 shows its poem prompt but not
  the partial first word that was on screen at kill time (the on-screen "Zar" was not persisted).
  One more turn streamed a new answer, model-filtered stub POSTs going 35 to 37. No recovery,
  rollback or corruption message anywhere after relaunch; the only in-session notice, "Error:
  Thinking loop detected: the model repeated near-identical content (repeated an exact
  31-character cycle 7x back-to-back). Treating as a stream stall and retrying." is the omp
  stream-stall detector firing on the stub repeating answer, recorded during the original turns
  and replayed on resume, not a crash recovery message.
- opencode: the relaunched TUI opens a fresh session banner with no auto-resume and no
  message of any kind, but the Sessions switcher (ctrl+x l) lists the interrupted session under
  Today, and the sqlite session store holds its row. Selecting it shows all five ok turns with
  full streamed answers and turn 6 as its prompt with no answer and no cut-off partial words
  (the kill landed on the single confirmed opencode pid 3.02 s after stream start, with four
  streamed words visible in the pane; those words are not saved into the turn). One more turn
  in the selected session streamed a full 40-word answer with a stub POST. The headless surface
  is `opencode session list` (a bare `opencode sessions` errors).
- claude: the interrupted session is listed by the resume picker as "reply with the single word
  ok / 1 minute ago · wip/perf-docs-readme · 216.5KB". Resumed by id, all five ok turns are
  present with full answers. Turn 6 shows the prompt with no assistant entry; the kill landed
  about 200 ms before the first streamed word would have arrived. One more turn streamed new
  content 1046 ms after send with a stub POST. The quoted recovery line is a model-restore
  notice triggered by the stub model id in the interrupted turn metadata, not corruption.

Same three empty-row reasons as Table 1.

## Caveats

- The box is shared and was busy during the fan-out; the load rule (discard any timing rep with
  load1 above 8) is what keeps the medians clean, and it cost real attempts (4 of 9 for codeaf,
  1 for claude, 2 completed omp reps of the first pass). Medians are of 5 kept reps each.
- The stub log is shared across the parallel workers, so POST counts are filtered by the model
  name each CLI requests (codeaf requests deepseek/deepseek-v4.1-flash, claude requests
  claude-opus-5, omp requests claude-opus-4-8, opencode requests claude-sonnet-4-6 and
  claude-haiku-4-5-20251001) and window deltas are upper bounds. claude opens two identical
  /v1/messages requests about 1 ms apart per long turn and holds both open. opencode fires one
  extra haiku POST for the session title on the first turn of each fresh session.
- The CodeAF row was measured against a benchhead binary rebuilt from rev 4f002ca2 after the
  checked-in binary arrived as a broken 9-byte shim; the tree is clean at that rev and the
  version stamp differs only in build time. codeaf runs a session host engine daemon outside the
  pane tree; it was included in the CPU tracked set and in the crash kill set.
- claude was 2.1.278 for every measurement (auto-updated from 2.1.274 before the first launch,
  auto-update disabled during runs). opencode was 1.18.31 for every measurement (the npm package
  moved from 1.18.22 between the 2026-09-19 probe and today).
- opencode finding worth its own line: a single Escape does NOT interrupt a streaming turn in
  1.18.31; the footer advertises "esc interrupt" but the first press only arms it ("esc again
  to interrupt"). The spec key Ctrl-C stops the turn by exiting the whole CLI with no
  confirmation, 9 to 237 ms after the keypress. The designed interrupt is a double Escape,
  which works cleanly and keeps the session usable (supplementary row: 70 ms output stop,
  3844 ms CPU stop, answered y).
- opencode TUI instances launched from short-lived ssh connections were observed to exit
  silently about 20 to 70 s after the creating connection closed; all opencode measurement
  phases therefore ran inside single held-open invocations, and the crash build was completed
  inside one held-open invocation spanning launch, turns 1 to 5 and the turn 6 kill.
- omp turns do not self-complete against this stub: the omp stream-stall detector fires on the
  stub repeating its 31-character filler cycle and the agent loop re-samples, so left alone a
  turn chains POSTs. Each crash build turn therefore streamed the full 40-word answer and was
  stopped with Escape (two presses, since one is swallowed if it lands in the roughly 1.7 s gap
  between chained streams); each turn cost about 157 s and 2 stub POSTs (answer plus the retry).
  This is stub-filler behavior, not a footprint of omp against a real provider.
- omp displayed an update notice (18.2.6 versus the measured 18.1.13) in its TUI; it was
  ignored and no update was run.
- Coordinator housekeeping log for leftover processes from the first omp and codeaf passes:
  cleanup-coordinator.log (both leftovers confirmed by command line and profile HOME before a
  TERM, tmux servers killed by name, rescan showed zero survivors).

Raw logs: one raw subdir per CLI (steer reps with cpu series, crash build/kill/resume, load
readings, versions), plus stub-8093.log (wire-side record) and cleanup-coordinator.log.
