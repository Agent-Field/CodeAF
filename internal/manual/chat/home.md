# Home — one list of everything on this machine

## See all my projects — /home

Type `/home`. It takes the whole screen and shows **every conversation on this machine,
from every project**, not just the folder this window was started in.

The top line reads `aforge` on the left, with the machine's own vital signs on the
right — `2 want you · 4 moving · $0.55 / $500.00 · tue 1:11pm` (its own section below).
`esc` puts you back in exactly the chat you came from, untouched — nothing was closed and
nothing was sent while you were looking. The resting foot does not spend a cell naming it:
it reads exactly `type to search or start something new · ↑↓ pick · enter open · tab next
place`, four keys and no more, and `alt+.` draws the whole map over the cells you are
already reading.

There is no argument form. The screen is how you name what you want; a command that took a
project name would be asking you to type out the very thing home exists to show you.

Under the tab bar it is **one flat list**, ranked by what wants you first, with the project
demoted to a tag out at the right of each row. There is no project tree and no folded block
of other projects; `alt+g` groups the same list by project when you want that shape.

Home is the **first of seven places** — home, tasks, standing, memory, spend, search,
settings — drawn as a tab bar under the top line, with `tab` and `alt+1`…`alt+7` between
them. See **Places**.

It still does nothing on its own: no notifications, no charts, no history graphs. You open
it, you see where things stand, and you either act on something or leave.

## Why did a dashboard open when I started aforge — home greets you

**Home is the first thing you see when you open aforge.** The conversation your launch
would have opened is loaded and waiting underneath it: `esc` drops straight into it. In
effect the launch is the launch you always had, with home already open on top of it.

**The cursor starts on the conversation this window is holding** — the row `esc` drops
back into, visibly selected, wearing the one band on the screen. So the first frame already
answers "where am I". `↑` off the top of the list walks up onto the **tab bar** — the row of
seven words over the list, which the cursor can stand on and walk along (see *Where the
cursor starts*, and *How do I move between the tabs with the arrow keys* on the `places`
page); `esc` goes on with what you were doing.

Nothing about *which* conversation opens is changed by this. The door picks it exactly as it
always did — this directory's most recently spoken-in chat, or a fresh one — before home is
drawn at all.

It greets you only when it has something to say. All of these have to be true:

- You opened aforge **without naming a conversation**. `aforge` or `aforge chat`.
- The machine holds **a conversation other than the one this launch opened**. Somewhere
  else to go, in other words.
- It is a real terminal session — not `--once`, not `--host`.

When home greets you there is no welcome box: home's list already holds every conversation
the box's `recent sessions` would have, and more.

## Skip the home screen — launching straight into a conversation

Four ways, and each of them is you saying which conversation you mean:

| What you run | What you get |
|---|---|
| `aforge chat --session <path>` | that conversation, no home |
| `aforge resume` | the session picker, no home |
| `aforge chat --once "text"` | one reply, printed; no surface at all |
| `aforge --host <machine>` | the far machine's session, no greeting — `space` `space` opens that machine's home |

And on a machine with only one conversation — a first run — home does not greet you.
There is no setting for this and no flag to turn it off: whether home greets you follows
from how you launched and what the machine holds, both of which answer themselves.

Not being greeted is not the same as being out of reach. Once you are in a conversation,
`/home` — or `space` twice on an empty box — opens the screen whenever you want it, on a
machine with one conversation and on one with none; what opens there is an empty home
rather than nothing (see *Home is empty — what an empty home shows*).

## Everything I have ever worked on — what home shows

**One column, no borders**, hanging from the top of the frame with nothing typed. A card
stands beside it only on a frame **160 columns or wider** (see *Why is there no preview on
the right*). Top to bottom:

```
 aforge                            2 want you · 4 moving · $0.55 / $500.00 · tue 1:11pm
  home   tasks 1   standing   memory 2   spend   search   settings
 ─────────────────────────────────────────────────────────────────────────────────────────
 since you left · 3h
 a watch fired at 6am — nothing had changed, and it says so                      standing
 2 tasks landed                                                                     tasks

 20 chats · what wants you first     alt+g group by project · alt+q hide the quiet ones
 ? Swarm Task Splitting     aforge-v2   asks: add a --report-only mode?              2h
 ? Gmail cleanup routine    ~           wants to send on your behalf                 6h
 ◐ Bounty Reward Companies  leadgen     2 tasks running · reading filings            3h
 ○ Researching Santosh      aforge-v2   3 files made                               here
 ▸ 15 more, quiet since aug 21
```

1. **the pulse line**, the machine's own vital signs (its own section below);
2. **the seven-place tab bar** and a dim rule under it — that is the router's frame, and
   *Places* describes it;
3. **the `since you left` ledger**, drawn only when something happened on its own while you
   were away, each line a door into the place it names;
4. **the section line** — `20 chats · what wants you first` on the left, and the two views
   on the right — drawn only when something needs you or is moving;
5. **the rows**, one flat ranked list;
6. **one fold** over everything the list is not drawing.

**A row is:** the state mark, the name, and then out at the right margin the project as a
tag, the note, and the age — or the word `here` in place of the age on the conversation
this window is holding. The marks are `?` (amber) it is asking you something, `◐` it is
moving, `○` at rest, `=` a standing item you paused. The note is the one fact the row is
about: `asks: add a --report-only mode?`, `wants to send on your behalf`,
`2 tasks running · reading filings`, `3 files made`, `ran a saved shape`.

**A narrow frame drops facts in one order and never the name:** the note goes first, then
the project tag, then the age. Under **80 columns** there is no note at all.

Both kinds of row are on the one list: a conversation, and a standing item that is asking
you something or firing right now. An `ask here` errand sits above everything, because it
is a thing you asked for a minute ago.

## Why is my project tree gone — home is one flat ranked list

**It is gone on purpose, and nothing under it is out of reach.** Home used to be a tree:
every project a heading, three of them drawn open, the rest folded away under an
`elsewhere` rule, with two strips standing over the whole thing. That shape answers "where
is my work", and somebody opening this screen twenty times a day is not asking that — with
ten to twenty live conversations and three to five projects the tree spent five rows of
scaffolding to reach twenty rows of content.

So the project became **a tag on the row** and the list became one ranked column. The
order is:

1. **what needs you** — anything stopped on a question, **longest wait first**. A thing
   that has been stopped for six hours has already cost more than one stopped for ten
   minutes, so it is above it whatever else is true.
2. **what is moving** — **busiest first**, most tasks running, with the longest-running
   settling a tie.
3. **everything else**, most recently spoken in first.

`alt+g` puts the projects back as headings whenever you want them (*How do I group home by
project*). Nothing else about the list changes when you do.

## What is since you left — the ledger at the top of home

**Things that happened on their own while you were not looking**, above the list, under
one heading that says how long ago that was:

```
 since you left · 3h
 a watch fired at 6am — nothing had changed, and it says so                      standing
 2 tasks landed                                                                     tasks
```

Three kinds of line, newest first:

- **a standing item that fired** — its own last-look line, in its own words;
- **`N tasks landed`** — work that finished anywhere on the machine since your last look;
- **what memory learned or let go** — `learned 2 things, let go of 1`.

**Every line is a door.** The lowercase word out at the right of the line is the place it
goes to — `standing`, `tasks`, `memory` — and `enter` takes you there. That is the whole
of how you find these pages: you meet `memory` on the day it has something to tell you,
rather than being told a list of places exists. The hint under the box says
`enter opens the place this happened in · esc close`.

**It is drawn only when there is something in it.** A machine that did nothing while you
were away has no `since you left` heading at all — it is not a section that stands empty.
The look stamp it measures from is when you last **closed** home.

**The memory line is not wired yet.** The figures behind `learned … , let go of …` come
through a seam this surface does not have a memory store on, so they read zero and, under
the rule that nothing zero is drawn, the line does not appear. When that seam is wired the
line appears with no other change.

## How do I group home by project — alt+g

**Press `alt+g`.** The same rows are drawn in blocks, one per project, each under a dim
heading with the project's name — **this window's own project first**, then the rest by
what has happened in them most recently. The project tag comes off the rows, because the
heading is saying it.

Press it again and the flat ranked list comes back.

- **The rows are chosen before they are grouped.** Home still draws eight rows and a fold;
  grouping arranges the rows it was going to draw, so a project whose only conversation is
  behind the fold has no heading either.
- **A heading is not a row.** `↑`/`↓` walk straight over the project names; there is
  nothing to open on one and no card for one.
- **It is remembered for as long as aforge is running** — closing home and opening it
  again keeps it — and it is **not a setting**: nothing is written to disk, and a fresh
  aforge starts flat.
- **It does nothing while you are typing**, and nothing at phone width. With something in
  the box the column is the matches rising out of it, and under 60 columns home is already
  an inbox with the projects under it — in both cases a key that silently changed a list
  that is not on screen would be the worst kind of chord.

## How do I hide the quiet chats — alt+q hide the quiet ones

**Press `alt+q`.** Everything that is neither asking you something nor moving leaves the
list, and the fold at the foot says so in one word — `▸ 12 more, quiet`. Press it again
and they come back.

It is the same key everywhere the section line names it: the right of that line reads
`alt+g group by project · alt+q hide the quiet ones`, and on a frame with no room for both
clauses it keeps `alt+g group by project` alone.

Like `alt+g` it is **remembered for as long as the program is running and is not a
setting** — nothing goes to disk — and like `alt+g` it does nothing while something is
typed or at phone width, because then this list is not what is on the screen.

`alt+q` never hides a row that wants you. That is what it is for: on a machine with two
questions and eighteen quiet conversations it leaves the two.

## Where did the rest of my chats go — eight rows and one fold

**Home draws eight rows and then one fold over everything else:**

```
▸ 15 more, quiet since aug 21
```

The line says **how many** rows it stands for and, when the first hidden row is a quiet
one, **how far back** they go. Under `alt+q` the clause is simply `quiet`.

**It is a door.** `enter` or `→` on it shows every row with no cap at all, and its mark
becomes `▾`; `enter` or `←` folds them back. The hint under the box says
`enter or → show them · esc close`, and `enter or ← fold them away · esc close` while it
is open. The fold line stays on screen while it is open, because it is the way back.

**The eight are the eight that want you most** — the top of the ranked list — so the fold
never hides a question or a running task while showing something quiet. And **typing sees
straight through it**: a search has no cap and no fold, and matches every conversation on
the machine including the ones the fold was holding.

## What needs me — the ? rows at the top of home

**The top of the list, and they wear an amber `?`.** There is no `needs you` strip any
more, because the list is sorted by exactly what that strip used to gather — so the strip
would have been the same reading twice, one row further up the screen.

A row is there when something has stopped and cannot go on without you:

- a conversation in another window stopped on a question — a command to approve, a task to
  approve, a card about something standing;
- a reminder, watch or rule that stopped and wants an answer;
- an `ask here` errand holding a card.

The note on the row is **what it is asking**, in the question's own words — `asks: add a
--report-only mode?`, or `wants to send on your behalf` for a command it wants to run —
and the age is **how long it has been waiting**. The longest wait is at the top.

**You can answer most of them without going anywhere.** With the cursor on the row, the
question and its answers are drawn on the line above the box and the digits answer it
(*Answer a question from home*), and `→` opens the row's verbs in the question's own words
(*How do I answer without opening the chat*).

Amber is spent on the `?` mark and on the answers under the list, and on nothing else. A
machine with nothing waiting has no accent on it at all.

## What opens when I press a landed row on home — I clicked a needs you row and it opened the chat

**It opens the conversation, and that is now all it does.** Home no longer draws a row of
its own for a task that landed and needs your look: a row on this list is a conversation
or a standing item, and pressing `enter` opens it — whatever project it belongs to, with
the conversation you were in left running behind it.

Work that landed reaches home two other ways instead, and neither is a row named after a
task:

- **the `since you left` ledger** says `2 tasks landed`, and `enter` on that line opens the
  **tasks** place, which is the record of every piece of work and where one waiting for you
  is named;
- **the conversation's own row** carries it in the note — `3 files made` — and its card,
  on a wide frame, has the work band with what each task came to.

A conversation another terminal is holding is not refused: `enter` offers to MOVE it here,
and a second `enter` does it — see *Continue a conversation from another terminal*.

## How do I clear a needs your look row — settling work that landed

Work that landed needing a look **waits until you decide about it**, however many days that
is. It is not stale and it does not age out: nothing more happens to that work until
somebody accepts it or sends it back.

Three ways to settle it, and they are the same door:

- **the landing card in the conversation** — `[a] accept`, `[l] look again`, `[n] not
  right`, `[d] decide these for me`;
- **the task's room** — enter on its roster row opens it, and the same four choices stand
  at the foot of the page; `a`, `l`, `n`, `d` over an empty box answer it with nothing
  selected;
- **just say so.** "accept task 7", "that one isn't finished", "have another look at task
  7" — aforge settles it through its `tasks` tool. Whichever is used first wins; the other
  says `already answered`.

Accepting merges the task's branch and unblocks everything queued behind it. The tasks page
has the whole of it under *Why is the task waiting for me*.

**Home is not where it is settled.** It is where you find it: the ledger line `N tasks
landed` opens the tasks place, and the conversation's row opens the chat the work ran in.

## What is running everywhere — the ◐ rows, and what goes in the moving column

**The `◐` rows, straight under the ones that are asking you something.** There is no
`moving` strip and no moving column any more; being in flight is a rank in the one list
rather than a place on the screen.

A row is `◐` when it has tasks running, and also when it is simply **mid-turn** — the model
thinking or a tool out, with no task ever made. Conversations this terminal is holding open
behind the one on screen count, and so does a watch that is firing this instant.

They are ordered **busiest first** — most tasks running — with the longest-running settling
a tie, and the note says what is actually happening: `2 tasks running · reading filings`,
or `· checking what it left` while a node is being looked at, wherever it is being run.

Two other places say the same thing more briefly:

- **the pulse line** at the top — `4 moving` — which is the same count over the whole
  machine, a conversation with three tasks out counting as three;
- **the tasks place**, which is the full record rather than a count.

## Why does only one row spin — the one spinner on home

**The resting list does not animate at all.** `◐` is a still mark: a row with work running
wears it, and the page stays a still page redrawn every three seconds.

The cell that can turn is an **`ask here` errand's own row** while its answer is coming,
and — **while you are typing** — the most recently started conversation among the matches,
which wears the spinner instead of its `◐`. However much is happening, **exactly one cell
on the frame ever turns.**

Two reasons, and they are the same reason:

- **Calm.** Eleven braille cells turning at once is a screen you cannot glance at, and one
  moving cell says *this machine is working* exactly as well.
- **A flat wire.** The frame the spinner costs is the same frame whether one thing is
  running or twenty, so a busy machine costs an ssh connection no more than a quiet one.

In screen-reader (linear) mode nothing turns at all.

## Nothing needs me this morning — what a quiet home draws

**Nothing that has nothing to say is drawn.** On a machine where nothing is asking and
nothing is moving there is no section line, no `since you left`, no accent anywhere — just
the pulse, the tab bar, the rule, the rows and the fold:

```
 aforge                                                                   tue 8:04am
  home   tasks   standing   memory   spend   search   settings
 ────────────────────────────────────────────────────────────────────────────────────
 ○ Swarm Task Splitting Ideation     aforge-v2                                    2h
 ○ Bounty Reward Companies           leadgen                                      3h
 ○ Thor Fight Clip Generation        media                                       13h
 ▸ 17 more
 › say what you want done                                        here ~/aforge-v2
 type to search or start something new · ↑↓ pick · enter open · tab next place
```

Eight rows is a legitimate home. The section line
(`20 chats · what wants you first`) appears the moment something needs you or is moving,
and goes again when it stops.

**Typing changes the shape and not the data.** A search is the matches rising out of the
box with `? ask here` and `+ start a new conversation` against it, at every width — no
section line, no ledger, no fold, no grouping.

A machine with **no conversations at all** draws `nothing here yet — say something and this
fills up` where the first row would be, over two rows split at the dash. An empty home is
the same screen with fewer rows, never a different screen.

## Why is there no preview on the right — the card, and the 160-column rule

**Below 160 columns home is one column and there is no card at all.** That is deliberate:
at an ordinary width the card was showing you what pressing `enter` shows a beat later, and
the **note on the row** now carries the one fact it was really for — what a conversation is
stopped on, what is in flight in it, what it made.

The exact number is the sum of its parts: 120 cells is what a row wants to carry its mark,
name, project tag, note and age with none of them giving way, the gutter is 4, and a card
that can still say a whole sentence is 36 — so the card tier begins at **160**, and the
card is never paid for out of the list.

**Past 160 the list and the card split every extra cell down the middle**, and the card
stops growing at **56**. So the card is 36 wide on a 160-column frame, 48 at 184, 56 at 200
and 56 on anything wider, and the list is never below the 120 it asks for at any width —
that is arithmetic rather than a promise. The card stops at 56 because its clauses have the
room they need there and the cells after that do more good in the list you read twenty rows
of.

**At 160 and wider the card stands on the right and it ACTS.** It is the title with the
place line under it, and five bands, each drawn only if it has something to say:

```
Swarm Task Splitting Ideation
~/aforge-v2 · master, 1 file dirty · here


it is stopped on you
Add a --report-only mode so the report can be
regenerated without re-running the sweep?
1 do it · 2 leave it · enter open and talk

work
✓ toy-scale validation of decomposition             $1.63
▸ 3 more tasks                                      tasks

made for you
· swarm-decomposition.md                               2h


spent $1.63 · 3.6M tokens · thinking high


→ verbs: put it away, new chat here, open folder, copy path
```

The two blank rows are not an accident — they are where one **group** of the card ends and
the next begins (*How much air is on the card*).

- **the place line**, drawn directly under the title with no blank row between them because
  the address is the title's second line rather than a fact of its own, carries the repository — the branch and the dirty count
  as one clause, `master, 1 file dirty`, and `that folder is gone` in its place where the
  directory is not there any more — then the door word: `here` for the conversation this
  window is holding, `open in another window` for one somebody else has;
- **`it is stopped on you`**, the question in its own words, and the keys that answer it
  from here. A digit sends the answer without opening anything, and `enter open and talk`
  on the end of that row is the third thing you can do with it — open the conversation that
  asked and answer it there, which is what you want when one line is not the whole story.
  The answer keys are amber, the way out is dim, because they are two different offers.
  The band is absent entirely for a conversation nobody is asking anything of;
- **`work`** — each task with its mark and what it cost, a run that is not done keeping its
  outcome under it, then `▸ N more tasks` with `tasks` out at the margin. That line **names
  the tasks place** rather than unfolding: a card is not the place that holds them.
  **Clicking a task's own row opens that task's record** — the same card `enter` opens on
  the tasks place;
- **`made for you`** — the files it left behind, each a door you can click, with what it is
  called and how long ago it landed. A conversation that made nothing has no band at all;
- **one facts line** — what it cost, the tokens, and the rung work started here would think
  at (`thinking high`); `ctrl+v` still does not move a conversation's own rung from here;
- and **`→ verbs: …`**, which names what can be done and never the letters.

The old `keys` legend is gone from the card. Its last line names the strip instead —
`→ verbs: …` — because a letter is a verb only while the strip naming it is on screen.

**The cursor is always on a row of the list**, so there is no state in which this card is
about something else (*Where did the machine's own card go*).

## How do I answer without opening the chat — the verb strip on a row

**Press `→`.** A strip opens **directly under the row you are standing on**, carrying that
row's own verbs, and **while it is drawn those letters are the verbs and the composer is
asleep**:

```
y let it send   n not this time   a put it away   t new chat here
esc or ← to leave · enter opens it instead
```

The first line is the strip, drawn in the list itself with the row it acts on directly
above it; the second is the hint line under the box, which says both ways out while it is
up. `esc` or `←` closes it, `enter` still opens the row, and moving off the row with `↑` or
`↓` closes it too — verbs belong to one row. **The strip pushes the rest of the list down by
its own height** and the frame stays exactly as tall as it was, so the composer does not
move; that visible displacement is exactly what makes the bare letters safe.

**The verbs are the row's own and nothing invents one:**

- a row that is **asking a question** offers the question's own first two option words on
  `y` and `n` — `y let it send   n not this time` — and answering from here is the same act
  and the same record as answering it in the window it belongs to;
- a **conversation** offers `a put it away`, and — where it has a folder —
  `t new chat here`, `o open folder`, `c copy path`;
- a **standing item** offers `p pause it`, or `r resume it` when it is already paused.

A conversation that is not asking anything has no `y`; a row with no folder has no
`open folder`.

**The digits still answer on the row.** A question with more than two answers is answered
with `1`, `2`, `3` — they are drawn on the line above the box (*Answer a question from
home*) — and the strip carries the first two of them in words.

**The chords are unchanged and need no strip:** `ctrl+t` new chat here, `ctrl+o` open
folder, `ctrl+y` copy path, `ctrl+e` put away or pause, `ctrl+x` stop a standing item for
good, `ctrl+v` think harder. And every printable key still goes to the box — the foot
promises `type to search or start something new`, and the strip is the one state on this
screen where that is suspended, which is why it has to be visible.

The card names it — `→ verbs: …` — and `alt+.` draws it on the map; the resting foot does
not, because that line is four keys exactly. A row with verbs is any row
under the box.

## Which column am I in — there is one column, and the band is the cursor

**One column.** The zones' own column went with the strips, so there is no crossing between
columns to keep track of and no marked heading over a section: `↑`/`↓` walk every row on
the page in reading order.

**The cursor is the band.** The row you are on wears a quiet background and its name goes
bold inside it; every other row is dim. Exactly one row on the frame wears that ground, and
the row under your mouse pointer wears the same one while the pointer is on it — whether
you arrived with `↓` or with the mouse, the row you are on is the row you are on.

There is **no `›` lead mark** on the row. The state mark already sits in the first cell of
every row, and two more cells for a pointer would push every name two columns right for
something the ground already says.

**A project heading is never marked.** Under `alt+g` the project names are dim and stay
dim — they are grouping rather than a place the keyboard can be.

## Why is the needs you heading highlighted, why is one project name darker than the others — it is not there any more

**There is no `needs you` heading and no `moving` heading.** Both were labels over strips,
and the strips are gone; what needs you is the top of the one list, wearing an amber `?`,
and what is moving is under it wearing `◐`.

The only heading-shaped things left on the resting list are the `since you left` line, the
section line — `20 chats · what wants you first` — and, under `alt+g`, the project names.
**None of them ever brightens** and none of them is a cursor stop. The one thing on the
frame that is lifted out of the dim is the row your cursor is on.

If **one project name is darker** than the others, or looks different from them in any way,
it is because `alt+g` is grouping the list: under it the project names are dim headings, and
this window's own project sorts first. Press `alt+g` again for the flat list.

## Why are most projects collapsed on home — they are not, there is one list

**Nothing is collapsed by project any more.** Every conversation on the machine is on one
list, ranked by what wants you first, with its project as a tag on the row. There is no
`elsewhere` rule, no folded block of other projects and no per-project fold.

What *is* folded is the **tail of the one list**: eight rows, then
`▸ 15 more, quiet since aug 21`, which `enter` or `→` opens (*Where did the rest of my
chats go*).

**To see the projects again, press `alt+g`.** The rows are drawn in blocks under dim
project headings, this window's own project first. It is a view of the same rows and not a
different reading of the machine.

**Under 60 columns** — the phone shape — home is an inbox of triage sections with the
projects under them, and there a project other than this window's *is* one folded line that
`enter` opens in place (*Home on a phone*).

## Archiving a conversation — put junk away and clean up home

Walk the cursor onto a conversation (or point at it, or click it), then press **`ctrl+e`**,
or open the row's verbs with `→` and press **`a put it away`**. The row **leaves the list**
and home is cleaner by one line. The foot says
`put away · type its name to find it again`.

Nothing is deleted and nothing moves on disk — the conversation keeps its transcript, its
tasks and its project, and can still be opened. Putting away is a fact about home's list
and nothing else.

**Getting one back: type its name.** A search matches put-away conversations along with
everything else — a filter that hid a match would be lying about the machine — so the row
comes back into the column as an ordinary match, and `ctrl+e` on it there brings it back
for good, saying `brought back`.

There is **no archive fold** at the foot of the resting list any more: the resting list is
the ranked reading, and put-away rows are simply not in it. There is no bulk gesture
either — rows are put away and brought back one at a time, each with one keystroke.

## How do I open a collapsed project — enter on the ▸ line

**On the resting list there is no project line to open**, because there are no project
blocks: `alt+g` is how you see the projects, and the only `▸` line is the one fold over the
tail of the list (*Where did the rest of my chats go*).

Two places a `▸` project line still exists, and in both `enter` or `→` opens it in place,
its mark becoming `▾`, and `enter` or `←` folds it away again — clicking the line toggles
it in one press:

- **under 60 columns**, the phone shape, where every project but this window's own is one
  folded line under the triage sections (*Home on a phone*);
- **on a card**, where a band with more behind it says `▸ …3 more tasks` and the same two
  arrows open and close every fold on the card at once.

The folds you open by hand stay open for as long as home is up, including across a
refresh — folding is something you did, not something the data said.

## The line at the top of home — the pulse, want you, moving, spend and allowance, the clock

The top line of home is the program's name and, right-aligned, what is true of the
**whole machine** right now:

```
 aforge              2 want you · 4 moving · $0.55 / $500.00 · tue 1:11pm
```

- `2 want you` — how many things have **stopped on you**: a conversation waiting for an
  answer, a standing order that will not fire until you say so, an errand holding a
  question. It is drawn in **amber**, which on home and the places means one thing and only
  that thing — someone is waiting for a person. One row is one question here however many
  tasks are parked behind it, because what you do about it is answer it once.
- `4 moving` — how many things this machine has **in flight right now**, everywhere at
  once: task nodes out, conversations mid-turn in another window, an `ask here` errand
  answering, a standing order firing. It counts the same things the `◐` rows on the list
  are, and a conversation with three tasks out counts as three. It is drawn in **cyan**,
  the in-flight colour. It shows from **one** — one hand working is worth knowing — and
  disappears entirely at nothing, never `0 moving`.
- `$0.55 / $500.00` — what the machine has spent **since midnight** against what it is
  allowed to spend today: the tasks that ran and the standing things that fired, then your
  daily allowance. It is drawn in **green**, which is the money colour and is never spent
  on anything else. A machine with no allowance set draws the figure alone.
- `tue 1:11pm` — the day and the time.

**Every segment but the clock disappears unless it is true.** Nothing stopped on you means
no `want you` at all — never `0 want you` — and a day that has cost nothing says nothing
about money. The clock always draws. A quiet morning on an idle machine really is just
`aforge` and the time.

The counts on this line and the rows on the machine's card are **one reading**, taken once
every three seconds: the top line cannot say `4 moving` over a list showing three.

The allowance used to live on a card home drew when the cursor was on no row at all, and
this line used to say `$1.10 today` and change colour as the bound came close. It says the
fraction now, which is what the colour change was standing in for — and that card is gone
(*Where did the machine's own card go*), so this line is the only place either figure
appears.

## Where the cursor starts on home, and where the first down arrow goes

**Home opens with the cursor visibly on the conversation this window is holding** — the
row `esc` drops back into, wearing the quiet selection band, with the word `here` where its
age would be. The first frame answers "where am I" before you press anything, and `enter`
on the first keystroke means something safe: back into your own conversation.

A window whose own conversation is not on the list falls to the **first row the cursor can
stand on**.

**`↑` off the top row of the list walks up onto the tab bar** — the row of seven words over
the list. The word you are standing in wears the cursor's band there instead of its usual
mark, `←` and `→` walk along the seven without opening anything, `enter` or `↓` goes into
the one under the cursor, and `esc` puts the cursor back on the row it came from. The
*places* page has the whole of it under *How do I move between the tabs with the arrow
keys*.

**Where the first `↓` goes on home**: back onto the row you walked up from, which is the
first row of the list the cursor can stand on — under the `since you left` ledger and under
the section line, because neither of those is a row. Walking up onto the bar does not move
home's own cursor, so `↑` and then `↓` costs nothing.

**The cursor never leaves the list for nothing.** It is on a row of home at every moment,
so the card beside it always has something to be about, and the three-second rescan leaves
it wherever you put it.

`esc` does what `esc` always does here: back to the conversation you came from — and it
is the same conversation the cursor opened on.

## Where did the machine's own card go — the cursor walks up to the tab bar now

**It is gone, and `↑` off the top row reaches the tab bar instead.**

Home used to have a state called *rest*: `↑` off the top of the list put the cursor on **no
row at all**, and on a frame 160 columns or wider the right-hand side became a card about
the machine — `keeping an eye on`, an `agents` chart, `since you left`, `thinking`, and a
`today` line. The whole state existed so that card had somewhere to be reached from.

The tab bar is a better thing to find above the list, because every word on it is a room
you can open. So `↑` now lands there, and each of the things the card said has a place of
its own that says it in full:

| What the card said | Where it is now |
|---|---|
| `keeping an eye on` | the **standing** place — `alt+3`, or type `standing` |
| `since you left` | the **`since you left` ledger** at the top of home's own list, where each line is a door into the place it happened in (*What is since you left*) |
| `today` — chats, tasks, money | the **pulse line** at the top of every place says `$0.55 / $500.00`; the **spend** place — `alt+5` — has the days, the models and what each thing was for |
| `agents` — the little chart | the pulse line's `4 moving`, which is the figure the chart was the shape of |
| `thinking` — the install's rung | the `thinking` row of `/settings`, which is where that setting has always been written |

**`ctrl+v` on home is now the standing item's key and nothing else's.** It raises how hard
one `◦` row thinks (*Make a reminder think harder*). To move what the whole machine thinks
by default, open `/settings` and walk to the `thinking` row.

## The pane on the right of home — the card under the cursor or pointer

**There is a right-hand pane only at 160 columns and wider** (*Why is there no preview on
the right*). Where there is one, it is about **the row under your mouse pointer while the
pointer is on one, and the row the cursor is on otherwise** (its own section, below). It is
read top to bottom as bands separated by blank lines — no rules and no borders anywhere,
the separation made of nothing but space, and the size of that space saying which bands
belong together (*How much air is on the card*):

1. the conversation's **name**, the brightest text on the screen and the same treatment the
   banded row on the left wears, so the eye travels between them;
2. **directly under it, with no blank row between**, one dim line of **where it is**, which
   carries the repository and whether a window is
   holding it — `~/aforge-v2 · master, 1 file dirty · here`. The branch and the dirty count
   are one clause about one repository, so they are joined by a comma; the ` · ` separates
   the address, the repository and the door word. The whole line is a **link**: cmd+click it
   (ctrl+click on Linux) and the folder opens, in the terminals that make hyperlinks. A
   folder that is no longer on this disk says `that folder is gone` in place of the branch
   and is not linked;
3. `it is stopped on you`, the **question** in its own words, and the keys that answer it —
   the one band that acts;
4. `work` — each task with its mark and what it cost, `✓ toy-scale validation of
   decomposition   $1.63`, a run that is not done keeping its outcome under it, then
   `▸ N more tasks` with `tasks` at the margin, which **names the tasks place** rather than
   unfolding;
5. `made for you` — the **files it made**;
6. a dim line of **facts**: `spent $1.63 · 3.6M tokens · thinking high`;
7. one dim line naming the strip: `→ verbs: put it away, new chat here, open folder, copy
   path`. It **wraps** on a card too narrow for the whole list rather than ending in an
   ellipsis, and the second row hangs under the first word, so the offer is never cut off
   halfway through the thing it was about to offer.

The three section words — `it is stopped on you`, `work`, `made for you` — are **one shade
brighter than the facts under them**. They are labels on a group and not facts in their own
right, so the eye can find them without them competing with the title, which stays the
brightest thing on the screen. There is no bold under the title and no underline anywhere:
weight here is made of colour.

**It never moves.** When the list becomes a drop-up under your typing the card stays exactly
where it is, because a card is assembled downward from its title and lifting it would take
the facts off the bottom rather than move it down the screen.

**It follows the cursor through a search as well**, and there it is the same preview — what
the conversation is doing, the last thing said in it, what is next up, what came in while
you were away.

It goes **empty** — nothing drawn at all — on a row that is not a thing: a fold line, or the
`start a new conversation` row, which has nothing to preview because that chat does not
exist yet. There is no state in which it is about something other than a row of the list:
the cursor cannot leave the list any more (*Where did the machine's own card go*).

A frame too short for all of that drops bands from the bottom — the facts go first — and
never touches the name.

Nothing that is zero is drawn, anywhere. A chat that ran no tasks says nothing about tasks;
one that spent nothing says nothing about spending; a facts line with no facts is not
drawn at all.

## Why doesn't hovering change the right side — the mouse pointer previews a row

It does, where there is a right side at all, and this is the rule: **the right side shows
the row under the pointer while your mouse is over one, and the cursor's row otherwise.**

Move the pointer onto a row and the card becomes **that row's** card, at once. The cursor
does not move: the hovered row takes the same quiet ground the cursor's row wears, and the
two are allowed to be different rows. Move the pointer off the list — onto the card itself,
onto a heading, onto a blank line, or out of the frame — and the card goes **straight back
to the row the cursor is on**. There is nothing to press and nothing to remember.

**The pointer never moves the cursor** — only a click does that — and **the keyboard never
moves the pointer**. `↑`/`↓` walk the cursor and the card follows the cursor whenever the
pointer is not on a row; `enter` and `esc` always act on the cursor's row.

Every row the cursor can stop on can be previewed this way: a conversation, a standing item
that is asking or firing, an `ask here` errand. Rows the cursor cannot stop on — the
section line, a project heading under `alt+g`, the `since you left` heading — preview
nothing and leave the card where it was.

The card is the real card and not a sketch. Hovering a conversation in another project
shows **that project's** branch and dirty files, its work and its figures; `→` opens the
row's verbs, and the chords — `ctrl+e`, `ctrl+o`, `ctrl+y`, `ctrl+t` — act on the same row,
so while you are pointing at one they act on **that** row.

Two cases where the right side does not move under the pointer, both of them by design:
under 160 columns there is no right column at all, and while your terminal owns the pointer
— after `ctrl+s`, or with the `ui.mouse` setting off — there is no hover anywhere, so the
card is always the cursor's.

## How many conversations does a project have — what the right side shows for a whole project

**Nothing, and home no longer has a project card.** A card is drawn about the row the cursor
is on, and on the resting list there is no row that stands for a whole project: the project
is a **tag on each conversation's row**, and under `alt+g` it is a dim heading the cursor
walks straight over.

What home does say about size is on the **section line**:

```
20 chats · what wants you first
```

That is every conversation on the machine, from every project, the put-away ones excepted.
Press `alt+g` and the rows are grouped under their projects, so the rows in a block are that
project's — bearing in mind that home draws eight rows and a fold, so the quiet ones behind
the fold are not in the block until you open it.

Where to go for the whole figure instead:

- **the tasks place** for the work a project has run, which is the record rather than a
  count;
- **the spend place** for what it has cost;
- **a conversation's own card**, at 160 columns and wider, for that one chat's work, files
  and figures.

A project's own card existed while home was a tree of project headings. The tree is gone
(*Why is my project tree gone*), and a card nobody can put a cursor on is a card that is not
drawn rather than one that is empty.

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

- `◐ running · <what it is doing>`
- `◐ running · checking what it left`, `◐ running · closing gaps` — the minutes when the
  node's own worker is not the one at it, said here for another window's node as well as
  for this one's, and never for a window that has gone (the page on how tasks run has both).
- `◌ incomplete` — work that was under way when the window went
- `? needs your look · <what it came to>` — brought up out of the dim, because it is asking
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

## I clicked a task on home and nothing happened — open a task from the card

**Click the task's own row on the card** — the line carrying its name, its mark and what it
cost — and the task's **record** opens: what it was asked to do, what it came to, the files
it touched, and the tail of its own output. It is the same card `enter` opens on a row of
the tasks place, and the same one a tap opens on a phone, so `esc` comes back one layer at
a time — the record to the list, and the list to where you were.

Which rows on the card answer a press:

- **a task's name row** — opens that task's record;
- **a fold line** (`▸ …5 more files`) — opens that band alone;
- **a file under `made for you`** — opens the file.

The `▸ N more tasks` line at the foot of `work` is **not** one of them. It names the tasks
place out at the right margin because a card is not the place that holds them; `ctrl+g`, or
`tab` onto `tasks`, is the way there.

**Move the mouse over the card and the doors show themselves.** The line under the pointer
lights — the same highlight the row under the pointer wears in the list on the left — and
lines that do nothing stay plain. So the way to find out whether something on the card can
be pressed is to point at it: a task's name row lights, a fold line lights, and the
headings, the title, the place line, the facts and the `▸ N more tasks` line do not.

The card is only drawn where the frame is genuinely wide — around 160 columns and up. On a
narrower window there is no card, and the tasks place (`ctrl+g`) is where the work is
listed; `enter` or a click on a row there opens the same record.

## Why did the list jump to the bottom when I typed — home's two shapes

Home has **two shapes**, and only the list changes shape.

**With nothing typed it is a dashboard.** The list hangs from the top of the frame and the
cursor sits on the conversation this window is in. That is what home is for: one page of
everything the machine holds, ranked by what wants you first and read top to bottom.

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

**The card, where the width draws one, never moves and never changes shape.** It is about
whatever the cursor is on, in both shapes, and it is read downward from the conversation's
name. It goes empty only when the cursor is on something that is not a thing — the fold
line, or the `start a new conversation` row, which is a chat that does not exist yet and so
has nothing to preview.

## Switch between sessions — enter on home

`↑`/`↓` (or `ctrl+p`/`ctrl+n`) walk the rows, stepping over the headings and the section
line. `pgup`/`pgdown` jump a screenful. The card, where there is one, follows the cursor.
Clicking a row puts the cursor on it; clicking the row the cursor is already on opens it.

`enter` opens the session under the cursor — **any row on the screen, in any project.**
The chosen journal is opened and replayed, and **the conversation you were in stays open
behind it**, still streaming its turn, still running its tasks, one `tab` away. It is not
closed and it is not paused.

`enter` on the one you are already in simply steps into it and says nothing — it is
already loaded underneath, so there is nothing to reopen and nothing to announce. That is
what makes `enter` the calm keystroke on a launch: the cursor starts on that very row.

`enter` on a conversation **this** terminal is already holding behind the screen goes
straight back to it, for the same reason: it is alive, so there is nothing to reopen.

The foot line reads exactly:

```
type to search or start something new · ↑↓ pick · enter open · tab next place
```

and it says what THAT row's keys do on every row that has its own — a `since you left`
line, the fold, the action row — always with `alt+. map · tab next place` before the way
out.

## Typing a long question on home — does the box wrap, and where does a paste go

**The box wraps.** Home's foot box is drawn by the same editor as the chat's message box:
a sentence longer than the frame wraps onto continuation rows — up to three — and past
that the window scrolls with the caret, marked with `…` where the `›` was. Nothing you
type is ever truncated out of view. There is no key to open a new line here (that is the
chat box's `ctrl+j`); on home and on every other place, **`alt+enter` sends what you typed
off as a task** — which on home is `ask here`. The first press opens the composer layer,
where the three facts a task needs are settled (the places page, *the composer layer*), and
the second press is the send. `ctrl+enter` does the same thing on the terminals that can
send it.

**A paste lands in home's box.** Paste while home is open and the text goes into the foot
box — filtering the list, exactly as typing does — or into the ask-here exchange's own
box when that pane holds the keyboard. Pasted newlines are kept, so a pasted paragraph is
fine as an `ask here` question. (It used to fall through to the chat's own draft, which
home was covering, so pasting looked like it did nothing.)

## Continue a conversation from another terminal — move it here, it says open in another window

A conversation another terminal has open is a **door**, and pressing `enter` on it **moves
it here**. Not two windows on one chat — the conversation leaves that terminal and arrives
in this one, with its work and its half-typed sentence.

**It takes two enters, and the first one only offers.** The first press arms the row and the
foot line says what the second will do and what it costs:

```
open in another window · working — enter again to move it here (it moves when that window's reply ends; its tasks resume here)
```

Anything else — an arrow, a letter, `esc` — disarms it. Nothing has been written and nothing
in the other window knows you looked.

**The second press asks, and then waits**, because a reply is never cut:

```
moving it here — waiting for the other window…
```

If that window is in the middle of an answer, it finishes it first and lets go straight
after. There is **no time limit** on the wait; after fifteen seconds the line explains
itself — `still waiting — the other window finishes its reply first · esc stops waiting` —
and `esc` withdraws the request, leaving the other window untouched. The moment it lets go,
the row opens here.

**What comes with it.** Tasks that were running land `paused — it resumes` and start again
from their checkpoint in this window. The unsent sentence in the other window's box arrives
in yours. The transcript is the same transcript, whole.

**What the other window shows.** One line — `moved to another window` — and it lands on
whatever else it was holding: another conversation you had open there, or a fresh one in the
same folder. Nothing it was doing is lost.

**`aforge chat` in a folder whose conversation is open elsewhere** does not start a second
one silently any more. It opens home with that row pointed and already armed, so one `enter`
continues where you left off, and `esc` gets on with the new conversation instead.

**The one exception is `--host`.** The holder is a window here and the journal is on the
other machine, so there is nobody to ask, and the row still says
`open in another window — go there, or start a new conversation here`.

## Open another project from home

**`enter` opens any row on this screen, whatever project it belongs to.** There is nothing
to go to another terminal for and nothing to type: put the cursor on the row and press
`enter`. There is nothing on this list that is only there to be looked at: the project is a
tag on the row, and every row is a door.

What happens is a **second conversation**, not this one moving. The conversation you were
in is left running exactly where it was — its turn keeps streaming into its own transcript,
its tasks keep running, it keeps its lock — and the new one is built on **its own**
workspace, with that project's approval rules, its crew, its spend ceiling and its saved
shapes of work. Nothing is carried across, because nothing crosses.

The status line then reads `2 open`, and `tab` over an empty message box goes back.

One refusal is still possible and it leaves home standing: the project's folder is gone —
`that folder is gone · <path>`, and nothing is opened. Home already knew — the row reads
`folder gone` in its right margin and the card's place line says so too, see *Enter does
nothing on a row — the folder is gone* — and this line is the check made again on the
keystroke, for a folder deleted in the seconds since.

**How many you already have open is never a refusal.** See *How many conversations can one
terminal hold*.

## How many conversations can one terminal hold — is there a limit, and why can I not open another

**As many as you open.** Nothing counts them and nothing refuses another: the ninth, the
twentieth and the fiftieth open exactly like the first, from home's `enter`, from a typed
sentence, from a typed path, from the switcher (`ctrl+k`), from search and from `/new`.

There used to be a cap of eight, and taking a ninth said `8 open is as many as aforge holds
— /quit closes this one`. That sentence is gone and nothing says it any more.

What is still true is what an open conversation costs. Each one is fully alive — its turn
streams, its tasks run, it holds its transcript's lock, it heartbeats a presence file every
five seconds — and **nothing closes one for you**. So a window with fifty open is holding
fifty live conversations' worth of memory until you say otherwise. The two ways to say so:

- `/quit` closes the conversation in front and brings the last one forward;
- `ctrl+w` on the switcher card closes the conversation under the cursor without leaving the
  one you are in.

`2 open · 1 waiting` on the status line is the count, and `ctrl+k` shows the first twelve as
rows. Home is the page that shows every one of them.

## Enter does nothing on a row — the folder is gone

**Its project folder is not on this disk any more.** A conversation started in a directory
that has since been deleted, renamed or moved — a scratch folder under `/tmp` wiped by a
reboot is the usual way — cannot be opened, because the conversation would come up as an
agent whose tool root does not exist and every command and every relative path in it would
fail.

Home says so **on the row**, at every width: `folder gone` in the right margin, where the
age would be. And **on the card**, at 160 columns and wider: the place line under the title
carries `that folder is gone` where the branch would be, against the address it is about.

`enter`, `ctrl+t new chat here` and `ctrl+o open folder` all want that directory — `enter`
opens a conversation rooted in it, `ctrl+t` starts a fresh one there, and `ctrl+o` hands it
to your file manager — so each of them refuses rather than pretending. The row's verb strip
drops `t new chat here` and `o open folder` for the same reason: a strip only ever names
letters that work. `ctrl+y copy path` and `c copy path` still do, because a path is a
string.

**On enter.** Nothing is opened, home stays up, the conversation you were in is untouched,
and `that folder is gone · <path>` appears on home's message line at the foot of the screen.

**The conversation itself is not lost.** Everything aforge recorded about it lives under
`~/.aforge/v3/projects`, not in the workspace: its card still says what it did and what it
cost, and `ctrl+y` — or `c copy path` on the row's verb strip — still copies the workspace
path. What cannot happen is *continuing* it, because there is nowhere to continue it.

**What to do:** recreate the folder at that exact path and the row opens again on the next
refresh (home re-checks every three seconds); or type a message at the foot of home and
start a new conversation in a project that exists.

## Why does it say elsewhere — on a standing item, and nowhere else

**Almost never, and never on a conversation.** Every project but the one this window
launched in used to carry a dim `elsewhere` on its heading, and `enter` on one of its rows
opened nothing and said `elsewhere · <the project's path>`. That is gone, and so is the
`─ elsewhere ────` rule that folded the other projects away underneath it: home is one flat
list now, the project rides each row as a tag, and `enter` opens any of them.

**One refusal still uses the word.** Pressing `enter` on a **standing item** — a reminder,
a watch, a rule — asks to open the conversation that set it up, and where that conversation
belongs to another project this window cannot resume it. Home says
`elsewhere · <the item's workspace>` at the foot and opens nothing. An item that was set up
from home and never became a conversation says `made from home — no conversation to open`
instead.

Your home directory's project is spelled `~ home` where it needs a heading, because a bare
`~` over a column reads as furniture rather than as a name. Rows and sentences still say
plain `~`.

The reasoning behind the old refusal was right and is still kept — a window's approval
rules, its crew, its spend ceiling and its saved shapes of work are resolved from the
workspace it launched in, and carrying a conversation across without carrying those would
be a window quietly running under another project's permissions. What changed is the
answer: a second project is a second **conversation**, built the way the first one was, on
its own workspace, with its own gate. A conversation still never moves between projects —
though it can be **about** another folder without moving, which is what to reach for when
you want the work somewhere else rather than a second set of settings: see *What also about
means on a row* below and *Choosing a folder*.

`another window` is a different sentence and still means what it always did — see *Why
can't I open a session from home*.

## What also about means on a row — the conversation is about another folder

A conversation is filed under the project it is **standing in** — the folder aforge was
opened in. It can also be **about** other folders: ones you named with `/folder` or
`/attach`, and ones a task's ground settled on and the conversation wrote down. When that is
true of a row, its dim tail ends with the name:

```
Flaky pipeline               2 running · 3h · also about wisp
Tuesday notes                            1d · also about wisp +2
```

One name and a count, never a list — a row is the same shape whatever it is about. The card
on the right names them in full under `also about`, three at a time, with `▸ …N more
folders` behind the rest; `→` opens every fold on the card.

**A row with nothing to add says nothing.** A conversation about exactly the project it is
filed under draws no such clause, and neither does one whose folder would only repeat the
project's own name. Nothing on this screen is a permanent "attached" list: a folder appears
when it tells you something you could not already see.

Where the work itself goes is *Which folder does a task work in*, and how a conversation
comes to be about a folder is *Choosing a folder*.

## Switch between projects without leaving — work on two projects or two repos at once in one terminal

**One conversation can be about more than one folder.** Name the other project — `/folder`,
`/attach ~/code/other`, or the path in your own words when you ask for the work — and the
work you ask for goes there; you do not need a second conversation for a second repository.
What does not move is where the conversation is **standing**: its own working directory, its
`AGENTS.md` and its settings stay the folder it was opened in. *Choosing a folder* has both
halves.

For a genuinely separate conversation — a different project's settings, its own model, its
own history — one terminal holds **as many as you open**, with no cap on the number. One is
on screen; the rest are open behind it, fully alive. Nothing closes one for you, so `/quit`
and `ctrl+w` are how a conversation you are done with actually ends.

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

It matches five things, and the first that scores highest wins the row: the conversation's
name, the project it is in, **the folders it is about**, the titles of the tasks it ran, and
**what those tasks came to** — the one-sentence outcome in the project's record. That last
one is the closest thing to remembering something by what happened rather than by what it
was called: typing `postgres` finds the conversation whose task outcome mentions the
connection pool, even though nothing in its name does.

Matching a project's name keeps every conversation in it.

**A folder's name finds the chats about it wherever they were held.** A conversation opened
in your home directory that spent an afternoon on `~/code/wisp` is filed under `~` and not
under wisp — so typing `wisp` finds it too, alongside the conversations held inside wisp
itself. Those come first: standing in a folder is a stronger claim on its name than being
about it.

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
  ordinary direction — what wants you first, then what is moving, then the rest by recency.
  The inversion is part of the drop-up, and the drop-up is what typing does.

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

## Start something new from home — typing does all three at once

Whatever you type is **three things at the same moment**: a new conversation waiting to be
sent, a live query over the machine, and — if it starts with `/` — a command. You do not
choose between them before you start typing.

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

A line beginning with `/` is the third thing typing can be — a command, run rather than
sent. See *Running a slash command from home*, directly below.

With **nothing** typed there is no action row and the list hangs from the top again — see
*Why did the list jump to the bottom when I typed*.

Starting a conversation this way is `/new` followed by your sentence, so everything `/new`
does applies. On a surface with no fresh-session seam it refuses in `/new`'s own words,
`/new is unavailable here`.

**However many conversations are already open, this opens another** — there is no cap, and
*How many conversations can one terminal hold* says so in full.

**Where the door itself fails, your sentence is not sent anywhere.** On `new session
failed: <error>` — a session folder that could not be made — home closes, the error is said
on the entry line, and your words are put in the message box unsent. They are **not**
delivered to the conversation this window was already holding: a sentence typed for a new
conversation never lands in an old one.

**Typing a path starts a conversation there instead.** When what you typed resolves to a
directory on this machine — an absolute path, a `~` path, a `./` path, or a project name
that matches exactly one heading on the list — the action row reads
`start a new conversation in <that folder>` and `enter` opens a fresh conversation in it,
with the one you were in left open behind. A name that two projects share resolves to
neither, because opening whichever sorted first would be the screen guessing.

**An absolute path begins with `/` too, and a folder that is really there still wins.**
`/tmp/alpha` opens a conversation in that directory; it is not read as a command. The one
exception is a word the command table already knows — `/home` is the command, on a machine
that has a `/home` directory as much as on one that does not, because the commands are a
short list somebody chose to learn and the disk is not. The row always says which of the
two it is about to mean.

**The path is resolved and never created.** A directory that does not exist is not a path
at all as far as this row is concerned: the row goes back to quoting your words, and
`enter` starts a conversation here and sends them. Nothing makes a folder because somebody
mistyped one.


## Running a slash command from home — can I type /settings on the home screen

**Yes. A line that begins with `/` is a command, and `enter` runs it rather than sending
it.** Typing `/settings` on home and pressing `enter` opens the settings panel; it does not
start a conversation whose first message is the word `/settings`. Home's box behaves exactly
as a chat's box does here — same commands, same dispatcher, same rules.

**The screen says which `enter` you are about to press, before you press it.** With a command
in the box the action row reads `+ run /settings` in place of `+ start a new conversation:
"…"`, and the foot under the box reads `enter runs this command · ctrl+enter ask here ·
↑ pick a match · esc clear`.

**The command list opens over home's box too.** Typing `/` raises the same ranked drop-up a
chat shows — best match nearest the box — and `↑` walks up into it, `enter` runs the row
you land on, and `esc` puts the list away. A command that TAKES words leaves `/model ` in
the box with the caret after it rather than running on the spot. A slash word in the middle
of a sentence is a mention and never a dispatch: choosing a row there rewrites the word and
nothing runs. *Typing a slash to see the command list* has the whole of that behaviour.

**A path is not a command.** `/tmp/alpha` is a folder, so the row goes on offering
`start a new conversation in /tmp/alpha` — see *Start something new from home* for the
whole of that rule, and for the one word (`/home`) where the table wins.

**Commands that are about a conversation act on the one this window is holding** behind the
screen — home always has one open behind it, so `/model`, `/rewind` and the rest are not
refused here.

**`ctrl+enter` still asks here.** So you can ask a question *about* a command — type it and
press `ctrl+enter` instead of `enter`, and the answer comes back in the pane on the right
without the command being run (see *Asking from home*).

## How do I see the collapsed sessions — …13 more

**There is one fold on home and it is at the foot of the whole list**, not one per project:
`▸ 15 more, quiet since aug 21`. Home draws the eight rows that want you most and puts
everything else behind that line.

**That line is a door.** Put the cursor on it and press `enter` or `→` and every row is
drawn, with no cap at all; the mark becomes `▾`, and `enter` or `←` folds them back.
Clicking the line toggles it in one press. It is the same fold gesture the task column uses,
with the same two marks.

Per-project folds — `…13 more, quiet since 1d` under a project's own heading — belong to
the **phone shape**, under 60 columns, where home is still an inbox with the projects under
it.

**And searching sees straight through it.** While anything is typed there is no cap and no
fold at all — every match is drawn, including the rows the fold was holding. A search that
could not see what it hides would be a search lying about the machine.

A fold you open by hand stays open while home is up, including across a refresh.

## How do I get back to the dashboard — press space twice

**From inside any conversation, press the space bar twice with an empty message box.**
That is the way back to home.

There is no `ctrl+` chord for it: every `ctrl+<letter>` this surface has is already taken,
and `esc` was not available either — on an idle conversation it already arms rewind and
already sends a message you parked, and a third meaning on one key is how a surface stops
being predictable. What was left is the one keystroke that reliably means nothing: a
message that starts with two spaces is a message nobody meant to send that way.

**The first space types itself, plainly.** There is no pending state and no ghost
character. It is the *second* space, arriving to find a box that still shows nothing with
that space behind the cursor, that takes the whole draft away and opens home. So a space
you actually wanted is never eaten: space then `x` leaves ` x` alone, because the gesture
only fires on a space and only over a box with no words in it.

**Wherever the door is drawn, two spaces open it.** That includes a box that looks empty
and is not — one holding only blank lines, from a `ctrl+j` or an `alt+enter` you did not
mean, or from `ctrl+enter`/`shift+enter` on a terminal that cannot send them. The gesture
used to ask for exactly one space and refuse those, so the foot advertised `space space
home` over a chord that could not fire; it does not any more, and the blank lines go with
the draft when home opens.

It works with a turn running. Home takes the frame the way the settings panel does, and the
answer goes on streaming underneath — `esc` puts you back in it, still running.

One thing it will not do: it does nothing when the box already has words in it. It works on
a machine with one conversation, on one with none, and over `--host` — where what opens is
the **far machine's** home (*Home on a fresh machine, and over --host*).

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

It appears on a fresh machine too, from the first minute, and over `--host` as well: a
machine with one conversation or with none still has a home to go to, and a door that is
drawn is a door that goes somewhere. The rule that keeps home from *greeting* a first run is
a different rule — not being greeted by home and not being able to reach it are two
different things.

## space space does nothing — why the gesture did not open home

Three reasons, and neither the machine holding nothing nor `--host` is one of them any
more:

- **The box had words in it.** The gesture fires only when the second space arrives to find
  a box with nothing in it a person would call text. ` x` and then two spaces is a draft.
  Blank lines are not words — a box holding only those still answers the gesture, and the
  dim line at the foot is the honest test: if it reads `space space home`, two spaces open
  home.
- **It was a paste.** Pasted text arrives whole and never reaches the key router, so two
  leading spaces in a paste are two spaces (*Is there a key for home?*).
- **Home is already open.** On home, space is a character in the search box.

A machine with one conversation, or with none, opens home all the same: an empty home is
a screen (*Home is empty — what an empty home shows*), not a refusal. So does a session
over `--host`, which opens the **far machine's** home. Both used to be otherwise — the
gesture stayed inert until the machine held a second conversation, and it was not bound at
all over `--host` — and both those rules are gone.

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

- the pulse line, with the time on the right
- the tab bar and the dim rule under it
- no `since you left` and no section line, because there is nothing that happened and
  nothing that wants you
- `nothing here yet — say something and this fills up` where the first row will be, dim —
  over two rows, split at the dash, so a narrow frame never cuts it short
- the box at the foot reading `› say what you want done`, and under it the foot,
  `type to search or start something new · ↑↓ pick · enter open · tab next place`

Typing there works exactly as it does anywhere: `? ask here: "…"` and `+ start a new
conversation: "…"` rise out of the box, and `enter` starts the conversation. The arrows
have nothing to land on until there is a row; `esc` goes back to the conversation.

A machine with one conversation — the one you are in — is not empty, and home does not
say it is: the conversation is on the list from its first minute and before anything has
been said in it, wearing `here` where its age would be. The conversation on this terminal is
always on the list.

**And the door never waits for a second conversation.** The gesture and the line are there
from the first minute on this machine, whatever it holds, and stay there through every
`/new` and every row opened off the welcome box; the only things that shut the door are
`--host` and home being already open (*space space does nothing*).

## Is there a key for home?

Three of them. **`alt+1`** goes straight there from anywhere — home is the first of seven
places, and each answers to its own position on the tab bar, `alt+1` through `alt+7`.
**Space twice on an empty box** goes there from inside a conversation, and **`tab`** walks to
it from any other place. `/home` opens it too.

`alt+<digit>` arrives in every terminal aforge runs in — it is sent as escape-then-digit and
has been for forty years — which is why the place keys are on `alt`. `ctrl+<digit>` has no
encoding a terminal can send at all.

There is still no `ctrl+` chord for home: the plain ones are all taken (`ctrl+.` is the
tasks place, `/history`).

## What landed while I was away — since you left, and the note on the row

Home remembers when you last closed it, and says what finished after that in two places:

- **the `since you left` ledger** at the top — `2 tasks landed`, whose door is the tasks
  place (*What is since you left*);
- **the note on the conversation's own row** — `3 files made`, which is what its work wrote
  since your last look, or `ran a saved shape` for work that came out of a saved shape.

That is the whole mechanism **on home**: no badge, no list of unread things, and no mark of
its own on the row — a resting conversation keeps its `○` whether or not something landed in
it. You open home and the ledger and the notes show you where work accumulated. (A session's
own window does send a desktop notification when its turn finishes or it stops on a question
while you are looking elsewhere — see "Why a session says it needs you" below. Home itself
never does.)

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
where you see it without opening the window it is in. The row wears an amber `?`, its note
is **what it is asking** in the question's own words — `asks: add a --report-only mode?`,
or `wants to <the command>` for something it needs permission to run — and **it sorts to
the very top of the whole list**, above work that is running and above everything you spoke
in more recently, longest wait first.

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
counts as needing you like the rest, and the line beside it is the gate's own — `out of
fuel · $100.00 of $100.00`.

The line above the box then repeats the question in full with the answers it will take, and
the card — where the width draws one — has it too. A session that gave no words for what it
is waiting on shows no line at all rather than a placeholder.

This is read out of a small file each live session keeps in its own folder, refreshed
every five seconds and believed for fifteen. So a window that was killed, or a laptop that
closed, stops claiming to need you within a glance — nothing on home asks you for
something that nobody is waiting for any more.

`enter` on the row opens it under the ordinary rule, so a question in this project is one
key away and one in another project tells you where to go. **For the ordinary questions
you do not have to go at all** — see the next section.

## Answer a question from home — approve a command in another window

**You can answer it here, without opening the window it is in.** Put the cursor on the `?`
row (or point at it). One line above the box, home draws the question and the answers that
session will take, as chips out at the right — and pressing the digit answers it. A click on
a chip does the same, and where the frame is wide enough for a card the same chips are on it.

**`→` answers it in words instead.** The row's verb strip carries the question's own first
two option words on `y` and `n` — `y let it send   n not this time` — which is the same act
and the same record as pressing the digit (*How do I answer without opening the chat*).

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

**A question raised by _this_ window closes home on its way in.** An approval question, a
connect offer, a subharness offer, a task proposal and a standing card all take home down
the moment they arrive, so the card is on the screen you are looking at rather than behind
it. And while home is up, every letter belongs to home's box — `y`, `n`, `e` and the rest
type there, and never answer a card that is off screen. The one exception is the verb strip,
which has to be **on screen** before its letters are verbs. This window's own question, found
behind a home you opened over it, is answered the way any other window's is: put the cursor
on its row and press the digit.

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
loud, every few seconds, which task nodes it currently has out; a task is counted as
running — and its conversation drawn as moving, with `2 tasks running` in the note — only
when the session that ran it is still alive and still names that node. Every other
live-looking row is **work that was under way when the window went**, and it counts for
nothing: the conversation sits with the quiet rows wearing `○`, and its card says
`incomplete` against that piece of work.

A session too old to keep that file, but whose journal a window is holding, falls back to
the older answer: the lock is asked, and its rows are believed. That is the same rule with
less to go on, not a different one.

The card's place line says `open here` for the one this window is in and `open in another
window` for one a second aforge has, with what it is doing after it — `open in another
window · working`. `idle` is not spelled out, because it is what an open session usually
is. Nothing at all is said when nobody has it.

## Home on a fresh machine, and over --host

A machine that has held nothing yet draws an empty home — the pulse line, the tab bar,
`nothing here yet — say something and this fills up` where the rows will be, and the box and
keys at the foot (*home is empty — what an empty home shows* has the whole screen). The
conversation you opened it from is on it as soon as there is one.

Over `--host` home lists **the machine your session is running on**. The projects, the
conversations in them and the work each of those ran are read on the far end and carried
here, so what you are looking at is the server's afternoon rather than your laptop's — and
the right end of the tab bar says `on <machine>` so you can see which. Enter on a row opens
that conversation the way `aforge resume` opens one locally: the engine swaps to it and this
window keeps drawing.

Two things a remote home does not do, and both are silences rather than sentences. **No row
is ever marked `that folder is gone`** — the folders are on the other machine and a stat here
would report every one of them as deleted. And in the fraction of a second before the far
machine's first answer arrives, home draws **no rows and no sentence at all**: `nothing here
yet` over a server full of work would be the one wrong thing this screen can say about
somebody else's disk.

It has been three screens. It used to refuse to open at all; then it opened with one dim line
saying its projects belonged to the wrong machine; it now opens on the right machine's.

## Does home update while I look at it?

Yes, every few seconds, by reading the folders again. A task landing in another window,
work somebody starts in a second terminal, or a session stopping to ask a question all
show up without you doing anything. There is no file watcher: home reads, and closing the
screen stops the reading. (Standing items — reminders, watches, rules — are a different
mechanism and do keep going; see the keeping-an-eye page.)

The resting list itself does not animate: a row with work running wears the still `◐` and
the ages simply change on the next reading. The one cell that turns is an `ask here`
errand's own row while its answer is coming (*Why does only one row spin*), and in
screen-reader (linear) mode nothing ever animates.

The cursor stays on the row it was on rather than on the line number — the order genuinely
changes when work starts or finishes, and a cursor that stayed put would move you onto
something else between two glances.

## What is the ◦ row on home — where the things keeping an eye on your project are

**A standing thing — a reminder, a watch, a rule, an overnight job — is on home's list only
while it is asking you something or firing right now.** It is then an ordinary row of the
one list, ranked with everything else: an amber `?` when it needs an answer, `◐` while it is
running, and `=` when it is paused. The note is what it is asking or what it is doing, and
`enter` opens the conversation that asked for it.

```
 ? tell me when CI on main goes red     aforge-v2   asks: the fix touches migrations  1h
 ◐ every Monday at 9, post the standup  aforge-v2   drafting from the git log         2m
```

**An item that is simply waiting for its time is not on home.** There is no
`keeping an eye on` band under a project any more — the resting list is what wants you now,
and a watch due on Monday wants nothing. Three places have those:

- **the standing place** (`alt+3`, `/standing`), which is every promise this machine has
  made, with how much rope each has — and which is where the `keeping an eye on` list lives
  in full;
- **the `since you left` ledger**, on the morning one of them has actually fired.

The `◦` mark itself belongs to those readings and to the `next up` band, where a line says
`◦ leave for the train · in 4m`. `∙` is a paused item there, and `◆` means the thing went
off after the last time you spoke in the conversation behind it.

The tail on those readings says where an item stands, and it says **only what is true**:

- `Mondays 9am · last Mon` — a reminder or a routine: when it goes off, and when it last
  did. An item that has never gone off says only when it will.
- `checked 6m ago · nothing` — a watch on the world: when it last looked, and what it
  found. `nothing` is a finding, and the commonest one — it is the whole difference between
  a watch that is working and one that never ran.
- `needs your look · <what it is stopped on>` — the one line that is not dim.
- `running · 4m` — it is doing something right now.
- `paused` — you pressed `ctrl+e` on it.

**Retired items are nowhere on home.** Something that fired once and finished, or that you
stopped, is a thing that happened; the conversation that made it still has the whole record.

**Typing hides them.** The box at the foot searches conversations — by name, by project, by
what their tasks came to — and a standing row riding along under a query would be a row the
query never considered, drawn as though it had.

## How do I pause a reminder from home — the → strip, and ctrl+e / ctrl+x

Put the cursor on the row (or point at it) and press **`→`**. A strip opens under the list
offering `p pause it` — or `r resume it` when it is already paused — and **while that strip
is drawn those letters are the verbs**. `esc` or `←` closes it. The chords **`ctrl+e` to
pause** and **`ctrl+x` to stop for good** still work and need no strip. An item that is
asking you something carries its own answer words on the strip too, on `y` and `n`, before
the pause verb.

A third chord, **`ctrl+v`**, raises how hard that item thinks — see "How hard does a
reminder think" below. Home says `paused · <your words>` or `stopped · <your words>` at the
foot and redraws the row from the store, so what you see is what is on disk rather than what
the keypress hoped for.

Home's box takes every letter, always, so a `p` is a `p` in your sentence wherever the
cursor rests — *unless the strip is on screen*. That visible strip is what buys the two bare
letters, and it is the only state on this surface where a printable key is not a character.
Before it existed home printed `p pause · s stop` on this card while binding neither, which
is exactly the sort of promise the strip was built to stop.

A window whose build cannot write to the store says `this window cannot change
it` rather than pretending.

`enter` on the row **opens the conversation that asked for it** — that is the
answer to "why did I get this?", and it is the same door a conversation row has,
which means **whatever project it belongs to**. Something you set up from home
that never became a conversation says
`made from home — no conversation to open`.

## What is on the right of home when I'm on one of these rows — the item's card

On a frame **160 columns or wider** — the only width home draws a card at — the right column
shows the same kind of card it shows for a conversation, about the other kind of thing, read
downward:

1. **your own words**, the brightest text on the screen;
2. **directly under them, with no blank row between**, one dim line of **where it is** —
   project · path, and that line is a link;
3. **when it goes off**, and — if it is stopped on something — that line, in the
   one hue on the card that is not dim;
4. **what it has done**: `checked 6m ago · nothing`, `last went off Mon · <what
   came of it>`;
5. a dim line of **how much, ever**: `4 runs · spent $0.08`;
6. a dim line of **how much lately**: `ran 3 times this week · $0.04`, counted
   over the last seven days of the standing ledger;
7. **how hard it thinks**, if you have said: `thinking high`;
8. the keys, dim: `enter open where it was asked · → pause · stop ·
   ctrl+v think harder`.

Nothing that is zero is drawn. Something set up ten seconds ago is a title, a
place and a cadence, and nothing else — no `0 runs`, no `$0.00`, no weekly line.
A window with no ambient side wired to it draws no weekly line at all.

Below 160 columns there is no card, and the row's own note is what it says — what it is
asking, or what it is doing right now.

## Make a reminder think harder — how hard a standing item thinks, and ctrl+v on its card

**Standing things think at `low`, however deep you have dialled this machine.**
A reminder, a watch and the sentinel check behind it are unattended and repeat
forever, so they are held to the cheapest rung on purpose — an install set to
`max` does not turn every check on the machine into a deep pass.

**`ctrl+v` on an item's row is how you raise the one that deserves it.** Put the
cursor on the standing item (or point at it) and press it: the rung climbs one step
each press — `low`, `medium`, `high`, `xhigh`, `max`, then back to `low` — and
home says `thinking high · <your words>` at the foot. The card then carries a
dim `thinking high` clause, read straight back from the item's own document.

An item nobody has dialled says **nothing** at all on that line, which is not the
same as `low`: it means nobody chose, and the standing floor is what applies. The
rung is kept with the item, so it survives closing aforge, and it is what that
item's firings **and** its checks ask for from then on.

A window that cannot write to the store says `this window cannot change it`, and
its card does not offer the key at all.

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
? keep main green · needs your look: the fix touches migrations
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
it without leaving the box — the chord goes by way of the composer layer, so `alt+enter`
twice is the whole gesture and the layer in between is where you say where it runs, what it
runs on and how much it may spend (the places page).

**Every exchange is a row on this column**, marked `?`, **at the very top of the list** —
above everything the machine has to say for itself, because it is the thing you asked for a
minute ago — with what it is doing in the tail:

```
 ? remind me at 6 to leave                          ? waiting on you
 ? what did we decide about pricing                 ⠹ working · 4s
 ? tell me when CI goes red                         ∙ stood
```

They sort with the hot things — what wants you, then what is moving, then what is done —
and **several can be open at once**: a second `ask here` adds a row rather than replacing
the first.

The pane on the right is **about the row under the cursor**, exactly like every other card
in that column: walk onto an exchange row and you get the exchange, walk off it and the row
you land on draws its own preview again. `enter` or `→` on the row hands the keyboard to
the pane; `esc` hands it back. (`tab` used to be that toggle and is the way to the **next
place** now.) On a window too narrow for two columns the pane is **stacked** over the list
instead of drawn beside it, and `esc` brings the list back.

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

**The `since you left` ledger at the top of home is where that lives now** — one line per
thing that happened on its own, each a door into the place that owns it (*What is since you
left*).

A conversation's own **news band** — `◆ N things since you left`, with each item underneath
reading `<age> · <words> · <text>`, for example `4m · keep main green · the tests passed` —
is still drawn where a fuller card is drawn: on the **phone sheet** under 60 columns, and on
the card while you are **searching**. It shows three items, then a `▸ …N more things` door.
An absent or empty inbox draws no news band at all, and looking at the band does not consume
the news. The resting card at 160 columns does not carry it, because the ledger above the
list already said it once.

## Where are the files it produced — deliverables on a conversation

The card lists files produced by that conversation, newest first, as `· <basename>` with
its age out at the right — `· report.md   2h`. The basename is a clickable path in terminals
that support file links, and opens the full recorded path. It shows three files, then a
`▸ …3 more files` door. A conversation with no indexed files draws no files band at all.

It is one of the five bands the resting card keeps at 160 columns and wider (*Why is there
no preview on the right*), so at any narrower width the way to the files is to open the
conversation.

## Where did we leave off — the last exchange on a conversation

The card keeps the two sides of the last exchange together: the person's last message is
one muted line beginning `› `, followed by the reply in dim text wrapped to at most two
lines. A conversation with no turns draws no last-exchange band.

**The resting card does not carry it.** At 160 columns the card is the five bands that act,
and what a conversation last said is a thing pressing `enter` shows a beat later. It is
drawn on the **phone sheet** and on the card **while you are searching**, where telling two
similarly named conversations apart is the whole job.

## Where does this repository stand — branch and dirty files on home

**On the card's place line, under the title**, where the address it is about is:
`~/aforge-v2 · master · 1 file dirty · open here`. A changed branch can read
`feature/home · 2 files dirty · ahead 1 · behind 3`. Every unknown or zero clause
disappears, so a clean repository on main reads only the path and `master`; a folder that is
not a repository adds nothing to the line. Home refreshes this reading for a workspace at
most once every five seconds, and a failed or timed-out Git check draws nothing.

The branch is on the place line rather than in a band of its own because a branch and a
dirty count are facts **about that address**, and a band between the address and them would
be saying the address twice. A folder that is no longer on this disk says
`that folder is gone` there, in place of the branch it cannot have.

## What do the keys on a home card do — open, new chat, folder, and copy path

**The card no longer lists letters.** Its last dim line names the strip and the words
instead — `→ verbs: put it away, new chat here, open folder, copy path` — because a letter
is a verb only while the strip naming it is on screen (*How do I answer without opening the
chat*), and a card printing `t new chat here` would be advertising a keystroke the box is
about to eat.

**The chords still work and need no strip**, and each acts on the row you are looking at:
the row under your pointer when there is one, the cursor's row otherwise.

| chord | what it does |
| --- | --- |
| `ctrl+t` | a fresh conversation **in that row's own project**, whichever one it is — the conversation you were in steps aside and keeps running (the mnemonic is the browser's new-tab key; `ctrl+n` is the walk down the list) |
| `ctrl+o` | asks the machine to open that conversation's workspace folder |
| `ctrl+y` | copies the workspace path |
| `ctrl+e` | puts the conversation away, or pauses a standing item |
| `ctrl+x` | stops a standing item for good |
| `ctrl+v` | raises how hard a standing item thinks. It does nothing on a conversation row: that rung belongs to the window that conversation is open in, and the machine's own default is the `thinking` row of `/settings` |

A row whose folder is no longer on this disk says `that folder is gone · <path>` and starts
nothing — `enter`, `ctrl+t` and `ctrl+o` all need that directory. A failed folder open says
`could not open <path>` on home's message line, and a copied path says `copied <path>`.

The hint line under the box says when the arrow does anything at all: a row with verbs adds
`→ verbs` to it.

## What is next up on home — scheduled items coming soon

The `next up` band lists active items belonging to the card's project, soonest first and
two at a time. Rows look like `◦ leave for the train · in 4m`, `◦ draft the update ·
Mondays 9am`, or `◦ check CI · checked 6m ago`. When more than two are present the card adds
a dim `▸ …N more items` door. Paused, stopped and absent items draw nothing.

**It is not on the resting card.** The card beside the flat list is the bands that act
(*Why is there no preview on the right*); `next up` is drawn on the **phone sheet** and on
the card **while you are searching**. What is coming up across the whole machine is the
**standing** place — `alt+3`, or type `standing`.

## What has this conversation cost — the spend band on home

The dim facts line at the foot of a conversation's card reads `touched 12 files · spent
$1.25 · 34k tokens · last active 12m`. Each clause is independent: zero or unknown files,
spend and tokens are omitted, and a line with no true fact at all is not drawn. On the
resting card the thinking rung is folded into that same line where there is one to state —
what a thing spent and how hard it thinks are one sentence of arithmetic about it. A chat
states no rung, because that setting belongs to the window it is open in.

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

A whole **project's** figures are on the spend place — home has no project card any more.

## How much air is on the card — the four groups and the one gap law

**The blank rows on the card mean something.** It used to put exactly one between every
band, which is a rhythm with no information in it: the address under the title, the bill
under the work and the verbs under the bill all stood the same distance apart, so a card of
seven bands read as seven facts in a heap. It is four **groups** now, and the gap says which
is which:

| group | what is in it |
| --- | --- |
| identity | what this is called, and where it lives |
| activity | what it is stopped on, what it ran, what it made |
| economics | what it cost |
| verbs | what can be done with it |

**One gap law, everywhere on the card: one blank row inside a group, two between groups.**
There is no third gap and in particular no tighter one — two lines that want to be closer
than a blank row are **one band**, which is exactly what the title and the place line are.

**Air is the first thing the card gives up.** On a frame too short to hold the card at that
rhythm it is redrawn at the flat one-blank-row rhythm, and only a frame too short for
*that* starts dropping whole bands from the bottom. Whitespace is the cheapest thing on the
card and a fact is the dearest, so they go in that order — and the title never goes at all.

The same four groups and the same law hold on **a standing item's card** and on the card
for a **project heading**; the phone sheet, which is one column and not a card, keeps the
flat rhythm.

## Why does the preview on the right look cramped — spacing, hierarchy and width

It should not any more. If it does, it is one of three things and they have separate
answers:

- **the gaps** — the card is four groups with two blank rows between them and one inside
  them (*How much air is on the card*). A card showing one blank row everywhere is a card
  on a frame too short for the wide rhythm; make the window taller;
- **the width** — the card is 36 cells at 160 columns and grows to 56 by 200, taking half of
  every extra cell and leaving the other half to the list (*Why is there no preview on the
  right*). Under 160 there is no card at all and the row's own note carries the fact;
- **the shades** — the title is the brightest text on the screen, the section words are one
  shade under it, and the facts, fold lines, place line and verbs are dim. That is the whole
  ladder: three roles, no fourth hue, no bold under the title and no underlines.

## What a narrow home card does with a long row

A card too narrow for a line breaks it between its clauses rather than cutting the end
off. Whole facts move onto following rows: the final key, branch fact, file age, scheduled
time, news text, task file count or cost, and answer chip remain visible. A single clause
wider than the card is still clipped. The card's place line clips from the left so the
path's basename remains visible.

**The verbs line breaks the same way** — `→ verbs: put it away, new chat here, open
folder,` / `         copy path` — keeping the comma on the row it ends and hanging the
second row under the first word, so a list of four verbs on a narrow card is still a list
of four verbs.

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
   other project is one folded line — `▸ wisp   6 · 2d` — that `enter` or a tap opens in
   place. This is the one shape of home that still draws projects as blocks; every wider
   frame is the one flat ranked list.

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
| `→` then `p` / `s`, or `ctrl+e` / `ctrl+x` | on a standing item: pause it, stop it |

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
