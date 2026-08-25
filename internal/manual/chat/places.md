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
- **`alt+1`** … **`alt+7`** — jump straight to one. The numbers are the tab bar's own order,
  so `alt+1` is home and `alt+7` is settings. Hold `alt` and press the digit.
- **type its name** — on home, typing `sta` offers the standing place beside the
  conversations that match. A place ranks first, wears `▸`, and says `a place` out at the
  right margin.
- **a command** — `/home`, `/history`, `/standing`, `/memory`, `/settings`. Each opens the
  place it names.

`alt+<digit>` arrives in every terminal aforge runs in. `ctrl+<digit>` does not exist as a
thing a terminal can send, which is why the numbers are on `alt`.

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

At the right of the box is the **scope chip** — the `here ~/aforge-v2` next to the box. It is where what you type
will land — the project the cursor is on, or this window's own project. It is drawn even
with nothing typed, because a verb that is always in reach has to always say where it goes.

`alt+enter` from a place that is not home takes you to home and asks there, because home is
where an errand's answer is drawn. You are left looking at the thing you just started.

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
| `shift+←` `→` `↑` `↓` | move this place's time window |
| `→` then a letter | act on the row — letters are verbs only here |

One of those is still thinly bound and says so: `alt+<letter>` today is `alt+s` on the memory
place, which walks the shelves, and nothing else. `shift+<arrow>` is a place's time window —
`shift+←→` moves it by its own length, `shift+↑↓` changes how coarse it is — and three places
have one: **tasks** (when it ran), **standing** (when it fired) and **spend** (which days).
A place with no window to move answers those keys with nothing rather than with something
that is not drawn, and so does a terminal too narrow to draw the control.

## What the right arrow does on a row — the verbs, and why letters are safe there

Press `→` on a row that can be acted on and a strip of verbs opens under the list:

```
p pause   s stop   n not here
```

While that strip is drawn, **those letters are the verbs** and the composer is asleep. The
strip pushes the body down by a row, and that visible displacement is exactly why the letters
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

The first place, and the one aforge opens on. Every project on this machine and every
conversation in them, with what needs you first.

Its own keys are in the **Home** page. Under the tab bar, two things changed: `tab` is the
way to the next place rather than the way between home's columns, and the errand pane is
taken into with `→` rather than `tab`. `←` from rest is the named way into `needs you`.

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

Three blocks: the window with its sparkline, `what ran it` by the model, and `what it was
for`. `enter` on a row of the last one opens the thing the money went on — a task, a standing
promise, or a conversation. `shift+←` and `shift+→` move the window by its own length;
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
nothing of its own to draw answers, in three sentences, and says nothing else.

Today that is **spend on a machine that has spent nothing inside the window it is showing**,
and **memory on a machine that has remembered nothing** — which says what memory is for
rather than what the place is.

There is no "coming soon", no greyed-out list and no empty table with headings over it. A
page that draws the furniture of a feature it does not have looks like a bug rather than like
a plan — which is also why a heading is never drawn over an absence.

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
