# Places

## What a place is, and the seven of them

A **place** is a full-screen room in aforge that is not this conversation. There are seven,
and they are always in the same order:

`home` · `tasks` · `standing` · `memory` · `spend` · `search` · `settings`

They are drawn as a **tab bar** on the second row of every place, under the top line. The
one you are standing in wears a filled band; the rest are dim. Nothing else on the surface
looks like that bar, so "which place am I in" is one glance.

Every place is drawn in the same frame:

1. the top line — this machine's own signs: what is on watch, what today has cost, the time
2. the tab bar — the seven words
3. a dim rule
4. the place's own body
5. a rule, then the **composer** — one line you can type into, wherever you are
6. the hint line — what the keys do here

`esc` leaves a place and puts you back in the conversation you were in. Places are not
stacked: opening one closes whichever was up, so `esc` is always one press from the chat.

## How to get to a place — the keyboard shortcut to jump between pages

Four ways, and they all reach the same seven rooms:

- **`tab`** — the next place, round again from the last. **`shift+tab`** — the one before.
- **`alt+1`** … **`alt+7`** — jump straight to one, **from a place or from a conversation**.
  The numbers are the tab bar's own order, so `alt+1` is home and `alt+7` is settings. Hold
  `alt` and press the digit. `tab` and the shift-arrows are not like them: in a conversation
  those already belong to path completion and to the caret, so the digits are the one class
  of place key that means the same thing wherever you are standing.
- **type its name** — on home, typing `sta` offers the standing place beside the
  conversations that match. A place ranks first, wears `▸`, and says `a place` out at the
  right margin. Home's list is a **drop-up** — it is read upward, out of the box you typed
  into — so ranking first means the offered place sits **below every conversation the same
  words matched**, one row above `ask here` and `start a new conversation`, which is the
  nearest row to your hand.
  Where the place can say what is behind it without going to the disk for it, the margin
  says that too: `a place · 6 orders, 1 fired today` on standing. A place that has nothing
  to count, or nothing in it, says `a place` alone.
- **a command** — `/home`, `/history`, `/standing`, `/memory`, `/settings`. Each opens the
  place it names.
- **click the word** — the tab bar itself is the control. A press on a place's word goes
  there; a press in the gap between two words does nothing, and a press on the word you are
  already standing on does nothing (going there would throw away what you have typed and
  the row you are on).

`alt+<digit>` arrives in every terminal aforge runs in. `ctrl+<digit>` does not exist as a
thing a terminal can send, which is why the numbers are on `alt`.

**`tab` walks the whole circle, and no room on it is ever shut.** `tab` and `shift+tab`
step from one place to the next in the bar's order and round again from the last; there is
no state of the machine in which one of them is skipped. A place with nothing in it opens
and spends the frame saying what it is for, which is the answer somebody arriving at an
empty room actually wants — see **Every place opens, always** further down this page.

That is true of every door onto a place and not only of the walk: a number, a click on the
word, and the command that names it all open the same room on the same machine. `/history`
and a bare `/task` on a machine that has run nothing open the tasks place, and the page
says what tasks are and ends `no tasks yet — /task <brief> starts one`.

## The mouse on a place — clicking a row, hovering, and the wheel

Three gestures, the same on all seven places:

- **the pointer previews and the cursor selects.** Whatever your pointer is resting on is
  what the card beside the list is about, and on home it is what the right-hand card shows.
  Moving the pointer moves nothing else — the cursor stays where you put it.
- **a click puts the cursor on that row.** On the places it never acts: the verbs are keys
  and `enter` leaves the conversation you are sitting in, so a click that opened something
  would be a gesture nobody can aim. Home is the exception it always was — the first click
  chooses the row, a second click on the same row opens it.
- **the wheel walks the list**, three rows a turn, on every place. There is no separate
  scroll offset: the window follows the cursor, so scrolling and choosing are one gesture.
  Walking off the bottom with `↓` scrolls the same way.

## The composer — typing on any place, and how to start a task from any page

Every place has one box at the foot, and **every printable key goes into it, always**. There
is no mode to enter and no key to press first.

The box has two readings at once, with no switch between them:

- what you type **filters** what the place is showing — the conversations on home, the runs
  on tasks, the rows in settings;
- and it is also the **first sentence** of something new.

Two keys tell those apart:

- **`enter`** — talk about it. On a row, it opens that row. With something typed and no row
  chosen, it starts a conversation carrying what you wrote.
- **`alt+enter`** — send it off as a task. It runs on its own and tells you when it lands.
  The first press opens the composer layer, where the three facts a task needs are settled;
  the second press is the send. The next section is that layer in full.

At the right of the box is the **scope chip** — the `here ~/aforge-v2` next to the box. It is where what you type
will land — the project the cursor is on, or this window's own project. It is drawn even
with nothing typed, because a verb that is always in reach has to always say where it goes.
The path is shortened the same way every path on this surface is: `~` for your home
directory, and a first letter for each folder above the last when the whole thing will not
fit. Home used to draw it in full while every other place shortened it; both are short now.

## The composer layer — set which project, pick the model and set a spend limit before starting a task

Press **`alt+enter`** with something typed into the composer, on any place, and the layer
opens. It is not a new screen: **the page behind dims to the faintest tier instead of being
covered**, the box stays exactly where it was, and three lines appear in the air under it.

```
› cut the opus spend in half without losing the sweep                       here ~/aforge-v2
 it will run on its own and tell you when it lands                                    a task
 · in ~/aforge-v2, on master                                                alt+w to move it
 · execution runs on opus 4.1                                                alt+o to change
 · it may spend up to $10.00 before it asks                                    type a number
 alt+enter send it off · enter talk about it first · esc back to spend
```

Those three are the only facts a task needs before it leaves: **where, on what, how much.**
Each one is edited on the line that shows it.

- **`in ~/aforge-v2, on master`** — the project the task will work in, and the branch that
  tree is on right now. **`alt+w`** cycles it through the projects aforge knows, this
  window's own first, and round again from the last. A machine with one project has nowhere
  to move a task to, so the `alt+w to move it` clause is not on the line and the key does
  nothing. A folder that is not a repository, or one on a detached head, draws the project
  and stops there rather than trailing a comma.
- **`execution runs on opus 4.1`** — the model the WORK will run on. That is the *execution*
  slot, which is a different thing from the model you are talking to: the errand still talks
  on this window's own model, and only the work it hands out moves. **`alt+o`** opens the
  model list — the same list `/model` opens, the same rows, the same filter box — drawn
  inside the layer, and `enter` on a row binds it **for this task only**. Nothing is written
  to your settings. With nothing anywhere able to say what execution runs on, the line is
  absent and so is the key.
- **`it may spend up to $10.00 before it asks`** — the cap. **Type a number** while the
  layer is up and the figure changes as you type; `backspace` takes a character off. It is
  a real limit and not a label: the task stops and asks you when it reaches it. See the
  tasks page, *a task started from the composer carries a cap*.

The foot names everything that is live: **`alt+enter` sends it off**, **`enter` talks about
it first** (which is the ordinary conversation, carrying the same sentence), and **`esc`
goes back to the place you were on** with your sentence still in the box. Nothing was
applied on the way in, so `esc` has nothing to undo.

While the layer is up it has the whole keyboard. `tab` does not walk to the next place and
letters do not reach the composer — the sentence is already written and is on the screen
above you.

## Where the task appears after you send it — `alt+enter` takes you to home

**`alt+enter` from a place that is not home takes you to home**, because home's column is
the only surface that draws an errand's answer. You are left looking at the thing you just
started rather than on the page you typed it from.

That is a deliberate limit and not a finished design. The right answer is for the errand to
be drawn on the place you sent it from — a small band the frame draws above the composer on
any place — and until that lands, the surface carries you to where the answer will arrive
rather than starting work somewhere you cannot watch it.

## The keys, and the one law behind them

**No key does anything that is not drawn on screen right now.**

That is the whole grammar, and it cuts both ways: a bare letter is never a verb, and a place
may not name a key it has not bound. Six classes, and a key belongs to exactly one:

| | |
| --- | --- |
| `↑` `↓` `enter` `esc` `tab` | move, open, back out, next place |
| any printable key | goes to the composer, always |
| `alt+enter` | send what you typed off as a task |
| `alt+1` … `alt+7` | jump straight to a place |
| `alt+<letter>` | change how THIS place is shown |
| `alt+w` `alt+o` | inside the composer layer only: move the task, change its model |
| `shift+←` `→` `↑` `↓` | move this place's time window |
| `→` then a letter | act on the row — letters are verbs only here |

`alt+w` and `alt+o` belong to the composer layer and to nothing else. No place binds either
of them, so they can never move a view out from under you while you are aiming at a
destination; pressed with no layer up, they do nothing.

The last two classes are bound where there is something to bind. `alt+<letter>` today is
`alt+g` and `alt+q` on home, which group the list by project and hide the quiet rows, and
`alt+s` on the memory place, which changes which shelf it is showing. `shift+<arrow>` is a place's time
window — `shift+←→` moves it by its own length, `shift+↑↓` changes how coarse it is — and
three places have one: **tasks** (when it ran), **standing** (when it fired) and **spend**
(which days). All three draw the same control on their own head row, at the right of the
line: `shift+← aug 12 – aug 25 →`, with `shift+↑ coarser` beside it where the line has room.
The label between the arrows is the control and the reading at once, so the span is on the
screen once and the keys that move it are beside it. A place with no window to move answers
those keys with nothing rather than with something that is not drawn, and so does a terminal
too narrow to draw the control — and the zoom is bound only where its own clause fits, for
the same reason. **memory has no time window**; its `shift+<arrow>` keys do nothing.

## What the right arrow does on a row — the verbs, and why letters are safe there

Press `→` on a row that can be acted on and a strip of verbs opens **directly under that
row**, on every place that has verbs — home, tasks, standing and memory:

```
p pause   s stop   n not here
```

On home the verbs are the row's own — a question's first two answers in its own words on
`y` and `n`, `a put it away`, `t new chat here`, `o open folder`, `c copy path`, and
`p pause it` or `r resume it` on a standing item.

While that strip is drawn, **those letters are the verbs** and the composer is asleep. The
strip pushes the rest of the list down by its own height — the frame stays the same height
and the composer does not move — and that visible displacement is exactly why the letters
are safe: you can see that typing has stopped.

`esc` or `←` closes it. `enter` still opens the row. Moving off the row with `↑` or `↓`
closes it too — verbs belong to one row, and carrying them onto the next is how a key acts on
something you were not looking at.

The verbs are the row's own. A conversation that is not asking anything has no `y`; a row
that cannot be paused has no `p`. Where a row has no verbs at all, `→` keeps every other
meaning it already had.

## alt+. — see all the keyboard shortcuts at once, on the page you are on

Press `alt+.` and the whole key map appears **in the cells you were already reading**:

- the tab bar's words grow their numbers — `1 home`, `2 tasks`, `3 standing`, …
- the hint line becomes the chord list

Nothing moves, nothing pops up, and the next key you press takes it away and then does what
it was always going to do. `esc` just takes it away.

It is a chord rather than a hold because a terminal cannot tell a program that a modifier is
being held down — it only reports what arrived.

## home — what wants you

The first place, and the one aforge opens on. Every conversation on this machine, from
every project, as **one flat ranked list** — what wants you first, then what is moving, then
the rest — with the project as a tag on each row.

Its own keys are in the **Home** page. Under the tab bar: `tab` is the way to the next place
rather than the way between home's old columns, the errand pane is taken into with `→`
rather than `tab`, and home's two `alt+<letter>` keys are **`alt+g` group by project** and
**`alt+q` hide the quiet ones**.

## tasks — the tasks page, and how to get to it without a command

Everything this machine has run, across every project and every conversation, grouped by
what you do next: `needs your look`, `running`, `done today`, `earlier`. `/history` and
`ctrl+.` both open it, and so does `alt+2`.

Type to filter. `enter` opens a task's room when this conversation is holding it, and goes
inside its record card otherwise. `→` opens the row's verbs, and this place has one —
`s stop it`, over a task this conversation is holding that is still queued or running.
Nothing is behind a fold; the list scrolls and its tail fades. The count of what is on the
page sits just above the composer, and the foot names only what is true of the row you are
on: `enter open its room · → verbs: stop it · type to filter`.

## standing — what runs without being asked, and where to type on the standing page

The orders that fire on their own, on four shelves each under its own heading: this
conversation's, this project's, the machine's, and then `in other projects` — everything
else standing on this computer that does not reach the conversation you are in.
`/standing` and `/orders` open it, and so does `alt+3`.

`enter` opens where an order was asked for. `→` opens the row's verbs — `p pause`, `s stop`,
and `n not here` on the three shelves that reach this conversation. Those three used to be
bare letters; they moved onto the strip when standing became a place with a composer under
it, because every printable key belongs to the composer. On the `in other projects` shelf
only `p` and `s` are offered: `n` names an exception in a place that order never reached,
so it is not there at all.

Each row also says **how much rope** the order has — `asks first`, `earning trust 3/5`, or
`trusted alone` — which is the fact that decides whether you have to watch it.

Nothing is behind a fold — `↑ ↓` walks every order and the list scrolls with the cursor.
Under the row you are on, an order that has been looked at adds a short `last look`
paragraph. The header carries a time window for **when it fired**: `shift+←→` moves it,
`shift+↑↓` changes how coarse it is, and it opens holding every firing this computer has.
The standing orders page has the whole of it.

## memory — what is held true

What aforge holds true about you and this machine, with what kind of thing each line is, how
it has done — `helped 19 · bore on 3` — and how old it is out at the right. `/memory` and `/memories` open it, and so does `alt+4`.

The page is **shelves** — you, this project, this machine — biggest first, with the biggest
one open and the rest rolled up. Type to filter what is already on the page, **every letter
including `u`**; the store is read when you walk in and on the place beat, never on a
keystroke. `enter` opens or closes a shelf, and `enter` on a line opens that line's card and
shows where it was learned. `alt+s` walks the shelves one at a time. `→` opens the verbs on a
line — `e fix the wording`, `f forget it`, and `u put it back` while there is something to
put back.

`delete` also forgets the line under the cursor.

## spend — what it cost

What this machine has cost, by the day, by the model, and by what it was for. `alt+5` opens
it. It reads one machine-wide ledger — a line per model call — so the figures are the bill
and not an estimate.

Three blocks: the window with its sparkline, `what ran it` by the model and the role that
model is **bound** to, and `what it was for`. `enter` on a row of the last one opens the
thing the money went on — a task, a standing promise, or a conversation. `shift+←` and `shift+→` move the window by its own length;
`shift+↑` and `shift+↓` change how coarse it is.

**There is nothing to set here.** The allowance is a rail and it is edited on the status
line's money segment, which is where it is shown. `/cost` (also `/usage`, `/tokens`,
`/spend`) still prints **this conversation's** figures into the conversation — a different
question from this place's, which is the whole machine.

With nothing spent inside the window, the place says what it is for and nothing else.

## search — finding anything said or run

Everything that has been said on this machine. `alt+6` opens it, and typing searches: the
matches come back with the conversation they were said in, how long ago, and the project it
belongs to, with your own words picked out in the line.

Nothing is indexed behind your back — this reads the record that was already being kept. The
read happens after the box has been quiet for a moment, never on the keystroke, so typing
never waits on a search.

`enter` opens the conversation the matching turn was said in. Above the results, a legend
counts the projects the matches came from. Twelve conversations are shown and the rest fold
into one line.

Typing here searches and nothing else. **Typing on home is what offers places** (`sta` offers
the standing place beside the chats that match) — the same offer made twice, one `tab` apart,
would be two rankings that could disagree.

## settings — how this machine is set

Every setting, in sections, with a search that crosses all of them. `/settings`, `/set` and
`/config` open it, and so does `alt+7`.

It has a **second bar** under the place bar: its own sections. Those two bars are not a
repetition — the upper one is the seven places, the lower one is settings' own pages. `←` and
`→` move between sections. `tab` does **not**: it is the way to the next place, here as
everywhere.

## Why a nearly-empty place says what it is for

You only ever arrive at a place on purpose — from the tab bar, from a number, or by typing
the word. That arrival is the one moment somebody is asking "what is this", so a place with
nothing of its own to draw answers, in a few sentences, and says nothing else.

**Every place opens, always.** There is no state of the machine in which one of the seven
words on the bar is a key that does nothing. On a machine aforge was installed on an hour
ago, `alt+2`, `alt+3` and `alt+4` all open:

- **tasks** on a machine that has run nothing says what tasks are, and ends
  `no tasks yet — /task <brief> starts one`.
- **standing** on a machine nothing stands on says
  `nothing stands here yet — say what should always be true, and I'll hold it.`
  and two lines about how far an order can reach. No shelf heading and no time window is
  drawn over it.
- **memory** on a machine that has remembered nothing says what memory is for. If this
  build is not remembering anything at all, it says the same thing with one dim line under
  it: `memory is off for this session · turn it on under /settings`.
- **spend** inside a window nothing was spent in says what spend is for.
- **search** with an empty box says what search is for.
- **home** over `--host` says `home shows this machine's projects, and this session is on
  another` where its rows would be — the projects under this process belong to the laptop
  and the session is on the server, so the list is the only part that cannot be drawn.

There is no "coming soon", no greyed-out list and no empty table with headings over it. A
page that draws the furniture of a feature it does not have looks like a bug rather than like
a plan — which is also why a heading is never drawn over an absence.

Commands behave the same way. `/history` on a machine that has run nothing, `/standing` on
one nothing stands on, and `/memory` with no store all open their place and let it teach.
They used to write one line into the conversation and open nothing; on a fresh machine that
was every door onto those three pages, so the first thing a new person tried appeared not to
work.

## What a number beside a place means

A tab wears a number when **something in that place has changed since you last looked at that
place** — not how many things are in there. A permanent count is furniture, and furniture is
what people stop seeing.

Home, tasks, standing and memory can wear one. Spend is a sum, search is something you do,
and settings is how this machine is set — a number in front of any of those would be a number
about nothing.

**Leaving a place is what counts as having looked at it.** The stamp is written on the way
out, not on the way in: a stamp taken on arrival would call everything seen the instant it
appeared, before your eye had crossed a row. A window closed with a place still open writes
nothing, and the same things are news again next time.

A place you have never left has no stamp and therefore **no number at all** — the first look
greets you with a bare bar rather than with a count over every tab. An unknown count is drawn
as nothing rather than as a zero.

On memory the number is the memories learned plus the ones let go of since you were last
there. The counts are recomputed on the same three-second beat the places read on, so a tab
loses its number within a few seconds of the place being read rather than the instant you
walk in.

## The rewind timeline is not a place

`/rewind` (also `/undo`, `/back`) opens a full-screen page too, and it is deliberately **not**
one of the seven. It is something you do to *this conversation* — pick a point and cut back
to it — rather than a room in the machine, so it has no tab and `tab` does not walk to it.
