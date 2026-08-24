# A task's room after a restart

## See what a task did after restarting — a finished task's room shows its whole transcript

Close aforge, open the same conversation again, and walk into a task that finished in the
earlier life of it — `enter` on its roster row, a click on its strip chip, a `task 7`
link — and its room replays **the whole transcript**: the instruction it was given, its
prose between calls, its thinking blocks, every tool call with its arguments and result,
anything you steered into it, and the report at the end. The foot line reads
`task finished — esc to return`, exactly as it did the moment the task landed.

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

An open room that shows nothing but `task finished — esc to return` used to be what a
finished task looked like after a restart: the transcript was on disk, but the resumed
conversation had not kept the file's name and so replayed nothing. That is fixed — the
name is kept on the checkpoint, and an older checkpoint is filled in by the task's id — so
a finished task's room now carries its transcript across restarts.

When a room is **truly** empty — the task has landed and there is no journal to read — it
says so in one dim line above the foot:

```
this task's transcript is not here any more
task finished — esc to return
```

That line means the file itself is gone: a session folder you deleted, or work that
happened on another machine. It is a fact about the disk, not a fault in the task. A room
with even one block in it never shows the line.

The room is one of two doors onto old work. The other is the task page (`ctrl+.`,
`/history`), whose `enter` on an `earlier` row opens a card with the task's report read off
the same journal — and which says `its transcript is not on this disk any more` in the same
case. If the room is empty, the card will be too; the file is the same file.

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
