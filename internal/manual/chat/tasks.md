# Work that runs on its own

## What a task is

A task is one self-contained piece of work handed off to run on its own while the
conversation carries on. It works from a written brief alone — it never sees the
conversation — in its own copy of the repository, and it reports back when it lands.

You do not start one directly. You ask for the work in words, and the model grooms it and
calls its `propose_task` tool. You then get a card asking whether the work should go. The
model's window onto work that is running or already landed is its `tasks` tool; that is
also the door it uses to steer a task or to settle one, when you say so in conversation.

Every task carries a title, a short summary, the brief, and a done-condition — the command
that must pass, the behaviour that must hold, the output that must appear. The brief and
the done-condition are frozen the moment the work is admitted. Steering can add a missing
fact or correct a step, but it cannot change what the work is for. If the objective itself
was wrong, the answer is to propose the work again.

You can keep working while a task runs. aforge tells you not to wait for it: its report
arrives in the conversation when it lands.

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

- **`done`** — a tick, muted. It goes to the `done` group on the roster.
- **`failed`** — a cross, in the bad hue, drawn with the word `failed`. It also goes to
  `done`, not to `needs you`: by the time you see the word it is settled news. Inside the
  `done` fold, work that came back short claims the first slots.
- **`needs your look`** — a `?` in the warn hue. It goes to the `needs you` group. The `?`
  is deliberately neither a tick nor a cross: it claims neither a finding nor a judgement
  nobody made.

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
frame at least 24 columns wide and 6 rows tall, and it is not drawn while the roster is
open over the whole frame.

Order: running first, then work that needs you, then idle. Parked and finished work never
appear on it — the strip is the live set, the roster is the history.

A chip carries one glyph and the name cut to 18 cells, and nothing else: no clock, no
spend, no tool name. The glyph is the task's state where there is one worth drawing
(spinner, `✓`, `✗`, `?`) and its own identity mark while it is queued. The room you are
standing in takes a colour band; the chip the roster's cursor is on takes an underline. A
chip can wear both.

The strip is pointer-only and adds no keys of its own — its cursor is the roster's cursor,
read rather than owned.

- Click a chip to walk into that task's room. Click the chip of the room you are already in
  to close it.
- Click the `+N` overflow mark to open the whole roster.
- A press anywhere on the row belongs to the row, even in the gaps, so a miss never falls
  through to the transcript.

On a frame too narrow for one whole chip plus its `+N`, the first chip is drawn cut and the
`+N` is dropped: a count of things you cannot identify is worth less than one name.

## The roster: the column of all the work

The roster is a column on the right holding every task this session has admitted, not just
the live ones. It is the session's record of its own work. Nothing puts it away except
`/new`.

It appears as soon as one task exists, at a frame width of 100 columns or more — 30 columns
wide from 120 up, a slim 24 columns from 100 to 119. Under 100 columns there is no column,
and `ctrl+t` opens the same roster over the body instead.

Five groups, in this order: `needs you`, `running`, `idle`, `parked`, `done`. Newest first
inside each. `parked` and `done` open closed, as one heading each with its population on it
(`▸ parked 148`). `needs you` is the one group with no fold. An empty group draws no
heading.

A task's row opens with two glyphs answering two questions: its state, which changes, and
its own identity mark, which never does. Then the name, then its id as `#7`, dim, at the far
end — and the id stands down when the name would be left under 12 cells. Under the row, at
most two more: what it is doing, what is holding it, what it waits on, or how its branch
came home. `conflicted · task/fix-nil` in the bad hue is the one loud row on the column.

There is no subtitle here. The column is a presence list; the proposal card and the landing
card both carry the sentence.

At the bottom, up to three dim lines: `Σ $1.42 · 312k tok`, `3 running · 1 needs you`,
`148 parked · 12 done`. The `Σ` is the whole session's spend — it already contains every
task in the column plus the conversation, so there is deliberately no per-task share. Zero
figures are left out entirely, because zero means "nobody published a price", never "free".

## Using the roster from the keyboard

`ctrl+t` hands the keyboard to the roster. It is asked for, never taken: the draft is the
rest state, so a person who starts typing is typing, not navigating.

| key | what it does |
| --- | --- |
| `↑` `↓` | move, headings included |
| `→` | expand the focused row's group |
| `←` | collapse it; from a task, close its group and land on its heading |
| `enter` | fold a heading, or open a task's room |
| `esc` or `ctrl+t` | give the keyboard back |

The legend hint while it holds the keyboard is `↑↓ move · →← fold · enter open · esc`, and
the focused heading says `— enter/→ expand` or `— enter/← collapse` when there is room.

Every other key is given back. The roster cannot take the keyboard while the exit
confirmation, a permission question, a task proposal, or any overlay is up, and with no
tasks at all `ctrl+t` falls through rather than being swallowed.

The cursor follows the work, not the row: it is held as a group plus an id, so a task that
lands carries the cursor into its new group instead of leaving it on a row that moved. If
the task went somewhere folded, the cursor lands on its new heading.

With the pointer: click a row to open that task's room, click the open room's row again to
close it, click a heading to fold or unfold it. A click that hits no task still belongs to
the column and does nothing — the column you aim at to switch rooms must not be the column
that throws you out. A click moves the cursor but does not hand the roster the keyboard.

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
| what `enter` does | sends to the model | **steers the task** |
| box placeholder | the draft prompt | `Steer <title>… (esc: main)` |
| pinned top line | none | the focus header |
| legend word | the workspace path and branch | `room · esc/←← main` |
| the model on the status row | the conversation's model | `task <the task's model>` |
| clicking that model | opens the model picker | inert — the picker moves the conversation |
| `ctrl+b` | freezes the transcript | freezes the room's own rows |
| scroll position | the conversation's | the room's own, kept separately |
| attachments | the tray sends pictures | a room's box sends words only |
| proposals | drawn as cards | never — a task does not propose work to you |

The focus header is one accent line pinned at the top:
`─ ⠙ main ▸ Fix the nil-map crash · running · 2m12s · $0.04 ──── esc/←← main ─`. It carries
the state glyph, a trail that always names `main` as the root, then the state word, the
clock, the spend and the model — each dropped when nobody published it. It is pinned
because a fact that scrolls away is only true at the top of the page.

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

## How many tasks run at once

**There is no limit by default.** aforge does not cap the number of tasks running at the
same time.

The setting `task.parallel` exists for anyone who wants a number anyway — settings panel
(`ctrl+,` or `/settings`), category "spending". Blank means no limit. A cap is a queue and
never a refusal: work past the cap waits and starts when a slot frees, and while it waits
its roster row reads `waiting · slot` under the `parked` heading.

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

## Stopping a task, and steering one

**There is no key, click or command anywhere in the chat that stops a running task.** The
way to steer or stop one is to say so in the conversation, in words — the model has the
door and you have the sentence.

What you can do yourself, on a task that is running:

| what | how |
| --- | --- |
| see it | its strip chip, its roster row, its room, an inline `task 7` link |
| see what it is doing this second | the roster row's tool line, or its room, live |
| see what it is costing | the roster's telemetry row, the room's focus header, the `Σ` |
| walk into it | click it, `enter` on it, or `→` over an empty box |
| talk to it | `enter` on a sentence in its room |
| read its whole transcript | its room |
| copy text out of it | `ctrl+b` in its room |
| refer to it in conversation | `@<slug>` |
| leave it | `esc`, `←`, or `←←` — the work keeps running |
| change its brief or its done-condition | **cannot** — frozen; propose the work again |

Steering sends your words into the task's own loop verbatim, and they land in its room as
your own line. If nobody is listening any more — it landed, it was stopped, its worker is
gone — the room asks rather than dropping the sentence or quietly sending it to the main
model: `<title> is parked — [r] revive and send · [m] send to main · [esc] cancel`, with
the engine's own reason on a dim second row. `r` leaves the room and asks the model to start
the work again with your instruction; `m` leaves the room and sends your words to the model
unwrapped; `esc` cancels and leaves your words exactly where they are in the box.

Whenever a task stops for any reason it wears `stopped — branch kept` and its branch name.
Nothing is thrown away: on every ending except a clean merge the branch is kept and named.

## Answering a task that needs your look

Some work lands with `needs your look`: it finished, but nobody could say whether it holds.
It is neither done nor failed. Nothing has merged, the branch is kept, and anything waiting
on it stays waiting until somebody decides. On the roster it sits at the top, under
`needs you`, and its row reads `finished — look it over`.

Read it first. Its room holds the whole of it, and its landing card expands to the changed
files, the branch, the model, the cost, the done-condition and the report.

**There is no key or click that settles it.** Like stopping, this one is done by saying so
in the conversation — "accept task 7", "that one isn't finished", "have another look at
task 7". The model holds the door through its `tasks` tool.

When it is settled, the task re-settles into `done` or `failed` — a state it has not been
in — so a second landing card is drawn. The decision is an event, and the card is the record
of it.

The `needs you` group holds only work that will not move without you: a landing nobody
could judge, and finished work still sitting on a branch that never came home. Work that
ran in your own tree, or that ended before there was a branch, is not undelivered — it is
over, and it goes in the `done` fold.
