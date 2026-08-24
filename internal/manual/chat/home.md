# Home — every project and session on this machine

## See all my projects — /home

Type `/home`. It takes the whole screen and shows **every project on this machine and
every session in them**, not just the folder this window was started in.

The top line reads `aforge` on the left, with the machine's own vital signs dim on the
right — `on watch · 4 orders · 3 working · $1.10 today · fri 9:41am` (its own section below). The way out
is named at the foot, on the hint line: `esc close`. `esc` puts you back in exactly the chat
you came from, untouched — nothing was closed and nothing was sent while you were looking.

There is no argument form. The screen is how you name what you want; a command that took a
project name would be asking you to type out the very thing home exists to show you.

The left column is **two tiers**: three projects drawn open — the one this window is in
first, then the two you spoke in most recently — and every other project folded to one line
each under a dim `elsewhere` rule, `enter` away from opening in place.

Home is a glance you take, not a place you live. It does nothing on its own: no
notifications, no charts, no history graphs. You open it, you see where things stand, and
you leave.

## Why did a dashboard open when I started aforge — home greets you

**Home is the first thing you see when you open aforge.** The conversation your launch
would have opened is loaded and waiting underneath it: `esc` drops straight into it. In
effect the launch is the launch you always had, with home already open on top of it.

**The cursor starts on no row at all** — home opens at rest, so the first thing you see is
the machine's own card rather than a highlighted row (see *Home opens at rest*). `↓` walks
into the projects list, `tab` into `needs you`; `esc` goes on with what you were doing.

Nothing about *which* conversation opens is changed by this. The door picks it exactly as it
always did — this directory's most recently spoken-in chat, or a fresh one — before home is
drawn at all.

It greets you only when it has something to say. All of these have to be true:

- You opened aforge **without naming a conversation**. `aforge` or `aforge chat`.
- The machine holds **a conversation other than the one this launch opened**. Somewhere
  else to go, in other words.
- It is a real terminal session — not `--once`, not `--host`.

When home greets you there is no welcome box: home's left column already lists every
conversation the box's `recent sessions` would have, and more.

## Skip the home screen — launching straight into a conversation

Four ways, and each of them is you saying which conversation you mean:

| What you run | What you get |
|---|---|
| `aforge chat --session <path>` | that conversation, no home |
| `aforge resume` | the session picker, no home |
| `aforge chat --once "text"` | one reply, printed; no surface at all |
| `aforge --host <machine>` | the far machine's session, no home |

And on a machine with only one conversation — a first run — home does not greet you.
There is no setting for this and no flag to turn it off: whether home greets you follows
from how you launched and what the machine holds, both of which answer themselves.

Not being greeted is not the same as being out of reach. Once you are in a conversation,
`/home` — or `space` twice on an empty box — opens the screen whenever you want it, on a
machine with one conversation and on one with none; what opens there is an empty home
rather than nothing (see *Home is empty — what an empty home shows*).

## Everything I have ever worked on — what home shows

Two columns, no borders — three of them on a frame 110 columns or wider (see *The three
columns on a wide home*).

**On the left**, the projects on this machine, in **two tiers**. **With nothing typed the
list hangs from the top of the frame**, as a page you are reading should:

```
  aforge-v2                          Pricing Research
  ▲ Pricing Research  4m
  ⠋ Port the Picker  12m             aforge-v2 · ~/src/aforge-v2
  ✓ Import Cleanup    3h
  ▸ …3 more, quiet…                  open in another window · waiting on you
                                     can I run: rm -rf build/
  pricing-api
  ○ Log Rotation     20d             Port the Picker                          2h
                                       the roster resumes cleanly · 14 files
  ─ elsewhere ────────────
  ▸ wisp        6 · ▲ 1 waiting      Fix the nil-map                          3h
  ▸ site-gen           2 · 3d          the parser handles nested tags
  ▸ notes              1 · 20d
```

**The top tier is three projects, drawn open** — a dim heading, its conversations under
it, the things keeping an eye on it, and the quiet fold. They are **the project this
window is in, first**, and then the two projects you spoke in most recently.

**Everything else folds to one line each**, under a dim rule that says `elsewhere`. That
rule is the only rule on the screen, and what is under it is not another project — it is
all the rest of them.

Home was an unorganised wall before this: every project on the machine got a heading and
four rows, most of them saying `elsewhere`, and the one project you could actually act in
was wherever recency happened to put it.

The cursor opens on **no row at all** — home opens at rest, with the machine's own card on
the right (*Home opens at rest*). Once you move it, the preview on the right follows the
cursor — **or follows your mouse pointer**, whenever the pointer is resting on a row of the
list. The pane shows the row under the pointer while the pointer is on one, and the
cursor's row otherwise.

A line is a glyph, the session's name, what it has going on, and how long since you last
spoke in it — or `open` where **this** terminal is holding the conversation behind the one
on screen, or `another window` where a *different* terminal is sitting on it and this one
therefore cannot open it (see below). The glyphs: `▲` it is stopped waiting
on you, `●` something is running in it, `◌` work was left
unfinished, `✓` work landed since you last looked (see *What landed while I was away*),
`○` at rest. **Exactly one row on the whole screen turns** — the most recently active one,
which wears the spinner instead of its `●` (see *Why does only one row spin*). In
screen-reader (linear) mode nothing animates and the glyphs are `!`, `*`, `o`, `+` and `-`.

The left list stays deliberately calm — every row is dim except the one the cursor is on,
which takes the highlight. The one exception is `waiting on you`, which is brought up out
of the dim wherever it appears, because a screen whose whole job is triage cannot render
its most urgent fact in the same grey as an age.

Inside a project the order is **what wants you first**: sessions stopped on a question,
then ones with work running, then ones with work left unfinished, then the rest by when
you last spoke. Quiet ones past the first four collapse into one dim line,
`…3 more, quiet since 2d`.

**The list never touches the rule above the box.** One blank row always sits between the
last line of the list and the foot of the frame, in both of home's shapes.

## What needs me — the needs you strip at the top of home

Above the projects, home draws a strip labelled `needs you`. It is **everything on this
machine that has stopped and is waiting on you**, from every project at once, whatever
kind of thing it is:

- a conversation in another window stopped on a question — a command to approve, a task
  to approve, a card about something standing;
- a task that has **landed and needs your look**, which is work nothing more will happen
  to until you read it — its row says `landed` and is named after the task;
- a reminder or a watch that stopped and wants an answer;
- an `ask here` errand holding a card.

```
  needs you
  ▲ Pricing Research                 beta · 3h
  ▲ keep main green                 alpha · 1h
  ▲ port the parser       landed · alpha · 30m
```

Each row is `▲`, the thing's own name, then the project and how long it has been waiting.
**The longest wait is at the top.** Nothing else re-orders it.

A row here is a **door of the kind it always was**: `enter` opens the conversation, or the
chat that asked for a reminder, exactly as it does on the row further down the screen. The
card on the right is that thing's card, and where the question can be answered from home,
`1`, `2` and `3` answer it from this row too (see *Answer a question from home*).

**A row named after a task opens on that task**, not on the bare conversation — see *What
happens when I press a landed row* below.

**A row appears in the strip and under its project both.** They are two views of one live
thing, not two things — pressing `enter` on either goes to the same place.

## What happens when I press a landed row — I clicked a needs you row and it opened the chat, and the task's own record with it

A `needs you` row that says `landed` is named after **one piece of work**, not after the
conversation it ran in. Pressing `enter` on it, or clicking it twice, does two things at
once:

- the **conversation that ran the task opens**, exactly as any other row on home opens it —
  whatever project it belongs to, with the conversation you were in left running behind it;
- and **that task's own record card is raised in front of it**: what the work was called,
  what it came to, what it cost, how many files it changed, the branch or worktree it left
  them on, and the last thing it said.

`esc` backs out of the card to the project's list of work, and `esc` again leaves the
conversation on the screen. `ctrl+.` closes the whole page from inside the card.

This matters in a conversation that has run a lot of tasks: the row names one of forty, and
landing on the conversation alone would drop you at the live edge of a transcript with no
trace of the thing you pressed.

**A row that is not a task opens the plain door.** A conversation stopped on a question, a
reminder, an `ask here` errand — those stand for no single piece of work, so nothing is
raised over them. And a row this window cannot open still refuses first: a conversation
another window is sitting on says `open in another window — go there, or start a new
conversation here`, and no card is raised over a conversation you never walked into.

## How do I clear a needs your look row — settling work from the conversation

Work that landed needing a look **stays on the strip until you decide about it**, however
many days that is. It is not stale and it does not age out: nothing more will happen to
that work until somebody accepts it or sends it back.

Two ways to settle it, and they are the same door:

- **the landing card in the conversation** — `[a] accept`, `[l] look again`, `[n] not
  right`, `[d] decide these for me`;
- **just say so.** "accept task 7", "that one isn't finished", "have another look at task
  7" — aforge settles it through its `tasks` tool. Whichever is used first wins; the other
  says `already answered`.

Accepting merges the task's branch and unblocks everything queued behind it. The moment it
is settled the row leaves `needs you`. The tasks page has the whole of it under *Why is the
task waiting for me*.

## What is running everywhere — the moving strip

Under it is `moving`: **everything in flight anywhere on the machine**.

```
  moving
  ● Odysseys Wave 4                 gamma · 8m
  ▸ …2 more
```

A conversation is here when it has tasks running, and also when it is simply **mid-turn** —
the model thinking or a tool out, with no task ever made. Conversations this terminal is
holding open behind the one on screen count too, and so do a watch that is firing and an
errand that is thinking.

The order is **busiest first** — most tasks running — and the longest-running settles a
tie. The row is `●`, the name, the project, and how long it has been going. **Five rows,
then `▸ …N more`**, which `enter` or a click opens in place, exactly like every other fold
here; the rest are all in the list below anyway.

Most of these rows hold a still `●`. One of them turns — see *Why does only one row spin*.

## Why does only one row spin — the one spinner on home

However many things are moving on the machine, **exactly one row on home animates**: the
one that started most recently. Every other live row — in `moving`, under its project, an
`ask here` errand mid-turn — holds the still `●` instead.

Two reasons, and they are the same reason:

- **Calm.** Eleven braille cells turning at once in three columns is a screen you cannot
  glance at, and one moving cell says *this machine is working* exactly as well.
- **A flat wire.** Home is otherwise a still page redrawn every three seconds. The frame
  the spinner costs is the same frame whether one thing is running or twenty, so a busy
  machine costs an ssh connection no more than a quiet one.

When the `moving` zone is on the screen, the spinner is in it — that zone's whole subject
is what is happening right now. The zone itself is ordered **busiest first**; the spinner
goes to the most recent, and those are two different questions on purpose.

In screen-reader (linear) mode nothing turns at all.

## Are the strips there when nothing is happening — an empty needs you or moving zone

At **80 columns and wider** the two labels are always drawn, even with nothing under them —
an empty `needs you` is the good news, and a screen whose shape moves every time a task
lands is not a screen you can learn. Under 80 columns, where every row is dear, an empty
strip is not drawn at all.

**At 110 columns and wider a label with nothing under it says what would be there.** One
dim line under `needs you` reads `questions and landed work`, and one under `moving` reads
`turns, tasks and watches` — the kinds of thing that arrive in that region, so a label you
have never seen filled still tells you what it is for. It is a caption, not a row: the
cursor walks straight past it and it goes the moment the zone has anything real to show.
Neither line ever announces that something is missing; nothing on this surface does.

**Typing takes both strips away.** A search is the matches rising out of the box with
`? ask here` and `+ start a new conversation` against it, at every width — no strips, no
headings, no tiers.

A machine with no conversations on it yet keeps both strips wherever a width draws them —
at 110 columns and wider each label stands in its own column with its teaching line under
it — and the places column says `nothing here yet — say something and this fills up`
where the first project's rows will be, over two rows split at the dash. An empty home is
the same screen with fewer rows, never a different screen.

## The three columns on a wide home — needs you, the projects, the card

At **110 columns and wider** the two strips stop standing over the list and take a **column
of their own** on the left. Home is then three columns and no borders — the separation is
made of alignment and space:

```
 aforge                          on watch · 4 orders · 1 working · $1.10 today · fri 9:41
 ──────────────────────────────────────────────────────────────────────────────────────────
  needs you                    aforge                         keeping an eye on
  ▲ approve schema      2h     ● odysseys wave 4      ⠹ 8m    ◦ tests sweep            in 2h
                               ○ rename plan      yesterday   agents
  moving                       hax-sdk                        ⢀⡠⠔⠒⠑⠢⡀⠀⠀⢀⠔⠊⠉⠉⠑⠢⢄⡀
  ● port sweep          8m     ▲ schema migration        2h   ⠊⠁⠀⠀⠀⠀⠈⠑⠒⠁⠀⠀⠀⠀⠀⠀⠀⠈
                               ─ elsewhere ─                  since you left
                                                              ◆ 2 tasks landed        aforge
                                                              today
                                                              3 chats · 5 tasks · $1.10
 ──────────────────────────────────────────────────────────────────────────────────────────
  type to search or start something new · ↑↓ pick · enter open
  ↑↓ move · enter open · tab next zone · esc close
```

- **Left — what needs you, and what is moving.** The same two zones, the same rows, the
  same doors: `enter` opens the thing, `1`/`2`/`3` answer a question on it, a click selects
  it. Rows here are short — the name, then how long it has been waiting or running, with
  the project where there is room for it. The card on the right is that row's card.
- **Middle — the projects.** The list, unchanged: headings, conversations, the things
  keeping an eye on each project, the quiet folds and the `elsewhere` block. It is the
  **widest** column at every width, because it is the one holding everything.
- **Right — the card.** Always a card, never a second list: the row the cursor is on, or
  the machine's own with the cursor on no row.

**`tab` moves between the zones** — `needs you`, then `moving`, then the list, then round
again — and lands on the first row of each. **From rest it enters `needs you`**, which is
the key's whole job: it is named for the zones, so it is the one that takes you to them. A
zone with nothing in it is skipped, because its label is not a row. The foot names the key
at this width: `tab next zone`. `esc` still closes home from wherever the cursor is.

**`→` and `←` cross the columns too, the way the geography reads**: `→` off a row in
`needs you` or `moving` crosses the gutter into the list, and `←` off a conversation in
the list crosses back into the zones. Both land on the **same conversation** when the far
column holds it — stepping across never loses the thing you were reading — and on the
first row otherwise. On a zone's `…N more` line the arrows stay the fold's, opening and
closing it, because the cursor is standing on the fold itself. With something typed the
arrows are the caret's, as everywhere on this screen.

**The first `↓` from rest lands at the top of the projects list**, in the middle — not in
`needs you`, and not somewhere that depends on what the machine happens to be doing. The
middle column is the widest thing on the screen and holds everything, so it is where the
cursor wakes; `←` or `tab` is one key away when what you want is the flank. After that `↑`
and `↓` walk **every** row on the page in reading order — the zones on the left, then the
list — so nothing needs `tab` to be reachable.

**One blank row separates `needs you` from `moving`.** They are two blocks, and a block
boundary on this surface is one empty row.

The narrower shapes are the same screen folded down:

| Frame | What home draws |
|---|---|
| 110 columns and wider | three columns: the zones, the projects, the card |
| 80–109 | the zones as two strips **above** the list, and the card beside it |
| 60–79 | the list alone, full width — no card, no strips over nothing |
| under 60 | the phone shape: one stacked column and a sheet (*Home on a phone*) |

## Which column am I in — the highlighted heading over the section your cursor is in

Home has several regions on one screen, and the row your cursor is on wears a very quiet
background — enough to say *which row*, not enough to say *which region*. So the region
says it too: **the heading over the section holding the cursor is highlighted**, with the
same faint background the row itself wears.

```
  needs you                    aforge
  ▲ approve schema      2h   ██████████████████████████
                             ›● odysseys wave 4      8m ██
  moving                       ○ rename plan   yesterday
  ● port sweep          8m
```

Move into a project's conversations and that **project's name** lights up its background.
Move into `needs you` or `moving` and **that word** does instead. It works the same way on
a narrower frame, where the strips sit above the list rather than beside it.

- **Exactly one heading is marked at a time.** If `needs you` is marked, no project name
  is, and the other way round — which is the whole point: one heading marked in one
  column is the answer to "where are my arrow keys".
- **Nothing is marked when home is at rest.** Home opens on no row at all, so no section
  is marked either; the first `↓` or `tab` puts the cursor somewhere and a heading lights
  up with it.
- **Nothing is marked while you are searching.** With something typed the column is a
  list of matches and its headings are just grouping.
- **The mouse never moves it.** Hovering a row previews its card on the right without
  moving the selection, and it leaves the marked heading where your cursor put it. The
  marked heading always answers *where the keyboard is*.
- **The heading's word does not change colour** — it stays the same dim grey it always
  is. Only the background behind it moves.
- Some sections have no heading to mark. `▸ archive · 4 put away` is a section of one
  line and nothing lights up above it.

## Why is the needs you heading highlighted — the section your cursor is standing in

Because your cursor is on a row inside that strip. Press `→` or `tab` to cross into the
projects list and the highlight moves with you: `needs you` goes back to plain and the
name of the project you land in takes the background instead. It is not a warning and it
is not saying anything about the rows underneath — it is the screen telling you which of
its regions your arrow keys are in.

The same is true when a **project name** on home looks highlighted: your cursor is on one
of that project's conversations. Press `esc` to close home, or walk out of the block, and
it goes back to plain.

## Why are most projects collapsed on home — the elsewhere block

Home opens **three** projects and folds every other one to a single line under a dim rule:

```
  ─ elsewhere ────────────
  ▸ wisp        6 · ▲ 1 waiting
  ▸ site-gen           2 · 3d
  ▸ notes              1 · 20d
```

The three that stay open are **the project this window is in — always first, whatever its
age — and then the two you spoke in most recently.**

A folded line is the fold mark `▸`, the project's name, **how many conversations it
holds**, and then the one thing worth knowing about it from out here:

- `6 · ▲ 1 waiting` — something in there is stopped waiting on you;
- `2 · ● 3 running` — work is running in there right now;
- `2 · 3d` — nothing is happening, so the line says how long since anybody spoke in it.

**Both kinds of row are counted.** A reminder, watch or rule standing in that project that
is stopped on a question counts in `▲ waiting` exactly as a conversation does, and one
firing at this moment counts in `● running` — the number is how many things want you, not
how many chats do. The leading number stays the count of **conversations**, because that is
what opening the line shows you.

**A fold never hides the row this screen exists for.** A project with something waiting or
something running says so on its one line and **sorts above the quiet ones**; the waiting
mark is brought up out of the dim, the same way it is on a conversation row.

**At most eight folded lines are drawn**, and the rest go behind one more fold —
`▸ …4 more` — which opens with the same keys as everything else here.

**And searching sees straight through all of it.** While anything is typed there are no
tiers at all: every project holding a match is drawn open, under its own heading, wherever
it lives.

**`elsewhere` is about the SHAPE of the list and never about a door.** Every row under that
rule opens with `enter` exactly like every row above it — see "Open another project from
home" below.

## Archiving a conversation — put junk away and clean up home

Walk the cursor onto a conversation (or point at it, or click it), then press **`ctrl+e`**,
and that row is
**put away**: it leaves its project's block and home gets cleaner by one line. Put-away
conversations from every project gather under **one folded line at the very foot of the
resting list**:

```
▸ archive · 4 put away
```

Nothing is deleted and nothing moves on disk — the conversation keeps its transcript, its
tasks and its project, and can still be opened. Archiving is a fact about home's list and
nothing else.

**Getting one back:** `enter` (or `→`, or a click) on the archive line opens it in place;
the put-away rows draw as ordinary conversations — newest first, each naming its project —
and `enter` on one opens it exactly as it would anywhere. Press **`ctrl+e` on a row inside
the open archive** and it returns to its project's block — the same key is its own undoing,
and the card's legend reads `ctrl+e bring back` there. `enter` or `←` on the archive line
folds it away again.

**Search sees everything.** Typing into home's box matches put-away conversations along
with everything else — a filter that hid a match would be lying about the machine — so a
conversation you archived a month ago is still three letters away.

The archive line is drawn only when something is in it: an empty archive is no line at
all. At phone width the inbox has no archive line — put-away rows stay out of the
sections, and search or a wider window is where they are found and brought back. There is
no bulk gesture: rows are put away and brought back one at a time, each with one
keystroke.

## How do I open a collapsed project — enter on the ▸ line

**Put the cursor on the project's line and press `enter` or `→`.** It opens **in place**:
the line stays exactly where it is, its mark becomes `▾`, and its conversations, its
standing items and its quiet fold appear under it — the same shape a project in the top
tier has. `enter` or `←` folds it away again, and **clicking the line toggles it in one
press**.

It does not move into the top tier and it does not push a project out of it. Nothing about
which three projects are open changes.

The projects you open by hand stay open for as long as home is up, including across a
refresh — folding is something you did, not something the data said.

**The cursor stops on those lines and never on the rule.** `↑`/`↓` walk over
`─ elsewhere ────` as though it were not there, because it names a section rather than a
thing.

The right-hand pane, while the cursor is on a project's line, shows the project's own
card: its name, and the folder it lives in.

## The line at the top of home — the pulse, on watch, orders, spend today, the clock

The top line of home is the program's name and, right-aligned and dim, what is true of the
**whole machine** right now:

```
 aforge              on watch · 4 orders · 3 working · $1.10 today · fri 9:41am
```

- `on watch · 4 orders` — the **standing orders** active across every project:
  reminders, routines, watches, the things keeping an eye on something for you. The words
  brighten slightly while a check or a firing is actually in flight, the way the status
  line's own `keeping an eye on 2` does.
- `3 working` — how many things this machine has **in flight right now**, everywhere at
  once: task nodes out, conversations mid-turn in another window, an `ask here` errand
  answering, a standing order firing. It is the same count as the rows in the `moving`
  strip on the left, and a conversation with three tasks out counts as three. It shows
  from **one** — one hand working is worth knowing — and disappears entirely at nothing,
  never `0 working`.
- `$1.10 today` — what the machine has spent **since midnight**: the tasks that ran and the
  standing things that fired. It turns amber once you are four fifths of the way through
  your daily allowance, and it never prints a fraction — what the day is allowed is on the
  machine's card, and nowhere else.
- `fri 9:41am` — the day and the time.

**Every segment but the clock disappears unless it is true.** Nothing standing means no
`on watch` at all — never `0 orders` — and a day that has cost nothing says nothing about
money. The clock always draws.

The count on this line and the rows on the machine's card are **one reading**, taken once
every three seconds: the top line cannot say `4 orders` over a card that lists three.

## Home opens at rest — the cursor starts on no row

**Home opens with the cursor on nothing.** No row is highlighted, `enter` has nothing to
open, and the right side is **the machine's own card** — what is keeping an eye on things,
what happened since you left, what the day has come to. The morning glance is the default
view rather than somewhere you navigate to.

**The first `↓` lands at the top of the projects list** on a frame 110 columns or wider —
always, whether or not anything is waiting. The middle column is the widest thing on the
screen and holds everything, so that is where the cursor wakes, and it wakes in the same
place every morning so the first key can become a habit. The flanks are one key away: `←`
crosses into `needs you` and `moving`, and `tab` — the key named for them — enters `needs
you` from rest exactly as it always did.

Below 110 columns the zones are strips standing **over** the list rather than a column
beside it, so the first `↓` walks down into the top of `needs you` there, which is what
that shape reads like. Where nothing is waiting the strip has no row to stop on and the
key carries on into the list.

`↑` off the top row of the list comes back out to rest again, and the three-second rescan
leaves the cursor where you left it either way. On a frame too narrow for a card (under 80
columns) home opens on the conversation this window is in, because there is no card there
to open onto.

`esc` from rest does what `esc` always does here: back to the conversation you came from.

## The machine's own card — what the right side shows with the cursor on no row

This is the card home opens on, and the one you get back by pressing `↑` off the top row of
the left column: the cursor **leaves the list**, nothing is highlighted, `enter` has nothing
to open, and the right side is **the machine's own card** — the morning glance. `↓` walks
straight back into the first row. The card stays put while you read it — the three-second
rescan leaves the cursor off the list.

It is four bands, each drawn only if it has anything to say:

```
keeping an eye on
◦ tests sweep                in 2h
◦ weekly review            mon 8am

agents · in flight, the last few minutes
▁▁▁▂▂▃▅▇▇▅▃▂▁▁▁▁▂▂▂▁▁▁

since you left
◆ 2 tasks landed             aforge
◆ the cert expires in 9 days hax-sdk

today
  3 chats · 5 tasks · $1.10 of $5.00
```

- **`keeping an eye on`** — every active standing order on **every** project, soonest
  first, each with its next due time (`in 2h`) or the cadence you asked for
  (`mon 8am`). Anything **waiting on you** is deliberately not here: that is a job for you
  rather than a thing being watched, and it belongs with the rest of what needs you. Paused
  and stopped things are not here either. Past four, the rest fold to
  `▸ …3 more keeping an eye`.
- **`agents`** — a small **chart**, one row of bars, of how many things this machine has
  had running over the last few minutes, oldest on the left and **newest on the right**.
  The heading says what it is — `agents · in flight, the last few minutes` — and the chart
  itself prints no number: the figure is on the pulse line at the top of the screen
  (`3 working`) and this is the shape it came from. See below for the whole of how it
  behaves.
- **`since you left`** — everything that happened across all your projects while you were
  not looking: news a standing thing left in a conversation or a project, and work that
  landed after you last spoke there. It is the same list the phone-shaped home shows under
  the same words. **Every row is a door**: click one and the conversation it names opens,
  exactly as clicking that conversation on the left would. Past four rows, `▸ …N more
  things`.
- **`today`** — the local day: `3 chats · 5 tasks · $1.10 of $5.00`. Chats are the
  conversations you spoke in today, tasks the work that ran today, and the money is what
  that work cost. `of $5.00` is your **daily allowance** — the same budget everything
  standing is held to — and this is the only place on the screen it appears.

`→` on the machine's card opens every fold on it, and `←` closes them again, exactly as
they do on any other card here.

**Nothing that is zero is drawn.** A day with no work in it says `2 chats` and stops; a
machine nobody has touched today has no `today` band at all — never `0 chats · 0 tasks ·
$0.00`. A machine with nothing standing, no news and an untouched day shows an empty right
side, which is the good news said with space.

## The little chart on home — the agents band, and what the bars mean

The `agents` band on the machine's own card is the one **chart** anywhere in aforge: one row
of bars tracing how many things this machine has had running over the last few minutes.

```
agents · in flight, the last few minutes
▁▁▁▂▂▃▅▇▇▅▃▂▁▁▁▁▂▂▂▁▁▁
```

- **The heading says what the shape is.** `agents · in flight, the last few minutes` — the
  chart is a mark and the clause beside it is the word that says what the mark is about.
  There is no figure in that clause, on purpose: this band prints no numbers at all.
- **One bar is one reading**, taken every three seconds — the same beat home rescans the
  disk on. Sixty readings fit, so the chart is about **three minutes** wide. Oldest on the
  left, **newest at the right**.
- **The bars fade backwards in time.** The newest bars, on the right, are drawn in the same
  quiet colour aforge paints everything that is working right now; older bars step back
  through fainter shades toward the background. That gradient is the only thing the colour
  means here — it tells you which end is which. It never changes colour with the count, and
  on a terminal with no colours to spend the whole row is drawn in one shade.
- **The tallest bar is the busiest moment in that window** and the shortest is nothing at
  all. There is no fixed ceiling on how many things a machine may run, so it scales to its
  own peak: the chart is about **change**, not about size. The size is the pulse line's
  `3 working`, said once, at the top of the screen.
- **It moves because the work moves.** There is no animation in it. While something runs
  the readings differ and the line slides one bar every three seconds; while the machine is
  quiet the readings are all the same and the line lies **flat and still** along the floor.
- **It disappears when it has nothing to show.** A machine that has run nothing for the
  whole window draws no `agents` band at all — not a heading over a flat line on the floor.
  The first thing that runs brings it back.
- **And it does not draw half a chart.** The readings are kept only in memory, so for the
  first half-minute of a fresh aforge there are two or three of them — which is a smudge
  rather than a shape. The band stays away until it has enough readings to draw a line you
  can read, and then it appears.
- **The readings live only in this run of aforge.** Nothing is written to disk and no
  history is kept between runs: quit and start again and the chart is absent until the next
  few beats fill it in.
- **It needs the room to be a chart.** On a frame too narrow for a card at all (under 80
  columns) there is no `agents` band, and the count on the pulse line is what you keep. In
  plain-text mode and with fancy glyphs turned off it is not drawn either — a row of bars
  read aloud is a row of noise — and again the count is what stays.

## The pane on the right of home — the preview of the session under the cursor or pointer

**On the right** is a preview of one row of the left column: **the row under your mouse
pointer while the pointer is on one, and the row the cursor is on otherwise** (its own
section, below). It is read top to bottom as bands separated by blank lines — no rules and
no borders anywhere:

1. the conversation's **name**, the brightest text on the screen and the same treatment the
   highlighted row on the left wears, so the eye travels between them;
2. one dim line of **where it is** — project · path. That line is a **link**: cmd+click it
   (ctrl+click on Linux) and the project's folder opens, in the terminals that make
   hyperlinks. A folder that is no longer on this disk is named and not linked;
3. what it is **doing right now**, and — if it is stopped on a question — that question, in
   full;
4. the **work it ran** — the tasks, name first, with what each came to underneath (its own
   section below);
5. the **last thing said** in it;
6. a dim line of **facts**: `touched 12 files · spent $1.25 · 34k tokens · last active 12m`.
   The file count is everything this conversation's tasks wrote.

**It is there in both of home's shapes** and it never moves: when the list becomes a
drop-up under your typing the card stays exactly where it is, because a card is assembled
downward from its title and lifting it would take the facts off the bottom rather than move
it down the screen.

**It follows the cursor through a search as well.** Walking `↑`/`↓` through filtered matches
switches the card to each one, so you are choosing between conversations by what they are
rather than by name alone. On a **folded project's line** it shows that project's card
instead — the project's name, and the folder it lives in. It goes **empty** — nothing drawn
at all — when the row it is about is neither: a project's `…13 more` line, or the
`start a new conversation` row, which has nothing to preview because that chat does not
exist yet. With the cursor on **no row at all** it is the machine's own card instead (its
own section, above).

A frame too short for all of that drops bands from the bottom — the facts go first — and
never touches the name. Under 80 columns the right column is dropped entirely and the list
takes the whole frame, because an index you can read beats a preview you cannot.

Nothing that is zero is drawn, anywhere. A chat that ran no tasks says nothing about tasks;
one that spent nothing says nothing about spending; a facts line with no facts is not
drawn at all.

## Why doesn't hovering change the right side — the mouse pointer previews a row

It does, and this is the rule: **the right side shows the row under the pointer while your
mouse is over one, and the cursor's row otherwise.**

Move the pointer onto a row on the left and the card on the right becomes **that row's**
card, at once. The cursor does not move: the hovered row takes the hover highlight, the
cursor's row keeps the selected one, and the two are allowed to be different rows. Move the
pointer off the column — onto the card itself, onto a project's heading, onto a blank line,
or out of the frame — and the card goes **straight back to the row the cursor is on**.
There is nothing to press and nothing to remember.

**The pointer never moves the cursor** — only a click does that — and **the keyboard never
moves the pointer**. `↑`/`↓` walk the cursor and the card follows the cursor whenever the
pointer is not on a row; `enter`, `tab` and `esc` always act on the cursor's row.

Every row the cursor can stop on can be previewed this way: a conversation, a `◦` standing
item, a folded project's line — which shows that **project's** card. Rows the cursor cannot
stop on, like a project's dim heading, preview nothing and leave the card where it was.

The card is the real card and not a sketch. Hovering a conversation in another project
shows **that project's** branch and dirty files, its work, its last exchange and its
figures; `→` opens every fold on the card you are looking at (and the chords — `ctrl+e`,
`ctrl+o`, `ctrl+y`, `ctrl+t` — act on the same card), so while you are pointing at a row
they act on **that** row.

Two cases where the right side does not move under the pointer, both of them by design:
under 80 columns there is no right column at all, and while your terminal owns the pointer
— after `ctrl+s`, or with the `ui.mouse` setting off — there is no hover anywhere, so the
card is always the cursor's.

## The project card — what the right side shows for a whole project

Put the cursor on a **project** rather than on one of its conversations — its name, or the
line that stands for it when it is folded away — and the right side stops being about one
chat and becomes about the whole container. The name is the title, its folder is the dim
line under it, and then three bands:

1. **its conversations**, the same rows the left column draws them as: the state glyph, the
   name, and the dim tail — `▲ Fix Flaky Auth Test    waiting on you · 2m`,
   `● Task Bar View More         1 running · 12m`. They are in the order home always puts
   them in: what needs you, then what is running, then what was left unfinished, then the
   rest by when you last spoke in them. Past four the rest go behind one line,
   `▸ …3 more conversations`;
2. **what is keeping an eye on it** — the reminders, watches and rules standing in this
   project, each `◦ every Monday at 9, draft the weekly update      Mondays 9am · last Mon`,
   with the same glyph and the same tail its row on the left wears. Past three:
   `▸ …2 more keeping an eye`, and under that one dim line for all of them together,
   `4 runs this week · $0.06`;
3. one dim line of **how big the project is** and when anybody was last in it —
   `12 conversations · 34 tasks · spent $4.10 · last active 2h`. That line is where to look
   for **how many conversations** a project has and how many tasks have run in it.

**Nothing that is zero is drawn.** A project nobody has run a task in says nothing about
tasks; one that has spent nothing says nothing about spending; a project with a single chat
in it and no work reads `1 conversation · last active 5m` and no more. A project with
nothing standing has no second band at all — not an empty heading.

A frame too short for all three drops whole bands from the bottom — the counting line goes
first — and never touches the project's name.

## The work on the right of home — what each task came to

The **work band** of the card is the tasks this conversation ran, **name first**, two lines
each with a blank between them:

```
Port the Picker                          2h
  the roster resumes cleanly · 14 files

Fix the nil-map                          3h
  the parser handles nested tags · 2 files · $0.12
```

- **The first line is what the task was called**, and how long ago it landed, hard against
  the right edge.
- **The second line is what it came to**, indented under the name and clipped to one line —
  it never wraps. The file count and the cost join it, each **only when it is not zero**.

**A task that is done wears no mark at all.** There is no `✓` and no `done`: on this screen
a mark means something is *happening*, and done is the absence of one. Every other state
leads the second line instead, with the glyph it wears everywhere else here:

- `● running · <what it is doing>`
- `◌ incomplete` — work that was under way when the window went
- `▲ needs your look · <what it came to>` — brought up out of the dim, because it is asking
- `✗ failed · <what stopped it>`

## How do I see more tasks on the right — ▸ …5 more tasks

**The band shows three tasks and folds the rest**, saying how many it is holding back:

```
▸ …5 more tasks
```

Three ways to open it, and all three fold it again:

- **click the line** — it opens that band alone;
- **press `→`** with nothing typed — it opens **every** folded band on the card at once,
  and `←` folds them all back. The right column has no cursor of its own, so the arrows
  act on the card rather than on a line. While something *is* typed, the arrows move the
  caret in the box instead — and `m`, which once did this, is always just an `m` going
  into the box;
- the opened band says `▾ …5 fewer`, which is the way back.

**The fold always cuts between tasks**, never through one — you will not find a sentence
under the fold line with nothing above it saying what it was about.

The project's whole history is somewhere else: `ctrl+g` opens the task page, which is the
record of everything this project has ever run.

## Why did the list jump to the bottom when I typed — home's two shapes

Home has **two panes and two shapes**, and only the left pane changes shape.

**With nothing typed it is a dashboard.** The list hangs from the top of the frame, the
cursor sits on the conversation this window is in, and the preview card is beside it. That
is what home is for: one page of everything the machine holds, read top to bottom.

**The moment you type a character the list becomes a drop-up.** It lifts so that its last
row — the action row, `start a new conversation: "…"` — lands directly above the box you
are typing into — with `ask here: "…"` between it and the matches — and the matches rise
above the pair **best one first**: the strongest match is two `↑` away, and each `↑` past it
walks into a weaker one. Everything to do with typing is then one cluster at the foot: your words, the
row saying what `enter` will do with them, and the hint under it. Clearing the box puts the
dashboard back.

**So the cursor does move between the two**, from up in the list to the foot and back.
That is deliberate. It was tried the other way — anchored at the bottom in both shapes so
the cursor never moved — and the cost was the dashboard itself: most of a frame of empty
space with a clump of rows against the box. One keystroke of re-anchoring is cheaper than
that.

**The right pane never moves and never changes shape.** It previews whatever the cursor is
on, in both shapes, and it is read downward from the conversation's name. It goes empty
only when the cursor is on something that is not a conversation — a project's
`…13 more` line, or the `start a new conversation` row, which is a chat that does not
exist yet and so has nothing to preview.

## Switch between sessions — enter on home

`↑`/`↓` (or `ctrl+p`/`ctrl+n`) walk the rows, stepping over the project headings.
`pgup`/`pgdown` jump four. The right column follows the cursor. Clicking a row puts the
cursor on it; clicking the row the cursor is already on opens it.

`enter` opens the session under the cursor — **any row on the screen, in any project.**
The chosen journal is opened and replayed, and **the conversation you were in stays open
behind it**, still streaming its turn, still running its tasks, one `tab` away. It is not
closed and it is not paused.

`enter` on the one you are already in simply steps into it and says nothing — it is
already loaded underneath, so there is nothing to reopen and nothing to announce. That is
what makes `enter` the calm keystroke on a launch: the cursor starts on that very row.

`enter` on a conversation **this** terminal is already holding behind the screen — a row
reading `open` — goes straight back to it, for the same reason: it is alive, so there is
nothing to reopen.

The foot line reads exactly:

```
type to search or start something new · ↑↓ pick · enter open
```

and the line under it says what the keyboard does, which changes with what the cursor is
on — at rest, `↑↓ move · enter open · esc close`.

## Typing a long question on home — does the box wrap, and where does a paste go

**The box wraps.** Home's foot box is drawn by the same editor as the chat's message box:
a sentence longer than the frame wraps onto continuation rows — up to three — and past
that the window scrolls with the caret, marked with `…` where the `›` was. Nothing you
type is ever truncated out of view. There is no key to open a new line here (that is the
chat box's `alt+enter` / `ctrl+j`); on home, `alt+enter` and `ctrl+enter` are `ask here`.

**A paste lands in home's box.** Paste while home is open and the text goes into the foot
box — filtering the list, exactly as typing does — or into the ask-here exchange's own
box when that pane holds the keyboard. Pasted newlines are kept, so a pasted paragraph is
fine as an `ask here` question. (It used to fall through to the chat's own draft, which
home was covering, so pasting looked like it did nothing.)

## Why can't I open a session from home — open in another window

A conversation that a **different** terminal already has open cannot be opened by this one:
two aforge windows on one journal would both append to it and neither would end up with the
conversation. (One this terminal is holding is a different matter entirely — `enter` goes
to it.) Home knows this **before you press anything**, so it says so twice over.

**On the row.** A conversation sitting idle in another terminal reads `another window`
where its rollup would be, so a locked door does not look like an ordinary one.

**A conversation *this* terminal is holding never says that.** It reads `open`, and `enter`
goes to it. The lock it would meet is our own, so home asks itself before it asks the
kernel — see *Switch between projects without leaving*.

A row already showing `waiting on you` or `N running` keeps those words instead, and does
not need the extra label: **both of them are read from a file only a live session writes**,
so a row wearing either is already telling you a window has it. Saying it twice would cost
the name the width it needs.

**On enter.** Nothing is tried. Home stays open, nothing is written into the conversation
underneath, and one dim line appears at the foot of the screen:

```
open in another window — go there, or start a new conversation here
```

Pressing enter again says it once more in the same place rather than piling it up, and you
can move straight to another row. **No file path is printed** — the path is aforge's own
bookkeeping and not a thing you can act on.

The right-hand pane spells the same fact out as `open in another window`, along with what
that window is doing.

So the two things to do are exactly the two the sentence names: go to the terminal that has
it, or type something here and start a new conversation instead.

There is one case the check cannot cover: a lock taken in the instant between home drawing
the row and your keystroke. You get the same sentence in the same place, and the
conversation you were in is untouched.

## Open another project from home

**`enter` opens any row on this screen, whatever project it belongs to.** There is nothing
to go to another terminal for and nothing to type: put the cursor on the row and press
`enter`. That is as true of a row under the `─ elsewhere ────` rule as of one at the top of
the screen — the rule says which projects home drew open, not which ones it will let you
into.

What happens is a **second conversation**, not this one moving. The conversation you were
in is left running exactly where it was — its turn keeps streaming into its own transcript,
its tasks keep running, it keeps its lock — and the new one is built on **its own**
workspace, with that project's approval rules, its crew, its spend ceiling and its saved
shapes of work. Nothing is carried across, because nothing crosses.

The status line then reads `2 open`, and `tab` over an empty message box goes back.

Two refusals are still possible and both leave home standing:

- the project's folder is gone: `that folder is gone · <path>`, and nothing is opened. Home
  already knew — the row reads `folder gone` and the card says so too, see *Enter does
  nothing on a row — the folder is gone* — and this line is the check made again on the
  keystroke, for a folder deleted in the seconds since;
- this terminal already holds eight: `8 open is as many as aforge holds — /quit closes this
  one`.

## Enter does nothing on a row — the folder is gone

**Its project folder is not on this disk any more.** A conversation started in a directory
that has since been deleted, renamed or moved — a scratch folder under `/tmp` wiped by a
reboot is the usual way — cannot be opened, because the conversation would come up as an
agent whose tool root does not exist and every command and every relative path in it would
fail.

Home says so **before you press anything**, in two places:

**On the row.** A dim `folder gone` sits where its rollup would be, in the same slot and the
same grey as `another window`. Every conversation in that project carries it, because the
cursor stops on rows and never on a heading.

**On the card.** Directly under the place line, above everything else on the right-hand
pane, one dim line reads `that folder is gone`. The last line of the card — its legend —
loses the three keys that need a folder and reads:

```
that folder is gone · ctrl+y copy path · → more
```

`enter open`, `ctrl+t new chat here` and `ctrl+o open folder` are not offered, because all
three want that directory: `enter` opens a conversation rooted in it, `ctrl+t` starts a
fresh one there, and `ctrl+o` hands it to your file manager. Pressing them anyway refuses.

**On enter.** Nothing is opened, home stays up, the conversation you were in is untouched,
and `that folder is gone · <path>` appears on home's message line at the foot of the screen.

**The conversation itself is not lost.** Everything aforge recorded about it lives under
`~/.aforge/v3/projects`, not in the workspace: `→` still opens the card's folded bands —
what it did, what it cost, where it left off — and `ctrl+y` still copies the workspace
path. What cannot happen is *continuing* it, because there is nowhere to continue it.

**What to do:** recreate the folder at that exact path and the row opens again on the next
refresh (home re-checks every three seconds); or type a message at the foot of home and
start a new conversation in a project that exists.

## Why does it say elsewhere — it does not any more

**It used to.** Every project but the one this window launched in carried a dim `elsewhere`
on its heading, and `enter` on one of its rows opened nothing and said
`elsewhere · <the project's path>`. That is gone: the heading is the project's name and
nothing else, and `enter` opens the row.

The reasoning behind the old refusal was right and is still kept — a window's approval
rules, its crew, its spend ceiling and its saved shapes of work are resolved from the
workspace it launched in, and carrying a conversation across without carrying those would
be a window quietly running under another project's permissions. What changed is the
answer: a second project is a second **conversation**, built the way the first one was, on
its own workspace, with its own gate. A conversation still never moves between projects.

`another window` is a different sentence and still means what it always did — see *Why
can't I open a session from home*.

## Switch between projects without leaving — work on two projects or two repos at once in one terminal

One terminal holds up to **eight** conversations at once. One is on screen; the rest are
open behind it, fully alive.

- **`enter` on home** opens any row, in any project, and leaves the one you were in open.
- **`tab`**, pressed with an empty message box, goes to the conversation you were in before
  this one. Press it again and you are back. It is `cd -`.
- **`/new`** adds a conversation in this project — unless the one on screen is fresh and
  empty, in which case it takes its place.
- **the status line** reads `2 open · 1 waiting`: how many this terminal holds, and how many
  of them are stopped on a question. It is absent when only one is open.
- **`/quit`** closes the one in front and brings the previous one forward. It leaves aforge
  only when that was the last one.
- **`ctrl+c` twice** closes all of them, and the warm line says how many:
  `ctrl+c again to quit · 3 conversations · 2 tasks and a job will stop`.

## How do I switch to my other chat — and is it still running

**`tab` with an empty message box**, or `enter` on its row on home. Either goes straight to
it; nothing is reopened and nothing is replayed from cold that does not have to be.

**Yes, it is still running.** A conversation you are not looking at is **not paused**: its
turn finishes into its own transcript, its tasks run, its background jobs run, its lock is
held and it keeps writing itself to disk. A desktop notification tells you when it finishes
a turn or stops on a question — even while the terminal is focused, because a focused
terminal is no longer evidence that anybody is looking at *that* conversation.

The status line says how many are open and how many want you: `2 open · 1 waiting`. Home
says it per row: a conversation this terminal holds reads `open`, or `waiting on you` when
it is stopped on a question, and those two are read from the conversation itself rather than
from a file, so they are never a few seconds behind.

## Does my draft move when I switch — what a switch keeps

**What you were in the middle of stays with the conversation you were in.** Coming back to
it redraws it from its own transcript, and these come back with it:

- the unsent sentence in the message box, and the pictures attached to it — including any
  message you typed while it was busy, which is folded back into the box rather than
  dropped;
- where you were reading;
- how much of an approval countdown was left, given back to you whole rather than run down
  while you were away — and only if the conversation is still asking;
- the room you had open;
- the transcript, the task column, the meters, the model, the title and any card still
  waiting for an answer — all of which are read back from the conversation itself.

**These are forgotten:** copy mode, a rewind you were part way through, the settings panel,
the model picker, `/history`, the deliverables shelf, a task column focus. Each is
something you are in the *middle* of, or a door onto something the whole terminal shares.

`/new` is the exception, and deliberately: **the draft goes with you**, not with the
conversation. `/new` carries the box's text into the new conversation and clears it in the
old one.

## Searching from home — find an old chat from anywhere

**Just type.** There is no prefix and no mode: the box at the foot of home searches every
project on the machine as you type, live, and narrows the list in place.

It matches four things, and the first that scores highest wins the row: the conversation's
name, the project it is in, the titles of the tasks it ran, and **what those tasks came
to** — the one-sentence outcome in the project's record. That last one is the closest thing
to remembering something by what happened rather than by what it was called: typing
`postgres` finds the conversation whose task outcome mentions the connection pool, even
though nothing in its name does.

Matching a project's name keeps every conversation in it.

Ranking is match quality first — a whole word beats a name that starts with what you typed,
which beats a word inside it, which beats the letters appearing in order. Then two things
break ties: a conversation **waiting on you** beats a cold one it ties with, whatever their
ages, and after that the more recent one wins.

`↑`/`↓` walk the matches, `enter` opens the highlighted one. `esc` clears the box; a second
`esc` closes home.

**The matches grow upward out of the box, best one first** — see *Why is the best search
result at the bottom* below.

**The preview keeps up.** The right-hand card switches to whichever match the cursor is on,
so walking up into the list puts the best one in front of you in full: its project and path, what it is
doing, the work it ran and the last thing said in it. That is how you tell two similarly
named conversations apart without opening either. On the `start a new conversation` row the
card is empty, because there is no conversation there yet.

## Why is the best search result at the bottom — the order of the matches

**The strongest match is the first conversation above the two typing rows — `ask here` and
`start a new conversation` — so two `↑` get you to it.** Each `↑` past that walks into a
weaker match, and `↓` comes back down toward the box.

That is upside-down next to an ordinary ranked list, and deliberately so. A list you read
*downward* puts its best answer at the top. While you are typing, home's list is read
*upward* out of the box — so its best answer belongs at the bottom, under your hand and one
keystroke away, instead of being the furthest row from the key you reach for. With a dozen
matches on screen the top-ranked one is still the first one the walk reaches.

**The ranking itself is unchanged** — match quality, then `waiting on you`, then recency
(see *Searching from home*). What the drop-up changes is only which end of the column that
ranking is drawn at.

Two things do **not** turn over with it:

- **A project's heading stays above its own rows.** The projects stack by rank and so do the
  conversations inside each one, but a project name drawn under the things it names would
  read upside-down.
- **The list with nothing typed.** At rest home is a dashboard read top to bottom in the
  ordinary direction — most recently spoken-in project first. The inversion is part of the
  drop-up, and the drop-up is what typing does.

## Can home search by meaning — semantic search

**No, and it is not going to.** Home's search is lexical and local — it matches the words
you type against names, task titles and outcomes already in memory, and answers inside a
keystroke with nothing loaded and no model called.

Two things cover what a meaning-search would have been for:

- **The row that offers to start a conversation never goes away.** A query that matches
  nothing still reads `start a new conversation: "…"` above the box, so the worst case of a search that
  missed is that your words become the first message of a new chat — which is very often
  what you wanted.
- **Ask the chat instead.** It has a `tasks` tool over the whole project record and you can
  ask it in sentences: *"what was that thing where we fixed the flaky auth test?"* Home is
  the fast layer; the conversation is the thoughtful one.

## Start something new from home — typing does both at once

Whatever you type is **two things at the same moment**: a new conversation waiting to be
sent, and a live query over the machine. You do not choose between them before you start
typing.

**Everything about typing sits together at the bottom of the screen.** The moment you type
a character the list becomes a drop-up: the matches rise from the foot, and the **last** row
of the list is the action row — `start a new conversation: "…"` with your words quoted
back — sitting directly above the box you are typing into.

```
 …
 gamma
 ○ Quarterly Pricing Deck                                               3d

 alpha
 ○ Pricing Sheet Import                                                 2h
 ○ Pricing                                                             12m   ← two ↑

 ? ask here: "pricing"
 + start a new conversation: "pricing"
 ──────────────────────────────────────────────────────────────────────────────────────────────────
 › pricing
 enter starts a new conversation and sends this · ctrl+enter ask here · ↑ pick a match · esc clear
```

**The cursor rests on the action row by default.** So typing and pressing `enter` starts a
fresh conversation in this project and sends what you typed, exactly as it always has,
however many matches are on screen.

One `↑` steps off that row **up** onto `ask here: "…"`, which answers the same sentence in
the pane on the right instead of opening a conversation for it — see *Asking from home*. A
second `↑` reaches the best match — the order runs weakest at the top, best at the bottom,
and *Why is the best search result at the bottom* says why. Then you are
picking from the list: `enter` opens the highlighted conversation, further `↑` walks into
weaker matches, `↓` walks back down to the action row, and the cursor stays where you put it
while you keep typing.

The hint under the box tracks which of the two `enter` means: the line in the example above
on the action row, and `enter open · ↓ back to starting a new conversation · esc clear` once
you are on a match.

With **nothing** typed there is no action row and the list hangs from the top again — see
*Why did the list jump to the bottom when I typed*.

Starting a conversation this way is `/new` followed by your sentence, so everything `/new`
does applies. On a surface with no fresh-session seam it refuses in `/new`'s own words,
`/new is unavailable here`.

**Typing a path starts a conversation there instead.** When what you typed resolves to a
directory on this machine — an absolute path, a `~` path, a `./` path, or a project name
that matches exactly one heading on the list — the action row reads
`start a new conversation in <that folder>` and `enter` opens a fresh conversation in it,
with the one you were in left open behind. A name that two projects share resolves to
neither, because opening whichever sorted first would be the screen guessing.

**The path is resolved and never created.** A directory that does not exist is not a path
at all as far as this row is concerned: the row goes back to quoting your words, and
`enter` starts a conversation here and sends them. Nothing makes a folder because somebody
mistyped one.

## How do I see the collapsed sessions — …13 more

A project **that home is drawing open** shows its first four conversations and folds the
rest into one dim line, `…13 more, quiet since 1d`, with a `▸` in front of it. (A project
folded to a single line under the `─ elsewhere ────` rule is a different fold, one rung up
— "Why are most projects collapsed on home" is that one.)

**That line is a door.** Put the cursor on it and press `enter` or `→` and the project opens
in place; the line becomes `▾ …13 fewer`, and `enter` or `←` folds it away again. `←` on any
conversation inside an opened project folds it too. Clicking the line toggles it in one
press. It is the same fold gesture the task column uses, with the same two marks.

**And searching sees straight through it.** While anything is typed the collapse is not
applied at all — every match is drawn wherever it lives, including rows that were behind
the fold. A search that could not see what it hides would be a search lying about the
machine.

Projects you open by hand stay open while home is up, including across a refresh.

## How do I get back to the dashboard — press space twice

**From inside any conversation, press the space bar twice with an empty message box.**
That is the way back to home.

There is no `ctrl+` chord for it: every `ctrl+<letter>` this surface has is already taken,
and `esc` was not available either — on an idle conversation it already arms rewind and
already sends a message you parked, and a third meaning on one key is how a surface stops
being predictable. What was left is the one keystroke that reliably means nothing: a
message that starts with two spaces is a message nobody meant to send that way.

**The first space types itself, plainly.** There is no pending state and no ghost
character. It is the *second* space, arriving to find a box holding exactly one space, that
takes both away and opens home. So a space you actually wanted is never eaten: space then
`x` leaves ` x` alone, because the gesture only fires on a space and only when a single
space is all there is.

It works with a turn running. Home takes the frame the way the settings panel does, and the
answer goes on streaming underneath — `esc` puts you back in it, still running.

Two things it will not do: it does nothing when the box already has words in it, and it is
not bound at all over `--host`, where home refuses (*Home on a fresh machine, and over
--host*). It works on a machine with one conversation, and on one with none — what opens
there is an empty home.

## What does pressing space twice do — the home door at the foot of a conversation

When the box is empty, the dim line between the conversation and the box reads exactly:

```
space space home · / commands
```

That is the whole advertisement. It costs no extra row — it is the hint slot that line
already carried — and it **vanishes the moment you type anything**, because it is a door
and not decoration. It also goes while a turn is running, where the same slot has something
more urgent to say (`esc interrupt`); the gesture still works then, it is just not being
advertised.

**You can click it.** A press on the words `space space home` opens home; a press on the
rule beside them is a press on a rule.

It appears on a fresh machine too, from the first minute: a machine with one conversation
or with none still has a home to go to, and a door that is drawn is a door that goes
somewhere. The one session it never appears in is one over `--host`, where home refuses.
The rule that keeps home from *greeting* a first run is a different rule — not being
greeted by home and not being able to reach it are two different things.

## space space does nothing — why the gesture did not open home

Four reasons, and the machine holding nothing is not one of them any more:

- **The box had something in it.** The gesture fires only when the second space arrives to
  find a box holding exactly one space. ` x` and then two spaces is three characters.
- **It was a paste.** Pasted text arrives whole and never reaches the key router, so two
  leading spaces in a paste are two spaces (*Is there a key for home?*).
- **The session is over `--host`.** Home refuses there — the projects it would read are on
  this computer and the work is on the other — so the gesture is not bound and the foot
  does not advertise it.
- **Home is already open.** On home, space is a character in the search box.

A machine with one conversation, or with none, opens home all the same: an empty home is
a screen (*Home is empty — what an empty home shows*), not a refusal. It used to be
otherwise — the gesture stayed inert until the machine held a second conversation — and
that rule is gone.

## how do I get back to home with one chat

Three ways, and they all work from the first minute on a fresh machine:

- `space` twice on an empty message box
- `/home`
- a click on the words `space space home` in the dim line above the box

The launch itself does not greet you with home while the only conversation on the machine
is the one it just opened — that is a rule about greeting, not about reach — so on a
machine with one chat, home is something you go to rather than something you land on.
What you find there is that chat, under its project, with its card on the right saying
`open here`.

## home is empty — what an empty home shows

A machine that has held nothing yet draws the same screen a full one does, with nothing in
its rows:

- the top line, with the time on the right
- at 80 columns and wider, `needs you` and `moving` standing where they always stand; at
  110 and wider each in its own column with its teaching line — `questions and landed
  work`, `turns, tasks and watches`
- `nothing here yet — say something and this fills up` where the first project's rows
  will be, dim, in the places column — over two rows, split at the dash, so the column
  never cuts it short
- the box at the foot reading `type to search or start something new · ↑↓ pick · enter
  open`, and the key line under it

Typing there works exactly as it does anywhere: `? ask here: "…"` and `+ start a new
conversation: "…"` rise out of the box, and `enter` starts the conversation. The arrows
have nothing to land on until there is a row; `esc` goes back to the conversation.

A machine with one conversation — the one you are in — is not empty, and home does not
say it is: the conversation is on the list under its project, from its first minute and
before anything has been said in it, with `open here` on its card. The conversation on
this terminal is always on the list.

## Is there a key for home?

`/home` opens it, and **space twice on an empty box** goes there from inside a conversation
— see the two sections above. There is no `ctrl+` chord for home: the plain ones are all
taken (`ctrl+.` is the task page, `/history`), and the chords that were left — the `alt+`
letters — arrive in some terminals and do nothing at all in others. The double space is
the chord home has instead.

## What landed while I was away — the ✓ and "since you last looked"

Home remembers when you last closed it, and marks what finished after that. A resting
session whose tasks landed while you were not looking wears `✓` instead of `○`, its line
reads `2 landed` where the task count would be, and its card puts a dim
`since you last looked` over the whole work band. That is the whole
mechanism **on home**: no badge, no list of unread things. You open home and the ticks
show you where work accumulated. (A session's own window does send a desktop notification
when its turn finishes or it stops on a question while you are looking elsewhere — see
"Why a session says it needs you" below. Home itself never does.)

The marks are measured from the moment home was last **closed** — looking at the screen
is what counts as seeing, so nothing is marked seen the instant it appears. They hold
while the screen is open (a refresh does not silently unmark the news between two
glances), and closing home writes the new mark for next time.

Three honest edges: the very first time home opens there is no "last time", so nothing is
marked rather than everything; work that lands in **the conversation this window is in**
is never marked, because you watched it happen; and if aforge is killed with home open,
the same work is simply marked as news once more on the next open — repeating news is the
safe direction to fail in.

## Why a session says it needs you — waiting on you

A session that has asked you something and can go no further writes that down, and home is
where you see it without opening the window it is in. The row wears `▲`, its rollup reads
exactly `waiting on you`, and **it sorts to the top of its project** — above work that is
running, above everything you spoke in more recently.

**Five things count as being asked**, and they are one list so that no window can say
`working` about a session that is really stopped:

- an **approval question** — a tool call held at the gate;
- a **connect offer** — a service it wants to sign you in to;
- a **sub-harness offer** — a saved shape it wants to hand the work to;
- a **task proposal** waiting for your yes;
- an **adaptive run out of fuel**, parked at its gate until you top it up, finish on what
  is done, or stop it.

The run is the one that used to be missing: a run that had spent its tank read as `working`
to every other window on the machine, which is the one thing this row must never do. It now
reads `waiting on you` like the rest, and the line beside it is the gate's own — `out of
fuel · $2.00 of $2.00`.

The right column then shows the one line it is stopped on, and it is the only thing on
that pane that is not dim: everything else there is a fact about what happened, and this
is a thing somebody has to do. A session that gave no words for what it is waiting on
shows no line at all rather than a placeholder.

This is read out of a small file each live session keeps in its own folder, refreshed
every five seconds and believed for fifteen. So a window that was killed, or a laptop that
closed, stops claiming to need you within a glance — nothing on home asks you for
something that nobody is waiting for any more.

`enter` on the row opens it under the ordinary rule, so a question in this project is one
key away and one in another project tells you where to go. **For the ordinary questions
you do not have to go at all** — see the next section.

## Answer a question from home — approve a command in another window

**You can answer it here, without opening the window it is in.** Put the cursor on the
`▲` row. Under the line it is stopped on, the card on the right shows the answers that
session will take, as chips, and pressing the digit answers it. A click on a chip does
the same.

The chips are the ones the question has:

- It is **waiting for permission to run something**: `1 allow once · 2 always · 3 deny`.
- It is **asking whether to start a task**: `1 yes · 2 no`.
- It is **asking whether to keep an eye on something**:
  `1 yes · 3 just once · 0 not set up` — or `1 yes · 0 not set up`, when what it
  is asking about is a **one-off reminder**, which has no `once` answer at all (the
  reminders page says why). **`0` is how you say no from home**, and it is on every
  standing card there is: nothing is set up, nothing is run, and the card in that window
  settles as `not set up`. It is a `0` rather than a fourth digit because the chips in the
  conversation are numbered by their position — a `4` would move under your hand the day a
  card drew one chip fewer — and `esc` cannot be borrowed here, because `esc` on home
  closes home.
  There is no `2 change when or where` here, on purpose: that answer is a request for a
  text box, and a card in a column has no box. To say a different time or place, open the
  conversation: the card is still waiting there, because this card never times out.

`2 always` means what it means in the window: **that session stops asking about that
tool** for the rest of its life. It does not write a permission rule into your settings —
the card in the window writes that from the command it has in front of it, and home has
only the one line the session is stopped on. The handful of shapes aforge always asks
about — the ones that wipe a disk — are asked about again whatever you press here.

The digits are keys **only while nothing is typed and the cursor is on a row that is
waiting**. Every other moment a `1` is a `1` going into the box, which is a search and a
new conversation at the same time.

Home then says `answered · deny` at the foot, and the card reads `answered · waiting for
it to pick that up`. That wait is real and short: the other session looks for your answer
on the same heartbeat it uses to say what it is doing, every few seconds. When it picks it
up the row stops needing you and the card goes back to being a card.

**An answer that arrives too late is ignored**, exactly as a late click in the window
itself is: a task whose countdown already started it, a card somebody answered in its own
window a moment earlier, a turn that was interrupted. Nothing is said about it — the
question was simply over.

**A window that has stopped refreshing cannot be answered from here.** A live session
rewrites its little status file every five seconds; past fifteen it is not believed, the
chips are not drawn, and the digits go back to being characters. That is the same rule
that stops home claiming a killed terminal still needs you.

You do not have to be on home to find out. A session that stops on an approval question
while its window is unfocused also sends a **desktop notification** — headed `aforge`,
reading `<conversation> · waiting on you` — and so does a turn that finishes on a window
you are not looking at, reading `<conversation> · turn done`. Terminals that do not know
the sequence print nothing. See "Does my other window keep working when I switch away" in
the permissions page for the whole of what an unfocused window does and does not do.

## Why a task says incomplete on home

The project's record of its work is append-only: a task writes a row when it starts and
another when it lands. So a machine that lost power, or an aforge that was killed, leaves
rows on disk that say `running` forever.

Home never repeats that claim. **It asks the session itself.** A live session says out
loud, every few seconds, which task nodes it currently has out; a `running` row is drawn
as `running` only when the session that ran it is still alive and still names that node.
Every other live-looking row is `incomplete` — work that was under way when the window
went — and the same rule decides the glyph: `◌`, never the spinner.

A session too old to keep that file, but whose journal a window is holding, falls back to
the older answer: the lock is asked, and its rows are believed. That is the same rule with
less to go on, not a different one.

The right column says `open here` for the one this window is in and `open in another
window` for one a second aforge has, with what it is doing after it — `open in another
window · working`. `idle` is not spelled out, because it is what an open session usually
is. Nothing at all is said when nobody has it.

## Home on a fresh machine, and over --host

A machine that has held nothing yet draws an empty home — the top line, the two strips
wherever the width draws them, `nothing here yet — say something and this fills up` where
the rows will be, and the box and keys at the foot (*home is empty — what an empty home
shows* has the whole screen). The conversation you opened it from is on it as soon as
there is one.

Over `--host` home refuses to open and says
`home shows this machine's projects, and this session is on another`. The projects it
would read are under *this* computer's `~/.aforge/v3`, and the work is on the far end — a
screen full of the wrong machine's projects would be a confident lie.

## Does home update while I look at it?

Yes, every few seconds, by reading the folders again. A task landing in another window,
work somebody starts in a second terminal, or a session stopping to ask a question all
show up without you doing anything. There is no file watcher: home reads, and closing the
screen stops the reading. (Standing items — reminders, watches, rules — are a different
mechanism and do keep going; see the keeping-an-eye page.)

And while any row has work running, home is **visibly alive**: the spinner on that row and
on its card turns continuously, and a running task's count-up climbs, exactly as they do
in the conversation that owns the work. The moment nothing on screen is running the page
falls still again — a home full of finished work animates nothing. In screen-reader
(linear) mode nothing ever animates.

The cursor stays on the row it was on rather than on the line number — the order genuinely
changes when work starts or finishes, and a cursor that stayed put would move you onto
something else between two glances.

## What is the ◦ row on home — things keeping an eye on your project

Under each project's conversations home draws a **band of the things that keep
working after a window is closed** — a reminder, a watch on something, a rule, an
overnight job. Each one is its own row: a glyph, the words you said, and one line
saying where it stands.

```
  aforge-v2
  ▲ Pricing Research  4m
  ● Port the Picker    12m
  ○ Import Cleanup     3h
  ◦ every Monday at 9, post the standup note   Mondays 9am · last Mon
  ◦ tell me when CI on main goes red         checked 6m ago · nothing
  ▸ …2 more keeping an eye
  ▸ …3 more, quiet since 2d
```

The glyphs share the two loud ones with the conversations above and have three
of their own: `▲` it needs your look, `●` it is running right now, `◦` it is
waiting for its time, `∙` it is paused, and `◆` something happened since you last
spoke in the conversation that asked for it. On a terminal that cannot draw them
they are `!`, `*`, `-`, `.` and `+`.

`◆` is worked out and never claimed: it means the thing went off *after* the last
time you spoke in the conversation behind it. Something with no conversation on
this machine to compare against never wears it.

The tail says where it stands, and it says **only what is true**:

- `Mondays 9am · last Mon` — a reminder or a routine: when it goes off, and when
  it last did. An item that has never gone off says only when it will.
- `checked 6m ago · nothing` — a watch on the world: when it last looked, and
  what it found. `nothing` is a finding, and the commonest one — it is the whole
  difference between a watch that is working and one that never ran.
- `needs your look · <what it is stopped on>` — the one line on the band that is
  not dim.
- `running · 4m` — it is doing something right now.
- `paused` — you pressed `ctrl+e` on it.

**Where the rows sit is triage, not grouping.** An item that needs you or is
running sits *above* the conversations, with the other rows that want you; one
still waiting for its time sits under them. A project draws three of them and
folds the rest into `…2 more keeping an eye`, which is a door — `enter` or `→`
opens it, `←` folds it away, a click toggles it. Something that needs you or is
running is drawn whatever that count says: those are the rows the screen exists
for, and the fold only ever takes the ones still waiting.

**Retired items are not on this band.** Something that fired once and finished,
or that you stopped, is a thing that happened; the conversation that made it
still has the whole record.

**A project with items and no conversations still gets a heading.** A watch is
content. If you set a reminder in a directory you have never held a conversation
in — or a machine-wide one, which belongs to `~` — home draws that workspace as a
heading of its own, named the way every other heading is named (the folder's last
part, `~` for your home directory), with its items under it. It sits in the
recency order by the newest thing those items have done. A heading is never
marked at all — it is the project's name and nothing else — and `ctrl+e` and
`ctrl+x` on its rows work exactly as they do anywhere else: they go through the
store, not through a door.

**Typing hides the band.** The box at the foot searches conversations — by name,
by project, by what their tasks came to — and rows the query never considered
would be rows drawn as though it had.

## How do I pause a reminder from home — ctrl+e and ctrl+x on an item row

Put the cursor on the row (or point at it) and press **`ctrl+e` to pause it** or
**`ctrl+x` to stop it for good**. Home says `paused · <your words>` or
`stopped · <your words>` at the foot and redraws the row from the store, so what
you see is what is on disk rather than what the keypress hoped for.

They are chords rather than bare letters on purpose: home's box takes every
letter, always, so a `p` is a `p` in your sentence wherever the cursor rests.
The chords need no such care — they can never begin a word — and they act
whether or not something is typed.

A window whose build cannot write to the store says `this window cannot change
it` rather than pretending.

`enter` on the row **opens the conversation that asked for it** — that is the
answer to "why did I get this?", and it is the same door a conversation row has,
which means **whatever project it belongs to**. Something you set up from home
that never became a conversation says
`made from home — no conversation to open`.

## What is on the right of home when I'm on one of these rows — the item's card

The right column shows the same kind of card it shows for a conversation, about
the other kind of thing, read downward:

1. **your own words**, the brightest text on the screen;
2. one dim line of **where it is** — project · path, and that line is a link;
3. **when it goes off**, and — if it is stopped on something — that line, in the
   one hue on the card that is not dim;
4. **what it has done**: `checked 6m ago · nothing`, `last went off Mon · <what
   came of it>`;
5. a dim line of **how much, ever**: `4 runs · spent $0.08`;
6. a dim line of **how much lately**: `ran 3 times this week · $0.04`, counted
   over the last seven days of the standing ledger;
7. the keys, dim: `enter open where it was asked · ctrl+e pause · ctrl+x stop`.

Nothing that is zero is drawn. Something set up ten seconds ago is a title, a
place and a cadence, and nothing else — no `0 runs`, no `$0.00`, no weekly line.
A window with no ambient side wired to it draws no weekly line at all.

The project card's `keeping an eye` band ends with the same count over all of
that project's items: `4 runs this week · $0.06`, below the fold line, because it
counts the items the fold hides too.

## Keeping an eye on — the status line, and /status

When the project this window is in has something standing, the status row at the
foot of the frame grows one dim segment:

```
◦ keeping an eye on 2
```

**Nothing at all when there is nothing** — a line that permanently read
`keeping an eye on 0` would be a permanent reminder of the absence of a thing.
The glyph **breathes** — it becomes the same spinner every running thing here
wears — only while one of them is actually firing. The rest of the time it is
still.

`/status` adds one line under the same heading:

```
keeping watch   installed · last check 4m
keeping watch   while a window is open · last check 4m
keeping watch   nothing is checking · say "remind me…" to start
keeping watch   nothing is checking · background checks are off · /settings
```

It says **`installed`** when the machine's own timer is set up, so the checking
happens with no terminal open at all. It says **`while a window is open`** when
there is no timer but this window is running the pass itself, every five
minutes. When neither is true it says **`nothing is checking`** — nothing was
switched off, and the items are still there and still due; there is simply
nothing running the checks right now. The tail says which way it got there:
`say "remind me…" to start`, because setting up the first standing thing is what
installs the timer, or `background checks are off · /settings`, because something
has stood here before — so the timer went on once — and it is not on now. That
one covers both ways it can happen, a row you turned off and an install that did
not take, because the row reads `off` for both and the row is where both are
fixed.

The `last check` half is dropped when nothing has ever run. A window with no
ambient side at all — `--host`, a build without it — prints **no line**, and so
does one whose timer could not be read: an absent answer is left absent rather
than reported as "off".

## Why did a card appear asking me about a reminder — saying yes to something standing

When you say something that would keep working after this window closes — "remind
me at 6 to leave", "tell me when CI on main goes red", "every Monday post the
standup note" — a card appears in the conversation and **nothing is set up until
you answer it**:

```
╭─ ? ◦ every Monday at 9, post the standup ──────────────────────────────────
│ every Monday at 9, post the standup note from the git log
│ when · Mondays at 9am
│ where · for this project
│ costs · about $0.02 a run, at most once a day
│ [ 1 yes, set it up ]  [ 2 change when or where ]  [ 3 just once ]  [ 0 no ]
│ I'll keep doing this Mondays at 9am, for this project, until you stop it
╰────────────────────────────────────────────────────────────────────────────
```

Three bands make it different from the card that proposes a task: **`when ·`**, in
the words you said or the words it worked out; **`where ·`**, how far it reaches;
and **`costs ·`** — what one run may spend and how often it may run. A watch that
has to *look* at something adds `checked every 5 minutes`, because that is when
the looking happens; a reminder does not, because nothing is examined between now
and Monday.

If it made the timing up rather than reading it off what you said, the band asks
instead of stating: `Mondays at 9am — you didn't say, so that's my guess.
Right?`

**The answers**, by key, by `←`/`→` and `enter`, or by clicking one:

- `1 yes, set it up` — it gets set up and starts happening.
- `2 change when or where` — the box below becomes a place to say the **time or
  the place** you want instead: "make it 8", "only in this project", "everywhere".
  `enter` sends your words back and nothing is set up until a new card comes with
  them in it. This is the one door for **both** — the `where ·` band is changed
  through it exactly as the `when ·` band is.
- `3 just once` — do it now and leave nothing behind.
- `0 no` — nothing is set up, nothing is run, and the row settles as
  `not set up`. `esc` does exactly the same thing.

**The line under the answers says what the one you are on will actually do**, and
it is written out of this card's own facts rather than being a fixed sentence:
`I'll keep doing this Mondays at 9am, for this project, until you stop it` on the
yes, `I'll do it now, this once — nothing is kept and nothing happens later` on
`3`, `nothing is set up yet — type the time or the place you want, then enter` on
`2`, and `nothing is set up and nothing happens later` on `0`. Walk the row with
`←`/`→` and the line follows the answer you are on, so you can read what each one
does before you take it. The answer under the cursor is the one `enter` takes;
it is lit and lifted, and its digit is the key that takes it outright.

A **one-off reminder's card draws no `3`**: "do it now" for a line meant for six
o'clock is not a smaller version of the reminder, it is the wrong thing at the
wrong moment. Watches, rules, routines and overnight work keep it. **The `0` is on
every one of them** — a way to say no is never missing, and on a narrow card the
words shorten to `yes`, `change`, `once` and `no` rather than any answer being
dropped. The hint under the box says which digits are really there —
`1 yes · 2 change when or where · 3 just once · 0 or esc, no`, or
`1 yes · 2 change when or where · 0 or esc, no`.

`esc` says no, and so does `0`. **There is no clock on this one**: no bar, no countdown, and no
moment where it answers on your behalf — it waits while you read it. That is the
opposite of the task card, whose clock approves on silence: something that spends
money forever with nobody in the room is not a thing silence should agree to. If
the turn ends with the card still up — you interrupt it, or the window closes —
it says `ended · nothing was set up`, and nothing was.

**An answered card stays where it is.** It does not vanish: it settles, the frame
goes grey, and the bottom edge carries the answer and what it came to —
`yes, set it up · set up`, `just once · done now, nothing kept`, `not set up`,
`change when or where · you asked for something different`,
`ended · nothing was set up`. That is true of a card in a conversation and of a
card in home's `ask here` pane alike; it is one card with one renderer.

**The first time you ever set one up** the machine's own timer goes on, without
asking, and one dim line says so under the card you just answered:

```
checks every 5 minutes, window or not · background checks under /settings
```

That line is said **once, ever**, and it is the only thing ever said about it. If
the install did not take it says so instead, with the reason, and points at the
same row. The switch is `/settings` → **Workspace** → **background checks**;
turning it off leaves everything standing and checks it only while a window is
open (the keeping-an-eye page has the whole of it).

## What does a ◦ line in the middle of my conversation mean — news from something standing

Once something is set up it writes **one line and never more** into a
conversation you have open — the one that asked for it when that is open, and
otherwise whichever one of that project you are sitting in (the keeping-an-eye
page has the whole order):

```
◦ every Monday at 9 · set up
◦ every Monday at 9 · said: the standup note is in notes/standup.md
▲ keep main green · needs your look: the fix touches migrations
∙ remind me at 6 to leave · stopped
```

**When the line appears.** If the chat is open when the thing fires, the line is
drawn **at once**, the moment the firing arrives — not when the reply comes, and
not when you next type. Whatever the chat then says about it is a separate turn
underneath. If the chat was **shut** when it fired, the same line is drawn when
you open it: one row for each thing that was waiting, oldest first, above the
first thing you type.

That is the whole of it. **A check that found nothing writes nothing** — a watch
that ran faithfully for thirty mornings and found nothing leaves your
conversation exactly as quiet as it was, and the row on home is where you go to
confirm it really did look.
## Ask here — a reminder or a watch without opening a conversation

While you are typing, the row directly above `start a new conversation` is
`ask here: "…"`. It answers the sentence **in the pane on the right** — a real conversation
with a real transcript, kept outside `~/.aforge/v3/projects` so this list never grows a
session row for a one-off errand. One `↑` reaches it, and `ctrl+enter` (or `alt+enter`) does
it without leaving the box.

**Every exchange is a row on this column**, marked `?`, in its project's block above the
conversations, with what it is doing in the tail:

```
 ? remind me at 6 to leave                          ▲ waiting on you
 ? what did we decide about pricing                 ⠹ working · 4s
 ? tell me when CI goes red                         ∙ stood
```

They sort with the hot things — what wants you, then what is moving, then what is done —
and **several can be open at once**: a second `ask here` adds a row rather than replacing
the first.

The pane on the right is **about the row under the cursor**, exactly like every other card
in that column: walk onto an exchange row and you get the exchange, walk off it and the row
you land on draws its own preview again. `enter` or `tab` on the row hands the keyboard to
the pane; `esc` or `tab` hands it back. On a window too narrow for two columns the pane is
**stacked** over the list instead of drawn beside it, and `esc` brings the list back.

Inside that pane, the heading, exchange rows, any question card and the `continue as a
conversation` offer are separate blocks. Exactly one blank line divides adjacent blocks;
optional blocks never leave a second blank line when they meet.

**An exchange outlives home.** Closing this screen does not end it, and neither does opening
another conversation; the row is still here, still waiting, when home opens again. It is
filed only once it is over, you have seen what it came to, and you have moved off its row —
or when you quit.

The whole of it — where the record goes, how the card is answered, what the spinner and the
live strip say, how to get back to the list, and how to turn the exchange into an ordinary
conversation — is on its own page: *Asking from home*.

## What happened in this conversation while I was away — news since I last looked

A conversation with queued news draws `◆ N things since you left` in its home preview.
So does a **project** whose card is up, for news that belongs to the project rather than to
one chat — what a reminder you set up with `ask here` left waiting when no window of that
project was open. Opening any conversation there folds it in and the line goes.
Each item underneath reads `<age> · <words> · <text>`, for example
`4m · keep main green · the tests passed`. Home shows three items, then a
`▸ …N more things` door; `→` opens the preview's folds and `←` closes them. An absent or
empty inbox draws no news band at all. Looking at the band does not consume the news.

## Where are the files it produced — deliverables on a conversation

The home preview lists files produced by that conversation, newest first, as
`<basename> · <age>`, for example `report.md · 2h`. The basename is a clickable path in
terminals that support file links, and opens the full recorded path. Home shows three
files, then a `▸ …N more files` door; `→` opens the preview's folds and `←` closes them.
A conversation with no indexed files draws no deliverables band.

## Where did we leave off — the last exchange on a conversation

The home preview keeps the two sides of the last exchange together. The person's last
message is one muted line beginning `› `, followed by the reply in dim text wrapped to at
most two lines. A conversation with no turns draws no last-exchange band.

## Where does this repository stand — branch and dirty files on home

A conversation whose workspace is a Git repository gets one dim repository line on its
home card. A changed branch can read `feature/home · 2 files dirty · ahead 1 · behind 3`.
Every unknown or zero clause disappears, so a clean repository on main reads only `main`;
a folder that is not a repository draws no line. Home refreshes this reading for the
workspace at most once every five seconds, and a failed or timed-out Git check draws
nothing.

## What do the keys on a home card do — open, new chat, folder, and copy path

The last dim line of a conversation card reads `enter open · ctrl+t new chat here · ctrl+o
open folder · ctrl+y copy path · ctrl+e put away · → more`. They are chords and arrows
rather than letters so that typing always types — every bare letter goes into the box,
whatever the cursor is doing — and each acts on the card you are looking at: the row
under your pointer when there is one, the cursor's row otherwise. `ctrl+t` starts a fresh
conversation **in that row's own project**, whichever one it is — the conversation you
were in steps aside and keeps running, exactly as it does for `enter` (the mnemonic is
the browser's new-tab key; ctrl+n is the walk down the list). A row whose folder is no
longer on this disk says `that folder is gone · <path>` and starts nothing.
`ctrl+o` asks the machine to open that conversation's workspace folder, `ctrl+y` copies
the workspace path, `ctrl+e` puts the conversation away into the archive (`ctrl+e bring
back` on a put-away row), and `→` opens the card's folded bands while `←` closes them. A
failed folder open says `could not open <path>` on home's message line.

**A card whose project folder is gone carries a shorter legend** — `that folder is gone ·
ctrl+y copy path · → more` — because `enter`, `ctrl+t` and `ctrl+o` all need that
directory. See *Enter does nothing on a row — the folder is gone*.

An item's card carries the exact legend `enter open where it was asked · ctrl+e pause ·
ctrl+x stop · → more`.

## What is next up on home — scheduled items coming soon

The `next up` band lists active items belonging to the card's project, soonest first and
two at a time. Rows look like `◦ leave for the train · in 4m`, `◦ draft the update ·
Mondays 9am`, or `◦ check CI · checked 6m ago`. When more than two are present, the card
adds a dim `▸ …N more items` door; `→` opens it. Paused, stopped, and absent items draw
nothing.

## What has this conversation cost — the spend band on home

The dim spend band under a conversation's card reads `touched 12 files · spent $1.25 ·
34k tokens · last active 12m`. Each clause is independent: zero or unknown files, spend
and tokens are omitted, and a line with no true fact at all is not drawn.

`touched 12 files` is how many files this conversation's work wrote, summed over its
tasks. It is here because nothing else on the card carries it, and it is the most physical
number the index holds: tokens are what the work cost, files are what it DID.

**It counts the talking and the work the talking started, in one figure.** The turns —
your messages, the answers, and the small calls beside them, such as the one that names
the conversation — are added up by the session itself and written to the session folder at
the end of every turn, so home can read them without opening the transcript. Every task
and every unattended run this conversation commissioned is added from the project's task
index. `spent $1.25` is those two halves together, which is what "what did this
conversation cost" means.

`tokens` is input plus output as one sum, over the same two halves.

A conversation held before this build has no figure of its own written down yet; the next
turn writes one, and resuming an old conversation folds its transcript's own usage lines
in once on the way in. `/cost` and `/status` inside the conversation still answer for the
live session, and agree with this line about the talking.

The project card's own facts line (`12 conversations · 34 tasks · spent $4.10 · last
active 2h`) adds the same two halves over every conversation in the project.

## What a narrow home card does with a long row

A card too narrow for a line breaks it between its clauses rather than cutting the end
off. Whole facts move onto following rows: the final key, branch fact, file age, scheduled
time, news text, task file count or cost, and answer chip remain visible. A single clause
wider than the card is still clipped. The card's place line clips from the left so the
path's basename remains visible.

## Home on a phone — waiting on you, running, since you left

Under **60 columns** home stops being a directory of projects and becomes an **inbox**
across all of them. It is the same screen and the same data; the shape is what changes,
because a phone-width frame is walked one row at a time rather than scanned.

Top to bottom:

1. `waiting on you` — every conversation stopped on a question, every standing item that
   needs a look, and every `ask here` errand holding a card, from **any** project.
2. `running` — everything with work in flight, wherever it is.
3. `since you left` — what landed while you were not in the room: a note left in a
   conversation's inbox or in a project's, and any task that finished after the last
   thing you said in that conversation.
4. **The projects.** This window's own project is drawn open with its remaining rows; every
   other project is one folded line — `▸ wisp   6 · 2d` — exactly as the `elsewhere`
   block draws them, without the rule line.

Each section shows **three rows** and folds the rest into `▸ …N more`; `enter` or a tap on
that line opens it in place. A section with nothing in it is not drawn at all, so a quiet
machine shows only its projects. **A row appears once**: a conversation lifted into
`waiting on you` is not drawn again under its project.

Every row is **two lines** — the label, and its dim tail indented under it — which is the
two-line law every list keeps at this width.

**Typing still searches**, and a search has no sections and no tiers: the matches rise out
of the box exactly as they do at every other width, with `? ask here` and
`+ start a new conversation` against it at the foot.

A tap on a section's heading folds that section away. Mouse motion does nothing at this
width — there is no hover on glass — and every key still works, because a phone with a
hardware keyboard is a laptop.

## Opening a row on a phone — the sheet, and ‹ back

There is no second column under 60 columns, so `enter` — or a **tap**, in one gesture
rather than two — opens the row's card over the **whole frame**. The top row reads
`‹ back`, the title and the place are under it, and everything below is the same bands
the card draws on a wide screen.

The order is what you can act on first: the answers, then the state, then
`since you left`, then the work, then what is next, then where it left off, then the
files it produced. The repository, the keys and the spend are behind one `▸ more` at the
foot; `→` or `m`, or a tap on that line, opens it.

Keys on the sheet:

| key | what it does |
| --- | --- |
| `esc`, `←`, or a tap on `‹ back` | back to the inbox, with the cursor exactly where it was |
| `enter`, or a tap on the title | open the conversation this card is about |
| `↑` `↓` `PgUp` `PgDn` `g` `G` | scroll the card |
| a digit | answer the question the card is showing |
| `→` or `m` | open everything behind `▸ more` |
| `ctrl+e` / `ctrl+x` (or `p` / `s`) | on a standing item: pause it, stop it |

A frame too short for all the bands **drops them from the bottom** — never the title and
never the answers.

An `ask here` errand is one kind of sheet rather than a shape of its own: it opens the
exchange pane over the frame, the box at the foot is the errand's, and `tab` or `esc`
gives the keyboard back to the inbox with the row still standing on it.

**Rotating the phone costs nothing.** Crossing 60 columns swaps the inbox for the
two-column screen and back; a sheet open at 55 columns is simply the right pane's subject
at 90, and the cursor, what you typed and any errands are all still there.

## Approve a command from your phone — the answer bands

On a wide screen the answers to another window's question are chips on one row —
`1 allow once · 2 always · 3 deny`. Under 60 columns the same answers are drawn as
**full-width bands on the sheet**, one to a row, in the consent sheet's own shape: the
digit on the left, the label beside it, and the **whole row** is the target. A tap
anywhere on the band gives that answer, and so does the digit.

It is the same act and the same rules as at any width: the answers are the ones **that
session offered**, a window this one cannot reach is not answered, and once a key lands
the band reads `answered · waiting for it to pick that up` until the other session takes
it. A window with no way to leave an answer draws no bands at all.

The bar under the box is the phone's legend: at most three wide targets, drawn like the
answer bands but dim — `open · new · ask here` on the inbox, `‹ back · open · more` on a
sheet, `‹ back · send · more` on an errand. Tap one, or press the key it names. Below
width **24** the plain hint line is drawn instead.
