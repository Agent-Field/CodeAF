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

- **A task that has finished** ends its page with `task finished — esc to return`. There
  is nothing more coming; scroll up to read what it did.
- **A task that has not started** — one still queued behind the running ones — has a
  page with only its instruction on it, or with no blocks at all when nothing has been
  written for it yet. In that second case the page says
  `nothing on this page yet — it fills in as the task works`. The roster's row for it
  says `queued`; the page fills when it starts.
- **A row that was never a task.** A background job — a server, a build, a watch, a video
  render — sits on the same column, and its page has no chat in it because a job has no
  agent and writes no transcript. What it shows instead is the job's log: the
  `job 4 · log /path/…` line at the top, and under it the end of that file, re-read four
  times a second while the job runs. A job that has not written its first line yet says
  `a background job keeps a log, not a transcript` and nothing else. Its row is the one
  whose dim under-line starts with `job` and a number.

No task page ever draws an empty body. Whatever is true of the task, the page says it in
one dim line:

- a finished task whose transcript is gone from the disk keeps its report and says
  `this task's transcript is not here any more`
- a task that is queued, or one still working with nothing written for it yet, says
  `nothing on this page yet — it fills in as the task works`
- a background job whose log is empty or unreadable says
  `a background job keeps a log, not a transcript` — never an error, because a job that has
  written nothing yet is a job that started a second ago

The line comes off the moment there is anything to draw, because it answers one question
— why is there nothing here — and a page with something on it is not asking it.

If the page is long but the frame is short, scroll: the wheel over the page, `pgup`, or
`↑` over an empty box all move it. `ctrl+l` is not the key here — it returns the
conversation to its latest line, and inside a task scrolling down to the bottom does the
same for the task's page.

## Watch a background job's log — the page tails it live

Press `enter` on the job's row, or click it. A background job's page **is** its log:

```
job 4 · log /Users/you/.aforge/v3/jobs/4.log

  [1/3] fetching sources
  [2/3] building
  frame 640 · 22 fps
this log grows as the job works — esc to return
```

The top line is the path — the same string the roster draws under the row, and the handle
`jobs output 4` and `jobs kill 4` take. Under it is the **end** of that file: the last 200
lines, oldest at the top and newest at the bottom, dim, re-read four times a second for as
long as the job is running. Colour codes and control characters in the output are stripped
before anything is drawn.

The page follows the newest line as it grows; scroll up with the wheel, `pgup` or `↑` over
an empty box to read back, and scrolling to the bottom re-joins the live end.

When the job ends, the page takes **one more reading** — a process writes its last lines
and then exits, so the reading taken at the moment it exited would be short of the ending —
and the foot becomes `task finished — esc to return`.

Two limits worth knowing. `enter` inside a job's page does not steer anything: there is no
agent in a job to read a line, so the words raise the same question a finished task raises
and offer to send them to the main conversation instead. And over `--host` the page draws
no log at all — the path belongs to the other machine, so the page shows the far path and
`a background job keeps a log, not a transcript`; `jobs output 4` is how you read it there.

## Task page says finished but the work is still running

It does not any more. If you are on an older build, this is what you were seeing: a job's
page drew `task finished — esc to return` while the clock at the top of the frame was still
counting up — `video · working · 32s` over a foot claiming the work was over.

The cause was that a background job is a row and never a task in aforge's own graph, so
every attempt to open a page for one is refused — which is the **ordinary** answer for a
job, on a perfectly healthy conversation — and that refusal was read as "the work has
landed". Now the page asks the row instead, which is the same record the top bar above it
is drawn from, and the two cannot disagree.

What each foot means now:

- `this log grows as the job works — esc to return` — a background job that is still
  running. The lines above it are its log and they are still arriving.
- `task finished — esc to return` — the work is over, whatever kind it was. Nothing more is
  coming; scroll up to read what it did.
- **no foot at all** — an ordinary task still working. There is nothing to say at the bottom
  of the page, because the next thing to arrive is what happens next.

**The top bar is the other half of the answer**, and it has always been true. The room's
own pinned header is gone; the state word now rides in the bar's crumb, after the task's
title: `queued`, `working`, `checking what it left`, `closing gaps · round 1 of 2`,
`finishing`, `waiting`, `stopping`, `done`. If the bar says the work is running, it is
running.
