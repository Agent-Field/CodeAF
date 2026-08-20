# What is on the screen

## What the frame draws, top to bottom

aforge draws one screen in a fixed order every frame. From the top: the pinned room
header (only while a task room is open), the task strip, the conversation, a breathing
gap, the rule with the legend in it, the approval question, the connect offer, the
sub-harness offer, the steer guard, the follow-up row, any message waiting for the
answer to finish, another gap, the draft box where you type, any open list (picker,
menu, completion), and the status line last.

Beside the conversation, on the right, the task roster's column — the right-hand bar,
sidebar, task panel, whatever you call it — takes 30 columns (24 on a narrower frame)
from the session's very first frame, before any tasks exist. Work fills the column
rather than raising it. Empty it says one dim line — `no tasks yet` — unless the
project has a record from earlier sessions, in which case those dulled `earlier` rows
stand there instead. `ctrl+g` closes it and opens it again, remembered between
sessions, and the column's own last line says so: `ctrl+g — hide`. With it closed the
conversation is laid out at the full width of the terminal, running work still draws
the strip along the top, and the legend's hint slot reads `ctrl+g tasks` once the
session has tasks to come back to.

Three things take the whole frame instead of sharing it, at every width: the settings
panel (`ctrl+,`), the task page (`ctrl+.`, or `/history`), and home (`/home`, or space
twice on an empty box). While any of them is up nothing else is drawn — no conversation,
no box, no status line — and `esc` gives the frame back. **Only one of the three is ever
up:** opening any one closes the other two.

The status line is the last row of the frame, not the first. It sits at the bottom so
you read it in the same glance as the box above it.

## The box says which room you are typing into

While a task room is open the draft box carries the room in front of its own `› `: the
task's state glyph and its name, on the tinted background a selected row wears, in the
hue of what that task is doing — the same hue the roster paints its glyph with. So the
line you are typing on says where the words are going, and it says it whether the box is
empty or full.

```
 ⠋ Ship the port › fix the flake in the loader
```

- It is a **segment, not a row** — it costs the conversation nothing and the caret is
  counted through it, so the cursor is where the letter is.
- Continuation rows of a wrapped draft line up under the text, past the segment.
- The name is cut to at most 18 cells. On a frame with too few columns to leave a box
  worth typing in, the segment is dropped and the box's placeholder names the task
  instead.
- In the main conversation there is **no segment at all** — not a dim one, not an empty
  one. There is nowhere else the words could be going.

The same task is marked twice more while you are in it: its row in the roster wears the
same tint, and its chip on the task strip does too.

The rule above the input is the only horizontal line this surface draws. There are no
borders anywhere else. The draft box is inset one cell.

If the frame is taller than your terminal, rows are lost from the **top**, never from
the bottom. The chrome is the tail, and the tail survives.

## Blank rows, and the gap above the message box

Four rules decide every blank line in the conversation. One blank before a tool cluster
that follows text; none between the lines of a cluster; one blank after a cluster; one
blank before each of your own messages. A cluster that opens a turn — one that answers
your message directly — gets no gap of its own. A gap asked for twice is still one gap.
Nothing is ever drawn at the very top of the conversation.

The resting whitespace between the conversation and the draft box is a **height**
ladder, not a width one:

| terminal height | breathing rows |
| --- | --- |
| 16 rows or more | 2 |
| 6 to 15 rows | 1 |
| under 6 rows | 0 |

Below 6 rows the rule, the gap, the pinned room header and the task strip all go,
leaving the conversation, the box and the status line. The ladder steps down, never up.

## Getting back to the latest message

When you scroll away from the live edge, a small dim chip appears offering the way
back. It reads exactly:

```
↓ latest · ctrl+l
```

The arrow becomes `v` in the screen-reader tier. Click the chip, or press `ctrl+l`,
which works whether or not the chip is drawn. Either one re-arms sticking to the live
edge.

The chip is right-aligned and rides the first row of the breathing gap that is already
there, so the conversation is exactly as tall with the chip as without it. It is dim
normally and accent under the pointer, with no background band.

It is not drawn at all when there is no gap row (a short window), when the label is
wider than the frame, in copy mode, while a room is open, or while the fullscreen
roster is up. A room keeps its own edge: `ctrl+l` inside a room scrolls the room, not
the conversation.

## The line above the message box (the legend)

The rule that separates the conversation from your own business has the place written
into it, like the legend on a fieldset:

```
─ ~/s/aforge-v2 · chat-v3-task* ────────── / commands ─
```

The left is the workspace path abbreviated fish-style — home becomes `~`, every parent
is cut to its initial, and the last segment is never abbreviated — plus the git branch,
with a `*` when the tree is dirty. On a remote session the machine's name is prefixed
and is never cut along with the path (`devbox:~/code/app`).

The right is a hint slot. It names the keys that work right now when a state has keys of
its own — for example `y allow · n deny · a always` while a question is up,
`esc interrupt` while a turn is running, `esc stops and sends` while a message of yours is
waiting for the answer to finish, or `↑↓ · enter · esc` while a list is open.

**At rest it carries the two doors out of the conversation.** With an empty box and
somewhere else on the machine to go, it reads exactly:

```
space space home · / commands
```

Pressing the space bar twice on an empty box opens the home screen, and clicking those
words does the same; `/` opens the command list. The home half is dropped when there is
nowhere else to go, leaving just `/ commands`, and the whole slot gives way the moment you
type or a state above claims it. It costs no row either way — this line is on the frame
regardless.

**Inside a task's room the slot is the room's**, and it never says `esc interrupt` there
— in a room `esc` leaves the page rather than interrupting anything. It reads `x stop`
while there is work here to stop, `↑↓ history` while a history walk is on, and nothing
otherwise. The left end of that legend is the room too: `room · esc/←← main`, or
`room · esc your line back` for as long as a walk is on, because that is the key's real
meaning until the walk ends.

One line in that slot is not about the next keystroke: `ctrl+g tasks`, which appears
when you have closed the task column and this session has run something. It is the
whole of what the frame says about a roster that is not on screen, and it says nothing
at all when nothing has been run.

While a question is waiting, the whole legend goes violet.

As the terminal narrows the legend gives things up in order: hints first, then the
branch, then the path shortens from `~/s/aforge-v2` to `…/aforge-v2` to `aforge-v2`,
then the plain rule. Below width **70** the branch and the hint slot are dropped
outright.

While a task room is open the left says exactly `room · esc/←← main` — and exactly
`room · esc your line back` while a history walk is on, since for those keystrokes `esc`
gives your own draft back before the room's own `esc` gets the key. The path and the
branch are not drawn either way. The pinned header at the top of the frame says the same
thing in its own words, `esc/← main`, and unlike the legend it answers to a press: click it and
you are back in the conversation. Clicking the page itself does not leave a room — a
press on empty space does nothing here as it does everywhere.

## The status line at the bottom

One row at the bottom of the frame, in two clusters. **Identity on the left** — which
conversation, and what is answering it. **Telemetry on the right** — what it costs, how
full it is, whether it is alive. The gap between them is the only separator: no pipe,
no bracket, no rule.

Nothing crosses. A number never appears on the left; a name never appears on the right.

The identity cluster reads like `porting the parser · gpt-4.1-mini:high`. The name is
the one the session chose for itself, falling back to the workspace's base name until
it has named itself, so it is never empty. The model is its **basename** — everything
after the last `/` — because the vendor prefix is nine identical cells on the row where
width is scarcest. The `:high` is the reasoning level, which is how the model is being
run, not part of its address. The full routing address stays in the picker, in the
`/model` note, in the session file and on the phone status sheet.

When the endpoint that answered is not already named by the model id, the segment gains
` · via deepinfra · 92 tok/s`. That goes silent after **10 minutes** rather than quoting
a stale rate.

Pressing the model segment opens the model picker.

While a task **room** is open the cluster renames itself to the room chip and the
room's model, and that model becomes an inert fact, not a door — the picker moves the
conversation's model, not the room's. One `esc` restores both.

Below width **100** the telemetry may take a row of its own, still right-aligned, and
only when the two clusters would otherwise collide — a short session still fits on one
row at 60 columns. If even the emptied line will not fit, the telemetry is what
survives, the identity is not drawn, and nothing on the row is pressable.

## What each part of the status line means

Nine segments, right to left of the identity, joined by ` · ` in a fixed order:

| # | segment | example | what the number is | when it is empty |
| --- | --- | --- | --- | --- |
| 1 | ambient | `2 jobs · 1 watch` | background work this screen saw start and has not seen killed — a `bash` with `background:true`, a `watch` call | zero of both draws nothing |
| 2 | delta | `Σ +128 −14` | lines added and removed by this whole session | only at width 120 or more; empty when both are 0 |
| 3 | cost | `$0.14` | the session's running spend | never empty |
| 4 | context | `12.4k/128k · 10% ▁▂▃▅` | tokens the conversation is carrying, the model's window, the percentage, then a 6-reading sparkline | empty when nobody has said what the window is, or tokens are 0 |
| 5 | cache | `⟲ saved $0.02 · 89%` | the session's cache hit rate, and what that share was worth in cash | empty until there is a cached share; on an unpriced model the cash half goes, leaving `⟲ 89%` |
| 6 | burn | `1.2k tok/s` | output tokens over the wall time of **this** turn | empty unless a turn is running and has run for at least 1 second |
| 7 | eta | `compaction in ~3 turns` | forecast from average growth | empty when the conversation is not growing, when the answer is more than 5 turns out, or when compaction is already due |
| 8 | yolo | `YOLO` | the approval gate is set to `allow` | empty in every other posture — absence is the safe state |
| 9 | state | `⠹ working · 4s` | what the screen is doing, and for how long | never empty |

The `N jobs` figure means "what you started". A background job that exited on its own is
still counted, because nothing on the wire says otherwise.

## Why the numbers on the status line fade

The telemetry cluster on the right of the status line is painted by how recently each
segment **changed**: changed under 4s is ink, under 10s is muted, otherwise dim. At rest
the whole cluster is one quiet grey, and the one segment that moved is the only thing
with weight.

A segment's first appearance is not a change, so a new segment starts at the bottom of
the ramp. A segment that vanishes loses its clock — the next thing of that kind is new,
not recently changed.

Four things override the ramp, in this order: the state word owns its own paint; `YOLO`
is always the bad hue, loud for what it means rather than for when it changed; the
context meter outranks its age with its own three-rung heat; and while you are being
asked something the whole ramp collapses to dim, so no number competes with your
decision.

There is no idle ticker driving this. During a turn the frame clock is already running,
and when a turn settles exactly two one-shot ticks are scheduled so the fresh tier can
expire on time.

## Why the status line says $0.00

A figure nobody measured is not drawn. Zero jobs, zero watches, an unknown context
window, an unpriced cache — every one of them draws nothing rather than a zero.

The spend segment is the one deliberate exception. The status line and the phone status
deck **do print `$0.00`** on a session that has spent nothing. The reason is that the
status row is a live row: a segment that came into existence on the first priced turn
would shove every segment beside it sideways.

The commands keep the law instead. `/status` filters the spend line out when the cost is
zero, and `/cost` only adds it when the cost is above zero — so the two commands say
`nothing spent yet — this session has not sent a turn.` where the row says `$0.00`. A
landed task card also refuses to print `$0.00`.

Other honest silences: the context percentage is dropped below 1% rather than shown as
`0%`; the cache cash half appears only when there is a published price pair, never
"saved $0.00"; and the saved figure uses four decimals under a dollar, so a real
fraction of a cent is not rounded away to nothing.

## The state word: idle, working, waiting

The last segment of the status line is the one thing true of the whole row. The exact
words:

| word | when | paint |
| --- | --- | --- |
| `idle` | nothing is running | dim |
| `⠹ working · 1m 4s` | a turn is running; spinner plus a count-up | accent |
| `waiting · your call` | a consent question or a task proposal is open | the question hue, bold |
| `interrupted` | the last turn was stopped by hand | the bad hue |
| `COPY` or `COPY · 12 lines` | copy mode | accent |

`waiting · your call` outranks `working`. Copy mode outranks everything, because it is
the only state about the keyboard rather than about the turn.

The spinner turns on the same 4-tick grid the tool rows use, so nothing on screen beats
against anything else. In the screen-reader tier the spinner is a still `*`.

## What the status line drops when it is narrow

When the segments do not fit, they are removed one at a time in a fixed order, by how
actionable each one is:

```
delta → cache → eta → burn → ambient → cost → context
```

The **state word and the `YOLO` badge are not in that list at all**. One is why you are
looking at the line; the other is why you should be.

The burn rate is damped on purpose. It is recomputed every frame, but the string is
held: the rate is rounded (steps of 5 under 100, two significant figures above) and the
shown value is only replaced every **500ms**. Between replacements the segment is
byte-identical. When a turn ends the empty value takes effect at once. The reason, in
the code's own words: a number that moves faster than it can be read is not
information, it is motion.

## The context meter and its sparkline

How full the conversation is, measured against the **compaction threshold** rather than
the model's window — the threshold is the thing that actually happens to you.

Three rungs: calm (dim), **near** (accent) past 80% of the threshold, and **due** (the
bad hue) past the threshold itself.

The sparkline is the last **6** turn-end readings, one glyph each from `▁▂▃▄▅▆▇`, also
scaled to the threshold.

No sparkline is drawn with fewer than two readings — one bar is not a trend, it is a
bar. There is no sparkline in the ASCII glyph tier or the screen-reader tier, and none
below width **70**. All three keep the number, which is the fact.

## Does this work on my phone? Narrow terminals

Yes. There is one size-class table, and every part of the surface reads it:

| width | tier | what it means |
| --- | --- | --- |
| 120 or more | wide | the full frame, task rail column and all |
| 80 to 119 | standard | the everyday laptop frame |
| 60 to 79 | narrow | split panes; the task roster already overlays |
| under 60 | **phone** | a phone in a terminal — everything stacks |

Phone width is **under 60 columns**, and it reaches down to about 20 columns. Some
comments in the source say "forty-four columns" as an illustration; the real threshold
is 60.

The frame itself clamps to a minimum working size of **8** columns and **1** row. A
headless boot and a terminal reporting zero size are the same case.

Phone width **reshapes six things and deletes none**. What will not fit is relocated,
not truncated away.

## What phone width reshapes: the six

Under 60 columns, six things change shape:

1. **The status line becomes a two-row deck**, with a fullscreen status sheet one tap
   away.
2. **Tool rows become a different sentence.** The state glyph moves to a fixed 2-cell
   gutter on the left, the target sheds its qualifier, and there is one clock figure
   only.
3. **Opening a tool call takes the whole frame** as a sheet, instead of expanding
   inline under the row.
4. **Markdown wraps instead of cutting.** Fenced code is re-wrapped with a `↳ `
   continuation marker; tables are stacked as `key: value` records.
5. **List rows take two lines** — the label on one, its dim tail indented under it.
6. **The consent question becomes a bottom sheet** with full-width answer bands,
   instead of a line of `[y]`/`[n]`/`[a]` targets. Below width **16** the one-line offer
   is used instead of the sheet.

On top of those six: preview blocks under a pending call are capped at 4 rows instead of
12; there is no task rail column (that already went at 100); and the legend has already
dropped its hint slot and its branch (that went at 70).

## Other width thresholds worth knowing

Beyond the four tiers, these are the exact points where parts of the screen give way:

| what | threshold |
| --- | --- |
| session delta (`Σ +128 −14`) drawn at all | width 120 |
| telemetry may wrap to its own row | below width 100 |
| legend loses branch and hint slot; no context sparkline | below width 70 |
| full task rail, 30 columns off the conversation | width 120 |
| slim task rail, 24 columns | width 100 |
| no rail column at all — `ctrl+t` overlays the roster instead | below width 100 |
| no rail column at any width — you closed it with `ctrl+g` | your choice, remembered |
| task strip | width 24 **and** height 6 |
| a room's pinned header | width 12 and a non-zero breathing gap |
| welcome box | not drawn below height 12 or width 40 |
| consent bottom sheet at phone width | width 16 |
| trailing stat on a tool row | dropped unless the target keeps 8 cells |
| tool preview and expansion | nothing below width 8 |
| phone code wrap | falls back to plain cut rendering below 8 content cells |
| opened-table columns | become stacked records below 8 |
| landed task card | nothing below width 8 |
| turn receipt | none below width 8 |

## The two-row status deck at phone width

Under 60 columns the status row becomes a deck of exactly two rows — never one, never
three:

```
 Fix the nil-map crash          $0.31 · 12%
 deepseek-v4-flash        ⏺ 2 running ▸
```

Row 1 is **what this is** (the session name, or the workspace place if it has not named
itself) against **what it has cost** (spend, and the context percent only — the
fraction is what the sheet is for), with a `▸` on the end. Row 2 is **what is
answering** (the model basename, no rider) against **what is still moving**
(`⏺ N running`, `N jobs`, then the state word). Identity left, telemetry right, the gap
as the only separator, same as the wide row.

On row 2, counts drop from the left when the row runs out. The state word is the last
to go.

The deck takes every press that lands on its two rows, so nothing falls through to the
draft box directly above. Tapping the model chip on row 2 opens the model picker — its
target is widened to at least 4 cells, because a finger is not a pointer. Any other tap
on either row opens the status sheet.

In a room, row 1 renames itself to the room chip and row 2 to the room's model, and the
chip stops being a door; that press falls through to the sheet, which labels both
models. A session with no model yet says nothing rather than "no model".

## The full-screen status sheet

At phone width, tapping anywhere on the status deck that is not the model chip opens a
fullscreen sheet listing every fact the status line can hold, one per line, label then
value.

Typing `/status` (aliases `/info` and `/context`) prints the same list as a note in the
conversation. That is the only door if you are on the keyboard with the mouse off.

The head is `status` on the left and `esc close` on the right. The foot names the keys:
`esc close · ↑↓ move`, plus ` · enter model` while the cursor is on the model line.
Rows carry a `▸` when a press acts on them. The selected row is painted by its label,
not by a band across the line.

The items, in order: `session`, `task` (in a room), `model` (the full routing address
with its `:level` — the one actable row), `task model` (in a room), `served`, then every
telemetry segment under its own word — `background`, `changes`, `spend`, `context`,
`cache`, `rate`, `compaction`, `approvals`, `state` — then `tasks`, `place` (full path,
branch and dirty star) and `keys`.

A press selects a row; a second press on the already-selected row answers it. A press
outside the list — the title, the rules, the keys line, the empty rows under a short
list — closes the sheet. The wheel moves the cursor.

The sheet closes itself the moment the frame grows back out of phone width. A sheet
standing in for a row that is back on screen is a sheet nobody asked for.

## Markdown: what aforge renders

The model's reply is rendered as markdown, parsed with goldmark and painted through
aforge's own token layer — there is no HTML renderer involved. What is supported:

- **Headings** — promoted by tier, never by size. h1 is primary ink and bold, h2 is
  primary, h3 and below step back down the grey ramp. Loudness is position on the ramp
  plus the whitespace around it.
- **Paragraphs** — wrapped to a measure of **88** cells, clamped to the pane width, not
  to your terminal width. A 200-column window is a wide window, not a wide sentence.
- **Emphasis** — CommonMark by delimiter count: one `*` is italic, two is bold, three is
  both. Nesting composes, so it cannot leak.
- **Strikethrough** — GFM `~~x~~`.
- **Inline code** — drawn on the one raised plane. Where the terminal has no raised
  plane (16 colours and below), the **backticks come back** rather than the span reading
  as ordinary prose.
- **Fenced and indented code blocks** — syntax-highlighted at 256 colours and above,
  ordinary text below. Drawn at the full width, because a figure is looked at, not read
  along.
- **Lists** — bullets and ordered. Wrapped items hang under their own first word, never
  under the marker. An ordered list sizes its column to its widest number, so `9.` and
  `10.` share a right edge. Tight lists get no gaps between items.
- **Blockquotes** — a dim gutter bar and one step down the ramp. Nesting demotes again
  rather than growing a second border. The bar is continuous, including across the blank
  rows inside the quote.
- **Thematic breaks** — a faint hairline across the measure, never carrying a title.
- **Links** — the label underlined, the destination dim in parentheses beside it, as
  `label (https://…)`, because a terminal has no status bar to reveal it on hover. Where
  the label and the destination are the same string, the address is drawn once.
  Autolinks get the same treatment.
- **Tables** — see the table sections.

## What markdown aforge does not render

Most of GFM is rendered in a reply, but two things are deliberately not.

**Raw HTML blocks** render as literal source — one row per line, truncated, at the
chrome tier. You see the tags as the model wrote them.

**Inline HTML tags are dropped.** Their text is already emitted beside them, so showing
`<em>` would show the author's punctuation twice.

Any block the parser produces that this renderer does not name renders its children
rather than being dropped, so nothing vanishes silently.

**Images** cannot be shown. An image is rendered as a link whose text is the alt text
plus the address.

There is no HTML renderer and no glamour involved anywhere — the parser is goldmark's,
and the painting is aforge's own token layer.

Every byte of a reply is sanitised and any surviving escape sequence is stripped, so a
reply cannot paint itself a heading.

## Markdown while a reply is still arriving

The live tail of a streaming answer is plain wrapped text, not markdown.

Every **1500ms** the settled prefix — everything up to the last newline — is promoted to
rendered markdown and remembered as promoted. Formatting catches up as the answer
arrives, without the tail flickering between two renderings. When the turn finishes, the
whole block is rendered at once.

The offer to open a wide table is only drawn on the settled render. A half-arrived table
has columns that will still move, and offering to open something still being written is
a promise this screen cannot keep.

## My message appeared in the middle of the reply — a message never lands mid-stream

It cannot any more. A message of yours is never drawn inside a streaming answer, never
splits a reply into two blocks, and is never interleaved with the paragraph being
written. The rule holds in the conversation and in a task room's own page alike:
whatever is still streaming stays one contiguous block, and your line goes **after** it.

There was a defect here. Pressing `enter` while an answer was streaming used to cut the
reply in two and wedge your sentence between the halves, so it read as though the model
had quoted you mid-thought. Two things fixed it. Your line now always goes below the
block that is still being written, whatever put it there — and plain `enter` no longer
sends into a running answer at all: it **waits**. See "A message you typed while the
answer was still coming" below.

## A message you typed while the answer was still coming (the waiting block)

Press `enter` while a turn is running and your message is **held**, not sent. It is
drawn in its own block directly above the message box — under everything that has
happened, above the box you typed it in — in your own accent hue, with the same `›`
glyph your messages wear in the conversation. Under it sits one dim line:

```
› do much more of a deep research please
  waits for this answer · esc stops and sends · ↑ or click to edit
```

The dim line trims from the right on a narrow terminal: the last piece goes first, then
the middle, and the narrowest frame keeps `waits for this answer` alone. With more than
one message waiting the first piece is counted — `2 wait for this answer` — and with
exactly one it is not counted at all.

What happens to it:

- **When the answer finishes**, it sends itself as an ordinary new turn and appears in
  the conversation as a normal message of yours. Several waiting messages go **one per
  finished turn**, oldest first, in the order you typed them.
- **`esc`** stops the answer and sends it immediately.
- **`↑` over an empty box**, or a **click on the block**, takes it back into the box to
  be edited. `enter` then holds the edited sentence again.
- The box is cleared the moment you press `enter`, so you can keep typing. Attachments
  in the tray go with the held message and come back on the tray if you take it back.
- If the conversation is replaced under it — `/new`, opening a session from the welcome
  box — the waiting messages are dropped and aforge says so: `1 waiting message dropped`
  or `N waiting messages dropped`.

While something is waiting, the hint slot in the legend reads `esc stops and sends`
instead of `esc interrupt`.

## Markdown at phone width

Phone width (under 60 columns) is the one tier where markdown is not the prose
renderer's byte-for-byte output. Every tier above it renders exactly as it always has.

The document is scanned for two shapes, **at column zero only** — a top-level fenced
code block and a top-level GFM table. Those two are rendered differently; everything
else goes straight to the ordinary renderer.

- **Fenced code wraps instead of truncating.** It is rendered two cells narrower than
  the column, and those two cells hold a dim `↳ ` on every row that continues a source
  line. The marker sits outside the code plane, so it can never be mistaken for
  something the code said. Breaks prefer a space in the back half of the row and go
  mid-token when there is none — a 40-cell URL in a 30-cell column has no break in it.
- **Tables stack** as one `key: value` line per cell, with one blank row between
  records. The header travels with each cell rather than standing once at the top. Empty
  cells are dropped. The key is bolded unless the header already carries markup. Each
  line goes back through the prose renderer, so cells keep their bold, their code spans
  and their links.

Limits: a fence indented inside a list item or a blockquote is **not** pulled out. It
stays in its prose segment and is cut, exactly as at every other width. This is a
deliberate gap. Below 8 content cells the wrap is abandoned and the fence is rendered
whole. Cells past the header's width are dropped, as GFM does. A header with no rows
under it renders as the list of column names.

## Task references in a reply become links

When the model writes "task 7" and this screen knows node 7, the phrase becomes a
pressable link — accent, underlined. Click it and that node's room opens.

The grammar is deliberately small: an anchoring `task` or `tasks`, a space, an optional
`id`, an optional `#`, then the digits. So `task 7`, `task #7`, `tasks id 7` and
`task id #7` all work.

These are **not** references: a bare `7`, a bare `#7`, `taskbar 7`, `task 7a`,
`task 7.1` (a version, a date, a range, a clock), and `task 0` — ids are minted from 1.
More than nine digits is not one either.

An id this screen has not seen is left exactly as the model wrote it. A link that opens
nothing is worse than no link.

A reference inside a fenced code block, inside an inline code span, or inside a URL
field gets no link.

## Why my table is cut off, and how to open it

A wide table is fitted by truncating its cells. When that happens, aforge grows one dim
row under the table offering to open it. The exact wording:

```
… open the table
```

and once it is open:

```
… tuck the table back
```

The leading `…` is the same ellipsis glyph that cut the cells.

**Click the words.** The affordance is the phrase and nothing else — a press in the
empty cells beside the words is not a press on the foot. It is dim by default and accent
under the pointer, never a background band. Pressing toggles it; pressing again tucks it
back. The choice is remembered per table on that reply, so a resize keeps it.

**There is no keyboard chord for this, at any width.** It is mouse-only. This is a
stated gap rather than an oversight: prose is not selectable here, so the block cursor
never visits a table. Note that `ctrl+s` hands the pointer back to the terminal for
drag-to-select, which also takes the affordance away until you take the mouse back.

When there is no offer:

- **A table that fit its column** renders as it always has, with nothing underneath. The
  door only appears where the problem did.
- **At phone width there is no foot** — the table is already stacked there, every cell
  whole, so a foot would be an offer to do what has been done.
- **Not while the reply is still streaming.**
- A table that cannot be located in the finished rows gets no foot.

An **open** table keeps its foot at every width, because the foot is the only way back
from a choice you made.

Copy mode yanks the rendered rows, so opening a table is the only way to put its real
content on the clipboard. A closed one offers the ellipses you can already see.

## What opening a table actually does

Opening is a layout decision, not a setting. You asked to see the cells, so aforge picks
the honest layout for the width it has, in this order:

1. If the cells cannot be recovered at all → **stacked records**.
2. Each column gets a `natural` width (its widest cell) and a `need` (its widest word,
   floored at 8 cells, but never more than its natural).
3. If the columns plus their 2-cell gaps do not fit, or the summed `need` exceeds what
   is available → **stacked records**. The grid is over, and pretending otherwise costs
   you the words.
4. Otherwise → **wrapped columns**.

**Wrapped columns** keep the same grid, the same 2-cell gap and the same hairline under
the header — a table that changed its column distance when it opened would read as a
different table. A cell that is too long becomes several rows inside its own column
instead of an ellipsis. Column widths are shared by max-min fairness. Alignment is read
off the table's own `:---:` row and honoured: left, right, centre. A word wider than its
column is broken mid-token, and every row is hard-truncated as a guard so nothing can
exceed its column. If any record spans more than one row, every record is separated by a
blank line; where every record is one row, no blanks are added.

**Stacked records** are exactly the phone tier's `key: value` layout.

Cells keep their formatting. Each column is handed back to the prose renderer alone, as
a one-column table at a width nothing can overflow, so `**opus**`, a link or a code span
shows the same words the closed table showed.

The header is painted secondary, the hairline tertiary and the body primary — the prose
renderer's own ramp. The foot is aforge's own chrome and wears its dim.

## Tool cards: a running call against a finished one

A turn's tool calls are one object on screen: a rail down the left, one row per call,
and an elbow closing the run.

```
├─▶ read internal/session/session.go   · 189 lines
├─▶ edit internal/session/loop.go      +3 −1
│ internal/session/loop.go
│ @@ -1,4 +1,4 @@
│ -const argsLimit = 400
│ +const argsLimit = 8192
╰─▶ bash go test ./internal/session    exit 1 ✗
```

`├─▶ ` for every call above the last, `╰─▶ ` for the last, `│ ` for an opened call's
detail rows. Every rail form is exactly 4 cells wide, so a cluster's names all start in
one column. A terminal that cannot draw those glyphs gets `+-> ` and `| ` at the same
widths.

Each state has its own mark:

| state | mark | the row |
| --- | --- | --- |
| arriving on the wire | `◌` pulsing dim | dim whole, e.g. `receiving · 1.2 KB` |
| queued | `◌` dim | ordinary row, quiet |
| waiting on you | `?` in the question hue, bold | the whole row is the question hue |
| running | braille spinner, muted | a count-up beside it |
| done, success | **nothing** | a quiet line is the success |
| done, failed | `✗` in the bad hue | plus `exit N` as the stat |
| unresolved when the turn ended | `·` dim | frozen; the clock stops |

The spinner means one thing only: something is turning. On success there is no glyph,
ever — a column of ticks is a column you must read to learn nothing. In the
screen-reader tier the marks are `o` queued, `*` running, `x` failed, `.` idle; `?` is
already ASCII.

At most **3** calls of a turn stay on screen. The rest fold into one line reading
`↳ 1 earlier tool call · ctrl+o` or `↳ N earlier tool calls · ctrl+o`. Press `ctrl+o`
or click the line to unfold. Three is the number you can hold without reading: the call
that is running and the two it followed.

## What a tool row says, part by part

A row is rail, name, target, stat, mark.

The **name** is chrome, so it is muted. The **target** is what you are reading, so it
leads in primary ink — and it is split in two within itself: what the call is about
stays ink, what merely qualifies it recedes to dim.

- `bash` — a `cd internal/session && ` prefix is context and goes dim; the command
  itself is substance and is shell-highlighted.
- `read`, `edit`, `write` — the path is ink, a trailing `120-240` line range is dim.
- `grep`, `find` — the **pattern is accent**, because it is the one target that is not a
  thing that exists, it is what the call is looking for; the place it searched is dim.

The **stat** is the dim figure trailing a finished call, derived from the call's own
arguments and output:

| tool | stat |
| --- | --- |
| `edit` | `+3 −1` |
| `write` | `+42 lines` |
| `read` | `· 189 lines` |
| `bash` | `exit 1`, **only on failure** — a zero exit says nothing |
| `grep` | `· 12 matches` |
| `find`, `ls` | `· 8 entries` / `· 1 entry` |
| anything else | nothing |

A `+` is appended to a count whose output was cut by the display cap, meaning "at least
this many". If you answered a consent question for the call, the word `allowed` or
`denied` rides the same slot, after a ` · ` on a wide row, and replaces the stat
entirely at phone width. **A command you sent to the background with `ctrl+g` rides
it too**, as `job 3`, and it is drawn last because it is the most recent thing to
have happened to the row.

A stat is only ever drawn for a finished call. An edit's `+N −M` is knowable early and
is deliberately withheld — it is the shape the preview collapses into when the change
lands. A result that never arrived draws no stat at all, because "0 matches" is a claim
about a search nobody made.

When the row is too narrow, the stat is given up first and the target is truncated last.
The stat is dropped entirely unless the target keeps at least 8 cells.

## The clocks on a tool row

Three different figures of time, and never two of them at once.

**Count-up (running)** — the age beside the spinner: `12s`, `1m 4s`, `12m 30s`. Spaced
(`1m 5s`, not `1m05s`) because it is read while it moves. Nothing is drawn under 1
second.

**Countdown (a bounded call)** — only `bash`, and only a foreground `bash`, is bounded.
More than 10 seconds out, the bound is stated beside the age as `1m 12s / 2m 0s`, dim.
Within **10s** the remainder replaces the bound: `1m 52s · 8s left` in the warn hue.
Within **5s** the remainder goes to the bad hue. Only the remainder is tinted; the age
stays dim. It rounds up, and a passed bound says `0s left` rather than a negative
number.

**Elapsed (finished)** — the call's own duration, measured where the call ran, never the
time its announcement spent streaming. Nothing under **100ms**, because `0.0s` on every
row is a column read for nothing. One decimal under 10s, whole seconds under a minute,
`2m12s` above.

Calls in one batch run together and their results are handed back together, after the
last of them returns. Each row's clock still stops when **that** call finishes: a `cd`
that took 5ms says so and stops counting while the `go build` beside it is still going,
rather than counting the build's minutes onto its own line. The row keeps its spinner
until its result lands, because until then nothing here knows whether it worked.

A call with no timeout gets no countdown, no bound and no colour — chrome implying a
deadline would be inventing one. A foreground `bash` always has one: a `timeout` that is
missing, null or zero counts down against the 120-second default the command will really
die on, never a number nothing is going to enforce. A background `bash` has no bound,
because it runs as a job. A turn that ended with a call unresolved stops every clock, and
it stays stopped:
the row takes the dim `·` mark at the moment the turn ends and keeps it through every
turn after, so an abandoned call can never start spinning again.

## Why does a tool row still say running, or seem stuck

Three different things look the same and only one of them is a problem.

**It is genuinely still running.** A call's row spins until its result arrives, and some
calls take minutes: `view_image` asks another model about the picture and is bounded at
**10 minutes**; `generate_image`, `generate_video` and `speak` are whole renders. The
count-up beside the spinner is the honest answer to "how long have I been waiting".

**Its batch has not finished.** When the model asks for several calls at once they all
start together and they all report back together — the results arrive after the **last**
one of them returns. So a fast call sitting beside a slow sibling spins for as long as
the slow one takes. Two `view_image` calls in one message settle as a pair, and both
settle: neither is waiting on the other's row.

**The turn ended around it.** An interrupt, a lost connection, or an attempt the session
retried can leave a call with no result coming. That row stops where it is, keeps a dim
`·`, and shows no duration — nobody measured one. It is not marked failed, because
nobody watched what became of it.

If a row is spinning and the state word at the bottom says `idle`, that is a bug worth
reporting: nothing spins on an idle session.

## What happens when the countdown runs out

Not a kill. A foreground `bash` command that reaches its bound is handed to the
background and **keeps running**: the row finishes normally, its result is the one
line `still running as job 3; log at …`, and the turn carries straight on.

So the coloured last seconds are a warning that the command is about to leave the
turn, not that it is about to be destroyed. Nothing is thrown away and nothing is
run twice.

`ctrl+g` does the same thing early, on purpose — see the keys page. A row you sent
away that way keeps its spinner until its result lands, and gains a dim `job 3`
beside its other trailing marks. The full account of both is on what-i-can-do.

## Seeing more of a tool call

Click the row, or select it with `↑`/`↓` and press `enter`. `ctrl+o` on a capped block
lifts the cap.

What you get, per tool, each with its own line cap:

| tool | what opens | cap |
| --- | --- | --- |
| `edit` | the path, then a unified diff | 40 rows |
| `write` | the content | 20 rows |
| `read` | the returned chunk | 30 rows |
| `bash` | the command whole and highlighted, uncapped, then the output; `exit N` in the bad hue at the foot when it failed | output 30 rows |
| `grep`, `find`, `ls` | the listing | 30 rows |
| `generate_image` | the picture itself, in colour, then its whole absolute path — or, where no picture can be drawn, that path alone | picture 20 rows |
| `view_image` | the picture itself, then what the looking model said | answer 30 rows |
| anything else | the arguments, then the output | 30 rows |

Rows truncate rather than wrap — "first 30 lines" has to mean thirty rows on screen or
it means nothing. The dropped remainder becomes a clickable `… N more lines` row, which
is a different click target from the row above it: one lifts the cap, the other closes
the call. Once you have pressed "more", that call has no window at all. An expansion
with nothing in it draws a dim `—`.

An unfinished call that you open shows its preview if it has one. Otherwise, for `bash`,
it shows the command — readable before it finishes, which is when you most want it —
plus one animated line reading `queued`, `waiting for you` or `running`, with the
count-up replacing the pulse once there is one.

Before a mutating call runs, a preview block appears under it on its own, with no click
and no waiting. It answers for exactly two tools: `edit` (a unified diff with 2 lines of
context each side) and `write` (the content). Its header is one word — `pending` in the
question hue while nothing has started or while you are being asked, `applying` in dim
once execution begins. The header changes, the rows do not, so nobody reads the same
diff twice. It is capped at **12** rows, or **4** at phone width, with the remainder
offered as `… N more lines`. There is no preview for `bash` (the command is already on
its own line in full) or `read`.

## Seeing the image itself in the terminal, in colour

**You do not have to do anything.** The moment a `generate_image` or `view_image` call
finishes, the picture is drawn under its row, in colour — no click, no key, no flag. It
is there in the conversation as you read it, and it is there in a task's room too.

It is drawn out of **half-block characters**: one cell carries two stacked pixels, its
top colour and its bottom one, which is how a terminal shows a photograph with nothing
but colour codes. No image protocol is involved and nothing is written outside the
frame, so the picture survives every repaint, scrolls with the conversation, and works
over ssh and inside tmux the same as anywhere else.

The picture under a row is a **thumbnail**: at most **12 rows** tall, or **4** at phone
width — the same ceiling the live preview takes, because a block nobody asked for should
not take the screen from the conversation it appeared in. It carries no heading, no
border and no caption. Nothing is held back behind a `… N more lines` foot either: the
whole picture is drawn into however many rows it has, because half a picture is not half
an answer.

**Open the row for the bigger look** — click it, or select it with `↑`/`↓` and press
`enter`. There the picture is drawn again at up to **20 rows**, and under it, dim, one
line: **the file's whole absolute path**, then its size in pixels and on disk —
`/…/harbour.png · 1024×768 · 1.4 MB`. At phone width the same gesture opens the call
over the whole frame. A `view_image` expansion also keeps what the looking model said,
under the picture.

The picture keeps its own shape and is **never enlarged** past its real pixel size: a
16-pixel icon is drawn 16 cells across, because blowing it up would be sixty columns of
blur claiming to be detail.

**png, jpeg, gif and webp** are drawn — the same four `view_image` will read.

A picture is **decoded once and kept**, so a row you scroll past, a row you leave open
and a row that repaints ten times a second all cost the same after the first frame.
Resize the terminal, switch your theme, or overwrite the file on disk and it is drawn
again — those are the only three things that make it re-read anything.

## When the image preview is not drawn — why don't I see the image, and where did my generated picture go?

If a row shows only words — something like
`/home/you/book/cover.jpg — 768×1376 jpeg, 776.9KB, generated on <model>` — then no
picture could be drawn, and **that line is the answer instead**: it names the file
**whole and absolute**, so you can open it yourself from anywhere.

The reasons, in the order they are worth checking:

- **Your terminal is below 256 colours**, or colour is off. The sixteen ANSI colours are
  your own theme, and a photograph painted out of them would be a lie about both.
- **Your terminal cannot draw box-drawing characters** — no UTF-8 locale, or no `TERM`
  at all. The half block is the whole technique.
- **Screen-reader mode**, where rows of block characters read aloud are rows of nothing.
- **The row is under 8 columns wide.**
- **The call has not finished.** A picture is drawn when the file exists, and
  `generate_image` writes the file last.
- **The file is missing, unreadable, over 24MB, over 64 megapixels, or not one of the
  four types** — a `svg`, a `tiff`, a `pdf`.

In every one of those the row is **exactly what it would have been** — its result line,
or what the looking model said — and never an error. Opening the row in those cases
gives you the file's whole absolute path on its own rows, wrapped rather than cut,
because a path with an ellipsis in it cannot be clicked, copied or pasted.

Where the files themselves land is on the "making pictures, audio and video" page.

## Opening a tool call on a phone-width screen

Under 60 columns a tool row is one line, never two:

```
├─▶ ◌ edit  loop.go
├─▶ ⠋ bash  go test ./…              12s
├─▶   edit  loop.go       +12 −4     1.2s
╰─▶ ✗ bash  go build ./…  exit 1     1.2s
```

The state glyph leads in a fixed 2-cell gutter — a column is the one thing a narrow
screen reads well, and it costs the target two cells flat. The target sheds its
qualifier and keeps its tail: a `cd …` prefix is dropped rather than dimmed, a `grep`
keeps only its pattern, a path collapses to its basename and the line range goes. The
clock is one figure — a duration or an age, never a bound stated beside an age. The rail
stays at four cells; it is worth more on a narrow screen than on a wide one.

Tapping the row, or pressing `enter` on it, opens the call over the whole frame. The
head is the tool's name on the left and `esc close` on the right. Then the target whole
and wrapped — the row showed a basename, and "elided from what?" is the first question
the sheet answers — except for `bash`, whose target is the command and is the first
block of the body. Then a rule, the body (the same rows the inline expansion draws, from
the same per-tool table, uncapped by the phone tier), another rule, and a keys line:
`esc close · ↑↓ scroll`, or `esc close · ↑↓ scroll · tap … for the rest` when there is a
cap to lift.

Every tap target is a whole row. Head and foot both close; the `… N more lines` row
lifts the cap; padding rows answer nothing.

The sheet is derived from the width every frame. Drag the terminal wider and the call
expands inline; narrow it again and the sheet comes back. It is not closed on the way
out of the tier. If the call it named has gone, it draws nothing and the frame falls
through to the conversation.

## Colour, and how it degrades

Four rungs, detected once from what your terminal says it can do:

- **TrueColor** — the palette exactly as authored.
- **ANSI256** — the nearest xterm index. Nothing ever resolves into indices 0–15, which
  are your own terminal theme.
- **ANSI16** — **no hue at all**. The sixteen are your theme, and its reds and greens
  are loud by definition, so this rung answers with weight instead: bold for what leads,
  faint for what recedes, plain for the body. Every distinction the design draws in
  colour is also drawn in text (`+` and `−`, `✗`, `exit 2`), so nothing is lost but the
  tint.
- **NoColor** — no escape sequences at all, weight included.

Backgrounds — the hover band, the selection band, and the chip behind a recognized slash
command — are drawn only at ANSI256 and above. There is no weight that means "this row",
so a slash command falls back to bold and a hovered row to nothing.

If a colour on this surface could be described as "bright", it is wrong.

## What each colour means

Every colour aforge draws is a role, and each role has one job.

The roles: ink for the body and every tool's target; accent for your own `› ` glyph and
your whole message, the tool rail, and aforge's own headings; muted for tool names and
the spinner; dim for everything the surface says about itself — stats, notes, hunk
markers, the status line; add and del for a diff's `+` and `−`; bad for `✗`, `exit N`
and an overdue context meter; warn for a bound about to be reached and only that; and
violet for **the question hue and nothing else**.

Violet is spent on the moment aforge is waiting for you and on nothing else: the consent
question, its glyph, its choices, the status word, and the legend while one is up. Its
whole value is that seeing it anywhere means one thing.

Hue carries identity; weight carries markdown. Your message is accent whole behind its
own glyph, and nothing the model writes is ever painted accent. On a 16-colour terminal
that accent degrades to bold, which is the only marker left there.

One thing inside your own words is painted differently: a slash command aforge
recognizes — `/task`, `/compact`, `/clear` — wears a **chip**, the selection band's tint
behind the letters of the command, in the message box and in the sent message alike. A
background is not a role on this surface, so a lifted run of cells reads as "this is not
prose" wherever it turns up. Nothing the model writes is ever chipped.

Six mid-tone hues form a separate identity ring, spent on exactly one cell: the glyph at
the head of a task row. No role ever paints that column, so a ring hue cannot be misread
as a state. The ring has no 16-colour tier — below the 256 rung the glyph alphabet
carries identity alone.

## What the task column looks like: dulled rows and its footer lines

The right-hand task column is read at a glance, so it is drawn as one bright thing and a
lot of quiet ones.

- A **running** task's name is in ink, the body colour. **Idle, parked and finished**
  names are muted — a step quieter — and the room you are standing in is the one name in
  the accent and bold, with a colour band across its whole row.
- The tree connectors (`├─ `, `└─ `, `│  `), the id at the end of a row (`#7`), every
  detail line under a title, and every footer line are dim. The one loud exception is a
  branch that did not merge: `conflicted · task/fix-nil` is in the bad hue.
- The state glyph at the head of a row takes its own identity ring hue and no role
  colour, so a glyph can never be misread as a state paint.

**Rows that are running never scroll off**, however long the list gets: they are pinned to
the top of the column and everything under them scrolls.

**Under this session's rows, the project's record is a dulled footnote.** After a blank
line comes the word `earlier`, dim, and under it at most six one-line rows of tasks earlier
sessions ran — glyph, the `⧉` mention mark, the title muted, the age dim. They are filled
into the rows the session's own list did not need, so they can never push a running task
off.

**Dulled does not mean inert.** Those rows take the cursor (`ctrl+t`, then `↓` past this
session's last task) and the hover step like any other row of the column, and `enter` or a
click puts that task's name in your message box. What they never take is the selection
band: that background means "this is the room you are standing in", and there is no room
to stand in for work another conversation ran. The `ctrl+. — view more` line below reaches
the rest of them.

**And dulled is a claim about the work.** A row that still says `running` is drawn undulled
and reads `running` only while the window that started it is open and still holds it;
otherwise the glyph becomes a dot `·` and the row reads `incomplete` — the window went and
nothing finished the work. Running work belonging to *other* windows is not on the column
at all; `/history` carries that.

The footer is up to three dim lines of totals — `Σ $1.42 · 312k tok`, `3 running · 1 needs
you` — and then up to three more dim lines, each of which is a button as well as a key:

```
ctrl+. — view more
w · click seam — widen
ctrl+g — hide
```

`ctrl+. — view more` is drawn only when the full-screen task page (`/history`) has
something this column cannot give: a folded family, or work an earlier session ran.
`w · click seam — widen` appears only while a title is actually being cut by its own
indent. `ctrl+g — hide` is always there.

## Light terminals, and why there is no theme setting

There are two fully authored palettes. The dark one is invisible on a white page, so a
light one exists with the same law and the opposite move: what carried by being lighter
than the background now carries by being darker. Ink darkens, accent darkens, and **dim
goes lighter** — the meta tier recedes toward the page. The question hue is inverted
rather than dimmed, because a question has to lead on a page too. No two roles resolve
to the same 256 index.

**The theme is auto-detected only. There is no theme setting to change.** aforge reads
`COLORFGBG` and nothing else — its last field is read as an ANSI index, where 0–6 and 8
mean dark and everything else means light. **Unset means dark.** The code can accept
`dark`, `light` or auto, but the settings row that would carry your choice does not
exist yet. It is a stated seam, one call away from being wired, and until it is wired
the answer comes from the environment.

OSC 11 is deliberately not queried. That would be a round trip inside a constructor that
must not block, to decide something a person who cares can pin.

## Box-drawing glyphs, and when they are dropped

Whether your terminal can be trusted with box drawing is a separate question from
colour, and it can only be answered **no**. There is no escape sequence that asks "can
you draw `├`", so the answer is yes unless something says otherwise.

Two things say otherwise: `TERM` is unset or set to `dumb`, and a locale (`LC_ALL`,
`LC_CTYPE` or `LANG`) that is not UTF-8. No locale set at all is the C locale, which is
not UTF-8, so that means ASCII too.

When ASCII is on: the tool rail becomes `+-> ` and `| `, the meter becomes `#` and `-`,
and the context sparkline is not drawn at all.

There is deliberately **no environment variable to pin this**. A human override belongs
in display settings, and inventing one would be a second door onto one question.

## Screen readers and plain terminals

There is a linear tier, and it is three subtractions.

**No animation.** A spinner read aloud is a word repeated forever, so every spinner
becomes a still `*`, the pulsing ellipsis becomes its last still frame, and a forming
tool row stops pulsing.

**No hover.** A pointer's shadow is nothing to a reader, so no row ever brightens under
the pointer.

**No glyphs.** Every shape-carrying marker gets an ASCII stand-in that carries it by
name: `> ` for you, `-> ` for a fold, `x` failed, `.` idle, `o` queued, `*` running, `>`
for the status deck's more mark, `v` for the jump-to-latest arrow.

The thinking window's gradient collapses to flat dim, because a gradient is an animation
held still.

What the linear tier **keeps**: the colours (a screen reader ignores them, and a sighted
reader loses nothing) and the **selection band**. The cursor is a position in a list, and
that list exists whoever is reading it. Only the hover is dropped.

## Two other things that move on screen

**The "still working" ellipsis.** When a turn is running and nothing else on screen is
moving, two spaces then a pulsing ellipsis cycles `·` → `··` → `···` in accent, about
300ms a step. After the stream has said nothing for **10 seconds**, ` · still working`
is appended in dim. It is suppressed entirely while text is actively streaming, while
any tool call is spinning, and while a sub-harness run has a step on the row under it
(see *Saved shapes of work*) — two answers to "is this alive?" is one too many. It says
"still working" and never "retrying": this screen does not know whether the session is
retrying, only that the stream has been silent. When the reply has not started at all —
a request is out and nothing has come back — the more specific waiting line below
replaces this suffix instead of sitting beside it.

**The compaction mark.** A compaction is drawn while it runs and left as a rule once it
lands, so the conversation never silently loses its middle. Running, it reads
`⠙ compacting ~84k tokens · 6s` — a braille spinner on the same grid as the tool
spinners, dim, with a count-up. Settled, it becomes a centred rule:
`───── ⚭ compacted from ~84k tokens · took 6s ─────`. The duration is dropped under one
second. It is never painted the question hue, because nobody is being asked anything.

## Why the reply is slow to start, why it says "waiting for" a model, and whether it is stuck

Between you pressing enter and the model's first word there is a gap, and it is sometimes
long — twenty seconds, a minute. A pulsing ellipsis claims exactly as much at second one
as at second fifty, so past a few seconds it starts saying what it is waiting on.

For the first **4 seconds** the line is the bare ellipsis. A fast reply never shows a
clock. Past 4 seconds it grows a dim tail naming the model and counting up:

```
  ··· waiting for kimi-k3 · 12s
```

Past **30 seconds** it says the plain fact outright:

```
  ··· waiting for kimi-k3 · 47s · nothing has come back yet
```

The model is its **basename**, the way the status deck's chip spells it — `kimi-k3`, not
`moonshot/kimi-k3`. When there is no model name to show, the line reads `waiting · 12s`.

**What it claims, and what it does not.** It claims only that a request went out and the
stream has said nothing since. It never says "retrying", "the network is slow", or "the
model is thinking" — this screen cannot see the wire and does not pretend to. So
`waiting for kimi-k3 · 47s` is not a report that anything is broken. It is aforge saying
it is still there and still waiting, which is the one thing a bare ellipsis could not tell
you apart from a hung program.

**Is it stuck? Is it frozen?** A clock that is counting up means the program is alive and
painting; a clock that has stopped means it is not. Nothing here kills the request on your
behalf and there is no cancel-and-ask-again — the wait runs until the answer starts, the
session's own retries resolve it, or you stop it. `esc` interrupts the turn.

**It never runs under a tool call.** A tool that is executing has its own spinner and its
own count-up, and the ellipsis stands down for it entirely. This clock is only for the
window between a request going out and the stream first speaking, so after a three-minute
`go test` the request that follows starts the clock at zero rather than inheriting the
call's runtime.

## The dim line under a finished turn

Under each finished turn there is a dim right-aligned receipt:

```
· 14:02 · 2m12s · 3 tools · $0.04 ·
```

Every field but the clock is dropped when its figure is zero. The receipt is frozen when
the turn commits; only its paint follows the clock, muted for the first hour and dim
after that. Pressing `ctrl+o` on a turn spends the first field on the whole RFC3339
timestamp instead of four digits.

Between sittings a centred rule carries the time or the date. It is drawn only at a gap
of **10 minutes** or at a day boundary.

No receipt is drawn below width 8, and none if the assembled line would be wider than
the frame. Receipts run over the conversation only — a room's page never draws them.

## How often the screen repaints

The repaint ceiling is decided once, when the session starts, from the environment.

Locally it is about 30 frames a second. If `SSH_CONNECTION` or `SSH_TTY` is set, the
ceiling becomes three times the local interval — about **10 frames a second** — so a
remote session spends less of the link on repaints.

Nothing about what is **drawn** changes. Animations are counted in frame slots, not in
frames, so a spinner still goes round in the same second and a half either way.

There is no setting for this and no round-trip probe. It is read once and never re-read.

## When a row brightens under the pointer

The row under the mouse pointer brightens. It is the last pass over a finished list of
rows, so nothing else has to know about it.

A hovered conversation row gets a background band padded to the full width. A hovered
fold line brightens its arrow. A hovered table foot, and the jump-to-latest chip, go
**accent** rather than taking a background — a highlighted rectangle would be the one
boxed thing on a surface with no boxes.

**Selected beats hovered.** A row that is already the thing you are looking at — the
roster row and the strip chip of the room you are standing in, the cursor row of an
open list — wears the selection band, which is the hover background one step louder,
and the pointer moving over it changes nothing. The two cannot both be drawn, and "you
are here" is the one still true when the pointer leaves.

There is no hover at all in the screen-reader tier. Terminals below ANSI256 get no hover
background either, because there is no weight that means "under the pointer".

The mouse is aforge's by default for the whole session, which is what makes the click
targets on this screen work. `ctrl+s` hands the pointer back to the terminal so you can
drag-to-select with it, and takes those targets away until you take the mouse back.
