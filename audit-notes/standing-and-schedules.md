# Standing items and schedules: how they work today

An audit of the machinery behind reminders, routines (`every Monday at 9`),
watches, rules and `/standing` — and of every other way codeaf does work on a
clock. Commit `973b7d22d` on `dev`, 2026-10-09. Evidence only: the last section
lists the questions a rebuild has to answer, not the answers.

**How this was checked.** Code reading (citations are `file:line` at that
commit), throwaway overlay tests that were never committed, and a read-only look
at one real install — its `~/.codeaf/v3/standing/` store, `wake.log`, item logs,
run folders and launchd state. The claims with the most weight were re-verified
independently of the first reading: the dead timer, the missing connected
accounts, the allow-all approval policy, the 120-second firing ceiling, the
outcome classification, the `max_per_day` default, the swallowed sentinel error,
the doctor row, and the daylight-saving runaway.

---

## The short version

- **There is no scheduler.** Time is only noticed inside a *pass*: one walk over
  every item, started every 5 minutes by whichever codeaf process takes a
  non-blocking file lock. Due-ness, probing, LLM judgment and the firing itself
  — including a whole headless agent session — all run synchronously inside that
  one pass, under one 120-second deadline.
- **Who runs passes** is the per-workspace engine daemon (it exits about 30
  minutes after the last terminal closes) and an OS timer running `codeaf tick`.
  On the machine examined, the timer has been dead since 2026-09-22, and the
  product can neither repair nor report it.
- **Firings do not run the way a conversation runs.** They have no connected
  accounts, an allow-everything approval policy, a different environment, and an
  outcome inferred from whether the model said anything.
- **Size:** about 18k non-test lines for the v3 feature, about 19k lines of
  tests and 2.1k lines of manual, beside about 8k lines of a v1 resident
  scheduler that no surface can reach any more.
- **The package's own tests are green** (`go test ./internal/standing/`, 4 s).
  Every defect below sits at a seam between packages or between the program and
  the operating system.

---

## 1. The moving parts

### 1.1 One object: the item

`internal/standing/standing.go:512-641`. One JSON document per item under
`~/.codeaf/v3/standing/<id>.json`, written temp + fsync + rename under a
per-item flock.

| Part | What it holds |
|---|---|
| `Words` | the person's sentence, verbatim, never rewritten |
| `When` | one of six kinds (`:206-230`): `at` (one moment), `every` (5-field cron or Go duration), `file` (glob fingerprint), `idle` (machine quiet for N), `probe` (shell command or belt tool, judged by an LLM "sentinel"), `hold` (a rule; never wakes) |
| `Does` | `say` (deliver a line) or `task` (run a headless agent session in the workspace, optionally in its own worktree) |
| `Rails` | `PerRunUSD` (default $5), `MaxPerDay` (default 10), `Expires` |
| reach | `Altitude` (conversation / project / machine), `Exceptions`, `Brief`, `Grant` |
| present state | `NextDue`, `LastChecked`, `Fingerprint`, `Positive`, `Previous`, `Pending[]`, `TaskInflight`, `Runs`, `CleanRuns`, `NeedsPerson`, `Revision`, … |

Status is `active`, `paused` or `retired`. "Needs you" is not a status but a
free-text field carrying five different things (a question, a permission
refusal, a wedged task, an undeliverable line, a baseline flag). Four schema
versions exist already, two of them "barriers" so an older build skips rather
than corrupts a newer document (`standing.go:89-164`).

Beside the items: `ledger-YYYY-MM-DD.jsonl` (money and firings per local day),
`wake.log` (one line per pass), `tick.lock`, `<id>/log`, `<id>/runs/NNNN/`
(each task firing is a session folder with a one-word `came-to` file),
`<id>/running` (a cross-process "checking now" marker), `exchanges/` and
`projects/<key>/inbox.jsonl`.

### 1.2 How an item is made

- The model recognises standing words and calls the one belt tool, `stand`
  (`internal/session/tools_standing.go:326-336`; ops
  `propose|list|pause|resume|stop|change`). `/standing <words>` (alias
  `/orders`) is the explicit door; it just tells the model to call `stand`.
- The schedule is a structured `when` the **model** compiles: an RFC 3339
  moment or `in` duration, a 5-field cron line, a glob, a probe. The prompt
  carries a `Now:` line (re-rendered only when 10+ minutes stale) and every
  `stand` result ends with the current time. A moment in the past is refused.
- A ratification card is drawn; nothing stands until "yes". On yes:
  `Store.Create`, and — for the first item ever in this store — the OS timer is
  installed silently (`tools_standing.go:1461-1484`). The "ask once" question in
  the design docs was replaced by install-by-default plus a one-line notice,
  and the attempt is never retried.
- What the card shows is the model's title and `when_words`, not the cron line,
  the probe command, the brief or the say text (`tui3/standing.go:973-987`,
  `tools_standing.go:685-694`). Nothing checks `when_words` against the spec.

### 1.3 Who runs the clock

| Driver | Where | Cadence |
|---|---|---|
| In-process ticker | `cmd/codeaf/chatv3_standing.go:269-304` | `time.NewTicker(5m)`; first pass one full interval after start; no pass on launch, after a yes, or on demand |
| `codeaf tick` (hidden command) | `cmd/codeaf/tick.go:27-54` | whenever the OS timer runs it |

- The in-process ticker runs in **every v3 process that opens a launch**, which
  on the ordinary road is the per-workspace engine daemon
  (`codeaf engine --daemon`), not the terminal UI. An engine keeps an idle
  conversation 30 minutes and exits 2 minutes after it is empty
  (`internal/enginehost/host.go:62-64`). Closing the terminal therefore leaves
  about half an hour of ticking.
- Both drivers build the pass through one constructor (`v3StandingTicker`,
  `chatv3_standing.go:129`) and take one flock, `v3/standing/tick.lock`, with
  `LOCK_NB`. The lock prevents overlap, not duplication: N live processes give N
  passes per 5 minutes on independent phases. The loser does nothing.
- **The OS timer** (`internal/standing/watch.go:812-868`) is one launchd label
  per login, `ai.agentfield.codeaf.tick`, with `StartInterval 300` and
  `RunAtLoad false`; on Linux a user timer `OnCalendar=*:0/5` with
  `Persistent=true` and no linger.
  - It bakes in `os.Executable()` of whichever process installed it — often a
    worktree build that is later deleted — and that home's `CODEAF_HOME`.
  - It carries no `PATH` beyond `/usr/bin:/bin:/usr/sbin:/sbin`, no API-key
    variables, and no log paths.
  - "Installed" is derived from the definition file's bytes and the named
    binary existing (`watch.go:313-344`). launchd is never asked whether the job
    loads, runs or exits 0.
  - Ownership: `Ensure` and `Repair` refuse any definition naming another home
    (`watch.go:193-231`, `:434-438`), even when that home and its binary are
    gone.
  - On macOS, an interval that falls during sleep is simply missed
    (`launchd.plist(5)`, `StartInterval`); only `StartCalendarInterval` catches
    up. The design doc's catch-up promise holds on Linux only.
- **`codeaf wake`**, listed in `--help` as "run one full background pass by hand"
  (`cmd/codeaf/main.go:562`), runs the **v1 resident** pass on `graph.db` and
  never touches v3 items. **`codeaf doctor`'s** "background timer" row reads the
  v1 timer through `internal/watchdog` (`cmd/codeaf/doctor.go:195-200`).

### 1.4 One pass

`internal/standing/tick.go:40-115`:

1. Take `tick.lock` or leave.
2. Read every document, retired ones included.
3. **Settle** leftovers from earlier passes: re-deliver unacknowledged lines,
   settle a task whose run already has a ledger line.
4. For each active item, newest first, until the 120 s context ends:
   expiry → skip holds → rails (`MaxPerDay`, then the daily dollar rail) →
   raise the running marker → refuse if something is still pending or a task
   is in flight → **look** → re-read the item and give up if the person changed
   it meanwhile → **act**.
5. Run the memory "tidy" (consolidation of remembered facts rides this pass).
6. Append one counts-only line to `wake.log`.

The look per kind (`tick.go:361-588`):

- `at`: due when `now ≥ At`.
- `every`: due when `now ≥ NextDue`; then `NextDue = next(now)`.
- `probe`: run the command (`bash -c`, 60 s) or the tool, then ask the sentinel —
  a low-tier model given the words, the hint, the probe and the last 8 KiB of
  output — to answer `yes`, `no` or `unknown`.
- `file`: hash a bounded fingerprint of the glob's files.
- `idle`: ask whether the machine has been quiet.
- Any non-probe kind with a `hint` is also put to the sentinel, with empty
  evidence.

"Unknown" writes nothing, so the item stays due on the next pass.

### 1.5 One firing

- **say:** write a `Pending` intent, append the line to the origin
  conversation's inbox (or the project inbox), offer it live, clear the intent.
- **task** (`internal/session/standing_run.go:722-768`): write a `TaskInflight`
  marker, then build a fresh headless session in `runs/NNNN` and submit the
  brief. The session's context is the pass's context, so the whole run lives
  inside what remains of 120 s. 60 steps max, `PerRunUSD` checked after each
  tool call.
- The firing's configuration (`v3StandingPosture`,
  `cmd/codeaf/chatv3_standing.go:193-249`, and `standingRunConfig`,
  `standing_run.go:1108-1188`) is assembled by hand: profile models and key,
  governance, media — and **no `Connect`** (connected accounts are set only on
  the conversation path, `chatv3.go:1162`), and
  `ApprovalPolicy = {Default: Allow}` (`standing_run.go:1138`).
- `came-to` (`standing_run.go:1008-1016`): `needs-you` if a call was refused;
  otherwise `landed` if a file was saved **or the model said anything**;
  otherwise `nothing`. Errors from the provider are not observed.

### 1.6 How news reaches a person

Four roads (`standing_run.go:24-40`, `:569-616`):

1. The origin conversation, if it is open **in the process that ran the pass**.
2. Another conversation of the same project in that process.
3. The origin's inbox, folded into "while you were away" when it is next opened.
4. The project inbox, for an item made from home's `ask here`.

Inboxes are drained only when a conversation is constructed or a surface
attaches (`agent.go:419`, `task_run.go:4922`). Nothing polls. There is **no
outward lane**: no desktop notification, no bell, no push — a reminder reaches
a person only when they look at codeaf.

### 1.7 Rules: the part that is not a clock

A `hold` never wakes. Its whole work is the "birth seam"
(`internal/session/standing_world.go`): every applicable rule is rendered into
each conversation turn's instructions and each task brief, resolved through
`Store.Applicable` (altitude minus exceptions). Non-hold items ride along under
a softer heading. This is the only consumer of altitude and exceptions — the
pass and the delivery never read either.

### 1.8 Other things on a clock

- **`watch` tool** (`internal/session/tools_watch.go`): an in-session polling
  job (every 2 s to 1 h, at most 3, deterministic diff/regex, first tick a
  silent baseline). Dies with the session. Shares no code with standing.
- **Background `bash` jobs** and the `jobs` tool: no schedule; die with the
  session.
- **The v1 resident** (`internal/resident`, `internal/store/charter*.go`,
  `internal/head/standing.go`, `internal/watchdog`, `internal/lease`,
  `cmd/codeaf/wake.go`; about 8k non-test lines): charters with
  cron/file/poll/graph watches, sentinels, probation and tenure, a practice
  loop, a regex cadence recogniser. Since #469 (2026-09-02) no surface can
  create a charter, and no production code installs its timer. It still runs
  through `codeaf wake` and through `codeaf do` with
  `CODEAF_TASK_BELT=node|legacy|off`. The only compile-time edge between the
  two systems is `standing.DefaultPerRunUSD`, borrowed by the resident.
- No third-party cron library anywhere; two hand-written schedule engines
  (`internal/standing/every.go`, `internal/store/charter.go:2013-2076`).
- No `/loop`, `/every`, `/remind` or `/schedule` command.

---

## 2. What one real install shows

Read-only, counts and metadata only.

**The OS timer has been dead since 2026-09-22.**

- The only plist names `/private/tmp/codeaf-restore-home/bin/codeaf` and a
  `CODEAF_HOME` inside a Go test's temp directory
  (`…/TestTUIE2Eask_here_end_to_end…/001/home`). Both are gone.
- `launchctl print`: `runs = 1`, `last exit code = 78: EX_CONFIG`, penalty box.
- The person said yes at 08:13 that day (`watch-offer.json`). At 20:28 a TUI
  end-to-end run with a temporary `CODEAF_HOME` and the real `HOME` approved a
  test item, and the then-unconditional `Install` took the login's only timer.
  #1638 (2026-09-27) stopped the suite touching the real timer; nothing cleaned
  up machines it had already touched.
- Since #1627 the real home reads that plist as "not ours": status off, `Repair`
  silently does nothing, `Ensure` is never called again because the marker says
  the person was already told. No `standing.log` was ever written.

**Passes come only from open processes.**

- `wake.log`: 6,353 passes from 2026-09-08 to 2026-10-09.
- 50 gaps longer than 15 minutes, 415 hours in total, including 70 h (Sep 25–28),
  36.5 h and 35.3 h. Sleep explains some; a working timer would have covered the
  rest.
- 1,155 passes started under 60 s after the previous one: several engines
  ticking on their own phases.
- 75 passes recorded `errors>0`, all with `examined=0`. Their reasons survive
  nowhere, because the pass's notes are discarded by both callers and the wake
  line carries counts only.

**The one routine that ran: every 15 minutes, a task against Notion.**

- 62 firings over 8 days. `came-to`: 55 `landed`, 7 `nothing`. `CleanRuns`: 62.
- The run transcripts say otherwise: every one reports that the Notion tools it
  was told to use (`services`, `use_service`) are not on its belt, and that it
  failed closed. The firing posture has no connected accounts (§1.5).
- On four of eight days, all ten allowed firings ran between 00:00 and 02:36, and
  nothing ran for the rest of the day: `MaxPerDay` defaulted to 10 and the card
  did not say so. On two days a single firing ran, because no process was alive
  and the timer was dead.
- Between firing starts (ledger): mean 17.1 min, minimum 15.0, maximum 56.1.
- About $2.24 spent, all on runs that could not do the job.
- The same request had been set up twice. All five items in the store are now
  retired, "stopped by you".

---

## 3. Defects

Each is confirmed by reading the cited code. Those marked *(run)* were also
exercised in an overlay test or seen in the live data above.

### 3.1 The clock

1. **One timer per login, owned by whoever wrote it last.** Absolute path to
   the writer's binary, the writer's home, no health check, and an ownership
   rule that can never reclaim a definition whose home and binary are gone
   (`watch.go:193-235`, `:434-445`). *(run)*
2. **Status is asserted from file bytes, not from launchd.** Exit codes are
   never read; `LastWake` comes from `wake.log`, which every driver writes, so a
   dead timer is masked by live engines (`watch.go:313-344`).
3. **No catch-up after sleep on macOS** (`StartInterval`).
4. **The de facto ticker is an engine daemon** whose life is the conversation
   idle policy, about 30 minutes after the terminal closes
   (`enginehost/host.go:49-64`).
5. **N processes, N passes.** The flock is take-or-leave, held for one pass
   only; nothing records when the last pass ran. *(run)*
6. **The environment depends on who won.** Under launchd: no shell `PATH`, no
   key variables. Shell probes behave differently by driver
   (`standing_run.go:389-421`).
7. **No staleness detection on `tick.lock`.** A stopped or wedged holder blocks
   every pass on the machine without a trace. `codeaf tick` has no signal
   handling.
8. **Wrong tools.** `codeaf wake` (visible) is the v1 pass; `codeaf tick`
   (hidden) is the real one; `doctor` reports the v1 timer
   (`doctor.go:195-200`). *(run)*

### 3.2 Schedule semantics

1. **Five-minute resolution**, and a fresh process's first pass is 5 minutes
   after launch.
2. **Duration rhythms drift.** `NextDue = now + d` from whenever the pass
   reached the item (`tick.go:438`), not from the scheduled slot. The phase fix
   applied to probes (`probeNextDue`, `:590-629`) was never applied here.
   *(run: a `15m` rhythm measured at a 17.1 min mean between firing starts)*
3. **Missed occurrences collapse into one late firing**, labelled "it was the
   time you asked for". Lateness is never recorded or shown (`tick.go:422-440`).
4. **A reminder more than 24 h late is retired as "expired" without being
   delivered** (`tick.go:237-253`, `:1290-1302`). *(run)*
5. **Daylight saving, fall-back, zones west of UTC.** `next()` builds its first
   candidate with `time.Date` on an ambiguous wall time and can return a moment
   that is not after `now` (`every.go:169`). In `America/Toronto` on 2026-11-01,
   a daily `30 1 * * *` item is due on every pass from 01:30 to 02:00 EST:
   eight firings. Hourly and quarter-hourly lines misbehave through the same
   hour. The comment at `every.go:163-166` says this cannot happen; the test
   at `every_test.go:150-163` checks two occurrences and never `next(second)`.
   *(run)*
6. **The default `MaxPerDay = 10` silently truncates any rhythm faster than
   ~2.4 h** (`tools_standing.go:79-82`, `:947-951`). The card shows "shares the
   day's allowance" unless money was named (`:1053-1070`). *(run: live data)*
7. **A rail-held routine slides to midnight.** Rails are checked before the
   look, so `NextDue` does not advance while held; the next day's first pass
   fires it. *(run)*
8. **Impossible cron lines are accepted** (`0 0 30 2 *` → zero time → "waiting
   for its time" forever).
9. **Zone is the ticking process's local zone.** Items store no zone; over
   `--host` it is the far machine's.

### 3.3 Running the work

1. **A task firing lives inside the pass's 120 s**, shared with every item
   walked before it (`chatv3_standing.go:362`, `tick.go:42`,
   `standing_run.go:749`). "Tonight, run the full suite" cannot work.
2. **A cut-off run is reported as success.** `came-to` ignores context expiry:
   any streamed text means `landed`, the in-flight marker clears and
   `CleanRuns` increments (`standing_run.go:758-767`, `:1008-1016`). Issue
   #1582 ("a standing firing that changed nothing is logged as landed") is
   open; its fix sits in draft PR #1670.
3. **Provider errors read as `nothing`** and also count as clean
   (`standing_run.go:889-904` observes no error events).
4. **No connected accounts.** Mail, calendar, Notion, Slack, Linear — every
   account-backed watch and task in the design's promise list — cannot work
   unattended, for tasks or tool probes (`chatv3_standing.go:193-249`;
   `connect.go:154-162` returns nil). *(run: live data)* Draft PR #1715,
   "restore connected accounts in standing firings", has been open and
   unmerged since 2026-10-01.
5. **Allow-all approval.** `standingRunConfig` replaces the person's policy with
   `{Default: Allow}` (`standing_run.go:1138`), dropping their deny rules. The
   comment explains why (the inherited prompt policy refused everything with
   nobody to ask). It contradicts the contract (`standing.go:37-46`), the door
   header (`chatv3_standing.go:19-27`) and the manual. Untrusted probe output is
   pasted into these allow-all briefs.
6. **`Grant` is stored, never enforced and never drawn.**
7. **One error or crash wedges a task item for good.** `TaskInflight` is cleared
   only on a recorded outcome (`tick.go:786`, `:1043`). Pause and resume do not
   clear it, because `Save` copies it from disk (`store.go:121-123`). *(run)*
8. **Altitude "conversation" does not die with its conversation**, contrary to
   the design and the manual. `Brief.Prompt` is never compiled.

### 3.4 Judgment (the sentinel)

1. **A failed sentinel call is invisible.** With zero cost, `chargeUndecided`
   returns `unknown` and a nil error, so the cause — "no API key", a provider
   outage — is dropped (`tick.go:681-707`). An undecided look writes nothing.
   The item never shows the `could not check: …` row the manual promises; a
   test pins `errors=0` for a keyless machine (`cmd/codeaf/tick_test.go:99`).
2. **"Unknown" bypasses the cadence.** The item is re-probed and re-billed every
   pass whatever `probe_every` says; the ledger is charged, the item's own spend
   and `LastChecked` are not. *(run)*
3. **Hints on clock kinds are judged blind** (empty evidence), and the
   sentinel's prompt says empty evidence means "unknown". A hinted routine can
   bill every 5 minutes and never fire. *(run)*
4. **`idle` items fire on every pass while the machine stays quiet**, up to
   `MaxPerDay`; standing's own runs are invisible to the idle check. *(run)*
5. **File watches go deaf** on one file over 256 KiB, more than 4,096 files,
   more than 8 MiB, or more than 2 s of hashing: undecided forever, nothing
   written. *(run)*

### 3.5 Delivery

1. **Live delivery depends on which process won the lock.** A firing for
   project B run by project A's engine, or by the timer, goes to B's inbox while
   the person sits in B's open conversation (`standing_run.go:113-116`,
   `:604-614`).
2. **No outward lane.** Nothing reaches a person who is not looking at codeaf.
3. **Double display** (live, then folded again on the next attach), **wakes in
   rooms nobody is in** (engines keep closed windows' agents ~30 min), and
   **news for deleted conversations** written into a folder nothing reads.
4. **An undeliverable line blocks the item** from firing again.

### 3.6 Observability

1. Pass notes are discarded by both callers; `wake.log` carries counts only.
   *(run: 75 unexplained error passes)*
2. `/status` on the engine road prints "nothing is checking" while engines tick.
3. Nothing records which driver ran a pass.
4. `wake.log`, ledgers and item logs grow forever; every retired document is
   parsed every pass.
5. The end-to-end suite's ticker is not the production wiring: legacy binary
   sentinel, runner without memory, a 4-minute context, an in-process tick
   (`internal/e2e/harness_test.go:374-411`).

### 3.7 The ordinary launch shows about half the feature

Plain `codeaf` runs the conversation in the session host, and the screen
reaches it through `*remote.Agent` (`cmd/codeaf/chatv3_host.go:699-701`), which
has none of `StandingHere`, `StandingExcept`, `StandingStandDown` or
`StandingPause` (`internal/session/standing_orders.go:39-149`); the wire carries
only `Standing.Items`, `Standing.Save`, `Standing.Watch` and `ResolveStanding`
(`internal/remote/wire.go:498`, `:651-653`). The host road's store seam sets
only `Items`, `Save` and `Watch` (`chatv3_host.go:785-800`). So on the launch
most people use:

- no `· N standing orders here` line when a conversation opens (#1643);
- no `standing` section in the right-hand column;
- no reach shelves and no `n not here` on `/standing` — every order is filed
  under `in other projects`;
- no `◐` checking-now mark, no weekly-runs line, no `alt+e`, and no
  `background checks` row in settings (so no way to repair the timer from the
  screen).

The repository's own door ledger lists this under "says something that is not
true" (`internal/remote/surfacedoors_law_test.go:96-114`; #901).

### 3.8 Documents against code

`docs/AMBIENT.md` and `docs/STANDING-ORDERS.md` say, and the code does not:
lock at `v3/locks/standing.lock` (it is `v3/standing/tick.lock`); `codeaf wake`
and `internal/watchdog` reused (neither is); the timer offered once (it is
installed silently); catch-up after sleep (macOS: no); "the brief says a check
was late" (nothing does); firings under the banked rules (allow-all);
conversation-altitude items die with the conversation (they do not); a compiled
brief and an enforced grant (neither). The chat manual repeats several of
these, and documents some failure modes instead of fixing them ("I have two
copies of codeaf and my reminders fired twice, or stopped firing").

---

## 4. Size and blast radius

| Area | Non-test lines | Tests that pin it |
|---|---|---|
| `internal/standing` | 6,284 | 177 (7,608 lines) |
| `internal/session` (standing proper¹) | 4,602 | 173 |
| `internal/tui3` (standing files and home exchange) | 6,423 | 196 |
| `cmd/codeaf` (`chatv3_standing.go`, `tick.go`) | 606 | 19 |
| `internal/e2e`, `internal/manual`, others | — | 17 |
| Chat manual (`keeping-an-eye`, `standing-orders`, `asking-from-home`, plus ~22 sections of `home.md`) | 2,129+ | ~80 retrieval probes |
| v1 resident scheduling (unreachable from any surface) | ~8,000 | ~6,300 lines |

765 tests touch the feature; 582 pin it and 183 touch it in passing. Two
untagged gates fail outright if `internal/standing` disappears
(`internal/e2e/tuiwords_test.go`, `internal/manual/truth_test.go`), and several
structural law tests name its files.

¹ `standing_run.go`, `tools_standing.go`, `standing_world.go`,
`standing_orders.go`, `standing_isolation.go`, `standing_mark.go`,
`standing_contract.go`. Not counted, despite their names: `standingtree.go`,
`standingbelt.go`, `standstill.go`, `taskstands.go` (folder working copies, the
standstill floor and task grounds).

44 non-test files import `internal/standing`: 24 in `tui3`, 8 in `session`, 4 in
`cmd/codeaf`, 3 in `internal/remote` (the `Standing.Items` / `Standing.Save`
wire methods), 2 in the demo-home seeder, 1 in `config`, and 2 in the v1
resident for `DefaultPerRunUSD`.

What rides on standing and would need a new home rather than deletion:

- the memory tidy, which runs at the end of every pass
  (`internal/session/memory_consolidate.go:219`, `chatv3_standing.go:165`);
- `ask here` exchange folders, which live under the standing root;
- spend attribution by standing item, `SweepStanding`, `codeaf do`'s rules
  seam (`doStanding`), the `background checks` config key, and the remote wire
  methods;
- timers already installed on machines (`codeaf-tick`, and the legacy-named
  one), and documents at schema 1–4 that older builds still write.

---

## 5. History and open work

**Built** 2026-08-20 to 08-25 in 19 commits. After that, 8 of the 9 commits to
`internal/standing` are fixes; across the feature there are 16 standalone fix
PRs and about 35 more fixes inside 6 squash PRs.

**#1777** (2026-10-06, titled "Contextual memory across chats, teams, and
tasks") rewrote much of the core: it is the last writer of 1,255 of `tick.go`'s
1,669 lines and 906 of `standing_run.go`'s 2,084. It is on `dev` only.

**Recurring categories**, most frequent first:

1. Screen and manual drift (about 25).
2. News not reaching the person — misrouted, not drawn, lost (about 12).
3. One machine, many builds, profiles and processes — timer takeover, double
   clocks, schema skew (about 12).
4. Missed or never-firing items and schedule arithmetic (about 11).
5. Duplicate delivery and exactly-once settlement (about 10).
6. Unattended authority (about 10), and errors in what the model proposes
   (about 10).
7. Sentinel judgment (about 9).
8. Cost limits; lifecycle leaks; Windows.

**Came back after being fixed:** timer takeover (#360 → #1618 → #1631), the
away fold missing (three times), double ticking (#756 → #1041), an expiry
eating a firing (#188 → #756 → #205, still open), and "landed" for a firing
that did nothing (→ #1582, still open).

**Open:** 25 standing-specific issues (5 of them already fixed on `dev`),
among them #205, #901, #1326 (a firing's task board is always empty), #1361
(the tmux suite is 7/18 red on the trunk with nothing in CI to say so), #1468,
#1582, #1643, #1644, #1645, #1678, #1742 and #1760. Draft PRs #1715
(connected accounts in firings) and #1670. The planning issues for the landing
seam and built-in orders (#12–#14) were closed "completed" on 2026-08-31 with
no linked work.

---

## 6. Questions a rebuild has to answer

Not a design; the decisions the evidence above forces.

1. **Who keeps time.** A long-lived per-user scheduler (launchd `KeepAlive` /
   systemd user service at a stable binary path, health read from the OS)
   versus today's "whoever is alive runs a pass". Windows as clients of it, or
   as tickers?
2. **Separate scheduling from execution.** Firings as supervised processes with
   their own deadlines, leases and heartbeats, not work inside a 120 s pass under
   a global lock.
3. **An explicit run record.** Occurrence → run with a real state machine
   (queued, running, succeeded, failed, timed out, cancelled, needs you), a
   success contract that is not "the model said something", and lateness
   recorded per occurrence.
4. **Misfire policy, said per item.** Skip, run once late, or run every missed
   occurrence, and what "late" means for a reminder.
5. **One execution posture.** The same construction path as a conversation —
   accounts, approval rules, environment — or a deliberate, displayed
   difference.
6. **Deterministic triggers first.** Time, file change and command exit or regex
   as plain triggers; the LLM sentinel as an optional filter whose failures are
   visible.
7. **A notification lane.** Desktop notifications for reminders and needs-you,
   and an inbox every open window watches, not "whoever won the lock".
8. **What survives.** Candidates: recognition through `stand` and the card, the
   rules seam (`standing_world.go`), the ledger and daily rail, the cron parser
   once its fall-back bug is fixed. Candidates to delete: the v1 resident
   scheduler, `codeaf wake`, the doctor's v1 row.
9. **Migration.** Items on disk at schema 1–4, the per-login timer already
   installed on machines (including orphaned ones like the one above), the
   remote wire methods, and 2.1k lines of manual that must be rewritten under
   the manual law.
