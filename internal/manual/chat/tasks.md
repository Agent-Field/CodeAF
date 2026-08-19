# Work that runs on its own

## What a task is

A task is one self-contained piece of work handed off to run on its own while the
conversation carries on. It works in its own copy of the repository and reports back when
it lands. It never sees the conversation: what it reads is one written brief — your own
message, word for word, then the work, what to produce and what done means. How that is
assembled is on the *how tasks run* page, under *What the task actually reads*.

You can ask for the work in words, and the model grooms it and calls its `propose_task`
tool. You then get a card asking whether the work should go. The
model's window onto work that is running or already landed is its `tasks` tool; that is
also the door it uses to steer a task or to settle one, when you say so in conversation.

You can also start one directly with `/task <brief>`. That form asks a small sizing judge
“can this parallelize?” and only surfaces the judge when its answer is yes. A yes opens a
two-row chooser with `adaptive` recommended and `single` below it; the adaptive row shows
the proposed parts and planner model. Arrow keys move and enter starts the chosen shape.
Esc means “just do it” and starts single rather than cancelling the work. A no, timeout,
or unreadable answer starts single in silence after `sizing it up…` disappears. Whether
you are asked at all is the `starting a task` setting, below.

`/task solo <brief>` skips the judge and starts single. `/task adaptive <brief>` also skips
the judge and starts the planner run.

**Every `/task` has its brief shaped before the work starts.** Your words are kept word for
word and a fuller brief is written around them — the constraints this kind of work needs,
what was decided on your behalf, and what done means. It is the next section.

Every task carries a title, a short summary, the brief, and a done-condition — the command
that must pass, the behaviour that must hold, the output that must appear. The brief and
the done-condition are frozen the moment the work is admitted. Steering can add a missing
fact or correct a step, but it cannot change what the work is for. If the objective itself
was wrong, the answer is to propose the work again.

You can keep working while a task runs. aforge tells you not to wait for it: its report
arrives in the conversation when it lands.

**One other thing on the roster is a task, and it is not work in a worktree.** A sub-harness
being designed runs as a task too — same row, same room, same `x` — with its own phases
(`designing`, then `awaiting your look`) in place of the states below, and no branch, no
changed files and no merge, because it writes none. It is admitted without a countdown,
because the question about a design is the card at the end of it. The page on saved shapes
of work has it in full.

A task can also break its own brief into smaller tasks when it finds independent parts in
it, and those are drawn as a family under it — see *When a task splits its own work*.

## Why my task's brief is longer than what I typed — the brief is shaped

A task you start with `/task` does not go out as the sentence you typed. Between the
command and the work, one model call reads your words and writes the brief the worker is
actually given: your request quoted word for word, then the things a worker alone with the
job needs settled — what kind of work this is, who the output is for and what makes it good
to them, the ways this particular kind of work goes wrong and the conditions that forbid
them, anything ambiguous decided one way with the assumption stated. It also writes a
separate done-condition that somebody other than the worker could check.

So the brief in the task's room really is longer than what you typed, and **the room is
showing you the truth** — that is the brief the worker read. Nothing shorter was sent and
nothing was kept back.

`shaping the brief…` is the note on screen while that call runs. It waits up to 25 seconds.

**If shaping cannot run, your words go as-is.** No model resolved for it, a timeout, an
answer that was not readable — the task starts with exactly your sentence and the plain
done-condition `Complete the brief and report the result and checks run.`, which is what
`/task` did before shaping existed. It is never a reason for your task to be refused, held
up, or lost.

The call is billed the way aforge's other calls-you-did-not-type are: to the session, not to
a turn. It runs on the `shaper` role, which follows the careful-work model.

## Does aforge change my task, or rewrite what I asked for?

No. The shaping pass adds around your words; it never replaces them.

Your sentence is carried separately from anything a model wrote, under the heading
`WHAT THE PERSON ASKED FOR, IN THEIR OWN WORDS`, followed by the line *“This is the message
this work came out of. Where anything below reads differently from it, their words are what
was asked for.”* That is a rule the worker reads: where the shaped brief and your sentence
disagree, yours wins. So a shaper that overreached is overruled by the document itself.

Two more things stay yours. **The title on the roster is made from the words you typed**, not
from the shaped brief — the row reads as the thing you asked for. And the short summary
beside it is the first line of what you typed.

What shaping is allowed to do is settle what you left open — which file, which format, how
long, which of two readings — and it must say in the brief that it decided, so you can see
it in the room. What it is told not to do is invent scope you did not ask for.

The same guidance reaches briefs the conversation model writes with `propose_task`, but as
part of that tool rather than as a second call: it already has the whole conversation, so
nothing needs to be re-read for it.

## Stopping the adaptive-or-single question — making adaptive or single the default

The chooser that asks “this parallelizes — how should it run?” can be answered once and for
all. `/settings` → Session → **starting a task**, or the `task.start` row:

- **ask** — the default, and today's behaviour: the sizing call runs, and the two-row
  chooser opens only when it finds parts that could run at the same time.
- **adaptive** — never asks. Parts found, the adaptive run starts straight away; nothing to
  split, one worker starts, because a planner over work with no independent parts in it is
  an extra model deciding nothing.
- **single** — never asks and never goes adaptive. The sizing call is not made at all, since
  its only purpose was the question you have already answered.

`/task solo <brief>` and `/task adaptive <brief>` always mean what they say, whatever the row
is set to.

**Choosing `single` closes nothing off.** A single worker can still break its own brief into
smaller tasks when it finds genuinely independent parts in it — see *When a task splits its
own work* — so the setting decides who plans, not whether work can ever run in pieces.

## The card that asks whether to run the work

While the model is still writing the proposal, a grey block opens in the transcript and
grows: a pulsing `◌`, the title (or just the word `task` until the title arrives), and one
row reading `forming…`. It is not a question yet — no options and no clock. If the turn
ends before the proposal finishes arriving, the block settles as
`cancelled · the proposal never arrived`.

When the proposal is complete, that same block turns into the question. The card shows:

- a head with the task's own identity mark and a two-or-three-word name;
- one dim sentence under it — the first sentence of the summary, capped at 90 cells, and
  left out entirely when it would only repeat the name;
- a row of model chips, but only when more than one model matched what was asked for;
- the three options `yes`, `redirect`, `no`;
- the countdown meter;
- a dim meta line reading `model <full id> · ctrl+e for the brief`. The model id leads
  because it is the one fact nothing else on screen will say again; on a narrow frame the
  hint is dropped and the model kept.

The card is **not modal**. Unlike the permission question, it leaves the input box live —
the box becomes the redirect lane, with the placeholder
`redirect this task… (enter sends it, esc declines)`.

Only one proposal is a live question at a time. If a second one arrives while the first is
unanswered, the older card settles as `expired · the turn ended`, because a question that
can no longer be answered must stop looking like one.

## Why aforge offered to run something as a task after answering me

There is a second, smaller card, and it is not the proposal above. After a turn that
answered a substantial message in **words alone** — no tool call — a cheap model reads what
you asked and the first two lines of the reply and decides whether that should have been
work. When it says yes, one row appears above the message box:

```
? run harness "task"? · one self-contained sweep · [enter] run · [esc] no
```

The name in quotes is the shape it is offering: `task` for one self-contained job,
`adaptive run` for a many-part goal. It says `run harness` because it is the harness
offer's row, reused; no saved harness is involved. `enter` or `y` admits the work straight
away — from a self-contained goal that model wrote, with no second countdown, because the
card **is** the consent. `esc` or `n` drops it and nothing happened.

It never starts anything by itself, it is asked at most once every three turns (so two
cards can never arrive in a row), and it is silent on short messages, on turns that called
tools, and wherever there is no screen to answer it. A task admitted this way behaves like
every other task on this page from that moment on: a row on the roster, a room, a report.

Nodes cut by an adaptive run also appear as rows under the run's own row — see *Adaptive
runs*, which explains what those rows can and cannot do.

## Every key the proposal card takes

| key | when | what it does |
| --- | --- | --- |
| `enter` | always | answers the focused option |
| `esc` | always | outright **no** — declines |
| `ctrl+e` | box empty | opens or closes the brief |
| `←` `→` | box empty, picker closed | move the focus between the three options |
| `y` | box empty, redirect not asked for | approve |
| `r` | same | ask for the redirect lane |
| `n` | same | decline |
| `1`–`4` | same | pick that model from the models row |

The card opens with `yes` focused, because that is what the block is proposing and what the
clock will do. `←`/`→` clamp at the ends and never wrap. You can also click any chip.

The letters and digits are given straight back the moment there is a sentence in the box,
or the moment the redirect lane has been asked for. "yes, but keep the tests" starts with a
`y`, and a surface that read that as approval would approve the thing you were in the
middle of correcting. `←`/`→` still work in the redirect lane, because there is no caret to
move in an empty box.

While the card is up, the legend hint reads `y yes · r redirect · n no`. A question the
session is blocked on outranks the roster, any open room, every overlay and the draft.

Expanding the brief: `ctrl+e` with an empty box, or `ctrl+o` on a card you selected with
`↑`/`↓`. It shows the whole summary, then the whole brief, then `done when: <acceptance>`
on its own labelled line. Clicking the card body does not open the brief — it opens the
task's room.

## The countdown on the proposal card

The meter is a draining bar and a number, recomputed every frame:
`████████░░░░  auto-starts in 3.2s`. The bar is at most 20 cells. Under ten seconds the
number is spelled in tenths (`3.2s`); above it, `47s` or `2m 13s`, always rounded up, so
the last second you have is drawn as a second.

**The clock runs toward yes.** Silence approves the work as briefed, with no redirect
appended, and the card settles as `approved · the clock`. This is the opposite of the
permission card's countdown, which runs toward denying. A task proposal is not a permission
gate — it is your window to redirect the work or wave it off before it starts.

The default window is 5 seconds. It is the setting `task.autoapprove_seconds`, under
`task.` in the "spending" category of the settings panel (`ctrl+,` or `/settings`).

**This is one of the rows aforge will not change for you.** It decides how long you get
before work starts on its own, so `change_setting` refuses it and points you back at
`/settings`. Same for `task.parallel` below, and for the whole approval and spending
family — the permissions page lists them.

Set that window to 0 and there is no clock at all: no bar is drawn and the row reads
`waiting on you`. The card then waits until you answer it, however long that takes.

## What yes, redirect and no each do

**yes** admits the work exactly as briefed.

**redirect** with an empty box does not answer — it takes the focus and waits for your
words. The `enter` after it carries the sentence. Your words travel verbatim and are
appended to the brief; this is the last moment the brief may change. With something already
typed, `yes` and `redirect` converge: a correction in the box is a correction whichever one
you reached for. The box is cleared on any answer, so your next `enter` does not send the
correction to the model as a message.

**no** (or `esc`) declines. Nothing is spawned, no row appears on the roster, and no room
exists. This is a normal answer, not an error.

Once answered, the card collapses to its head and one foot line that keeps both halves —
what you reached for and what it came to, joined by ` · `:

| what you did | the foot line |
| --- | --- |
| approved | `yes · approved` |
| approved with words in the box | `redirect · approved · you redirected it` |
| declined | `no · declined` |
| let the clock run out | `approved · the clock` |
| the turn ended under the question | `expired · the turn ended` |

When the card offered a choice of model, the model you picked is written on the end of that
line — it is the only place your own pick is recorded.

**Honest limit:** once a card has been answered, its brief is no longer reachable from the
card. `ctrl+e` and `ctrl+o` on a settled card do nothing you can see. The whole of a task's
life is in its room instead.

## The states a task passes through

These are the exact words on screen.

| what is happening | the word you see |
| --- | --- |
| the proposal is still arriving | `forming…` |
| the proposal is waiting, with no clock | `waiting on you` |
| the proposal is waiting, with a clock | `auto-starts in <time>` |
| queued behind something | `waiting · <reason>` |
| queued behind named work | `waits: <title of the work it needs>` |
| running | a turning spinner, and what it is doing this second |
| running and closing a gap | `finishing · <what it is closing>` |
| stopped by you | `stopped`, with `⊘` on the roster in place of the failure cross |
| stopped before it ever ran | `stopped before it started` |
| stopped | `stopped` |
| stopped, with work on a branch | `stopped — branch kept` |
| landed clean | `done` |
| landed short | `failed` |
| landed, but nobody could judge it | `needs your look` |
| …the same thing on the roster | `finished — look it over` |
| …the same thing when there was no report | `finished, but needs your look` |
| …its branch, on the card | `branch kept` |

`finishing` is not a separate state — the work is still running, and the sentence after the
word names the gap it is tying off.

The three reasons a queued task gives for waiting are `slot`, `machine busy` and
`rate limited`. A named prerequisite outranks any of them, because a name is something you
can act on and a queue clears itself.

How a branch came home is spelled `merged`, `conflicted`, or `inplace` (the work ran
directly in your own tree because there was no repository to branch from).

## The three ways work lands

Every landing writes a card into the conversation, with a blank row on each side, and moves
the task's row on the roster.

```
✓ ◆ Fix nil-map crash · done 4m12s · 3 files
  "the guard is in and the regression test passes" · spawned 14:02 · ctrl+o output
```

The head is what happened. The muted line under it is what came of it, in the task's own
first sentence, quoted because they are its words and not aforge's.

- **`done`** — a tick, muted. It is settled work on the roster.
- **`failed`** — a cross, in the bad hue, drawn with the word `failed`. It is settled too,
  not work that needs you: by the time you see the word it is news, not a decision.
- **`needs your look`** — a `?` in the warn hue. Its family rises above running work on
  the roster. The `?` is deliberately neither a tick nor a cross: it claims neither a
  finding nor a judgement nobody made.

After the name the card carries the span, the file count, and how the branch came home:
`merged`, `inplace`, `conflicted · <branch>`, `stopped — branch kept · <branch>`, or
`branch kept · <branch>`.

Click anywhere on the card, or press `ctrl+o` with it selected, to expand it: `changed`,
`worktree`, `model`, `cost`, `ran`, `done when`, the report, then the brief. `enter` on the
selected card opens the task's room instead. Each long field caps at 20 rows.

More than two landings in a row become one rollup — `✓ 3 tasks done · 9m14s` with a compact
row per task under it. Any failure in the batch swaps the header to `✗ N tasks landed`; any
`needs your look` swaps it to `? N tasks landed`. The header's span is wall-clock, first
spawn to last landing, not the sum of the parts, because tasks run at the same time.

## Watching work: the strip along the top

The strip is one row under the pinned header — a tab bar of doors into live work:

```
⠙ Fix nil-map · ◆ Auth tests · +2
```

It appears only while something is running, and goes away the moment nothing is. It needs a
frame at least 24 columns wide and 6 rows tall. It is the narrow-frame door: wherever the
roster stands — as the right column or open over the whole frame — the strip stands down.
A column you closed with `ctrl+g` is a roster standing down, so the strip comes back and
running work stays reachable.

Order: running first, then work that needs you, then idle. Parked and finished work never
appear on it — the strip is the live set, the roster is the history.

A chip carries one glyph and the name cut to 18 cells, and nothing else: no clock, no
spend, no tool name, no tree connector, no cursor mark, and no stop button. The room you
are standing in takes a colour band. The strip is one flat row even when a task has
children; the roster is where the family tree is drawn.

The strip is pointer-only and adds no keys or cursor of its own.

- Click a chip to walk into that task's room. Click the chip of the room you are already in
  to close it.
- Click the `+N` overflow mark to open the whole roster.
- A press anywhere on the row belongs to the row, even in the gaps, so a miss never falls
  through to the transcript.

On a frame too narrow for one whole chip plus its `+N`, the first chip is drawn cut and the
`+N` is dropped: a count of things you cannot identify is worth less than one name.

## The roster: the column of all the work

The roster is a column on the right holding every task this session has admitted, not just
the live ones. It is the session's record of its own work. Work finishing never puts it
away. Two things do: `/new`, which takes the tasks with it, and `ctrl+g`, which closes the
column and leaves the work exactly where it was. The bottom line of the column says so.

It appears as soon as one task exists, at a frame width of 100 columns or more — 30 columns
wide from 120 up, a slim 24 columns from 100 to 119. Under 100 columns there is no column,
and `ctrl+t` opens the same roster over the body instead.

The roster is a forest. Each root task is followed by its whole family, with children
joined by three-cell connectors (`├─ `, `└─ `, `│  `). Families are ordered by their most
urgent member: needs you, running, idle, parked, then done. There are no state-group
headings. The footer keeps those totals as counts, such as
`2 need you · 3 running · 12 done`.

Folding belongs to each node. Families with a running, needs-you, or idle member start
open. Settled families and families containing only parked work start folded to their
root; the root then carries the family's aggregate state glyph and a `▸ +N` badge for the
hidden descendants.

A task's row opens with two glyphs answering two questions: its state, which changes, and
its own identity mark, which never does. Then the name, then its id as `#7`, dim, at the far
end — and the id stands down when the name would be left under 12 cells. Under the row, at
most two more: what it is doing, what is holding it, what it waits on, or how its branch
came home. `conflicted · task/fix-nil` in the bad hue is the one loud row on the column.

There is no subtitle here. The column is a presence list; the proposal card and the landing
card both carry the sentence.

At the bottom, up to three dim lines: `Σ $1.42 · 312k tok`, `1 need you · 3 running`,
`148 parked · 12 done`. The `Σ` is the whole session's spend — it already contains every
task in the column plus the conversation, so there is deliberately no per-task share. Zero
figures are left out entirely, because zero means "nobody published a price", never "free".

Under those, always, one more dim line: `ctrl+g — hide`. It is the column's own door, and
it is a button as well as a key — click that line and the column goes away.

## Hiding the task column: closing the right sidebar, panel or task bar

`ctrl+g` closes the column of tasks on the right and gives its 30 columns back to the
conversation. Press it again and the column comes back with the current state of the
work in it, including anything that started or finished while it was gone — nothing here
is a snapshot; the column is redrawn from the tasks every frame. The `ctrl+g — hide` line
at the bottom of the column is the same door for the pointer: click it and it closes.

The choice is remembered. It is written to your profile the moment the column moves, as
the `ui.task_column` setting, which also appears in the settings panel (`ctrl+,`) on the
Display tab as **task column**. A change made in the panel lands the next time aforge
starts; `ctrl+g` acts immediately and wins for this session.

With the column closed, work is still visible:

- Anything **running** draws the task strip along the top — `⠙ Fix nil-map · ◆ Auth tests
  · +2` — because the strip stands up wherever the roster stands down. Click a chip for
  that task's room, or the `+N` for the whole roster.
- The legend above the message box carries `ctrl+g tasks` in its hint slot for as long as
  this session has any tasks at all, running or not. A session that has run nothing says
  nothing there — there is no column to miss.
- `ctrl+t` still works: asking for the roster brings the column back and gives it the
  keyboard in one press.

`ctrl+g` does nothing, and is not swallowed, when there is no roster on the frame to
close: no tasks at all, or a frame under 100 columns where nothing has raised the
roster over the body.

## Using the roster from the keyboard

`ctrl+t` hands the keyboard to the roster. It is asked for, never taken: the draft is the
rest state, so a person who starts typing is typing, not navigating.

| key | what it does |
| --- | --- |
| `↑` `↓` | walk the visible tree |
| `→` | open a folded family, or step to the first child |
| `←` | fold an open family, or jump to the parent row |
| `enter` | open the task's room |
| `w` | toggle the wider 46-column tree |
| `esc` or `ctrl+t` | give the keyboard back |
| `ctrl+g` | close the column altogether, or bring it back — this one works whether or not the roster holds the keyboard |

The legend hint while it holds the keyboard is `↑↓ move · →← fold · enter open · esc`.
When depth has forced a title to be cut, the footer adds `w · click seam — widen` (or
`w · click seam — narrow` once it is wide); that hint is clickable as well as available
from the keyboard, and so is the `ctrl+g — hide` line under it.

Every other key is given back. The roster cannot take the keyboard while the exit
confirmation, a permission question, a task proposal, or any overlay is up, and with no
tasks at all `ctrl+t` falls through rather than being swallowed.

The cursor follows the task, not the row, when families reorder or fold around it.

With the pointer, a row takes the hover background step. On a family root, only hovering
the glyph cell reveals its disclosure triangle (`▾` open, `▸` folded). Click that glyph
cell or the root's `▸ +N` badge to toggle the family; click its title to open the room.
A click that hits no task still belongs to the column and does nothing. A click moves the
cursor but does not hand the roster the keyboard.

## Walking into a task's room

A task is a place you can go. Opening its room makes the body stop being the conversation
and become that task's own transcript — its history off disk, then its present, live — and
the input box stops talking to the model and starts talking to the task.

A room is a view, not a second app. Nothing under it stops: the conversation keeps
streaming, the roster keeps ticking, landing cards keep landing. The conversation's scroll
is never touched, which is why leaving restores it exactly.

Ways in:

- click a roster row, a strip chip, a proposal card, or a landed card;
- click an inline reference in the model's prose — `task 7`, `task #7`, `tasks id 7`,
  `task id #7` become underlined links when the id names a task this session has seen;
- `enter` on a proposal card or a landed card selected with `↑`/`↓` over an empty box;
- `→` over an empty box walks into the next running task's room, wrapping at the end;
- `enter` on a row while the roster holds the keyboard.

Pressing the same door again is always the way back out.

Ways out: `esc` leaves and restores the conversation's scroll exactly. `←` over an empty box
steps back one level. `←` twice within 600 ms goes home — out of everything, at the live
edge, nothing selected. `/new` closes any open room, because a task dies with its session.

What refuses to open: a proposal whose task has had no update yet (the id is real, but a
room on it would be an empty page with nothing coming), and a queued task by way of `→`
(it has no worker yet, so there is nothing to talk to). An agent with no rooms at all
writes one note in the conversation: `room unavailable — this session has no task rooms`.

## What is different inside a room

| | the main thread | inside a room |
| --- | --- | --- |
| what the body draws | the conversation | that task's transcript |
| what `enter` does | sends to the model, or holds the message above the box while a turn is running | **steers the task** — never held |
| box placeholder | the draft prompt | `Steer <title>… (esc: main)` |
| pinned top line | none | the focus header, and the family lines under it |
| legend word | the workspace path and branch | `room · esc/←← main` |
| the model on the status row | the conversation's model | `task <the task's model>` |
| clicking that model | opens the model picker | inert — the picker moves the conversation |
| `ctrl+b` | freezes the transcript | freezes the room's own rows |
| scroll position | the conversation's | the room's own, kept separately |
| attachments | the tray sends pictures | a room's box sends words only |
| proposals | drawn as cards | never — a task's own pieces start without asking you |

The focus header is an accent line pinned at the top:
`─ ⠙ main ▸ Fix the nil-map crash · running · 2m12s · $0.04 ──── esc/←← main ─`. It carries
the state glyph, a trail that always names `main` as the root, then the state word, the
clock, the spend and the model — each dropped when nobody published it. It is pinned
because a fact that scrolls away is only true at the top of the page.

Under it, dim and indented, come up to three more pinned lines saying where this task sits
in its family — see *Who started this task, and what it handed out*.

## Reading a task's room, and its frozen clock

Inside a task's room the page is built from the same blocks the conversation is made of, so
a tool call expands to its diff or output, a reply renders as markdown, and anything you
steered wears your own hue. History comes off the task's journal, capped at the last 120
blocks; a missing or unreadable journal is not an error — the room opens on the live edge
instead. When the task has landed, a foot line reads `task finished — esc to return`.

`↑`/`↓` over an empty box scroll a line, `pgup`/`pgdown` a page, and reaching the bottom
re-sticks to the live edge. `ctrl+b` freezes the room's rows for copying — one known
wrinkle: leaving copy mode rejoins the conversation's live edge, so freezing a room while
the conversation was scrolled up loses that scroll.

The task's elapsed clock freezes while you stand in its room. That number exists to ask
whether you should go and look; being there is the answer. Nothing is stopped, only
unreported, and it thaws at the value it would have had when you leave.

## Seeing the whole conversation inside a task — a room never folds its work away

**A room shows everything the task said and did, and it stays shown.** Out in the main
thread a finished turn's machinery collapses into one chip —
`▸ worked 47s · thought 6s · 6 tool calls · ctrl+e` — so the page reads back as the question
you asked and the answer you got. **That never happens inside a room.** A task's whole life
is one long stretch of work ending in a report, so a chip there would hide the entire page
and leave you the report you already had. There is no `▸ worked` line in a room, nothing to
click open, and the `ui.work` setting does not reach one.

So a room you walk into — a running task, a task that has landed, a piece of a recursive
task, a harness being designed — reads top to bottom as the discussion it was: the
instruction it was given, its prose between calls, its thinking blocks, every tool call with
its arguments and result, anything you steered into it, and the report at the end. A landed
task's room is the whole transcript, not the summary.

Two bounded things do still hold something back, and both name themselves and open:

- a **thinking block** shows three lines until you press `ctrl+e` or click it —
  `⠿ thought for 6s · 148 tok · ctrl+e`;
- a run of **more than three tool calls in a row** shows the last three above a line reading
  `9 earlier tool calls · ctrl+o`; `ctrl+o`, or a click on that line, unfolds the run.

Inside a room `ctrl+e` over an empty box opens the thinking block and nothing else, because
there is no work chip for it to mean instead.

## Who started this task, and what it handed out

Standing inside a task, the pinned lines under the focus header say where it sits in the
family — who asked for the work, what the work handed out, and what it is still behind.
They are dim, indented two cells under the trail, and each one is simply absent when there
is nothing to say:

```
─ ⠙ main ▸ Write the tree · working · 2m 12s ────── esc/← main · ✕ ─
  part of: Ship the port
  spawned: Cut the goldens — queued · Wire the seam — running
```

- **`part of: <title>`** names the task that handed this work out — the parent. A task
  nobody spawned draws no such line, so its absence means *this is a top-level task*. A
  parent this session has had no update for is left unsaid rather than named as a bare id.
- **`spawned: <title> — <state>`**, one entry per piece, separated by ` · `, in the order
  the session met them. The state is the same word the roster uses: `queued`, `working`,
  `finishing`, `waiting`, `done`, `failed`, `stopped`, `needs your look`. A piece that is
  itself queued behind another piece says only `queued` here; open its own room to see what
  it is behind.
- **what this task waits on** is on the accent line itself, as its state word:
  `waits: <title>` names the prerequisites that have not finished. It is there rather than
  on a line of its own so the header never says the same thing twice.

Nothing new is being tracked for these lines — they are the roster's own tree, read from
the one node you are standing in, said in words because the tree shape is not on screen
here.

Limits, so you know when the page is not telling you everything: at most **three** lines,
wrapped on their spaces and cut there, because they are charged to the transcript
underneath them. They stand down entirely on a terminal shorter than **16 rows** or
narrower than **12 columns**, where the header itself is already fighting for room. An
adaptive run's page draws none of them: the graph with its edges is already on screen
there.

## Opening a task in the middle of its work — what the room shows

A room opens **at the bottom, on the newest thing**, never at the top: it is showing you
where the work is now, not replaying it from the start. Leaving with `esc` and coming back,
or walking from one task to another and back, lands on the same place — each room reads its
own journal fresh and each keeps its own scroll.

Two lanes fill the page and they meet at one instant. The **journal** is every message the
task has finished writing. The **live stream** is what happens from the moment you walk in;
none of the task's history is re-narrated onto it, because a stream that replayed half an
hour of somebody else's greps before reaching the present would make walking into a task
mean reading it slowly.

Between those two sits the step the task is **in the middle of**, and you are handed that
once, on the way in: the reasoning it is spilling right now, the reply it has written so
far, and any call it has finished asking for and not yet started. Live continues from
there. So a task caught mid-sentence shows the sentence, rather than the last thing that
finished and then nothing until the next word lands.

A call the task is **still running** is drawn as running — an unfinished row with no
duration on it, because nobody has measured one yet — and the row settles in place when the
call comes back. It carries no clock: the room learns of that call from the file, which
does not say when it started. If the task ends while a call is still open, the row stops
animating and keeps the dim mark for something nothing more is coming for; it is not
marked failed, because nobody watched what became of it.

## Mentioning a task in the conversation

Type `@` in the draft and a list drops up with task rows above the file rows. The sections
are `running`, `recent` (ended inside 24 hours) and `older`, in that order; the `older`
heading carries `older · N more` when the list was cut. At most 8 task rows are drawn,
though the search itself goes 40 deep so a match three sections down is still counted. A
running task is the top row whatever it scored — ranking decides order inside a section,
not between them.

A row reads `▸ ⧉ Sweep the deprecated call sites            4m`: a state glyph, the mention
mark, the title, and the age on the right — how long a live task has been going, or how
long ago a landed one landed. `↑`/`↓` move, `enter` takes the row, `esc` closes, and typing
keeps filtering.

Choosing a row types `@<slug>` — the title, lowercased and kebab-cased — and nothing else.
It is derived from the title, so it is a name you can type from memory without ever opening
the list.

## What mentioning a task sends

At `enter`, every `@<slug>` in your message that names a task grows a pointer block after
your sentence.
Your token stays exactly where you typed it; the block is the footnote under it:

```
[Task reference: Fix the nil-map crash — id 7 · done · ended 3h ago
 Outcome: "Added the guard and the regression test; the parser suite passes."
 Output: git:task/fix-the-nil-map-crash-9c1a2f · Transcript: file:///…/7.jsonl]
```

A running task instead carries its age, a `Live:` clause saying what it is doing, and a
`Steer:` clause. Any clause with nothing behind it is dropped rather than written empty.

The expansion happens before the message is sent and before it lands in the transcript, so
what you see on screen is exactly what went on the wire. It never inlines the work: what
travels is six facts and two addresses, and the model follows either address if it needs
more.

Unknown tokens are left alone in silence — `@santosh` is a person, `@internal/x.go` is a
path. A task mentioned twice gets one block. A slug pasted whole and submitted in the same
beat resolves against what is already in memory, so it may stay the plain word you typed.

## When a task splits its own work — sub-tasks, nested tasks, children

A task can hand pieces of its own work further out. If its brief turns out to hold two or
three parts that do not need each other — different files, different subsystems, nothing
half-finished passing between them — it proposes each part as a task of its own and keeps
the coordination for itself. The parts run at the same time instead of one after another.

Nothing asks you about those. **A sub-task starts without a card:** the countdown card is
how a person redirects work, and there is nobody inside a worktree to show one to, so a
task's own proposals begin the moment they are made. What you see instead is the tree.

Where they show up:

- **the strip along the top** keeps one flat row of live chips on narrow frames; it does
  not draw the family tree.
- **the roster** draws the whole family together, with each piece joined to its parent by
  tree connectors and carrying its own id and state.
- **the parent's room** shows the `propose_task` calls as they are made, and the parent's
  own words when the reports come back — and its pinned header lists each piece by name
  with the state it is in (*Who started this task, and what it handed out*).
- **the piece's own room** says `part of: <the parent's title>` under its header, so a task
  you walked into knows it is a piece of something.

Each piece works in a copy of the repository taken from its **parent's** copy, and its
branch merges back into the parent's — so a family's work comes home as the parent's work,
in one merge, not as three branches racing for yours.

A parent never lands while a piece of it is still running. Its own turn may end long
before; the task stays open, each report is put in front of it as it arrives, and only then
is the parent's work checked and merged. If you stop a parent, its unfinished pieces are
stopped with it and their branches are kept.

## How deep tasks nest, and how many pieces one task may hand out

Two hard bounds, and they behave differently on purpose.

**Depth: two levels.** The conversation proposes a task; that task may propose pieces; a
piece may not. The tool is simply not on a second-level task's belt — it does not have the
verb, so it cannot try and be told no.

**Fan-out: five pieces per task.** A task that asks for a sixth gets its call answered
with:

> no: you have already handed out 5 pieces of this work, which is as many as one task may.
> Do the rest in your own hands, or finish these and report what is left undone.

It reads that as an instruction and does the rest itself.

Neither bound is a setting. They are there because the third level and the sixth piece cost
more than they save: every piece pays for its own copy of the repository, its own check and
its own wait, so past a few of them fanning out is slower than working. A task is told the
same thing in its own words — split only what is genuinely independent, and never shard
work that fits in its own hands.

`task.parallel` still applies to the whole session: pieces queue behind it exactly as
top-level tasks do.

## How many tasks run at once

**There is no limit by default.** aforge does not cap the number of tasks running at the
same time.

The setting `task.parallel` exists for anyone who wants a number anyway — settings panel
(`ctrl+,` or `/settings`), category "spending". Blank means no limit. A cap is a queue and
never a refusal: work past the cap waits and starts when a slot frees, and while it waits
its roster row reads `waiting · slot`; a parked-only family starts folded.

What actually runs out is the machine, not a count of tasks. Two real ceilings hold new
starts instead:

- `task.max_load` — the one-minute load average divided by core count, default **1.5** per
  core. At or above it, nothing new starts and a held task's row reads
  `waiting · machine busy`.
- `task.min_free_mb` — a floor under available memory, default **1536** MiB. Below it,
  nothing new starts.

Both gate starts only. Nothing already running is ever touched; the pressure drains as
running work finishes, and the check is re-asked every 5 seconds.

**The honest caveat:** these two governors read `/proc/loadavg` and `/proc/meminfo`, so
they only apply on a machine that has them. Where there is no `/proc` — macOS, Windows —
the governor cannot say anything and therefore never holds. On those machines
`task.max_load` and `task.min_free_mb` do nothing at all.

Separately, a task that is already running can be held by the provider's own pacing. Its
row reads `waiting · rate limited` until the calls get through.

The frontier used to hold two tasks at once. Two was a guess standing in for a resource
nobody had measured: idle on a sixteen-core box, one too many on a laptop already compiling.

## Naming a model for one task

You ask in words — "let opus do this one", "run that on gpt-5". There is no key, command or
field for it: the model that grooms the work sets the model on the proposal. The word may
be a whole catalog id (`anthropic/claude-opus-5`), the tail after the vendor
(`claude-opus-5`), or any tokens that appear in one id (`opus 5`). Case, stray spaces and a
leading `~` are ignored. Three things can happen.

**One match — it is used and nobody is asked.** The card's meta line names the full id,
and the model's receipt reads `task 7 started on anthropic/claude-opus-5: <title>`.

**A few matches — a shortlist on the card.** Two to four candidates become the models row.
It is a correction, not a gate: the countdown is already running on the closest match,
which is chip 1, and that is what silence takes. Click a chip or press its digit `1`–`4`.
Picking a model answers nothing — the question is still whether the work goes at all. Only
a chip on the row can win. Chips are spelled with the part after the vendor unless two
vendors share a tail, in which case all of them keep their full id; a chip that does not fit
is dropped rather than cut, and a row that would show one chip is not drawn at all.

Name nothing and the task runs on `task.model` if you have set it, and otherwise on the
model the conversation is on right now — read live, so switching the conversation's model
moves it too. The resolved id is remembered for the task's whole life and survives a
restart.

## When no model matches the word you used

If you name a model for one task and nothing in your catalog answers to that word, the
attempt comes back as a refusal the model can correct in one round trip. With near misses:

```
no model here is called "opos-5" — did you mean anthropic/claude-opus-5, anthropic/claude-opus-5-thinking? Name one of those, or leave model out to run on <default id>.
```

With nothing in common at all (a word like "fast" or "cheap"):

```
no model here is called "fast". Name a model id the person has, or leave model out to run on <default id>.
```

A word matching more than four models is refused the same way, because that is a list and
not a shortlist: `"claude" matches several models — say which: a, b, c, d.`

No proposal reaches you until that is settled. The model can name one of the ids the
refusal offers, or leave the model out so the work runs on the default.

## Stopping a task — how to cancel or kill running work

**`x` stops it, and it asks first.** Press `x` with the roster's cursor on the task, or
inside the task's room, over an empty message box. One card comes up:

```
? Stop this task? Its work halts; the branch it wrote on is kept.
  [stop it]   [keep going]
```

The cursor opens on `keep going` — the destructive answer is never under the key you
press to dismiss a question. `left`/`right` move, `enter` takes, `esc` is `keep going`.
There is no bypass key: the card is always asked.

With a pointer, the `✕` at the right end of a room's pinned header raises the same card.
Strip chips do not carry a stop button.

**What stopping does.** A task that is RUNNING has its worker cut off where it stands: the
turn it was in the middle of ends, and the task settles as `stopped`. A task still QUEUED
is dropped instantly, reads `stopped before it started`, and anything waiting on it is
told its prerequisite will never finish. Either way:

- **its branch is kept, with its work on it.** Nothing it wrote is thrown away: whatever
  reached disk is committed onto the branch, and the landing card names the branch and the
  files, exactly as it does for every other early ending.
- **what it spent is what it spent.** The figure freezes where it was.
- **it is not a failure.** The roster draws `⊘` rather than the failure cross, the room's
  header reads `stopped`, and the model is told the task was *stopped* — so nobody goes
  looking for a fault that is not there.

Pressing `x` twice, or on work that has already landed, does nothing but say so.

**You can still ask in words instead** — "stop task 7" — and the model has the door
through its `tasks` tool. The key is faster and does not spend a turn.

What else you can do yourself, on a task that is running:

| what | how |
| --- | --- |
| see it | its roster row, its room, an inline `task 7` link, or its strip chip on a narrow frame |
| see what it is doing this second | the roster row's tool line, or its room, live |
| see what it is costing | the roster's telemetry row, the room's focus header, the `Σ` |
| walk into it | click it, `enter` on it, or `→` over an empty box |
| talk to it | `enter` on a sentence in its room |
| read its whole transcript | its room |
| copy text out of it | `ctrl+b` in its room |
| refer to it in conversation | `@<slug>` |
| leave it | `esc`, `←`, or `←←` — the work keeps running |
| stop it | `x`, or the `✕` in its room's header — one confirmation card, always |
| change its brief or its done-condition | **cannot** — frozen; propose the work again |

Steering sends your words into the task's own loop verbatim, and they land in its room as
your own line. If nobody is listening any more — it landed, it was stopped, its worker is
gone — the room asks rather than dropping the sentence or quietly sending it to the main
model: `<title> is parked — [r] revive and send · [m] send to main · [esc] cancel`, with
the engine's own reason on a dim second row. `r` leaves the room and asks the model to start
the work again with your instruction; `m` leaves the room and sends your words to the model
unwrapped; `esc` cancels and leaves your words exactly where they are in the box.

Whenever a task stops for any reason it wears `stopped — branch kept` and its branch name.
Nothing is thrown away: on every ending except a clean merge the branch is kept and named,
and what the task made is committed onto that branch before it lands — so the files it
produced are listed under `changed:` and `git merge task/…` brings them over. The merge is
never done for you, because only work that was checked reaches your branch.

## Answering a task that needs your look

Some work lands with `needs your look`: it finished, but nobody could say whether it holds.
It is neither done nor failed. Nothing has merged, the branch is kept, and anything waiting
on it stays waiting until somebody decides. Its family rises to the top of the roster, and
its row reads `finished — look it over`.

Read it first. Its room holds the whole of it, and its landing card expands to the changed
files, the branch, the model, the cost, the done-condition and the report.

**There is no key or click that settles it.** Unlike stopping, this one is done by saying so
in the conversation — "accept task 7", "that one isn't finished", "have another look at
task 7". The model holds the door through its `tasks` tool.

When it is settled, the task re-settles into `done` or `failed` — a state it has not been
in — so a second landing card is drawn. The decision is an event, and the card is the record
of it.

The `need you` footer count covers only work that will not move without you: a landing
nobody could judge, and finished work still sitting on a branch that never came home.
Work that ran in your own tree, or that ended before there was a branch, is not
undelivered — it is over, and its settled family starts folded.

## Stopping an adaptive run

An adaptive run is a run that keeps spawning tasks of its own for as long as its planner
has something left to want, and it has a page rather than a room: the nodes drawn as chips
in layers, with one fuel gauge pinned at the top — `planner: <model> · $0.87 / $2.00`, the
model doing the planning beside what it has spent of what you approved.

**`x` on that page stops the whole run**, over an empty message box, and it asks the same
one card the pointer's `✕` asks:

```
? Stop this run? In-flight nodes halt; partial results stay.
  [stop it]   [keep going]
```

The cursor opens on `keep going`. There is no bypass.

**What stopping a run does.** It means *stop spending now*:

- **nodes in flight are cut** where they stand, and **their partial output is discarded** —
  a half-answer handed on to the next node as though it were a finding is worse than no
  answer at all. Each of them ends drawn grey with `⊘` and the word `stopped`.
- **queued nodes are dropped instantly**, and stay on the page rather than vanishing: the
  shape you are looking at is the shape the run crystallized into.
- **nodes that already finished keep everything** — their digests, the planner's notes, the
  whole trace.
- **no write-up is produced.** The closing synthesis is one more model call, and stopping
  is you declining to pay for it.

The header's state word becomes `stopped` and the conversation gets one line saying where
it got to: `stopped — $0.42 spent, 5 of 9 nodes done`.

**The fuel gate's own `stop` is the same stop.** When a run spends its tank it parks and
offers three answers — `add $1`, `finish with what we have`, `stop` — and choosing `stop`
there does exactly what `x` does, leaves the same trace, and says the same sentence. One
stop, one word, wherever you reach it from.

Pressing `x` on a run that has already finished does nothing but say so.
