# Reading a task's page — scrolling, earlier tool calls, and a page that looks stuck

A task's page is the whole of what the task did, drawn with the conversation's own
blocks: its instruction, its prose, every tool call, and its report. This page is about
reading a long one — where the earlier calls went, how scrolling reaches them, and what
to do when the page seems empty or stuck at the top.

## Can't scroll in a task — the wheel and pgup do nothing

You can. The wheel, `pgup`/`pgdown`, and `↑`/`↓` over an empty box with no history to
recall all scroll a task's page. If the page is short — the task has said less than a
screen's worth — there is nothing to scroll and the page hangs from the top with the
slack under it, exactly as a young conversation does.

A page whose one turn made many tool calls used to be the case where the wheel really did
nothing: only the last three calls were on the page and the rest sat behind one fold line
with nowhere to scroll to. That is no longer how a task's page folds. The page keeps as
many of the newest calls as your window is tall, so it fills the frame — and scrolling up
at the very top opens the rest (next section).

## See earlier tool calls in a task — scroll up, or ctrl+o

When a turn on a task's page has made more calls than fit, the page shows a screenful
of the newest ones above a dim line reading:

```
↳ 87 earlier tool calls · scroll up or ctrl+o
```

Three ways open it, and they all do the same thing:

- **scroll up** when the page is already at its top — one wheel tick, `pgup`, or `↑`. The
  page opens the turn and keeps your place: the call that was under the fold line stays
  on the same screen line, the earlier calls appear above it, and the next tick up walks
  into them. Nothing jumps.
- **`ctrl+o`** with an empty box, which is the same key that opens the fold in the
  conversation. Press it again and the turn folds back to a screenful.
- **click** the fold line.

Scrolling back down to the newest line re-joins the live edge as usual — the page follows
the task again as it works — and the opened calls stay open. A task's page keeps its own
fold state, separate from the conversation's; `ctrl+o` inside a task never folds or
unfolds anything in the conversation you left behind.

## How many calls a task's page keeps on screen

As many as your window is tall, and never fewer than three. The number is taken from
the window at the moment the page is drawn, so resizing the terminal, growing the draft
by a line, or the task roster changing its width all re-fit it. Only the overflow folds.

The conversation is different on purpose: there the fold sits among the prose, and it
keeps exactly the last **3** calls of a turn above a line reading `N earlier tool calls ·
ctrl+o`. Scrolling the conversation never opens a fold — `ctrl+o` or a click does — because
in the conversation the fold is one block among many rather than the whole page.

## Task page is empty, looks stuck, or hangs at the top with a blank below it

A page that shows a few rows at the top and empty space beneath is a task that has not
said much yet: the space is the slack under a short page, the same as a new conversation
shows. It fills from the top as the task works, and the page follows the newest line
until you scroll up.

Three things that look like the same picture and are not:

- **A task that has finished** ends its page with `this task has finished — say it to main`
  — and, where it was spawned under another task,
  `this task has finished — say it to main, or open its parent, Ship the port`. There is
  nothing more coming; scroll up to read what it did, and the foot names where the words in
  your box can still go, because the box is still there and the worker is not.
- **A task that has not started** — one still queued behind the running ones — has a
  page with only its instruction on it, or with no blocks at all when nothing has been
  written for it yet. In that second case the page says
  `nothing on this page yet — it fills in as the task works`. The roster's row for it
  says `queued`; the page fills when it starts.
- **A row that was never a task.** A background job — a server, a build, a watch, a video
  render — sits in the `jobs` section under `tasks` and `standing`, not among the families,
  and its page is a card, not a chat, because a job has no agent and writes no transcript.
  What it shows is the name, the handle `job 4`, the command, the clock or ending, and the
  end of its log. A job that has not written its first line yet draws no tail and no error.
  Its row is the one under the `jobs` label.

No task page ever draws an empty body under its header. Whatever is true of the task,
the page says it in one dim line:

- a finished task whose transcript is gone from the disk keeps its report and says
  `this task's transcript is not here any more`
- a task that is queued, or one still working with nothing written for it yet, says
  `nothing on this page yet — it fills in as the task works`

A background job whose log is empty or unreadable draws no tail and no error — a job that
has written nothing yet is a job that started a second ago — and never the old sentence
`a background job keeps a log, not a transcript`, which is gone with the composer that
used to sit under the page.

The line comes off the moment there is anything to draw, because it answers one question
— why is there nothing here — and a page with something on it is not asking it.

If the page is long but the frame is short, scroll: the wheel over the page, `pgup`, or
`↑` over an empty box all move it. `ctrl+l` is not the key here — it returns the
conversation to its latest line, and inside a task scrolling down to the bottom does the
same for the task's page.

## Watch a background job's log — the page tails it live, where is the log

Press `enter` on the job's row in the `jobs` section, or click it. Opening a job opens a
**page**, not a chat: a full-frame card with the name, the handle `job 4`, the command, the
clock or ending, the log tail, and a foot. There is no composer on it at all.

The log is the last **200** lines of the file, oldest at the top and newest at the bottom,
dim, re-read four times a second for as long as the job is running. Colour codes and
control characters in the output are stripped before anything is drawn. The **absolute log
path is on this page**, in the foot — not on the row. `c` copies it; the confirmation
begins `copied `.

The page follows the newest line as it grows; `↑`/`↓` scroll, and scrolling to the bottom
re-joins the live end. When the job ends, the page takes **one more reading** — a process
writes its last lines and then exits, so the reading taken at the moment it exited would
be short of the ending.

The keys, quoted:

- running: `esc back · ↑↓ scroll · x stop it · c copy path · m puts it in your message`
- settled: `esc back · ↑↓ scroll · c copy path · m puts it in your message`

`x` stops a running job (the tasks page has the confirmation). `m` drops the name, the
handle, the ending, and the last few log lines into your message box underneath, then
closes the page. Over `--host` the body says `its log is on ` plus the host name, because
the file is on the engine's machine; `jobs output 4` is how you read it there.

A job that has written nothing yet draws no tail and no error. The old feet
`this log grows as the job works — say it to main` and
`a background job keeps a log, not a transcript` are gone: there is no composer to refuse.

## Task page says finished but the work is still running

It does not any more. If you are on an older build, this is what you were seeing: a job's
page drew `task finished — esc to return` (that was the old wording) under a header whose
clock was still counting up
— `video · working · 32s` over a foot claiming the work was over.

The cause was that a background job is not a task in aforge's own graph, so every attempt
to open a **room** for one was refused — which is the **ordinary** answer for a job, on a
perfectly healthy conversation — and that refusal was read as "the work has landed". A
job's page is a card now, not a room, and it draws the job's own clock or ending, so the
two cannot disagree.

What each foot means now:

- `esc back · ↑↓ scroll · x stop it · c copy path · m puts it in your message` — a
  background job that is still running. The lines above it are its log and they are still
  arriving. `x` stops it.
- `esc back · ↑↓ scroll · c copy path · m puts it in your message` — a background job that
  has ended. Nothing more is coming; scroll up to read what it wrote.
- `this task has finished — say it to main` — a **task** that is over, whatever kind it
  was. Nothing more is coming; scroll up to read what it did. Where the task has a parent
  the line offers that door too: `…, or open its parent, Ship the port`.

**A job's page has no composer, so none of those job feet name a door for typed words.**
`m` is how a job's ending reaches the conversation. A task's foot still names a door,
because the box is still there and the worker is not. `esc` is already on the legend under
the transcript (`room · esc/←← main`) and on the pinned header above a task's room, so a
task's foot spends its cells on the half nothing else on the screen is saying.
- **no foot at all** — an ordinary task still working. There is nothing to say at the bottom
  of the page, because the next thing to arrive is what happens next.

The header is the other half of the answer and it has always been true: `queued`,
`working`, `checking what it left`, `closing gaps · round 1 of 2`, `finishing`, `waiting`,
`stopping`, `done`. If the header says the work is running, it is running.
