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
  ● Port the Picker  12m             aforge-v2 · ~/src/aforge-v2
  ○ Import Cleanup    3h
  ▸ …3 more, quiet…                  open in another window · waiting on you
                                     can I run: rm -rf build/
  pricing-api
  ○ Log Rotation     20d             done  Port the Picker            2h
                                       the roster resumes cleanly
```

Projects are ordered by the one you spoke in most recently. The cursor opens on the
conversation this window is in, and the preview on the right follows it.

A line is a glyph, the session's name, what it has going on, and how long since you last
spoke in it — or `another window` where another terminal is sitting on that conversation
and this one therefore cannot open it (see below). The glyphs: `▲` it is stopped waiting on you, `●` something is running, `◌`
work was left unfinished, `○` at rest. On a terminal that cannot draw them they are `!`,
`*`, `o` and `-`.

The left list stays deliberately calm — every row is dim except the one the cursor is on,
which takes the highlight. The one exception is `waiting on you`, which is brought up out
of the dim wherever it appears, because a screen whose whole job is triage cannot render
its most urgent fact in the same grey as an age.

Inside a project the order is **what wants you first**: sessions stopped on a question,
then ones with work running, then ones with work left unfinished, then the rest by when
you last spoke. Quiet ones past the first four collapse into one dim line,
`…3 more, quiet since 2d`.

## The pane on the right of home — the preview of the session under the cursor

**On the right** is a preview of whatever the cursor is on, and it is read top to bottom
as bands separated by blank lines — no rules and no borders anywhere:

1. the conversation's **name**, the brightest text on the screen and the same treatment the
   highlighted row on the left wears, so the eye travels between them;
2. one dim line of **where it is** — project · path. That line is a **link**: cmd+click it
   (ctrl+click on Linux) and the project's folder opens, in the terminals that make
   hyperlinks. A folder that is no longer on this disk is named and not linked;
3. what it is **doing right now**, and — if it is stopped on a question — that question, in
   full;
4. the **work it ran**: up to four tasks, each with what it came to underneath;
5. the **last thing said** in it;
6. a dim line of **facts**: `spent $1.25 · 34k tokens · last active 12m`.

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
   `▸ …2 more keeping an eye`;
3. one dim line of **how big the project is** and when anybody was last in it —
   `12 conversations · 34 tasks · spent $4.10 · last active 2h`. That line is where to look
   for **how many conversations** a project has and how many tasks have run in it.

**Nothing that is zero is drawn.** A project nobody has run a task in says nothing about
tasks; one that has spent nothing says nothing about spending; a project with a single chat
in it and no work reads `1 conversation · last active 5m` and no more. A project with
nothing standing has no second band at all — not an empty heading.

A frame too short for all three drops whole bands from the bottom — the counting line goes
first — and never touches the project's name.

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

`enter` opens the session under the cursor. That is the same door `/resume` walks through:
the running turn is interrupted, the chat you were in is closed, the chosen journal is
opened and replayed, and the surface says `resumed <path>`.

`enter` on the one you are already in simply steps into it and says nothing — it is
already loaded underneath, so there is nothing to reopen and nothing to announce. That is
what makes `enter` the calm keystroke on a launch: the cursor starts on that very row.

The foot line reads exactly:

```
type to search or start something new · ↑↓ pick · enter open
```

and the line under it says what the keyboard does, which changes with what the cursor is
on — at rest, `↑↓ move · enter open · esc close`.

## Why can't I open a session from home — open in another window

A conversation that another terminal already has open cannot be opened by this one: two
aforge windows on one journal would both append to it and neither would end up with the
conversation. Home knows this **before you press anything**, so it says so twice over.

**On the row.** A conversation sitting idle in another terminal reads `another window`
where its rollup would be, so a locked door does not look like an ordinary one.

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

## What home will not do yet — opening another project's work

**`enter` only opens sessions of the project this window is in.** Every other project is
shown, and its heading carries a dim `elsewhere` to say so. Pressing `enter` on one of its
rows opens nothing and says `elsewhere · <the project's path>` — the path to start aforge
in. **That path is clickable**, so you can open the folder from here even though the
conversation cannot be; the "what is on the screen" page has which terminals do that.

This is a limit and not a bug. A window's approval rules, its crew, its spend ceiling and
its saved shapes of work were all resolved from the workspace it launched in; carrying a
chat across without carrying those would be a window quietly running under another
project's permissions. Until that is built, home shows you the whole machine and moves you
around inside one project of it.

So: home is the honest answer to "what have I been doing everywhere". It is not yet the
answer to "put me in that other project without changing terminals".

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

## Why a session says it needs you — waiting on you

A session that has asked you something and can go no further writes that down, and home is
where you see it without opening the window it is in. The row wears `▲`, its rollup reads
exactly `waiting on you`, and **it sorts to the top of its project** — above work that is
running, above everything you spoke in more recently.

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

## Why a task says incomplete on home

The project's record of its work is append-only: a task writes a row when it starts and
another when it lands. So a machine that lost power, or an aforge that was killed, leaves
rows on disk that say `running` forever.

Home never repeats that claim. **It asks the session itself.** A live session says out
loud, every few seconds, which task nodes it currently has out; a `running` row is drawn
as `running` only when the session that ran it is still alive and still names that node.
Every other live-looking row is `incomplete` — work that was under way when the window
went — and the same rule decides the glyph: `◌`, not `●`.

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
show up without you doing anything. There is no file watcher: home reads, and closing the
screen stops the reading. (Standing items — reminders, watches, rules — are a different
mechanism and do keep going; see the keeping-an-eye page.)

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
- `paused` — you pressed `p` on it.

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

**Typing hides the band.** The box at the foot searches conversations — by name,
by project, by what their tasks came to — and rows the query never considered
would be rows drawn as though it had.

## How do I pause a reminder from home — p and s on an item row

Put the cursor on the row and press **`p` to pause it** or **`s` to stop it for
good**. Home says `paused · <your words>` or `stopped · <your words>` at the foot
and redraws the row from the store, so what you see is what is on disk rather
than what the keypress hoped for.

They are bare letters, and they are only keys **while nothing is typed and the
cursor is on one of these rows**. With anything in the box a `p` is a `p` — the
box is a search and a new conversation at the same moment, and the rows are not
on screen then anyway.

A window whose build cannot write to the store says `this window cannot change
it` rather than pretending.

`enter` on the row **opens the conversation that asked for it** — that is the
answer to "why did I get this?", and it is the same door, with the same limit, a
conversation row has: a project this window is not in says `elsewhere · <path>`.
Something you set up from home that never became a conversation says
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
5. a dim line of **how much**: `4 runs · spent $0.08`;
6. the keys, dim: `enter open where it was asked · p pause · s stop`.

Nothing that is zero is drawn. Something set up ten seconds ago is a title, a
place and a cadence, and nothing else — no `0 runs`, no `$0.00`.

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
```

It says **`installed`** when the machine's own timer is set up, so the checking
happens with no terminal open at all, and **`while a window is open`** when it is
not — any aforge window that is up runs the same pass every five minutes. The
`last check` half is dropped when nothing has ever run. A build that cannot
answer the question prints no line rather than guessing.

## Why did a card appear asking me about a reminder — saying yes to something standing

When you say something that would keep working after this window closes — "remind
me at 6 to leave", "tell me when CI on main goes red", "every Monday post the
standup note" — a card appears in the conversation and **nothing is set up until
you answer it**:

```
╭─ ? ◦ every Monday at 9, post the standup ─────────────────
│ every Monday at 9, post the standup note from the git log
│ when · Mondays at 9am
│ costs · about $0.02 a run, at most once a day
│ [ 1 yes, set it up ]  [ 2 change when ]  [ 3 once, not standing ]
╰───────────────────────────────────────────────────────────
```

Two bands make it different from the card that proposes a task: **`when ·`**, in
the words you said or the words it worked out, and **`costs ·`** — what one run
may spend and how often it may run. A watch that has to *look* at something adds
`checked every 5 minutes`, because that is when the looking happens; a reminder
does not, because nothing is examined between now and Monday.

If it made the timing up rather than reading it off what you said, the band asks
instead of stating: `Mondays at 9am — you didn't say, so that's my guess.
Right?`

**The three answers**, by key, by `←`/`→` and `enter`, or by clicking one:

- `1 yes, set it up` — it stands.
- `2 change when` — the box below becomes a place to say when instead; `enter`
  sends your words back and nothing is set up until a new card comes.
- `3 once, not standing` — do it now and leave nothing behind.

`esc` says no. **There is no clock on this one**: no bar, no countdown, and no
moment where it answers on your behalf — it waits while you read it. That is the
opposite of the task card, whose clock approves on silence: something that spends
money forever with nobody in the room is not a thing silence should agree to. If
the turn ends with the card still up — you interrupt it, or the window closes —
it says `ended · nothing was set up`, and nothing was.

**The first time you ever set one up** there is one more question, on the same
card: `keep checking when no window is open?` with `1 yes, always` and
`2 only while a window is open`. Saying always is what installs the machine's own
timer. It is asked once, ever.

## What does a ◦ line in the middle of my conversation mean — news from something standing

Once something is set up it writes **one line and never more** into the
conversation that asked for it:

```
◦ every Monday at 9 · set up
◦ every Monday at 9 · said: the standup note is in notes/standup.md
▲ keep main green · needs your look: the fix touches migrations
∙ remind me at 6 to leave · stopped
```

That is the whole of it. **A check that found nothing writes nothing** — a watch
that ran faithfully for thirty mornings and found nothing leaves your
conversation exactly as quiet as it was, and the row on home is where you go to
confirm it really did look.
## Ask here — a reminder or a watch without opening a conversation

While you are typing, the row directly above `start a new conversation` is
`ask here: "…"`. It answers the sentence **in the pane on the right** — a real conversation
with a real transcript, kept outside `~/.aforge/v3/projects` so this list never grows a row
for a one-off errand. One `↑` reaches it, and `ctrl+enter` (or `alt+enter`) does it without
leaving the box.

The whole of it — where the record goes, how the card is answered, and how to turn the
exchange into an ordinary conversation — is on its own page: *Asking from home*.
