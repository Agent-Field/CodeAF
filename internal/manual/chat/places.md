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

Two of those are still nearly empty and say so. `alt+<letter>` today is `alt+s` on the
memory place, which changes which shelf it is showing, and nothing else. `shift+<arrow>` is
bound to nothing at all yet: no place has a time window to move, so the keys do nothing
rather than doing something that is not drawn.

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

Everything this project has run: the tree of what is running now, and the flat record of
everything before it. `/history` and `ctrl+.` both open it, and so does `alt+2`.

Type to filter. `enter` opens a run's room. The count of what is on the page sits just above
the composer.

## standing — what runs without being asked, and where to type on the standing page

The orders that fire on their own: this conversation's, this project's, and the machine's,
each under its own heading. `/standing` and `/orders` open it, and so does `alt+3`.

`enter` opens where an order was asked for. `→` opens the row's verbs — `p pause`, `s stop`,
`n not here`. Those three used to be bare letters; they moved onto the strip when standing
became a place with a composer under it, because every printable key belongs to the composer.

## memory — what is held true

What aforge holds true about you and this machine, with what kind of thing each line is and
how old it is out at the right. `/memory` and `/memories` open it, and so does `alt+4`.

Type to filter — **every letter, including `u`**. `alt+s` walks the shelves: everything, then
what is about you, then this project, then this machine. `enter` opens a line and shows where
it was learned. `→` opens the verbs — `e fix the wording`, `f forget it`, and `u put it back`
while there is something to put back.

`delete` also forgets the line under the cursor.

## spend — what it cost

**This place has no body yet.** It says what it is for, and nothing else:

> What this machine has cost, by the day, by the model, and by what it was for. Every model
> call writes a line, so the figures here are the bill and not an estimate. There is nothing
> to set here — the allowance is edited on the status line that shows it.

`alt+5` opens it. Until it is built, `/cost` (also `/usage`, `/tokens`, `/spend`) is where the
figures are, printed into the conversation.

## search — finding anything said or run

**This place has no body yet either.** It says what it is for:

> Everything that has been said on this machine, and everything that has been run. You type
> what you remember of it and the matches come back with the place they live in. Nothing is
> indexed behind your back: this reads the record that was already kept.

`alt+6` opens it. Until it is built, typing on home searches the conversations on this
machine, which is what home's box has always done.

## settings — how this machine is set

Every setting, in sections, with a search that crosses all of them. `/settings`, `/set` and
`/config` open it, and so does `alt+7`.

It has a **second bar** under the place bar: its own sections. Those two bars are not a
repetition — the upper one is the seven places, the lower one is settings' own pages. `←` and
`→` move between sections. `tab` does **not**: it is the way to the next place, here as
everywhere.

## Why a nearly-empty place says what it is for

You only ever arrive at spend or search on purpose — from the tab bar, from a number, or by
typing the word. That arrival is the one moment somebody is asking "what is this", so the
place answers, in three sentences, and says nothing else.

There is no "coming soon", no greyed-out list and no empty table with headings over it. A
page that draws the furniture of a feature it does not have looks like a bug rather than like
a plan.

## What a number beside a place means

A tab wears a number when **something in that place has changed since you last looked at that
place** — not how many things are in there. A permanent count is furniture, and furniture is
what people stop seeing.

Home, tasks, standing and memory can wear one. Spend is a sum, search is something you do,
and settings is how this machine is set — a number in front of any of those would be a number
about nothing.

**Today no tab wears a number at all**, because the record of when you last looked at each
place separately is not written yet. An unknown count is drawn as nothing rather than as a
zero.

## The rewind timeline is not a place

`/rewind` (also `/undo`, `/back`) opens a full-screen page too, and it is deliberately **not**
one of the seven. It is something you do to *this conversation* — pick a point and cut back
to it — rather than a room in the machine, so it has no tab and `tab` does not walk to it.
