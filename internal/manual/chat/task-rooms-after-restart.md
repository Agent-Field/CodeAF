# A task's room after a restart

## See what a task did after restarting — a finished task's room shows its whole transcript

Close aforge, open the same conversation again, and walk into a task that finished in the
earlier life of it — `enter` on its roster row, a click on its strip chip, a `task 7`
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

**A room never draws a blank body under its header.** Above that line the page draws what
it already knows, which is different for work that is still going and work that is over.

For a task that has **not landed**, the page draws **the instruction you gave it** — the
brief, held since the task was admitted — and then, where the engine has said one, the
sentence naming what the work is doing right now: what it is held behind (`rate limited`),
the gap it is closing, or the call it is on. So a task opened the second it starts reads
like this rather than as a blank:

```
Widen the import pipe so the nightly run stops timing out.
rate limited
nothing on this page yet — it fills in as the task works
```

Every one of those rows is drawn only while the page has no blocks, so the whole of it is
replaced — not added to — the moment the transcript arrives. Opening the page starts
nothing and restarts nothing: it reads work that is already running.

The header above it is saying the state in one word (`working`, `waiting`, `queued`) with
the clock beside it, so the body never repeats that word — what it adds is the reason
underneath it, which is the thing the header has no room for.

For a task that **has landed**, the report takes that place. If the roster still holds it —
and it does, for anything that landed in a conversation you have open — the report is drawn
above the line, so the page tells you what the work came to even when the transcript behind
it is gone:

```
Added the guard in parseRow and covered it with a test.
this task's transcript is not here any more
this task has finished — say it to main
```

The header is drawn from the same record, which is why it stays correct — the name, the
state, the elapsed — in every one of these cases.

The room is one of two doors onto old work. The other is the task page (`ctrl+.`,
`/history`), whose `enter` on an `earlier` row opens a card with the task's report read off
the same journal — and which says `its transcript is not on this disk any more` in the same
case. If the room is empty, the card will be too; the file is the same file.

## I clicked on the task and there is nothing there at all — the page is hidden or blank, no chat, no output

If you walked into a row and the body under the header showed nothing you could use, check
**what kind of row it was**. The column beside the conversation carries tasks and jobs as
different kinds of row, and only a task has a chat inside it.

- **A task** has an agent, a transcript and a report. Its room replays the whole thing. If
  it draws no transcript and the task has landed, the file is gone and the page says
  `this task's transcript is not here any more` — with the task's report above it when the
  roster still holds one. If it draws no transcript and the task has **not** landed,
  nothing has been written for it yet and the page says
  `nothing on this page yet — it fills in as the task works`.
- **A background job** — anything `bash background:true` started, a watch, a video render —
  has none of that. There is no agent inside a job and nothing was ever journaled for it,
  so opening it opens a **page**, not a chat: the name, the handle `job 4`, the command,
  the clock or ending, and the end of its log — the last 200 lines, newest at the bottom,
  re-read four times a second while the job runs. A job whose log is empty or unreadable
  draws no tail and no error. `jobs output 4` prints the log too, and over `--host` it is
  the only way to read the file: a far job's log lives on the far machine and the page
  says `its log is on ` plus that machine's name.

A job's row is easy to tell apart before you open it: it sits in the `jobs` section under
`tasks` and `standing`, not among the families, and it has no dim under-line starting with
`job` and a number — that line is gone. The handle `job 4` is on the page.

Earlier versions of aforge got this wrong in a way worth naming, in case you remember it: a
job's page came up with a correct header — its name, `done`, its elapsed — over a body
holding nothing but `· no task 4 in this session` and the foot. That sentence was aforge
talking to itself, not about anything you did, and it is gone. A task that had not landed
yet went wrong the same way and for longer: a queued task, or one opened the instant it
started, drew a correct header over a screen with **nothing at all** on it, because the
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
(`task 7 done: <title> · transcript file:///…`), which is the same file the room
replays. Nothing here is a summary: a landed task's room is the whole transcript.

## A saved task asks me to accept a different task with the same number

Task numbers belong to their conversation. A reading view of another conversation's
finished task never shows or answers the current conversation's answers. Open its owning
conversation to make a decision there.

## I handed a review to the chat — why does it still need me?

**`awaiting review` is gone from every row, and there is no such state.** The card said
it, then the rail, the roster, the record page and this room's own header said it too —
four surfaces wearing a fourth word for one reading. A task handed to aforge is still a
task that says `your call`; what changes is **who is holding the question**, and the card
says that rather than renaming the state. The room's header now reads the reading's own
sentence, `your call · nobody could check it`, whoever is deciding it.

After you press `[d] let aforge decide this one` — or when `task.settle` is `auto` — the
card's reason row reads

```
nobody could check it · aforge is deciding · [t] take it back
```

so the card is never quiet and never chip-less without saying why. Pressing `t` hands the
question back to you and draws the chips again; **it resolves nothing**, and anything
aforge was going to say it may still say.

**And it comes back to you by itself.** A task never stays unowned past the end of a turn:
if aforge's turn ends with the question still unanswered, the decision moves back to you
and the card draws its chips, whether or not you noticed it had been handed over. Where
aforge's last message asked you something about that task, those chips are the answer
surface for that question — its words above, the chips below, one ask.

**A conflict is never handed over at all.** Whatever `task.settle` says, a landing whose
branch would not merge stays yours: aforge cannot merge by decree, and which of two
versions of your own file survives is yours to say.

If handing over fails, the original answers stay available. A running parent alone does not
hide an unanswered child decision.
