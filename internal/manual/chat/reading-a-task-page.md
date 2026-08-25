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

## Task page is empty, or stuck at the top with a blank below it

A page that shows a few rows at the top and empty space beneath is a task that has not
said much yet: the space is the slack under a short page, the same as a new conversation
shows. It fills from the top as the task works, and the page follows the newest line
until you scroll up.

Three things that look like the same picture and are not:

- **A task that has finished** ends its page with `task finished — esc to return`. There
  is nothing more coming; scroll up to read what it did.
- **A task that has not started** — one still queued behind the running ones — has a
  page with only its instruction on it. The roster's row for it says `queued`; the page
  fills when it starts.
- **A row that was never a task.** A background job — a server, a build, a watch, a video
  render — sits on the same column, and its page has no chat in it because a job has no
  agent and writes no transcript. It shows the job's log line and
  `a background job keeps a log, not a transcript`. Its row is the one whose dim
  under-line starts with `job` and a number.

No task page ever draws an empty body under its header. If a finished task's transcript
is gone from the disk, the page still carries the task's report and says
`this task's transcript is not here any more`.

If the page is long but the frame is short, scroll: the wheel over the page, `pgup`, or
`↑` over an empty box all move it. `ctrl+l` is not the key here — it returns the
conversation to its latest line, and inside a task scrolling down to the bottom does the
same for the task's page.
