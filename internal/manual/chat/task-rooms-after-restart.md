# A task's room after a restart

## See what a task did after restarting — a finished task's room shows its whole transcript

Close aforge, open the same conversation again, and walk into a task that finished in the
earlier life of it — `enter` on its roster row, a `task 7`
link — and its room replays **the whole transcript**: the instruction it was given, its
prose between calls, its thinking blocks, every tool call with its arguments and result,
anything you steered into it, and the report at the end. The foot line reads
`this task has finished — say it to main`, exactly as it did the moment the task landed —
and where the task was spawned under another task it names that too:
`this task has finished — say it to main, or open its parent, Ship the port`.

That works because the task's transcript is a real file on disk, kept beside the
conversation that commissioned it — `<session folder>/tasks/<YYYYMMDD-HHMMSS>_<task id>.jsonl`
— and the conversation's task checkpoint remembers which file is which. A checkpoint
written by an older aforge that did not remember is no worse off: the file is named with
the task's id, so the room finds it by that id in the same directory, and remembers it from
then on. The audit and repair transcripts beside it (`…_<id>-audit-<6 hex>.jsonl`,
`…_<id>-repair1.jsonl`) are never mistaken for the task's own. A task that ran more than
once under one id — interrupted, then resumed — opens on the latest run.

Nothing about this changes what the room is while a task is **running**: that page is the
live edge, and history comes off the same journal as it always did.

## Task page is empty — I opened a task and there is nothing in it

An open room that shows nothing but `this task has finished — say it to main` used to be what a
finished task looked like after a restart: the transcript was on disk, but the resumed
conversation had not kept the file's name and so replayed nothing. That is fixed — the
name is kept on the checkpoint, and an older checkpoint is filled in by the task's id — so
a finished task's room now carries its transcript across restarts.

When a room is **truly** empty — the task has landed and there is no journal to read — it
says so in one dim line above the foot:

```
this task's transcript is not here any more
this task has finished — say it to main
```

That line means the file itself is gone: a session folder you deleted, or work that
happened on another machine. It is a fact about the disk, not a fault in the task. A room
with even one block in it never shows the line.

When the task has **not landed** — it is queued behind the running ones, or it has only
just started and nothing has been written for it yet — the page says the other half of
that, and there is no foot under it because nothing has finished:

```
nothing on this page yet — it fills in as the task works
```

Neither of the landed lines would be true there: nothing was lost, and nothing is over.
This one comes off by itself the moment the task's first block arrives.

**A room never draws a blank body.** If the roster still holds the task's
report — and it does, for anything that landed in a conversation you have open — the report
is drawn above that line, so the page tells you what the work came to even when the
transcript behind it is gone:

```
Added the guard in parseRow and covered it with a test.
this task's transcript is not here any more
this task has finished — say it to main
```

The top bar above the page is drawn from the same record, which is why it stays correct —
the name, the state, the elapsed — in every one of these cases. (The room has no pinned
header of its own any more; those facts are the bar's.)

The room is one of two doors onto old work. The other is the task page (`ctrl+.`,
`/history`), whose `enter` on an `earlier` row opens a card with the task's report read off
the same journal — and which says `its transcript is not on this disk any more` in the same
case. If the room is empty, the card will be too; the file is the same file.

## I clicked on the task and there is nothing there at all — the page is hidden or blank, no chat, no output

If you walked into a row and the page showed nothing you could use, check
**what kind of row it was**. The column beside the conversation carries two different
things, and only one of them has a chat inside it.

- **A task** has an agent, a transcript and a report. Its room replays the whole thing. If
  it draws no transcript and the task has landed, the file is gone and the page says
  `this task's transcript is not here any more` — with the task's report above it when the
  roster still holds one. If it draws no transcript and the task has **not** landed,
  nothing has been written for it yet and the page says
  `nothing on this page yet — it fills in as the task works`.
- **A background job** — anything `bash background:true` started, a watch, a video render —
  has none of that. There is no agent inside a job and nothing was ever journaled for it,
  so its page shows the log line `job 4 · log /path/…` and, under it, the end of that log
  itself: the last 200 lines, newest at the bottom, re-read four times a second while the
  job runs, under a foot reading `this log grows as the job works — say it to main`. A job
  whose log is empty or unreadable says `a background job keeps a log, not a transcript`
  and nothing else — never an error over work that is going fine. `jobs output 4` prints
  the log too, and over `--host` it is the only way: a far job's log lives on the far
  machine and the page draws the path rather than the file.

A job's row is easy to tell apart before you open it: it is the row whose dim under-line
starts with `job` and a number.

Earlier versions of aforge got this wrong in a way worth naming, in case you remember it: a
job's page came up correctly named at the top of the frame — its name, `done`, its elapsed — over a body
holding nothing but `· no task 4 in this session` and the foot. That sentence was aforge
talking to itself, not about anything you did, and it is gone. A task that had not landed
yet went wrong the same way and for longer: a queued task, or one opened the instant it
started, was correctly named at the top of the frame over a screen with **nothing at all** on it, because the
line explaining the blank was only ever drawn for work that had finished. Both are fixed.
**No room draws an empty body any more**, landed or not: whatever the row is, the page
says what it knows and what it does not.

## Task finished but no chat shown — where did the task's conversation go

A finished task's conversation is its **journal**, and the room replays it. If you opened
the room and saw only the foot line, the transcript was not found: either the file is gone
from the disk (the room now says `this task's transcript is not here any more` when that is
so) or you are on an aforge from before the room could find a task's file by its id after a
restart, in which case the file is still where it always was —
`<session folder>/tasks/<YYYYMMDD-HHMMSS>_<task id>.jsonl` — and the `read` tool, or your
editor, opens it.

The task's own tool rows never enter the main chat; they go to its journal and its room
only. The landing note in the conversation carries the transcript's URI on its first line
(`task 7 finished: <title> · transcript file:///…`), which is the same file the room
replays. Nothing here is a summary: a landed task's room is the whole transcript.
