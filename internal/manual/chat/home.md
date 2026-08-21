# Home — every project and session on this machine

## See all my projects — /home

Type `/home`. It takes the whole screen and shows **every project on this machine and
every session in them**, not just the folder this window was started in.

The top line reads `home`, with `esc close` on the right. `esc` puts you back in exactly
the chat you came from, untouched — nothing was closed and nothing was sent while you were
looking.

There is no argument form. The screen is how you name what you want; a command that took a
project name would be asking you to type out the very thing home exists to show you.

Home is a glance you take, not a place you live. It does nothing on its own: no
notifications, no charts, no history graphs. You open it, you see where things stand, and
you leave.

## Why did a dashboard open when I started aforge — home greets you

**Home is the first thing you see when you open aforge.** The conversation your launch
would have opened is loaded and waiting underneath it: `esc` drops straight into it, and so
does `enter` on the row you are already standing on, which is where the cursor starts. In
effect the launch is the launch you always had, with home already open on top of it.

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

And on a machine with only one conversation — a first run — home stays out of the way by
itself. There is no setting for this and no flag to turn it off: whether home greets you
follows from how you launched and what the machine holds, both of which answer themselves.

Once you are in a conversation, `/home` opens the screen whenever you want it.

## Everything I have ever worked on — what home shows

Two columns, no borders.

**On the left**, each project is a dim heading — the last part of its folder, or `~` for
your home directory — with one line per session under it. **With nothing typed the list
hangs from the top of the frame**, as a page you are reading should:

```
  aforge-v2                          Pricing Research
  ▲ Pricing Research  4m
  ⠋ Port the Picker  12m             aforge-v2 · ~/src/aforge-v2
  ✓ Import Cleanup    3h
  ▸ …3 more, quiet…                  open in another window · waiting on you
                                     can I run: rm -rf build/
  pricing-api
  ○ Log Rotation     20d             ✓ Port the Picker              2h
                                       the roster resumes cleanly
```

Projects are ordered by the one you spoke in most recently. The cursor opens on the
conversation this window is in, and the preview on the right follows it.

A line is a glyph, the session's name, what it has going on, and how long since you last
spoke in it — or `open` where **this** terminal is holding the conversation behind the one
on screen, or `another window` where a *different* terminal is sitting on it and this one
therefore cannot open it (see below). The glyphs: `▲` it is stopped waiting
on you, a **turning spinner** where something is running this instant, `◌` work was left
unfinished, `✓` work landed since you last looked (see *What landed while I was away*),
`○` at rest. In screen-reader (linear) mode nothing animates and they are `!`, `*`, `o`,
`+` and `-`.

The left list stays deliberately calm — every row is dim except the one the cursor is on,
which takes the highlight. The one exception is `waiting on you`, which is brought up out
of the dim wherever it appears, because a screen whose whole job is triage cannot render
its most urgent fact in the same grey as an age.

Inside a project the order is **what wants you first**: sessions stopped on a question,
then ones with work running, then ones with work left unfinished, then the rest by when
you last spoke. Quiet ones past the first four collapse into one dim line,
`…3 more, quiet since 2d`.

## The pane on the right of home — the preview of the session under the cursor

**On the right** is a preview of whatever the cursor is on, read top to bottom as bands
separated by blank lines — no rules and no borders anywhere. The first three bands are
always the same:

1. the conversation's **name**, the brightest text on the screen and the same treatment the
   highlighted row on the left wears, so the eye travels between them;
2. one dim line of **where it is** — project · path. That line is a **link**: cmd+click it
   (ctrl+click on Linux) and the project's folder opens, in the terminals that make
   hyperlinks. A folder that is no longer on this disk is named and not linked;
3. what it is **doing right now**, and — if it is stopped on a question — that question, in
   full.

**Then the card takes the shape of the session's state.** A session with **work running**
leads with the work, alive: each task wears the same one-cell state mark the rest of aforge
uses — a turning spinner while it runs, `◌` queued or left unfinished, `✗` failed, `?`
finished and needs your look, `✓` landed — with a count-up on the right of a running row
saying how long it has been going, and a **task's subtasks indented under it while any of
the family runs**. The last thing said trails underneath.

A **quiet session** leads with **the last thing said** — where you left off — and then
its record: up to four tasks, each with **what it came to** in a sentence underneath.
Nothing in a quiet card moves.

Last comes a dim line of **facts**: `touched 12 files · spent $1.25 · 34k tokens · last
active 12m`. The file count is everything this conversation's tasks wrote; a task count
(`7 tasks`) appears in front only when there were more than the card could show.

**It is there in both of home's shapes** and it never moves: when the list becomes a
drop-up under your typing the card stays exactly where it is, because a card is assembled
downward from its title and lifting it would take the facts off the bottom rather than move
it down the screen.

**It follows the cursor through a search as well.** Walking `↑`/`↓` through filtered matches
switches the card to each one, so you are choosing between conversations by what they are
rather than by name alone. It goes **empty** — nothing drawn at all — when the cursor is on
something that is not a conversation: a project's `…13 more` line, or the
`start a new conversation` row, which has nothing to preview because that chat does not
exist yet.

A frame too short for all of that drops bands from the bottom — the facts go first — and
never touches the name. Under 80 columns the right column is dropped entirely and the list
takes the whole frame, because an index you can read beats a preview you cannot.

Nothing that is zero is drawn, anywhere. A chat that ran no tasks says nothing about tasks;
one that spent nothing says nothing about spending; a facts line with no facts is not
drawn at all.

## Why did the list jump to the bottom when I typed — home's two shapes

Home has **two panes and two shapes**, and only the left pane changes shape.

**With nothing typed it is a dashboard.** The list hangs from the top of the frame, the
cursor sits on the conversation this window is in, and the preview card is beside it. That
is what home is for: one page of everything the machine holds, read top to bottom.

**The moment you type a character the list becomes a drop-up.** It lifts so that its last
row — the action row, `start a new conversation: "…"` — lands directly above the box you
are typing into, and the matches rise above it **best one first**: the strongest match is
the row immediately above the action row, one `↑` away, and each `↑` past it walks into a
weaker one. Everything to do with typing is then one cluster at the foot: your words, the
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
`enter`.

What happens is a **second conversation**, not this one moving. The conversation you were
in is left running exactly where it was — its turn keeps streaming into its own transcript,
its tasks keep running, it keeps its lock — and the new one is built on **its own**
workspace, with that project's approval rules, its crew, its spend ceiling and its saved
shapes of work. Nothing is carried across, because nothing crosses.

The status line then reads `2 open`, and `tab` over an empty message box goes back.

Two refusals are still possible and both leave home standing:

- the project's folder is gone: `that folder is gone · <path>`, and nothing is opened. Home
  never stats a folder until you press `enter` on it, so a repository deleted or moved since
  its last conversation is found here rather than after the fact;
- this terminal already holds eight: `8 open is as many as aforge holds — /quit closes this
  one`.

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

## Switch between projects without leaving — work on two projects at once in one terminal

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
so one `↑` puts the best one in front of you in full: its project and path, what it is
doing, the work it ran and the last thing said in it. That is how you tell two similarly
named conversations apart without opening either. On the `start a new conversation` row the
card is empty, because there is no conversation there yet.

## Why is the best search result at the bottom — the order of the matches

**The strongest match is the row directly above `start a new conversation`, so one `↑` gets
you to it.** Each `↑` past that walks into a weaker match, and `↓` comes back down toward
the box.

That is upside-down next to an ordinary ranked list, and deliberately so. A list you read
*downward* puts its best answer at the top. While you are typing, home's list is read
*upward* out of the box — so its best answer belongs at the bottom, under your hand and one
keystroke away, instead of being the furthest row from the key you reach for. With a dozen
matches on screen the top-ranked one is still one `↑`.

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
 ○ Pricing                                                             12m   ← one ↑

 + start a new conversation: "pricing"
 ───────────────────────────────────────────────────────────────────────────
 › pricing
 enter starts a new conversation and sends this · ↑ pick a match · esc clear
```

**The cursor rests on the action row by default.** So typing and pressing `enter` starts a
fresh conversation in this project and sends what you typed, exactly as it always has,
however many matches are on screen.

One `↑` steps off that row **up** onto the best match — the order runs weakest at the top,
best at the bottom, and *Why is the best search result at the bottom* says why. Then you are
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

A project shows its first four conversations and folds the rest into one dim line,
`…13 more, quiet since 1d`, with a `▸` in front of it.

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

Two things it will not do: it does nothing when the box already has words in it, and it
does nothing on a machine with nowhere else to go.

## What does pressing space twice do — the home door at the foot of a conversation

When the box is empty and there is somewhere else to go, the dim line between the
conversation and the box reads exactly:

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

It does not appear at all on a machine whose only conversation is the one you are in — the
same rule that keeps home from greeting a first run. A door that is drawn is a door that
goes somewhere.

## Is there a key for home?

`/home` opens it, and **space twice on an empty box** goes there from inside a conversation
— see the two sections above. There is no `ctrl+` chord for home: the plain ones are all
taken (`ctrl+.` is the task page, `/history`), and the chords that were left — the `alt+`
letters — arrive in some terminals and do nothing at all in others. The double space is
the chord home has instead.

## What landed while I was away — the ✓ and "since you last looked"

Home remembers when you last closed it, and marks what finished after that. A resting
session whose tasks landed while you were not looking wears `✓` instead of `○`, its line
reads `2 landed` where the task count would be, and its card captions those rows with a
dim `since you last looked` — their ticks lit up out of the grey. That is the whole
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
key away and one in another project tells you where to go.

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

A machine that has held nothing yet draws one line:
`nothing here yet — say something and this fills up`.

Over `--host` home refuses to open and says
`home shows this machine's projects, and this session is on another`. The projects it
would read are under *this* computer's `~/.aforge/v3`, and the work is on the far end — a
screen full of the wrong machine's projects would be a confident lie.

## Does home update while I look at it?

Yes, every few seconds, by reading the folders again. A task landing in another window,
work somebody starts in a second terminal, or a session stopping to ask a question all
show up without you doing anything. There is no file watcher and nothing runs when the
screen is closed.

And while any row has work running, home is **visibly alive**: the spinner on that row and
on its card turns continuously, and a running task's count-up climbs, exactly as they do
in the conversation that owns the work. The moment nothing on screen is running the page
falls still again — a home full of finished work animates nothing. In screen-reader
(linear) mode nothing ever animates.

The cursor stays on the row it was on rather than on the line number — the order genuinely
changes when work starts or finishes, and a cursor that stayed put would move you onto
something else between two glances.
