# Automations — work codeaf does on a clock

Design doc, 2026-10-10. The owner's decisions of 2026-10-09 are the contract;
`audit-notes/standing-and-schedules.md` is the evidence that led to them. This
replaces standing items (`internal/standing`, `/standing`, the OS timer) and the
v1 resident's scheduler. Where this document and the code disagree once the
build has landed, the code is right and this document is the bug.

## The one-paragraph design

An **automation** is a schedule, an optional look, and an action. Three kinds
fall out of that: a **reminder** says a fixed line at a time; **scheduled work**
runs a brief at a time or on a rhythm; a **watch** looks at something on a
rhythm, asks the model whether a condition is met, and on the change to "yes"
says a line or runs work. They run **only while a codeaf window is open**, in one
clock process that any window starts and the last window's closing stops. Every
run is a row in a small database before it starts and the same row when it ends,
so every window reads the same truth. **Rules** — "always use tabs here" — are not
automations: they are memories marked *always*.

## What the person gets

| Kind | Said like | Card heading | Answers |
|---|---|---|---|
| reminder | "remind me at 6 to leave" | `wants to remind you` | `1 Save` · `0 Don't save` · `o Change…` |
| scheduled work | "every Monday at 9, draft the weekly update" | `wants to schedule work` | `1 Save` · `2 Save and run it now` · `0 Don't save` · `o Change…` |
| watch | "tell me when CI on main goes red" | `wants to watch for something` | `1 Save` · `2 Save and check it now` · `0 Don't save` · `o Change…` |

`esc` puts a card off for later. Nothing is saved until the person says yes.

**The card shows the real thing, not a paraphrase:** the title and the person's
own sentence; the schedule in words *and* exactly (`every Monday at 09:00 ·
America/Toronto · cron 0 9 * * 1`) with the next time it will run; for a watch,
what it looks at (the command, the files or the tool) and the condition it is
held to; the line it says or the brief it runs; where work runs (`in your
checkout` or `in a separate worktree, kept for review`); and the limits (`30m ·
$5 a run`).

**Three ways in, one card.** Said in a conversation (the model calls the
`automation` tool); typed with exact syntax (`/automations add …`, no model in
between); or edited on the list (`e` puts the automation's own
`/automations edit …` line in the box). All three end on a card, and nothing is
saved before the person says yes on it. A typed line's card is the surface's
own (`save this automation?`, `save this change?`), because no model asked.

**The typed grammar** is a word and one value per clause, a value with a space
in it quoted (`internal/automation`'s `command.go`):

```
/automations add "weekly update" every "0 9 * * 1" do "draft the weekly update"
/automations add leave at 18:00 say "time to leave"
/automations add "ci on main" every 15m look "gh run list -b main -L 1" until "the latest run failed" once say "CI on main is red"
/automations edit 3f2a every 30m
/automations run 3f2a · pause 3f2a · resume 3f2a · delete 3f2a
```

The clauses are `title`, `at` (`18:00`, `tomorrow 18:00`, `2026-10-12T09:00`),
`in` (`20m`), `every` (a cron line or an interval), `zone`, `say`, `do`, `look`
(a command), `files` (a glob), `until` (the condition), `time` and `usd` (the
limits), `folder`, and the flags `once`, `worktree` and `checkout`. An id may be
any unique prefix. A moment that has already passed is refused. Every
automation writes back out as the `edit` line that would make it what it is, so
the line `e` puts in the box changes nothing when it is saved unedited.

**`/automations`** opens the list: every automation with its mark, title, kind,
schedule, next run and last result. `enter` opens its history (every run: when,
how late, what it came to, what it cost; `enter` on a run opens its transcript),
and `esc` steps back out. The row's own keys are drawn by `→`, the verb strip
every place uses: `r` run now (`check now` on a watch, `say it now` on a
reminder), `p` pause or resume, `s` stop a run in progress (drawn only then),
`e` edit, `d` delete (pressed twice), `o` open the conversation that made it.
The letters work while the strip is drawn.

**When a run ends** the conversation that made it gets one dim line — not in
the transcript, and the model is not woken — and the person gets a desktop
notification. Opening a conversation shows the lines for runs that ended while
it was closed. A watch's quiet looks ("nothing new") are never news; its row
says when it last looked.

**Closing codeaf** with a run in progress, from the last open window, warns
first: `1 automation is running — quitting stops it · ctrl+c again to quit`
(`· /quit again to quit` when it was `/quit`). The same gesture within ten
seconds quits; the clock then stops the run and records it as stopped. Closing
the terminal outright cannot warn; the run is stopped and recorded the same way.

### The words

The run words are the task-state words (`docs/design/task-states/DESIGN.md`):

| Outcome | Reads | Delivered? |
|---|---|---|
| done | `done` — a reminder said, work reported finished, a watch spoke | yes |
| your call | `your call · needed your ok to run gh pr list` | yes |
| incomplete | `incomplete · ran out of its 30m` / `· a fault: …` / `· it ended without saying how it went` | yes |
| stopped | `stopped · by you` / `· codeaf closed before it finished` | yes |
| couldn't check | `couldn't check · no API key` (a watch) | yes |
| nothing new | `nothing new` (a watch looked; condition not met, or still met) | no |

A run that started more than two minutes after its slot reads `late · 2h` beside
its outcome. `failed`, `fired`, `standing`, `keeping an eye` and the banned
machinery words do not appear. The feature's name is **automations**, everywhere.

## The object

`internal/automation` owns it. An `Automation` is: an id; a **title**; the
person's **words** (verbatim, when it was said); a **schedule** (one moment, or a
rhythm — a five-field cron line or an interval — read in a **zone** captured at
creation, with an **anchor** for intervals); an optional **look** (exactly one of
a command, a file glob, or a tool with arguments; a **condition** sentence; and
**once**); an **action** (exactly one of `say` text or `do` brief); a
**workspace**; **worktree** (work only); **limits** (time, default 30m; money,
default $5); its **origin** conversation; a **status** (active, paused,
finished); and the clock's own state (next slot, what a watch last saw,
revision). Its kind is derived: a look makes a watch, a line makes a reminder, a
brief makes work.

A **run** is one waking: the slot it was for, why (`on time`, `late`, `now`),
its phase (queued, running, over), when it started and ended, its outcome, one
line, the longer detail (the work's report, what a look saw), its cost, and its
transcript folder.

## Storage

One SQLite database, `~/.codeaf/v3/automations/automations.db` (WAL, FULL sync,
its own application id, refused if it is anybody else's). Two tables:
`automations` (the person's half as a JSON document, the clock's state as
columns) and `runs`, and a change counter every write stamps rows with.

Why a database, when v3 prefers folders: the feature this replaces kept one JSON
file per item rewritten by several processes, and most of its code was fences
against those writers disagreeing. Here:

- a slot becomes a run and the automation moves on **in one transaction**;
- `UNIQUE (automation, slot, why)` makes a double run impossible;
- the clock's writes are **column updates guarded by the person's revision**, so
  an edit made while a run was in flight is never written over, and the clock
  never rewrites the person's half from a stale copy;
- every window reads new runs with `WHERE seq > cursor`.

## Schedules

- **Cron** is five numeric fields (ranges, lists, steps; Sunday is 0 or 7;
  day-of-month and day-of-week OR when both are restricted), read in the
  automation's zone. The search steps in absolute time and reads the wall clock
  off each candidate, and **the wall clock may never go backwards**: on the
  night the clocks fall back a 01:30 line runs once (the old schedule ran it on
  every pass for an hour); in spring the missing hour has no minutes, so a 02:30
  line waits a day. A line that can never match is refused at the door.
- **Intervals** are a grid, `anchor + k × interval`, so a late run never moves
  the next one later (the old schedule drifted: a `15m` rhythm averaged 17.1
  minutes). `d` is a day; the minimum is a minute.
- **Missed slots catch up once.** However many slots passed while codeaf was
  closed or the machine slept, ONE run is taken, marked late, and the next slot
  is the first after now. A one-time reminder is never dropped for being late.
- **One run at a time per automation.** A slot that comes while the last run is
  still going is passed over, not queued.
- **A pause is not a miss.** Resuming a rhythm starts at its next slot.

## The clock

**Presence.** Every open window holds an operating-system lock on a file of its
own under `~/.codeaf/v3/automations/windows/`. The kernel drops it when the
process ends, however it ends, so "is a window open?" is "can I take any of
these locks?" — no heartbeats, no stale entries. Two kinds of holder:

- every **local window** holds its own presence for as long as its surface runs —
  the in-process door (`cmd/codeaf/chatv3.go`) and the ordinary road
  (`chatv3_local.go`) alike;
- a **window attached over `--host`/`--at`** is registered on the far machine
  by the far engine when the hello says it is a window (`Hello.Window`, set
  only by those doors, and only on the boot connection that lives as long as
  the window), and released when that connection ends, however it ends. It
  holds no presence on the machine it is sitting at: the automations it shows
  are the far machine's.

**The clock is its own process**, `codeaf clock` (hidden machinery, like
`codeaf engine`). Not a window: a run must not die with whichever window
happened to hold it while another is open, and a remote machine has no window
process. Not a session host: hosts linger thirty minutes, belong to one
project, and retire on their own schedule.

- Any window that registers presence starts one if `clock.lock` is free (the
  lock-then-spawn rule `enginehost.Attach` uses), and checks again every 30s so
  a clock that exited is replaced while windows remain.
- It holds `clock.lock` for its life. A second one finds it held and exits.
- Every two seconds it counts windows, takes due slots, starts queued runs (at
  most two pieces of work and four looks at once), and honours stop requests.
- With no window open it waits a 30s grace (a window restarting onto a new build
  closes and reopens its presence in that time), then stops every run — recorded
  `stopped · codeaf closed before it finished` — and exits.
- On start it closes runs a previous clock left `running` (that process is gone:
  the lock proved it), the same way.
- It retires when its binary has been replaced and nothing is running; the next
  window check starts the new build.

**Nothing is installed on the operating system.** The first run of this build
removes the timers the old feature installed — launchd `ai.agentfield.codeaf.tick`
and its legacy-named twin, the v1 `ai.agentfield.codeaf.wake`, and the systemd
units — whoever installed them.

## A run

**A reminder** records `done` with its line. No model is called.

**A watch** looks, then judges:

- *command*: `bash -c` in the workspace, 60s, its process group killed on the
  deadline, the environment stripped of provider keys (`bare.StreamingEnv`);
  output and exit status, tail-clipped to 8 KiB, are the sight.
- *files*: a bounded listing of the glob (names, sizes, a content digest) and
  what changed since the last look.
- *tool*: one call, made through the same configuration a conversation has —
  connected accounts included, the service armed first.
- **The model judges every look** (the owner's choice), on the low tier: the
  condition, the sight and what it last decided; it answers yes or no and one
  line. An answer it cannot give, a provider error, or no key is `couldn't
  check` with the reason, and changes nothing about what the watch has seen.
- It speaks on the change to yes. While the condition stays true it is quiet; a
  no re-arms it. `once` finishes it after it speaks.

**Work** runs as a headless session built exactly like one of the person's
conversations, in the clock process:

- the configuration comes from the same assembly as a conversation's
  (`v3ConfigFor`, the half of `openV3Launch` that builds a config; the launch
  itself is never called, because it resumes the person's newest
  conversation): models, keys, governance, **connected accounts**, media, and
  the brain lent **read-only** — its *always* rules are read before the first
  action, and nothing the run turns up is remembered;
- **the person's own approval rules** (the interactive reading, never the
  headless default and never allow-all), with nobody to ask: a call that would
  ask is refused, and the first such refusal stops the run as `your call`
  naming the call;
- `InTask` (no new tasks, no settings, no watches, no automations from inside
  an automation);
- in the live checkout, or in a worktree cut for the run whose branch is kept;
- its own deadline and money cap, checked after every call;
- **it reports how it went** with the `automation_report` tool (`done` or
  `incomplete`, and a summary). A run that ends without a report is
  `incomplete · it ended without saying how it went`; a cut turn is read from
  its context's cause, never from its text.

**"Save and run it now"** saves the automation, then the tool hands the action
back to the model to do in the same turn, attended: approval cards reach the
person, and an "always" answer banks the rule so later unattended runs go
through. It is not offered for reminders (nothing to try) or for worktree work
(an attended turn runs in the checkout).

## Delivery

The store is the truth; nothing is pushed between processes. Every window keeps
a cursor and reads `runs` changed since it, every two seconds — local windows
from the database, windows on another machine through one wire call,
`Automations.Changes`. From what it reads a window:

- draws the one line in the origin conversation, if that conversation is open
  in it;
- refreshes the list and the counts;
- raises a desktop notification for a delivered run — once per run per machine
  (the run's `told` column is claimed in the store, and only the window whose
  claim lands raises it) — through `osascript` on macOS and `notify-send` on
  Linux, and the terminal's own notification escape where neither answers;
  never while the window is known to be focused.

The quit question reads the same snapshot: runs in progress, and the windows
open other than this one.

## Rules become memories marked always

A rule is a memory with `always` set. It is put in front of every conversation
turn (in the system prompt, beside where standing orders were) and every task
brief, at its scope — you, this machine, or this project — with no ages, in a
stable order, capped. A recalled memory is unchanged: retrieved when relevant.

- `/always <text>` saves one for this project (for you, from home); the bare
  command lists the ones in force here.
- The `remember` tool gains `always`. Because an always rule is in front of
  every future turn, the model's own `remember` with `always` asks the person
  first; the person's typed `/always` does not.
- The memory place marks always rows and `a` toggles the flag.
- Extraction never sets, removes or supersedes always; a duplicate always write
  promotes the recalled copy, and a recalled write never demotes an always one.
- There is no rule for one conversation only and no "not here" exception.

## What is deleted

- `internal/standing`, the `stand` tool and every standing door in
  `internal/session`, `internal/tui3` (the page, home's standing band and panel,
  the margin section, the threshold line, the foot count) and `cmd/codeaf`
  (`chatv3_standing.go`, the ticker, the background repair, the host's
  standing cache); `/standing` and `/orders`; the remote wire's `Standing.*`
  methods, `ResolveStanding` and the marked submit; the background-checks
  setting, whose key is retired.
- The v1 resident's scheduler: its charters, watches, sentinels, tenure and
  practice loop, `internal/watchdog` and the doctor's timer rows. `codeaf tick`
  and `codeaf wake` survive only as hidden verbs that do nothing and exit 0, for
  a timer an older build installed until this build's first start removes it.
- The manual pages `keeping-an-eye` and `standing-orders`, and every standing
  passage elsewhere.

**Existing standing items are not migrated** (the owner's call). The folder
`~/.codeaf/v3/standing/` is left on disk and no longer read. Collections that
named a standing item keep the row; it resolves to nothing.

**Re-homed, not deleted:**

- The memory tidy, which rode the standing pass, rides the clock: asked every
  five minutes while a window is open, one at a time and bounded to two
  minutes, and it keeps its own gates — fifteen quiet minutes on the machine
  and six hours since the last pass.
- Home's `ask here` errands, which lived under the standing root, live in
  `~/.codeaf/v3/errands`. The launch sweep reaps one that went nowhere after a
  week, unless an automation was saved from it, because "open where it was
  asked" reads that conversation.
- Spend attribution names automations, and an older ledger line's standing item
  is read as one.
- `codeaf do` and plan workers read *always* memories where they read standing
  orders.
- An unattended goal owner no longer says yes to a card on the person's behalf:
  an automation needs the person's own yes.

## Build order

1. `internal/automation`: the object, schedule, store, presence, clock (done
   first, with its tests).
2. Memory *always*: column and migration, the store door, `remember`, `/always`,
   injection into turns, task briefs and `codeaf do`, the memory place, the
   manual.
3. The session side: `v3SessionConfig`, the runner (look, judge, work,
   `automation_report`), the `automation` tool and its card event.
4. The process side: `codeaf clock`, presence in `runSurface` and the far
   host, the spawn check, old-timer removal.
5. The surface: the card, `/automations`, the typed grammar, the lines, the
   notifications, the quit question, the wire calls.
6. Delete standing and the v1 scheduler; rewrite the manual; satisfy the gates.

## Not in this design

Push triggers (a webhook, a file event), running with codeaf closed, phone
notifications, and automations that make automations.
