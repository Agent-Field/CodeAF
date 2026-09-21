# Watching and steering a run-engine task — SURFACE

*2026-09-17. Design for the chat surface of the worker harness
(`docs/design/worker-harness/DESIGN.md`, branch `harness/worker-loop`). Read
against the tree on this branch, and against `docs/DESIGN-LANGUAGE.md`, which is
the visual contract every frame below obeys.*

The harness replaces the task engine (DESIGN §the one sentence). The early waves
land the loop and the store; wave 5 is *the chat*: "`/task` creates a run; the
pane's filter and linked rows; the thread with soft and hard steering; the offer
card". This document is the writing for that wave, and only for one question in
it: **what a person watching a run sees, and how they steer it, reusing the words
the surface already has.**

## The ask, in one sentence

A person watching a task that runs on the run engine must see which step it is
on and what command that step is running, and must be able to leave a note the
loop picks up at its next step — with no word on any screen that the surface does
not already use.

The vocabulary is fixed and small: **task, step, note, steer, the task rail, the
landing card, running / done / incomplete / your call.** Everything below is
built from those, and §5 names, for every frame, where the surface already says
each word.

---

## 1 · What exists today

### 1a · What the engine exposes, per task and per step

**The step record — the task's page, on disk.** `internal/run/trajectory.go`
defines the one record a run keeps. `Step` (trajectory.go:48) carries `Kind`,
`Step` (the number, from one), `Command` (what the worker asked the belt to run,
:56), `Observation` (the head of what came back, cut at `observationHeadBytes` =
2048, :40, :134), `FullOutput` (the path of the whole output when the belt filed
one, :63), `Writes` (the `plandb` verbs the command ran, :69), and `Children`
(the child ids the step created, :73). `Trajectory(storeDir, id)`
(trajectory.go:84) reads them back in append order; `appendTrajectory`
(trajectory.go:111) appends one line to `trajectory.jsonl` under
`plandb.TaskDir`. A line that will not parse is skipped, not reported.

**When a line is written — and when it is not.** `internal/run/bashworker.go`'s
loop records a step only on `session.EventToolEnd` or `session.EventToolFailed`
(bashworker.go:115): *the step is appended after it finishes.* `stepRecorder.record`
(bashworker.go:204) reads the command off the event's display arguments
(`stepCommand`, :222 → the `command` field for `bash`, the whole argument object
for a kept hand). The session agent *does* raise `session.EventToolBegin`
(session.go:71–75), which carries the tool name and its rendered `Args` — the
command — **before** the call runs. Nothing in the run writes it down.

**The end line.** The loop appends one `trajectoryEndKind` line with `Steps`,
`Result` and `Reason` (bashworker.go:129–131, trajectory.go:76–81).

**The store's notes, and the author.** `internal/session/plandb_tasks.go`'s
`PlanTaskNote` (:62) carries `Author`, `Person` (a bool set from
`plandb.NoteFromPerson`) and `At`. `plandb.Store.Notes(taskID, limit)` answers
them oldest first; `planLastNote` (:347) answers the newest body.

**`Changed(since)`.** `internal/plandb/store.go:864` answers the ids of the tasks
that moved since a moment — the task row, a note left on it, or a context entry
scoped to it. It names *which* tasks to look at, never *what* changed.

**The person's six verbs.** `internal/session/plandb_steer.go` is the one writer
the surface owns: `PlanNote` (:62) leaves a person-note (`AddPersonNote`),
`PlanPause`/`PlanResume` hold and release a subtree, `PlanCancel` cascades,
`PlanAmend` prepends to the work order, `PlanPriority` revises. All six take
`planSteer` (:42), which resolves the id *inside this conversation's plan* and
refuses a task another chat spawned (`errPlanOtherChat`). Every write goes through
a store method; the boundary is the only thing this layer owns.

**The read side.** `PlanTaskRow` (plandb_tasks.go:35) is one row ready to draw:
`ID, Title, Status, Seat, Parent, Steps, USD, Started, Ended, Note,
TrajectoryPath`. `PlanTaskPage` (:125) is one task's page: its row, `Description`,
`Notes`, `Steps`. `PlanStep` (:84) mirrors `internal/run`'s `Step` (it cannot
import that package back). `PlanTasks` (:105) is the plan this conversation
seeded; `PlanSpend(since)` (:204) is the ledger rolled up *by seat* — `Seat`,
`Model`, `USD`, `Calls`, dearest first. `planSpendBySeat` reads a read-only
connection so it never races the writer.

### 1b · Where the surface already shows and steers a task

**The task rail.** The always-on right column is the roster: `railLines`
(task.go:3301) renders `railEntry` rows, `railView` (:3340) windows them,
`railEntryRows` (:4595) draws one. The column walks `app.taskOrder` — **the
conversation's own graph, and nothing else**. A running row spends a block of at
most `railUnderRows = 2` rows under its title (:5226), chosen by `railUnder`
(:5027): `railWorking` (:5279) draws the *tool name* (e.g. `bash`) and, past
`taskToolFloor` = 10s, the call's own clock; `railTelemetry` (:5395) draws
`42s · 9.9k · $0.31 · gpt-5` — each segment dropped from the right when the
column is narrow, each omitted when it has nothing behind it. A row leads with
**one glyph, and it is the state** (`railLead`, :4797).

**The tasks place — where the store's rows are actually drawn.** `planItem`
(taskplan.go:153) turns a `PlanTaskRow` into a `tasksItem`; `tasksplace.go:293`
merges the store's plan *after* this window's own work, skipping any row a graph
node already carries (`planRowShown`, taskplan.go:231). So the plan's rows appear
in the **tasks place** (`/history`, `ctrl+.`, `alt+2`, and the roster raised over
the frame) — not on the always-on column, which draws only the graph. A plan row's
state cell is `planStateField` (taskplan.go:196): the state word plus `N steps`.
`planStateWord` (:69) maps the store's status onto the surface's one word —
`pending/ready/claimed/running → running`, `done → done`, `failed/cancelled →
incomplete`, `paused → your call`.

**The task's page.** `taskPlanFrame` (taskplan.go:541) draws the page in the
card's own slot: a head, a body, a three-row foot. `taskPlanBody` (:622) draws,
in order and each with nothing behind it omitted, `description`, then `notes`
(each with its author — `you` for a person — and its moment, drawn apart), then
`steps` (`N  <command>` in ink, the observation head dim under it). The foot is
the note composer (`› a note for this task`) and the key line
`taskPlanKeys` (:606): `↑↓ scroll · enter send · x stop it · p pause · esc back`.
`taskSheetPlanKey` (:425) takes `x` (cancel) and `p` (pause/resume) **over an
empty composer**; every other printable key is the note (`taskPlanKey`, :460).
`taskPlanNoteSend` (:393) writes the note through `PlanNote` and re-reads the
page so the receipt is on screen. `taskSheetPlan` (:274) opens the page and reads
it **once**; nothing re-reads it while it is open.

**The steering keys, spelled once.** `tasksPlanCancelWord = stopRaiseKey + " " +
stopActWord` (`x stop it`), `tasksPlanPauseWord = "p pause"`,
`tasksPlanResumeWord = "p resume"` (taskplan.go:316–322). `taskPlanNoteWord = "a
note for this task"` (:313). The manual quotes each verbatim.

**The landing card.** The settled card in the transcript — `taskCardRows`
(task.go:1813), `taskHead` (:1930) with the `╭─ … ` corner, `taskFoot` (:2028)
which *is* the answer once settled. It carries the outcome word, the span, the
file count, `merged` / `in your own folder` / `branch kept · <branch>`, the
quoting sentence and `ctrl+o`. It is the transcript's object for work that
**landed**; its keys are the answers and the expansion, never a note.

**The spend page's tasks-by-seat block.** `spendSeatsWord = "tasks by seat"`
(spendplace.go:1063), drawn last under whatever cut a person chose (:520);
`seatRow` (:1102) draws `Seat`, the model behind it, and the dollars.
`spendSeatsWhisper = "task spend by seat arrives here as tasks run"` (:1068) is
the block's one dim line when no seat was charged.

**The commands' pages.** `/task` (commands.md:1270) starts work you can walk away
from; a bare `/task` opens *the full-screen task page* (commands.md:1295) — the
same page `/history` and `ctrl+.` open. `/history` (commands.md:1376) is the
project's record; the page is explicitly **not** `/tasks`, "and there is no
`/tasks` command". The harness manual is
`internal/manual/chat/worker-harness.md`; the steering words are in
`internal/manual/chat/tasks.md` and `reading-a-task-page.md`.

### 1c · What a watcher cannot see today

1. **The live command of a running step.** The record is written on step *end*
   (bashworker.go:115), so the store has no "current command". The rail's
   `railWorking` shows the *tool name* for a conversation-graph task; for a run
   task the row carries `running · N steps` and nothing live
   (`planStateField`). The `EventToolBegin` that carries the command is dropped
   on the floor by the run.
2. **The observation as it arrives.** `Observation` is the finished head; there
   is no partial.
3. **Which worker seat is on it.** `PlanTaskRow.Seat` exists and the spend page
   draws seats, but no row a watcher reads says the seat beside the running work.
4. **A note's pickup.** The store records that a note was *left*; nothing records
   that the worker *read* it. The composer's receipt (`taskPlanNoteSend`) proves
   the note landed in the store, not that the loop saw it.
5. **That a run task is even on the column.** The always-on rail walks the graph;
   a store row with no graph node behind it is visible only in the tasks place.

Two smaller vocabulary facts the design leans on: the surface already has the
words **`queued`** and **`waiting`** for admitted-not-started work
(`session.TaskQueued`, `railWaits` → `waits: <what>`, tasks.md "the roster"), and
a run task whose store status is `pending` currently reads **`running`**
(worker-harness.md); and the surface's own word for a call in flight is already
the row's **step glyph** (`tokens.GStepRunning` `◐`, `GStepPending` `○`,
`GStepDone` `●`, `GStepBlocked` `⚑`; glyph.go:259–262).

---

## 2 · Three candidate designs

All three reuse `railSep = " · "` (task.go:5235), `prompt = "› "` (input.go:16),
the plain rule, `paintHint`/`hintFit`, and — for every mark — the one door
`palette.glyph` (styles.go:1124, "THE ONE DOOR EVERY ICON ON THIS SURFACE COMES
THROUGH"). No frame draws a border; the column is separated by air, not a rule.
Frames are 100 columns.

### A · The task rail grows a live step line per running task

One new thing: the running row's under-block gains the **live step** — the step
glyph and the command the step is running — where today it has only the step
count in the state cell. It is the under-block the column already spends on a
running row, fed from the run engine.

**At dispatch** — the row appears, the state cell says the surface's own word for
admitted work, and there is nothing live yet:

```
                                                                        tasks · 1 working
                                                                         ○ Add rate limiter…  #3
                                                                           queued · waits: slot
```

**Mid-run** — the live step leads the under-block, the telemetry stands under it
(`N steps · $`, existing words, in the surface's own order):

```
                                                                        tasks · 2 working
                                                                         ◐ Add rate limiter…  #3
                                                                           $ git grep -n RateLi…
                                                                           12 steps · $0.11
                                                                         ◐ Widen the import…  #8
                                                                           $ go test ./internal…
                                                                           3 steps · $0.02
```

`◐` is `pal.glyph(tokens.GStepRunning)`; `$` is the shell call's own lead
(`tokens.GlyphShell`, the same byte the conversation's tool line uses); the
command gives up its tail to the column's width exactly as `railWorking` already
fits `node.tool`. The under-block stays two rows, so the cap is untouched.

**When a note is left** — the note is left on the task's page (§2b), and the rail
carries the one visible consequence the store can give it: the row's dim note
tail (the surface already draws a task's `Note`; `planLastNote`). Pickup is not
shown, because nothing records it (§1c-4).

**At landing** — the row's glyph becomes `●` `GStepDone`, the live line goes, and
the under-block falls back to what it always was: the branch word, the state.

```
                                                                         ● Add rate limiter…  #3
                                                                           done · 14 steps · $0.11
```

**Keys that steer** — the row's own: `enter` opens the task's page (room/card door
as today), `x stop it`, `p pause`/`p resume` (taskplan.go:317–322). No new key.
The note is typed on the page A opens.

**Redraw cost.** The column already repaints while live work is on it — the paint
clock is gated to where something animates (`tasksAnimating`). One live field per
running row costs the under-block it is already rendering; nothing grows with the
length of the run. The cost that is *not* free is the store read: the live step
must be on the row `PlanTasks` answers, so the engine must publish it (D5's
record is append-only and end-of-step). Cheapest of the three.

### B · The task's page becomes a live trajectory, following the newest step

One new thing: the page stops being a snapshot read once on open
(`taskSheetPlan`) and becomes a reading that follows the live edge. The
`steps` section the page already draws gains a **running step** — the step glyph
and the command — as it happens, and the page follows it. The composer at the
foot is where the note is typed.

**At dispatch** — the page opens on the work order and the state line, the
sections with nothing behind them absent:

```
Add rate limiter to /api/upload
────────────────────────────────────────────────────────────────────────────
queued · waits: slot
description
  Add a per-IP rate limiter to the upload handler; keep it under the existing
  middleware chain.
────────────────────────────────────────────────────────────────────────────
› a note for this task
↑↓ scroll · enter send · x stop it · p pause · esc back
```

**Mid-run** — the steps stand as they do, and the newest one is live and follows
the bottom:

```
Add rate limiter to /api/upload
────────────────────────────────────────────────────────────────────────────
running · 12 steps · $0.11
description
  Add a per-IP rate limiter to the upload handler; …
steps
  11  $ sed -n 40,120p internal/api/upload.go
  12  $ git grep -n RateLimit internal/api
      3 hits
  ◐  $ go test ./internal/api/...
      running 41s
────────────────────────────────────────────────────────────────────────────
› a note for this task
↑↓ scroll · enter send · x stop it · p pause · esc back
```

`◐ … $ <command>` is the same two-glyph lead as A; `running 41s` is the call's
own clock the rail already counts (`taskClock`/`taskToolFloor`). The page already
draws `N  <command>` in ink with the observation head dim under it
(`taskPlanBody`), so the live line is the existing shape one step early.

**When a note is left and picked up** — `enter` sends the composer through
`PlanNote`; the page re-reads so the note appears with `you` and its moment (the
composer's existing receipt, taskplan.go:393). *Pickup*: the loop's next step
appears at the live edge, and — the one honest signal available — a note the
worker acted on is a step whose command runs the `plandb` verb the note implies
or a `plandb task note` reply. The design does **not** invent a "read" receipt;
it says in the manual that the note is picked up at the next step, which is what
the loop does (DESIGN D5: "it appears in the worker's next frame as `person
said: …`").

**At landing** — the live line settles to an ordinary `●` step, the state line
reads `done` and the landing word, and the composer's placeholder becomes the
foot's closing line.

**Keys that steer** — every key the page already has: `enter send`, `x stop it`,
`p pause`/`p resume`, `↑↓ scroll` over an empty composer (`taskPlanKeys`). A
person who has used the page has learned the whole design.

**Redraw cost.** The page redraws while it is open. Following the live edge costs
a re-read of `PlanTaskPage` on a beat while the page is up — a store read and a
repaint, bounded by the page being open and by the frame's height (only the tail
is drawn). The store read is the same cost A pays, so the two share it.

### C · The landing card is where steps stream while the task runs, and where the result lands

One new thing: the conversation's card becomes a live object — a block that
streams steps while the task runs and settles into the landing card it already
becomes.

**At dispatch** — a forming block, which the conversation already has for a
proposal arriving (`taskFormingRows`, task.go:1975):

```
╭─ ○ Add rate limiter to /api/upload ────────────────────────────────────────
│ ◐ queued · waits: slot
╰───────────────────────────────────────────────────────────────────────────
```

**Mid-run** — the card streams the newest steps in place:

```
╭─ ◐ Add rate limiter to /api/upload ────────────────────────────────────────
│ running · 12 steps · $0.11
│ $ git grep -n RateLimit internal/api          3 hits
│ $ go test ./internal/api/...                  running 41s
╰───────────────────────────────────────────────────────────────────────────
```

**When a note is left** — the card would need a composer, which it has never had
and which the design language puts under a page, not in a transcript block. So
steering here means opening the task's page and typing there — i.e. C borrows
B's only steering door.

**At landing** — the streaming block becomes the settled landing card, exactly as
`taskFoot` draws it once answered. The transition is the one thing C does well.

**Keys that steer** — `enter` opens the task's room/page; the card's own `ctrl+o`
and its answer chips, which are the *landing's* keys, not steering keys.

**Redraw cost.** The highest. A live block in the transcript re-lays the feed as
steps arrive; a 200-step run either streams 200 lines into the conversation (noise
the design language's "the transcript is where paragraphs live" refuses) or must
cap itself, at which point it is a rail under-block and not a card at all. The
card's meaning also blurs: it is the object for work that **landed**
(tasks.md "landing card", "the result lands"), and streaming makes it the object
for work **running**.

### What each candidate names, and where the surface says it

| word | A | B | C | already used at |
| --- | --- | --- | --- | --- |
| task / step / steps | yes | yes | yes | `planStepWords`, rail rows, `taskPlanBody` |
| note / steer | on the page | composer | borrowed | `taskPlanNoteWord`, `PlanNote`, tasks.md |
| running / done / incomplete / your call | state cell | state line | card head | `planStateWord` |
| queued / waiting | dispatch | dispatch | dispatch | `session.TaskQueued`, `railWaits` |
| the task rail / the task's page | rail | page | — | rail, `taskPlanFrame` |
| the landing card | landing row | landing foot | the card | `taskCardRows` |
| glyphs | `◐ ○ ● $` | `◐ ● $` | `◐ ●` | `palette.glyph`, tool line |

No candidate needs a word outside this table.

---

## 3 · Recommendation

**Recommend A, with B as the depth A opens.** The one live line belongs on the
**task rail**, because that is the surface a person is already watching while work
runs — the column already spends its two-row under-block on a running row, already
leads with a state glyph, and already says what a running node is doing for a
graph task. A run task's row is the same row, missing the same fact; feeding it
the live step is the smallest change that answers "which step, which command" at a
glance, and it costs the least redraws. **`enter` on that row opens the task's
page (B)**, which is where the whole trajectory already lives and where the note
composer already is — so B is not a rival design but A's second half: A answers
"is it alive and what is it running", B answers "show me everything and let me
steer".

Reasons, in order:

1. **It is the surface the person is looking at.** Watching is an ambient
   activity; the answer has to be on a screen that is already up. The rail is up.
   A page is a visit (B is A's visit), and the transcript is where replies live,
   not where progress lives (C).
2. **It reuses the rail's own machinery exactly.** The under-block, the state
   glyph, the drop-from-the-right fitting, the 10-second call clock, and the two
   words `N steps` and `$` are all present; A only stops the run's row from being
   the one running row that says nothing live.
3. **It costs the least.** No new block in the transcript (C's cost is unbounded
   with the run), no live block whose meaning fights the landing card, and one
   store read shared with B.
4. **Steering stays where steering already is.** The note composer, `x`, and `p`
   are on the task's page and need no new key. Putting steering on the rail would
   mean a composer in a 24-cell column, which the design language refuses.

**What is refused, and why.** C is refused on cost and on meaning: the landing
card is the object for work that landed, its keys are the landing's answers, and
streaming steps into the transcript is the one shape the design language bans
("the transcript is where paragraphs live"). B is not refused — it is kept as the
page A opens. Two corrections to A that stay inside the existing vocabulary:

- A store status of **`pending` should read `queued`**, not `running` — the
  surface's own word for admitted work with only a slot in its way
  (`session.TaskQueued`, `railWaits`). Today `planStateWord` folds `pending` into
  `running` (worker-harness.md), which is the one word on the frame that is not
  true of the moment.
- A task **held behind named work** should read `waits: <what>` (the dependency
  sentence `railWaits` already draws), rather than only the word `running`.

Both are the surface's words reassigned, not new ones.

**The one engine requirement both A and B rest on.** The live command must reach
the store. The run drops `session.EventToolBegin` (session.go:71), which carries
the command before the call runs; the design writes it down — a live step on the
task's record, cleared when the step's end line is appended. Without it, A and B
draw nothing live, and §1c-1 stands.

---

## 4 · Implementation plan

Four M-sized cells on `harness/worker-loop`, in order. Each is ≤4 items.

### Cell 1 — the engine publishes the live step (`internal/run`, `internal/session`)

1. `internal/run/bashworker.go`: on `session.EventToolBegin` (the run loop today
   ignores it), record the in-flight step — the step number, the command
   (`stepCommand` reused), and the moment — to a live field the store can answer.
   Keep the end-of-step append unchanged; the live field is cleared by it.
2. `internal/session/plandb_tasks.go`: read the live field back onto `PlanTaskRow`
   (a `Live` field: `Step`, `Command`, `Since`) and onto `PlanTaskPage`.
3. `internal/run`: clear the live field on the step's end line and on every
   ending, so a stopped or finished task never claims a present it is not in.
4. Tests: `internal/run` — a scripted completer's `EventToolBegin` appears as the
   live step and is gone at the end line; `internal/session` — `PlanTaskRow.Live`
   round-trips, and a task with no live step answers empty.

### Cell 2 — the rail's live step line (`internal/tui3`)

1. `internal/tui3/taskplan.go`: `planStateField` and `planItem` carry the live step
   onto the plan row; map store `pending` to the surface's `queued`, and a held
   task to `waits: <what>` (§3 corrections).
2. `internal/tui3/task.go`: where a plan row draws its under-block, draw the live
   step line — `pal.glyph(tokens.GStepRunning)` + the shell glyph + the command,
   fitted the way `railWorking` fits `node.tool` — capped at `railUnderRows`.
3. `internal/tui3/taskplan.go`: the telemetry under it is `N steps · $` (existing
   `planStepWords`/`planSpendWord`).
4. Tests: `internal/tui3` — a running plan row draws the live command and the
   telemetry; a done row draws neither; a narrow column drops the command's tail
   and keeps the step count, never a half-glyph.

### Cell 3 — the page follows the live edge (`internal/tui3`, `internal/session`)

1. `internal/tui3/taskplan.go`: while the plan page is open on a running task,
   re-read `PlanTaskPage` on the paint beat and follow the newest step (the page's
   `detailTop` pinned to the bottom until the person scrolls, exactly as the room
   follows its live edge).
2. Draw the live step as the page's `steps` section already draws one, one step
   early: `◐  $ <command>` in ink, the call's clock dim under it.
3. `internal/session`: the note dialog's receipt stays as it is (`PlanNote` +
   re-read); the page says the note is picked up at the worker's next step, in the
   manual's words.
4. Tests: `internal/tui3` — the page follows a step appended while it is open; a
   scroll up stops the follow; a note left on the page appears with `you` and its
   moment.

### Cell 4 — the manual (`internal/manual/chat`)

1. `internal/manual/chat/worker-harness.md`: the rail's live step line, the page's
   live trajectory, and the reading of `queued`/`waits:` on a run task.
2. `internal/manual/chat/tasks.md`: the rail section gains the live step line; the
   landing card's section is unchanged (C is refused).
3. `internal/manual/chat/reading-a-task-page.md` / `task-controls.md`: the page
   follows the live edge, and the composer's pickup sentence.
4. `internal/manual/chat/commands.md`: `/task` and the `/history` page name the
   live step, and the fact that there is no `/tasks` command is kept as it stands.

**Manual pages that must change:** `worker-harness.md`, `tasks.md`,
`reading-a-task-page.md`, `task-controls.md`, `commands.md`. All five are covered
by `internal/namelaw`.

**The e2e needles the tmux suite would wait for** (the suite reads the screen back
with `capture-pane`; `r.waitFor(timeout, needle)` — roomsteer_e2e_test.go:102
waits `"bash sleep 25"` today). For this design, in a new
`internal/e2e/harness_surface_e2e_test.go`, the needles are the strings the run
writes:

- at dispatch, the row: `queued · waits: ` — the surface's own word, not
  `running`;
- mid-run, the rail's live line: `$ git grep -n RateLimit internal/api` (the
  command, verbatim, cut by the column — assert on the `$ ` lead and the verb);
- mid-run, the row's telemetry: `12 steps · $` — the two figures beside the live
  line;
- on the page: `◐` beside the command and `running ` — the live step and its
  clock;
- the pickup: the worker's next step naming `person said` in the store note, or
  the `plandb task note` reply as the next step's command;
- at landing: `done · 14 steps · $` on the row, then the landing card's
  `branch kept · ` or `merged` — the words it already draws.

---

## 5 · Proof

- **The document exists and is under 30 KB.**
  `docs/design/worker-harness/SURFACE.md`. Size checked with `wc -c`.
- **`go test ./internal/namelaw/`** passes (the design tree is exempt from the
  law, and the doc spells no retired product name regardless).
- **`make test-quick`** is green: `build-check vet fmt-check test-packed-manual
  changelog-check manual-gates test-laws`.

Commands and their last lines are reported in the task's report.
