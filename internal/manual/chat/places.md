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
- **`alt+1`** … **`alt+7`** (**`⌥1`** … **`⌥7`** on a Mac) — jump straight to one, **from a
  place or from a conversation**. The numbers are the tab bar's own order, so `alt+1` is home
  and `alt+7` is settings. Hold `alt` and press the digit. macOS draws the modifier as `⌥`
  because that is what the keycap says; Linux and Windows draw it `alt+`, and it is the same
  chord either way. `tab` and the shift-arrows are not like them: in a conversation those
  already belong to path completion and to the caret, so the digits are the one class of
  place key that means the same thing wherever you are standing.
- **`ctrl+1`** … **`ctrl+7`** — the same jump, on the terminals that can send it. `ctrl` and
  a digit has no encoding in the scheme most terminals speak, so this is a second spelling and
  never the first: it works only where the terminal runs the kitty keyboard protocol and says
  so (kitty, ghostty, WezTerm, foot, Windows Terminal are the usual ones). `ctrl+.` draws the
  map there too. Where the terminal has said nothing, these do nothing and are never drawn —
  the map's own line names them exactly when they are live.
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

## How do I move between the tabs with the arrow keys — the tab bar is a row the cursor can stand on

**Press `↑` from the first row of the page you are on.** The cursor leaves the list and
lands on the **tab bar** — the row of seven words under the top line — and from there:

| Key | What it does on the bar |
| --- | --- |
| `←` `→` | walk one word along, wrapping round from either end. Nothing opens |
| `enter` | go into the place under the cursor |
| `↓` | the same as `enter` — go in |
| `esc` | back into the page, on the row you walked up from |
| `↑` | nothing. There is nothing above the bar but the top line, which is a reading rather than a control |
| `tab` `shift+tab` | the next and the previous place, exactly as everywhere else |
| `alt+1` … `alt+7` | jump straight to one, exactly as everywhere else |
| any printable key | goes to the composer, and the cursor comes back down into the page with it |

**The word your cursor is on wears the cursor's band**, in place of the mark the word you
are standing in usually wears. So while the cursor is up there the bar is saying "this is
where your cursor is" rather than "this is the room you are in" — and the page underneath
is still saying which room that is, on every one of its rows.

**Walking the bar opens nothing.** That is the whole difference between this and `tab`:
`tab` steps into each room as it passes, which closes the last one and throws away whatever
you had typed into it, so looking along seven words used to cost seven openings. `←` and
`→` move a cursor and nothing else; `enter` is what goes in.

**`←` and `→` do not open a row's verbs while the cursor is on the bar**, and they do not
open or close a fold either. Those are things you do to a **row**, and the bar is not a row
of any page's list — see *What the right arrow does on a row*.

**`↑` reaches the bar from every place**, and from the first row of every one of them.
Where a page has no row to stand on at all — an almost-empty room saying what it is for —
`↑` reaches the bar from there too. What it does **not** do is reach a bar that is not
drawn: home on a phone-shaped frame and a task's record card put something else in those
cells, and there `↑` is the walk it always was.

**Pressing `esc` on the bar does not close the place.** It puts the cursor back in the
page; a second `esc` is the page's own, and that one leaves.

## Does the tab bar do anything when I hover over it — the pointer on the words

**Yes: the word under your pointer lifts.** Resting the mouse on a place's word paints that
word in the brighter ink the word you are standing in wears, without the band under it — so
the bar says "this one is a door" without pretending you are already in it. Moving the
pointer off the word puts the ink back, and moving it into the gap between two words lifts
nothing, because the gap belongs to no room.

**Hovering moves nothing else at all.** No cursor, no page, no window — the pointer
previews and the cursor selects, which is the same law the rows underneath keep. If the
keyboard cursor is standing on the bar as well you will see both marks at once: the band on
the word the cursor is on, the lifted ink on the word the pointer is on.

**The wheel over the bar walks the places**, one room a turn — `tab` and `shift+tab` under
the hand that is already there. It is one room a notch and not three, because each step
opens a room, and two rooms nobody asked to see is two filters thrown away.

**Clicking still opens.** A press on a word goes there; a press in the gap does nothing;
a press on the word you are already standing on does nothing.

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

The **tab bar** answers all three too, and it answers them as itself rather than as a row of
the list: the pointer lifts the word it is on, the wheel walks the places one room a turn,
and a click opens. Its own section is above.

## The composer — typing on any place, and how to start a task from any page

Every place has one box at the foot, and **every printable key goes into it, always**. There
is no mode to enter and no key to press first.

The box has two readings at once, with no switch between them:

- what you type **filters** what the place is showing — the conversations on home, the runs
  on tasks, the rows in settings;
- and it is also the **first sentence** of something new.

Two keys tell those apart:

- **`enter`** — talk about it. On a row, it opens that row. With something typed and no row
  chosen, it starts a conversation carrying what you wrote. However many conversations this
  terminal already holds, it starts another: there is no cap, and nothing refuses one for
  being the ninth. Where the door itself fails — `/new is unavailable here`, or a session
  folder that could not be made — your sentence is not sent anywhere at all, and never into
  the conversation that was behind the place.
- **`alt+enter`** — send it off as a task. It runs on its own and tells you when it lands.
  The first press opens the composer layer, where the three facts a task needs are settled;
  the second press is the send. The next section is that layer in full.

**What the empty box SAYS is the place's own sentence where the two readings are not both
true.** With nothing typed it reads `› say what you want done` on home and on every place
you can start something from. On the **tasks** place it reads `› type to filter this list`,
because there is nothing to send from there — `enter` opens the row under the cursor and
nothing else — so the shared prompt was inviting an instruction into a slot that could only
ever narrow a list. The moment you type, the box is your text on every place alike, and a
click puts the caret where you clicked.

At the right of the box is the **scope chip** — the `here ~/aforge-v2` next to the box. It is where what you type
will land — the project the cursor is on, or this window's own project. It is drawn even
with nothing typed, because a verb that is always in reach has to always say where it goes.
The path is shortened the same way every path on this surface is: `~` for your home
directory, and a first letter for each folder above the last when the whole thing will not
fit. Home used to draw it in full while every other place shortened it; both are short now.

**Home has no chip.** It says the same thing one row up, on its rule, and says more:
`→ new conversation in ~/src/parser · glm-5.3-flash` is where a sentence will land *and*
what it will answer on, with `alt+w folder · alt+o model` naming the two chords that change
either. The chip on home was a reading nothing acted on — `enter` opened a conversation in
this window's folder whatever the chip said — and the rule is that reading with `enter`
honouring it. Home's own page has the whole gesture.

## The composer layer — set which project, pick the model and set a spend limit before starting a task

Press **`alt+enter`** with something typed into the composer, on any place, and the layer
opens. It is not a new screen: **the page behind dims to the faintest tier instead of being
covered**, the box stays exactly where it was, and three lines appear in the air under it.

```
› cut the opus spend in half without losing the sweep                       here ~/aforge-v2
 it will run on its own and tell you when it lands                                    a task
 · in ~/aforge-v2, on master                                                alt+w to move it
 · execution runs on opus 4.1                                                alt+o to change
 · it may spend up to $100.00 before it asks                                    type a number
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
- **`it may spend up to $100.00 before it asks`** — the cap. **Type a number** while the
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
| `↑` `↓` `enter` `esc` `tab` | move, open, back out, next place — and `↑` off the first row of the page moves onto the **tab bar**, which is a row the cursor can stand on (*How do I move between the tabs with the arrow keys*) |
| any printable key | goes to the composer, always |
| `alt+enter` | send what you typed off as a task |
| `alt+1` … `alt+7` (`⌥1` … `⌥7` on a Mac) | jump straight to a place |
| `ctrl+1` … `ctrl+7` | the same jump, only on terminals that report they can send it |
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

On memory they are `c open the card`, `e fix the wording` and `f forget it`. On home the
verbs are the row's own — a question's first two answers in its own words on
`y` and `n`, `a put it away`, `t new chat here`, `o open folder`, `c copy path`, and
`p pause it` or `r resume it` on a standing item.

While that strip is drawn, **those letters are the verbs** and the composer is asleep. The
strip pushes the rest of the list down by its own height — the frame stays the same height
and the composer does not move — and that visible displacement is exactly why the letters
are safe: you can see that typing has stopped.

`esc` or `←` closes it. `enter` still opens the row. **Anything that moves the cursor off
that row closes it** — `↑`, `↓`, `ctrl+p`, `ctrl+n`, `home`, `end`, the page keys, a click on
another row, the wheel, or the list being rebuilt under you by the three-second beat. The
strip belongs to the row it was opened on and not to a list of keys: verbs belong to one row,
and carrying them onto the next is how a key acts on something you were not looking at.

The verbs are the row's own. A conversation that is not asking anything has no `y`; a row
that cannot be paused has no `p`. Where a row has no verbs at all, `→` keeps every other
meaning it already had.

## Why does the line at the bottom say fewer things on a narrow window — the hint line drops whole clauses

**The foot never cuts a word, and it never cuts a key in half.** When the frame is too
narrow for everything the line has to say, whole clauses are dropped, in a stated order,
and what is left reads as a shorter sentence rather than as a truncated one. There is no
`…` on this line.

**What goes, and what never goes.** The way out is the last clause and it is kept to the
last cell there is — `tab next place`, and the `esc` after it where a place adds one. What
is dropped is taken from the clause *nearest* that protected tail, working backwards, so at
80 columns the composer's own line loses `alt+. for the map`, then
`alt+enter send it off as a task`, leaving `enter talk about it · tab next place`. The head
clause — what `enter` does on the row you are standing on — is the last thing to go, being
the only one on the line about the thing under the cursor.

**A foot that is a sentence rather than a key list is cut the same way.** Home's own foot
says things like

```
 open in another window — enter again to move it here (that window's reply stops there; its tasks resume here)
```

which has no `·` in it at all. The bracketed gloss goes first, because it explains a clause
that is still on the line; then the clause hanging off the dash; and what a 40-column
terminal is left with is the statement, `open in another window`. It used to read
`open in another window — enter again to move it here (it …`, which named a key and then ate
it.

**A clause that begins `or` goes first, because it is an alternative and not a way out.**
The gate card in an adaptive run says
`↑ ↓ pick · enter answers · or keep typing to steer the planner`, and the last clause there
is not the escape hatch the rule above protects — it offers a *second* route to something
the clause in front of it already offers a first route to. So it is the first thing off the
line, and a narrow gate card keeps `↑ ↓ pick · enter answers`, which is the pair the card
exists to be answered with.

## Why the note above the composer drops its *end* while the key line drops its middle

They are two different kinds of line and they are fitted by two different rules.

- **A key line** — the foot, a filter box's placeholder, a gate card's gestures — has the
  way out at the **end**, so the end is protected and the clause nearest it is what goes.
- **A note** — the dim line under the rule that says what just happened — has the answer at
  the **front**, so the front is protected and the line gives up its later clauses. Settings
  says `saved to your profile · a project's own .aforge-v3/config.json is a hand edit`; at
  sixty columns that becomes `saved to your profile`, which is the half you asked for. The
  macOS chord note behaves the same way: `your terminal sends ⌥ as a letter — turn on "use
  option as meta" in Terminal: Profiles › Keyboard` shortens to
  `your terminal sends ⌥ as a letter`.

Neither of them ever ends in `…`, and neither ever cuts inside a word.

## alt+. — see all the keyboard shortcuts at once, on the page you are on

Press `alt+.` and the whole key map appears **in the cells you were already reading**:

- the tab bar's words grow their numbers — `1 home`, `2 tasks`, `3 standing`, …
- the hint line becomes the chord list

`?` over an empty box draws the same map, which is what that key means on a place — show me
the keys for where I am standing. In a conversation the same key opens the `/help` sheet.

**The chord list is built from the place you are on.** It reads
`alt+1…7 go to a place · alt+enter send it off as a task · → show what this row can do · esc close`
— and the `→` clause is left out on a place whose rows have no verbs, such as search, rather
than naming a key that would open nothing there.

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
what you do next: `your call`, `running`, `waiting`, `finished today`, `earlier`.
`/history` and `ctrl+.` both open it, and so does `alt+2`.

The groups describe each task's own tier. An unrelated approval in its conversation
does not move running tasks into `your call`. A live design approval stays there
until you answer; a provider or dependency wait belongs under `waiting`. A retained
branch stays available to inspect and does not, by itself, ask you to merge it.

**Typing here narrows the list.** While this place is up every printable key goes to its
filter, and **the box at the foot says so itself** — it rests on `› type to filter this list`
rather than the `› say what you want done` every other place shows. It used to show the
shared prompt with the correction two rows further down on the foot, which meant the loudest
row on the screen was inviting a message the page cannot send. `enter` opens a task's room
when this conversation is holding it, and goes inside its record card otherwise. `→` opens
the row's verbs, and this place has one — `s stop it`, over a task this conversation is
holding that is still queued or running. Nothing is behind a fold; the list scrolls and its
tail fades. The count of what is on the page sits just above the composer, and the foot names
only what is true of the row you are on: `enter open its room · → verbs: stop it`.

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
keystroke. `enter` opens or closes a shelf; `enter` on a LINE is `ask me about it` — the
line goes into a fresh conversation as its opening message and you are taken there. `alt+s`
walks the shelves one at a time. `→` opens the verbs on a line — `c open the card`, which is
where the full text and where it was learned live, `e fix the wording`, `f forget it`, and
`u put it back` while there is something to put back.

The foot follows the row: `enter ask me about it · e fix the wording · f forget it` on a
line, `enter open a shelf · type to filter · alt+s walk the shelves` on a shelf heading.
**On a machine that has remembered nothing there are no shelves, so none of those keys is
named** — the foot is `tab next place · esc` and nothing else, and the body's last line says
what to do instead: `nothing learned yet — say "remember that …" and the first line lands
here`.

`delete` also forgets the line under the cursor.

## spend — what it cost, and why it counts another session or another machine

What this machine has cost, by the day, by the model, and by what it was for. `alt+5` opens
it. It reads one machine-wide ledger — a line per model call — so the figures are the bill
and not an estimate.

**It is the whole machine and not this session**, which is why a figure here can be larger
than anything this conversation did: every window, every task and every standing run on
this machine writes into that one ledger, including a session opened from another machine
over `--host` whose calls are still made here. For this session alone, ask `/cost`.

Three blocks: the window with its sparkline, `what ran it` by the model and the role that
model is **bound** to, and `what it was for`. `enter` on a row of the last one opens the
thing the money went on — a task, a standing promise, or a conversation. `shift+←` and `shift+→` move the window by its own length;
`shift+↑` and `shift+↓` change how coarse it is.

**There is nothing to set here, and the page says where to go instead.** Its first line is
one dim pointer — `today $3.42 of $500 · /budget sets the limits` — and `enter` on that line
opens the Spending tab of `/settings`, which is the one editor for every money limit. `→` on
any row of the page opens a verb strip with one letter on it, `b the limits`, which opens the
same tab; `b` works there because the strip naming it is drawn, and everywhere else on this
surface a bare letter belongs to the message box, which is why `/budget` is the keyboard
door. `/cost` (also `/usage`, `/tokens`) still prints **this conversation's**
figures into the conversation — a different question from this place's, which is the whole
machine.

**`/spend` is the typed door onto this place.** It used to be an alias of `/cost`, so the
one word most people guess for "what has this cost" printed one conversation's bill and
never mentioned the machine-wide ledger. It opens the place now.

**The foot names the keys this place has**, and it is built from the row under the cursor:
`enter opens what spent it · → the limits · shift+←→ move the days · tab next place · esc`.
Where the head row is too narrow to draw its own arrows the window clause is dropped, and
over an empty ledger only the way out is named.

With nothing spent inside the window, the place says what it is for and then says which
kind of empty it is: `nothing spent yet — the first model call writes a line here.`
Without that sentence a machine that has spent nothing looks exactly like a ledger that
could not be read, because the emptiness law keeps every `$0.00` off the page.

## search — finding anything said or run

Everything that has been said on this machine. `alt+6` opens it, and typing searches: the
matches come back with the conversation they were said in, how long ago, and the project it
belongs to, with your own words picked out in the line.

Nothing is indexed behind your back — this reads the record that was already being kept. The
read happens after the box has been quiet for a moment, never on the keystroke, so typing
never waits on a search.

`enter` opens the conversation the matching turn was said in, and **the foot says so**:
`enter opens it at that turn · ↑↓ pick · type to search · esc clears the words · tab next
place`. With nothing typed there is no row to stand on, so the foot drops to `type to
search · esc clears the words · tab next place` — `enter` is not named where it does
nothing. (This foot used to be the router's own default, which said `enter talk about it`
and was wrong in both states.) Above the results, a legend counts the projects the matches
came from. Twelve conversations are shown and the rest fold into one line.

**`/search` is the typed door onto this place**, beside `alt+6`, `tab` and the tab bar. It
takes no argument: the place is a box, and typing in it searches.

With nothing typed the place says what it is for, and the last of its four lines says what
to type: `type words you remember — "the docker error", a person's name, a filename`. Those
sentences **wrap** at a reading measure like every other place's — none of them is cut short
with an ellipsis on a narrow window.

**A search that finds nothing says what to do about it**:
`nothing on this machine says "amber rail" · try fewer words, or a name`.

**And a window with no index behind it says so** rather than reporting an empty result:
`there is no index of this machine's conversations behind this window, so nothing can be
searched from here.` — which is a different sentence from "nobody has said that", and the
difference matters. Over `--host` the sentence names the machine instead: the index is the
one this machine's conversations were written into, and the conversation you are in was
written on the other one.

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

## Why the tab bar looks squashed on a narrow terminal — the places at 60 columns

**Every one of the seven words is still on the bar at 60 columns.** What a narrow frame
gives up is the *air between the chips*, not a place: the seven words with the padding each
chip carries are 57 cells, and the spaces between them are six more, so a 60-column
terminal — a split pane, an ssh session, a phone in landscape — draws

```
  home  tasks  standing  memory  spend  search  settings
```

with two cells between the words instead of three. Nothing else changes: the band under the
word you are standing in, the counts, `tab`, `←` `→` and `alt+1`…`alt+7` all mean exactly
what they mean on a wide screen.

**Under about 57 columns the bar carries what it can and counts the rest.** It keeps the
place you are standing in, the word the cursor is on, and any place wearing a count, then
fills in the bar's own order until the row is full and ends with a dim `▸ 3` — the number of
places that are not on the row:

```
  home  tasks  standing  ▸ 4
```

`▸ 3 more` where there are cells for the longer spelling, `▸ 3` where there are not — the
same fold mark every other list on this surface puts over the rows it is not drawing, so a
count on the bar reads as the same idea as `▸ 15 more` at the foot of home. The word gives
way before the mark does: `▸` is what says there is more behind the row. **The
count is a sign and not a button** — pressing it does nothing, because it stands for several
places at once and no single one of them is the answer.

**The key that reaches them is `tab`**, and it is named on the foot of every place —
`… · tab next place` — which is the last clause a narrow foot gives up. `alt+1`…`alt+7`
still go straight to a place whether or not its word is on the row, and the numbers never
move: they are the bar's full seven-place order, not the order of what happens to be drawn.

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
- **spend**, **search** and **memory** over `--host` each say one dim line where their rows
  would be — see *The places over --host* below for the exact words and why three of the
  seven still say them.

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

## The places over --host — whose machine am I looking at

**A place is a listing of one machine's disk, and over `--host` that machine is the one your
session runs on.** Home lists the conversations under `~/.aforge/v3`; tasks lists the work
those conversations ran; standing lists what keeps an eye on that machine; spend adds up the
ledger every model call there writes a line into; search reads the index of what was said
there; memory reads what those sessions learned. All six are directories, and over a
connection there are two machines with those directories on them.

**Home, tasks and standing now read the far machine's.** They ask the engine for its own
reading and draw that. Settings is deliberately mixed; the other three places state that
their readings have not crossed:

| Place | Over `--host` |
|---|---|
| **home** | the far machine's projects and conversations |
| **tasks** | the far machine's work, out of the same reading |
| **standing** | the far machine's orders — both what stands on this conversation and what stands anywhere else on that machine |
| **settings** | this computer's rows; the sheet says the far conversation reads its profile on the other machine |
| **spend** | the far machine's priced model calls |
| **search** | the far machine's conversation index |
| **memory** | the far machine's memories; fixing and forgetting a line write there too |

**No place silently substitutes this laptop's rows for the far machine's.** Settings names
the split as it opens. Spend, search and memory draw no local rows at all over `--host` and
say why. A screen full of the wrong machine's work is a confident lie, and one honest
sentence is better than eight rows and a total in dollars that belong to somebody else's
afternoon.

**The tab bar says whose machine it is.** Over a connection the right end of the bar reads
`on <machine>` — the same name you typed after `--host`, and the same one the status line's
place segment and the legend under the box already carry. On a local session it is not there
at all: a machine name is worth a word only when there is more than one machine in play.

## Why is home empty over ssh when I connect to another machine — space space over --host

**It is not empty any more, and this is the answer if you have seen it be.**

`space` `space` over `--host` opens the home of the machine your session runs on: its
projects, its conversations, and what each of those ran. `enter` on a row opens that
conversation beside the one you are in — the engine gives it a connection of its own and
the chat you came from keeps running, the same door `aforge resume` uses locally.

It used to draw **one dim line** where the rows would be —
`home shows this machine's projects, and this session is on another` — because the projects
it could reach were the laptop's while the work was on the server. Before that it refused to
open at all. If you press space space over a connection and get one line, the machine you are
attached to is running an older aforge than the one you are sitting at, and the fix is the
same as for any version mismatch: update the older one.

In the fraction of a second before the far machine's first answer arrives, home draws **no
rows and no sentence at all**. `nothing here yet` over a server full of work would be the one
wrong thing this screen can say about somebody else's disk, so nothing is said until there is
something true to say.

## Does the tasks page show the other machine's work over --host

**Yes — the far machine's work, and none of this one's.**

The tasks place reads its rows out of the same reading home lists, so the door that carried
home carried this too. `/history`, `ctrl+.` and the tabs all open the same page.

This was the worst of the seven before it crossed. The page walked *this* computer's
`~/.aforge/v3` and drew what it found — a count and a total in dollars, `work aforge ran on
its own. 8, $22.54 of it.` — under a conversation on a server that had run none of it. A page
that reads a real disk and names the wrong machine is worse than a page that says nothing.

## Can I search my old chats, or see spend and memory, over --host

**Yes. All three follow the machine the session runs on.** Spend reads that machine's priced
model calls, search asks its conversation index, and memory reads and writes its memory
store. `e` and `f` on a memory line therefore change the other machine's memory, not this
computer's. If the engine is an older build without one of these doors, the place keeps its
honest dim sentence instead of falling through to this computer's files.

## Can I put away a conversation on the other machine from home

Yes. `a` or `ctrl+e` writes the archive mark on the machine whose home you are viewing.
`enter` on a far conversation opens it in this window. `o open folder`, `c copy path`, and
starting a new conversation in that folder are absent on far rows because those paths do not
name folders on the computer holding your file manager and clipboard.

## Do the tab numbers follow the machine too

Yes, and the stamp behind them is kept per machine.

A tab's number is *what changed since you last looked at that place*, so it needs two facts:
what is in there now, which belongs to the machine the place describes, and when you last
looked, which belongs to the terminal you are sitting at. Those are two different machines
over a connection, so **the look stamps for a remote session are kept on this computer in a
folder of their own** — `~/.aforge/v3/looks/<machine>` — beside the local ones rather than in
them. Glancing at the server's tasks does not clear the number over your laptop's tasks tab,
and your laptop's own windows do not overwrite the origin a remote one measures from.

## Why doesn't home say folder gone over --host

Because the folder is on the other machine, and this one cannot see it.

A local home stats every project directory it is about to draw and marks the missing ones
`that folder is gone · <path>`. Over a connection those paths are the far machine's —
`/srv/code/api` is almost certainly not on your laptop — so a stat here would mark **every**
remote row as deleted. It is not made at all, and nothing is claimed: a row whose folder was
never asked about is not a row with a missing folder.
