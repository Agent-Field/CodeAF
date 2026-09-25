# Places

## What a place is, and the seven of them

A **place** is a full-screen room in codeaf that is not this conversation. There are seven,
and they are always in the same order — the four on the tab bar, then the three reached by
their command:

`home` · `sessions` · `spend` · `settings` · `standing` · `memory` · `search`

**The tab bar draws five words:** `home  chats  tasks  spend  settings`. `chats` is not a
room: it is the way back to your conversations (*The `chats` word on the bar* below). Standing, memory and search are
places all the same — `/standing`, `/memory`, `/search`, their digit, the map and the typed
box all reach them — and while you are standing in one its word is on the bar after the
four, so the bar always says where you are.

They are drawn as a **tab bar** on the second row of every place, under the top line — the
row of words at the top of the screen, `home  chats  tasks  spend  settings`, is this bar. It
is the same row as the conversation's tab strip: the same line, the first word in the same
column, the same air between two words, and the word you are in on the same ground as the
conversation in front, so moving between a place and a chat never moves the top of the screen. The
one you are standing in wears a filled band; the rest are dim. Nothing else on the surface
looks like that bar, so "which place am I in" is one glance.

Every place is drawn in the same frame:

1. the top line — this machine's own signs: what is on watch, what today has cost, the time
2. the tab bar — the four words, and the one you are in when it is not one of them
3. a dim rule
4. the place's own body
5. a rule, then the **composer** — one line you can type into, wherever you are
6. the hint line — what the keys do here

`esc` dismisses an editor or filter first, then returns to Home. The resting tasks,
spend and settings footers say `esc home`, including compact task screens. Places are not
stacked: opening one closes whichever was up. Further Escape presses stay on Home.

## The `chats` word on the bar: how do I get back to my conversation from a place

**`chats` is the second word on the bar, and it is the way back.** Click it, press `alt+2`
(`opt+2` on a Mac), or walk the bar's cursor onto it and press `enter`: the place closes and
the conversation that was in front before you opened it is in front again, with its draft
and its scroll where you left them. With no conversation open at all, it opens the new-chat
page instead, so the word never leads nowhere.

It is not a room. It never wears the filled band, `tab` and `shift+tab` step over it
(they walk the rooms), and it has no count. `esc` still goes to home, as on every place.
`alt+k` is a different key: it opens the chats switcher to choose *which* conversation, while
`chats` on the bar goes straight back to the one you were in.

## How to get to a place — the keyboard shortcut to jump between pages

Four ways, and they all reach the same seven rooms:

- **`tab`** — the next place **on the bar**, round again from the last. **`shift+tab`** —
  the one before. From standing, memory or search, `tab` goes on round the bar to home.
- **`alt+1`** … **`alt+8`** (**`opt+1`** … **`opt+8`** on a Mac), jump straight to one, **from a
  place or from a conversation**. `alt+1` … `alt+5` are the tab bar's own order (home,
  chats, tasks, spend, settings), and `alt+6`, `alt+7`, `alt+8` are the three places off the bar:
  standing, memory, search. Hold `alt` and press the digit. macOS draws the modifier as `opt`
  because that is the key's name on a Mac keycap; Linux and Windows draw it `alt+`, and it is the same
  chord either way. `tab` and the shift-arrows are not like them: in a conversation those
  already belong to path completion and to the caret, so the digits are the one class of
  place key that means the same thing wherever you are standing.
- **`ctrl+1`** … **`ctrl+8`**, the same jump, on the terminals that can send it. `ctrl` and
  a digit has no encoding in the scheme most terminals speak, so this is a second spelling and
  never the first: it works only where the terminal runs the kitty keyboard protocol and says
  so (kitty, ghostty, WezTerm, foot, Windows Terminal are the usual ones). `ctrl+.` draws the
  map there too. Where the terminal has said nothing, these do nothing and are never drawn —
  the map's own line names them exactly when they are live.
- **type its name** — on home, typing `sta` offers the standing place beside the
  conversations that match. A place ranks first, wears `▸`, and says `a place` out at the
  right margin. Home's list is a **drop-up** — it is read upward, out of the box you typed
  into — so ranking first means the offered place sits **below every conversation the same
  words matched**, nearest the message box. One `↑` selects it.
  Where the place can say what is behind it without going to the disk for it, the margin
  says that too: `a place · 6 orders, 1 fired today` on standing. A place that has nothing
  to count, or nothing in it, says `a place` alone.
- **a command** — `/home`, `/history`, `/standing`, `/memory`, `/settings`. Each opens the
  place it names.
- **click the word** — the tab bar itself is the control. A press on a place's word goes
  there; a press in the gap between two words does nothing, and a press on the word you are
  already standing on does nothing (going there would throw away what you have typed and
  the row you are on).

`alt+<digit>` arrives in every terminal codeaf runs in. `ctrl+<digit>` does not exist as a
thing a terminal can send, which is why the numbers are on `alt`.

**`tab` walks the whole circle, and no room on it is ever shut.** `tab` and `shift+tab`
step from one place to the next in the bar's order and round again from the last; there is
no state of the machine in which one of them is skipped. A place with nothing in it opens
and spends the frame saying what it is for, which is the answer somebody arriving at an
empty room actually wants — see **Every place opens, always** further down this page.

That is true of every door onto a place and not only of the walk: a number, a click on the
word, and the command that names it all open the same room on the same machine. `/history`
and a bare `/task` on a machine that has run nothing open the sessions place, headed `sessions`
over one line: `work you send off with /task lands here, and its record stays`.

## How do I move between the tabs with the arrow keys — the tab bar is a row the cursor can stand on

**Press `↑` from the first row of the page you are on.** The cursor leaves the list and
lands on the **tab bar** — the row of place words under the top line — and from there:

| Key | What it does on the bar |
| --- | --- |
| `←` `→` | walk one word along, wrapping round from either end. Nothing opens |
| `enter` | go into the place under the cursor |
| `↓` | the same as `enter` — go in |
| `esc` | back into the page, on the row you walked up from |
| `↑` | nothing. There is nothing above the bar but the top line, which is a reading rather than a control |
| `tab` `shift+tab` | the next and the previous place, exactly as everywhere else |
| `alt+1` … `alt+8` | jump straight to one, exactly as everywhere else |
| any printable key | goes to the composer, and the cursor comes back down into the page with it |

**The word your cursor is on wears the cursor's band**, in place of the mark the word you
are standing in usually wears. So while the cursor is up there the bar is saying "this is
where your cursor is" rather than "this is the room you are in" — and the page underneath
is still saying which room that is, on every one of its rows.

**Walking the bar opens nothing.** That is the whole difference between this and `tab`:
`tab` steps into each room as it passes, which closes the last one and throws away whatever
you had typed into it, so looking along the words used to cost one opening each. `←` and
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
previews the tab words; list rows instead share one selection with the keyboard. If the
keyboard cursor is standing on the bar as well you will see both marks at once: the band on
the word the cursor is on, the lifted ink on the word the pointer is on.

**The wheel over the bar walks the places**, one room a turn — `tab` and `shift+tab` under
the hand that is already there. It is one room a notch and not three, because each step
opens a room, and two rooms nobody asked to see is two filters thrown away.

**Clicking still opens.** A press on a word goes there; a press in the gap does nothing;
a press on the word you are already standing on does nothing.

## The mouse on a place — clicking a row, hovering, and the wheel

Three gestures, the same on all seven places:

- **List rows have one selection.** On home, tasks, standing, memory, spend and search,
  moving the mouse onto a row selects it. Keyboard navigation immediately takes over
  and clears the old mouse highlight. A parked pointer cannot reclaim the selection;
  move it again to switch back. Leaving the list keeps the latest selection. Settings
  retains its separate hover preview.
- **a click on a row opens it**, exactly as `enter` on it would: on standing, spend and
  search the first press puts the cursor there and opens what the row names. **A click
  never spends**: on memory, where `enter` on a line asks the model about it, the press
  opens the line's card instead, and on a shelf it folds the shelf. The verbs stay keys.
  Home keeps the same grammar — one click on a row or a fold opens it, and a panel's
  heading opens the place it names — and settings only chooses, because its `enter`
  changes a value.
- **the wheel walks the list**, three rows a turn, on every place. There is no separate
  scroll offset: the window follows the cursor, so scrolling and choosing are one gesture.
  Walking off the bottom with `↓` scrolls the same way.

`→` opens options for the selected row, and a letter such as `a` acts on that row.
Moving to another row closes the old options, so they cannot affect the previous row.
The chat switcher uses the same mouse/keyboard handoff.

The **tab bar** answers all three too, and it answers them as itself rather than as a row of
the list: the pointer lifts the word it is on, the wheel walks the places one room a turn,
and a click opens. Its own section is above.

## Typing on a place — only home starts things, how to start a task from any page, why is there no message box on tasks or spend, where did the box go

**Only home has a message box.** Type a sentence on home and `enter` starts a conversation
carrying it; `alt+enter` sends it off as a task instead (the composer layer, below). No
other place starts anything: there is no box under tasks, standing, memory, spend or
search, `enter` on those pages opens the row under the cursor and nothing else, and
`alt+enter` does nothing there. `tab` to home, or `alt+1`, when you want to start something
— its rule already says where the conversation will land and what it will run on.

There used to be a box at the foot of every place, reading `› say what you want done`,
whose `enter` "talked about it" in a new conversation and whose `alt+enter` sent a task. On
six of the seven places that box was a lie in a slot: `enter` opened rows, typing on tasks
went to the filter at the top of the list, and on spend the words went nowhere at all. It
is gone (2026-09-17).

**Typing still filters where a list is worth filtering.** On **sessions** every printable key
narrows the list, and the letters draw on the control row at the top of it beside the `⌕`
mark. On **search** the words you type are the query, drawn on the first row of the body the
same way, and `esc` clears them. On **memory** the head row echoes the filter in place of
`type to filter`. Spend and standing take no text.

**Escape backs out to Home from every place.** Filters clear first where present;
editors and nested views close before their parent page. Two spaces no longer navigate.

## The rule above home's box — where it lands, the model, thinking, approvals, what happened to the here ~/codeaf chip

The line over home's box is the same shape as the line over a conversation's own message
box:

```
─ glm-5.3-flash:auto · ◇ asks ─── project: ~/src/parser
› type to search or start something new
alt+p project · alt+e effort · alt+a approvals · alt+k chats · / commands
```

At the left it says **what model** answers, then a colon and **how hard it thinks**
(the rung or `auto`, without a badge), and **what it runs without asking** (`◇` and `asks`, `guardian`, `YOLO` or `refuses` — the same words the
approvals chip uses inside a conversation). The model is always bold and bright cyan,
on home and in conversations. At the far right, `project: <path>` names where the
next conversation opens; in a conversation it names that conversation's workspace.
Long paths truncate on the right, and the field disappears if there is no room. The bottom row names the
available project, effort and approval controls; the cells can also be pressed:

| cell | chord | or |
|---|---|---|
| the folder | `alt+p` walks to the next project | press the path |
| the model | `/model` opens the model list | press the name |
| the rung | `alt+e` walks auto → low → … → max → auto | press the cell |
| the gate | `alt+a` walks asks → guardian → YOLO → asks | press the cell |

Each one changes **the draft** — the next conversation you start from home — and says so
on the line under the box: `thinking · high · for the next conversation you start here`,
`approvals · YOLO · every tool runs without asking · dangerous commands still ask · for the
next conversation you start here`. The conversation behind home is not touched. The model is always bold and bright. A pinned
rung that differs from what this window would have used is drawn in the accent; an
open gate is painted in the warning hue, exactly as it is inside a conversation.

**Which pins last:** the project, model and rung last as long as this window does. The
project stays selected when a conversation starts and when you return home. Only the gate
is **spent** by the conversation that uses it — back to `asks` (or whatever the settings
rows say) — so an open gate is never quietly the default for the conversation after
the one you opened it for. With nothing pinned the rung and the gate are what a fresh conversation on this install
would run at: the `thinking` and `ask before running` rows in `/settings`, or `--yolo` if
this process was started with it.

**The `here ~/codeaf` chip is gone**, and so are the rules that the other places used to
draw over their boxes. The arrow and `new conversation in` lead are gone from home too;
the model starts the seam, and the project sits at the far right. A place with something to say about its page — `nothing matches`
on tasks when a filter emptied it, a receipt on memory, the "this session is on another
machine" line over `--host` — says it on its rule, where the box's rule would have been.

**Over `--host`, and on a session with no dial**, the rung and the gate are simply not on
home's rule — the folder and the model still are. The far machine's rows decide what a
conversation there runs without asking.

## Why did pressing alt+enter not send my task straight away

On home, the first `alt+enter` opens the task composer so you can check the project,
execution model and spending cap. Press `alt+enter` again to send it off. `enter`
talks about the sentence in a conversation instead; `esc` returns to home with the
sentence still in the box. Other places have no message box and do not start tasks.

## The composer layer — `alt+p` and `alt+o`, what does alt+p do, set which project, pick the model and set a spend limit before starting a task

Press **`alt+enter`** with something typed into home's box, and the layer opens. It is not
a new screen: **the page behind dims to the faintest tier instead of being covered**, the
box stays exactly where it was, and three lines appear in the air under it. It opens from
home alone — no other place has a box to send from.

```
› cut the opus spend in half without losing the sweep
 it will run on its own and tell you when it lands                                    a task
 · in ~/codeaf, on master                                                alt+p to move it
 · execution runs on opus 4.1                                                alt+o to change
 · it may spend up to $100.00 before it asks                                    type a number
 alt+enter send it off · enter talk about it first · esc back to home
```

Those three are the only facts a task needs before it leaves: **where, on what, how much.**
Each one is edited on the line that shows it.

- **`in ~/codeaf, on master`** — the project the task will work in, and the branch that
  tree is on right now; it is the folder home's rule names. **`alt+p`** cycles it through the
  projects codeaf knows, this window's own first, and round again from the last. A machine
  with one project has nowhere to move a task to, so the `alt+p to move it` clause is not on
  the line and the key does nothing. A folder that is not a repository, or one on a detached
  head, draws the project and stops there rather than trailing a comma.
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
goes back to home** with your sentence still in the box. Nothing was applied on the way in,
so `esc` has nothing to undo.

While the layer is up it has the whole keyboard. `tab` does not walk to the next place and
letters do not reach the box — the sentence is already written and is on the screen above
you.

## Where the task appears after you send it — the errand is drawn on home

The task you send from the layer is drawn on home's own column, which is the one surface
that draws an errand's answer, and you are already standing there: the layer only opens
from home. Its row says what the errand is doing until it lands, and the pane beside it
holds the exchange (*Asking from home* has the whole of it).

## The keys, and the one law behind them

**No key does anything that is not drawn on screen right now.**

That is the whole grammar, and it cuts both ways: a bare letter is never a verb, and a place
may not name a key it has not bound. Six classes, and a key belongs to exactly one:

| | |
| --- | --- |
| `↑` `↓` `enter` `esc` `tab` | move, open, back out, next place — and `↑` off the first row of the page moves onto the **tab bar**, which is a row the cursor can stand on (*How do I move between the tabs with the arrow keys*) |
| any printable key | goes to the composer, always |
| `alt+enter` | send what you typed off as a task |
| `alt+1` … `alt+8` (`opt+1` … `opt+8` on a Mac) | jump straight to a place |
| `ctrl+1` … `ctrl+8` | the same jump, only on terminals that report they can send it |
| `alt+<letter>` | change how THIS place is shown |
| `alt+p` `alt+o` | inside the composer layer only: move the task, change its model |
| `shift+←` `→` `↑` `↓` | move this place's time window |
| `→` then a letter | act on the row — letters are verbs only here |

`alt+o` changes a model only inside the task composer layer; on home use `/model`
or press the model name instead. `alt+p` also works on home, where it moves the draft
to the next project. Other places bind neither chord.

The last two classes are bound where there is something to bind. `alt+<letter>` today is
`alt+s` on the memory place, which changes which shelf it is showing; home's panels have no
second shape, so `alt+g` and `alt+q` do nothing there. `shift+<arrow>` is a place's time
window — `shift+←→` moves it by its own length, `shift+↑↓` changes how coarse it is — and
three places have one: **sessions** (when it ran), **standing** (when it fired) and **spend**
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
verbs are the row's own — a question's first two answers on its own answer keys, `x close`, `n new in project`, `o open folder`, `p copy project`, and
`p pause it` or `r resume it` on a standing item, `s stop` on a task this window runs,
`its chats` and `open folder` on a project. On home, `→` opens the selected row's
options at every width; the arrows stay in the list.

While those options are drawn, **those letters are the verbs** and the composer is asleep.
On wide home layouts they appear below the selected description in the middle column,
without moving the list. Elsewhere the strip pushes the rows below it down; the frame
stays the same height and the composer does not move.

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

**A foot that is a sentence rather than a key list is cut the same way.** A sentence like

```
 open in another window — enter again to move it here (that window's reply stops there; its tasks resume here)
```

has no `·` in it at all. The bracketed gloss goes first, because it explains a clause that is
still on the line; then the clause hanging off the dash; and what a 40-column terminal is
left with is the statement, `open in another window`. It used to read
`open in another window — enter again to move it here (it …`, which named a key and then ate
it. That particular sentence is gone — moving a conversation from a window with no engine asks first on home's foot line now,
see *home* — but the rule it taught is what every foot on this surface is cut by.

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
- **A note** — the dim words written into the rule over the composer, saying what just
  happened or what the page holds — has the answer at
  the **front**, so the front is protected and the line gives up its later clauses. Settings
  says `saved to your profile · a project's own .codeaf/config.json is a hand edit`; at
  sixty columns that becomes `saved to your profile`, which is the half you asked for. The
  macOS chord note behaves the same way: `your terminal sends opt as a letter — turn on "use
  option as meta" in Terminal: Profiles › Keyboard` shortens to
  `your terminal sends opt as a letter`.

Neither of them ever ends in `…`, and neither ever cuts inside a word.

## alt+. — see all the keyboard shortcuts at once, on the page you are on

Press `alt+.` and the whole key map appears **in the cells you were already reading**:

- the tab bar's words grow their numbers, `1 home`, `2 chats`, `3 tasks`, `4 spend`,
  `5 settings`, and the three places off the bar are drawn after them with theirs:
  `6 standing`, `7 memory`, `8 search`
- the hint line becomes the chord list

`?` over an empty box draws the same map, which is what that key means on a place — show me
the keys for where I am standing. In a conversation the same key opens the `/help` sheet.

**The chord list is built from the place you are on.** It reads
`alt+1…8 go to a place · alt+enter send it off as a task · → show what this row can do · esc close`
— and the `→` clause is left out on a place whose rows have no verbs, such as search, rather
than naming a key that would open nothing there.

Nothing moves, nothing pops up, and the next key you press takes it away and then does what
it was always going to do. `esc` just takes it away.

It is a chord rather than a hold because a terminal cannot tell a program that a modifier is
being held down — it only reports what arrived.

## home — conversations, tasks and project panels

The first place, and the one codeaf opens on. Everything on this machine, from every
project, in one, two or three columns — one `sessions` list of the fifteen most recent
conversations, then question rows, `projects`, `since you left`, `spend`, and `scheduled`.
Open tabs and saved history share that list, with closed conversations dimmed. Which
column a panel stands in follows what it holds: every panel with rows is in the **field** at
the left, and the **rail** at the right holds `projects` and `spend` at its top and, under
them, whichever panels are quiet today. An empty panel keeps its heading and one dim line
naming what arrives there.

Its own keys are in the **Home** page: `↑↓` walk a panel, `←→` cross columns, a digit
answers the question row drawing its answers, and `enter` opens a row. `tab` is the way to the
next place, and the errand pane is taken into with `→` rather than `tab`. Home has no
`alt+<letter>` keys — `alt+g` and `alt+q` are unbound there.

Clicking Home’s `since you left` heading opens memory. Questions appear as amber `?`
bullets on the conversation or task, with no separate `needs you` heading.

## tasks — the tasks page, and how to get to it without a command

The full-screen conversation tree groups every chat and its tasks into **running** and
**completed**. A conversation stays under running while it is answering or has running,
queued, waiting or unanswered work. Once that work settles, its whole tree moves to
completed; new work moves it back. Each task retains its own state word.

Both sections default to newest activity first; click the age heading to reverse. Conversation titles match Home, and the same
bullets mark answering, unread and unanswered states. Every conversation and nested task
starts expanded, with connecting tree lines and fold arrows immediately after titles in the left column. Projects
have their own column. You can fold a branch yourself. `/history`, `ctrl+.` and `alt+3` open it.

**Typing here narrows the list.** While this place is up every printable key goes to its
filter — the one exception being `1` and `2` over a row the record pane beside the list is
offering those two answers for, which answer it — and **the box at the foot says so itself** — it rests on `› type to filter this list`
rather than home's `› type to search or start something new`. It used to show the
shared prompt with the correction two rows further down on the foot, which meant the loudest
row on the screen was inviting a message the page cannot send. `enter` opens a task's room
when this conversation is holding it, and goes inside its record card otherwise. `→` opens
the row's options: `x close`, `n new in project`, `o open folder`, and
`p copy project` where the local conversation and project are available. A task this
conversation is holding that is still queued or running also offers `s stop it`. Everything starts expanded; the list scrolls and its
tail fades. The rule under the list is a bare line — the counts are on the section headings
the list already draws, and it says `nothing matches` only when your filter has emptied the
page — and the foot names only what is true of the row you are on: `enter open its room ·
→ verbs: stop it`.

## Close or put away a task, find an archived task, or reopen it

On home or the Sessions list, select the task, press `→`, then `x close`.
This hides only that task from home's panels and the unfiltered Sessions list. Its work
continues if it is running; its record, conversation, and other tasks are unchanged.
The choice is saved with the conversation and survives reopening the app.

To recover it, type its name in the Tasks filter. Search includes put-away tasks within
the selected time window; expand that window if the task is older. Select the matching
task and use `→`, then `x reopen`. `enter` can still open its record.

`n new in project`, `o open folder`, and `p copy project` use the project of the conversation
that owns the selected task. A new chat is independent of the task. These folder actions
and per-task put-away are local capabilities; a connected remote window does not offer
them. Its existing stop action remains available when that engine supports it.

## standing — what runs without being asked, and where to type on the standing page

The orders that fire on their own, on four shelves each under its own heading: this
conversation's, this project's, the machine's, and then `in other projects` — everything
else standing on this computer that does not reach the conversation you are in.
`/standing` and `/orders` open it, and so does `alt+6`. It is not on the tab bar.

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

What codeaf holds true about you and this machine, with what kind of thing each line is, how
it has done, `helped 19 · bore on 3`, and how old it is out at the right. `/memory` and `/memories` open it, and so does `alt+7`. It is not on the tab bar.

The page is **shelves** — you, this project, this machine — biggest first, with the biggest
one open and the rest rolled up. Type to filter what is already on the page, **every letter
including `u`**; the store is read when you walk in and on the place beat, never on a
keystroke. `enter` opens or closes a shelf; `enter` on a LINE is `ask me about it` — the
line goes into a fresh conversation as its opening message and you are taken there. A shelf
shows three lines and five shelves show at once; `enter` on `▸ 37 more, on this shelf` or
`▸ 2 more, shelves` draws the rest where they stand, and the same line, now `▾ 37 fewer`,
puts them back. `alt+s` walks the shelves one at a time. `→` opens the verbs on a line — `c open the card`, which is
where the full text and where it was learned live, `e fix the wording`, `f forget it`, and
`u put it back` while there is something to put back.

The foot follows the row: `enter ask me about it · e fix the wording · f forget it` on a
line, `enter open a shelf · type to filter · alt+s walk the shelves` on a shelf heading.
**On a machine that has remembered nothing there are no shelves, so none of those keys is
named** — the foot is `tab next place · esc` and nothing else, and the body is the heading
`memory` over one line saying what to do instead: `what it has learned about you and this
machine · /remember adds a line`.

`delete` also forgets the line under the cursor.

## spend — what it cost, and why it counts another session or another machine

What this machine has cost, by the day, by the model, and by what it was for. `alt+4` opens
it. It reads one machine-wide ledger — a line per model call — so the figures are the bill
and not an estimate.

**It is the whole machine and not this session**, which is why a figure here can be larger
than anything this conversation did: every window, every task and every standing run on
this machine writes into that one ledger, including a session opened from another machine
over `--host` whose calls are still made here. For this session alone, ask `/cost`.

The window with its sparkline, and then **one cut of the ledger**: `by topic` — what the
money was for — or `by model`, which is the same money added up the other way. The heading
is the control that swaps them: walk the cursor onto it and it wears arrows, `← by topic →`,
and `←`, `→` or `enter` step between the two. It opens on `by topic`. Standing orders get a
heading of their own under that cut, because a promise is one of the things money was for.

`enter` on a row of `by topic` opens the thing the money went on — a task opens its own
record card, a standing promise opens the standing place on that order, and a conversation
opens where you left it. A thing the record no longer holds says so and stays put. The
dearest twenty are shown; `enter` or a click on `▸ 11 more` draws the rest, and `▾ 11 fewer`
folds them back. The cursor arrives on the first of them — the biggest thing the money went
on. `shift+←` and `shift+→` move the window by its own length; `shift+↑` and `shift+↓`
change how coarse it is.

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
`enter opens what spent it · → the limits · shift+←→ move the days · tab next place · esc home`.
Where the head row is too narrow to draw its own arrows the window clause is dropped, and
over an empty ledger only the way out is named.

On a machine that has spent nothing the place is its heading `spend` over one line:
`every chat and task is priced here as it runs`. A window paged onto a quiet fortnight is a
different thing — its head row stays, with the arrows that page it back.

## search — finding anything said or run

Everything that has been said on this machine. `alt+8` opens it, it is not on the tab
bar — and typing searches: the
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
came from. Twelve conversations are shown and the rest fold into one line, `▸ 38 more`;
`enter` or a click on it draws them all, and `▾ 38 fewer` folds them back. A new search
starts folded.

**`/search` is the typed door onto this place**, beside `alt+8` and the map. It
takes no argument: the place is a box, and typing in it searches.

With nothing typed the place is its heading `search` over one line saying what to do:
`type a word · every conversation on this machine is searched`.

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
`/config` open it, and so does `alt+5`.

It has a **second bar** under the place bar: its own sections. Those two bars are not a
repetition — the upper one is the places, the lower one is settings' own pages. `←` and
`→` move between sections. `tab` does **not**: it is the way to the next place, here as
everywhere.

## Why the tab bar looks squashed on a narrow terminal — the places at 60 columns

**All five words are on the bar at 60 columns, and down to 42.** The five words with the
padding each chip carries and the air between them are 50 cells; under that the bar gives up
the *air between the chips*, a cell at a time, before it gives up a word, so a 42-column pane
draws

```
   home  chats  sessions  spend  settings
```

with two cells between the words instead of four. Nothing else changes: the band under the
word you are standing in, the counts, `tab`, `←` `→` and `alt+1`…`alt+8` all mean exactly
what they mean on a wide screen.

**Under 42 columns the bar carries what it can and counts the rest.** It keeps the place
you are standing in, the word the cursor is on, and any place wearing a count, then fills in
the bar's own order until the row is full and ends with a dim `▸ 2` — the number of the
bar's words that are not on the row (standing, memory and search are never counted: they
are not the bar's to give up):

```
   home  chats  sessions  ▸ 2
```

`▸ 3 more` where there are cells for the longer spelling, `▸ 3` where there are not — the
same fold mark every other list on this surface puts over the rows it is not drawing, so a
count on the bar reads as the same idea as `▸ 11 more` at the foot of the spend place. The word gives
way before the mark does: `▸` is what says there is more behind the row. **The
count is a sign and not a button** — pressing it does nothing, because it stands for several
places at once and no single one of them is the answer.

**The key that reaches them is `tab`**, and it is named on the foot of every place —
`… · tab next place`, which is the last clause a narrow foot gives up. `alt+1`…`alt+8`
still go straight to a place whether or not its word is on the row, and the numbers never
move: the five on the bar are `alt+1`…`alt+5`, then standing, memory and search are
`alt+6`…`alt+8`, whatever happens to be drawn.

## Why a nearly-empty place says what it is for — why is the tasks page empty

You only ever arrive at a place on purpose — from the tab bar, from a number, or by typing
the word. That arrival is the one moment somebody is asking "what is this", so a place with
nothing of its own to draw answers with **its heading and one dim line under it** — the line
names what arrives there and the one thing that puts it there, the way home's empty panels
do. It never says the place is empty.

**Every place opens, always.** There is no state of the machine in which a word on the bar,
or any of the seven digits, is a key that does nothing. On a machine codeaf was installed on
an hour ago, `alt+3`, `alt+6` and `alt+7` all open:

- **sessions**, headed `sessions`:
  `work you send off with /task lands here, and its record stays`
- **standing**, headed `standing orders`:
  `reminders, watches and routines · "remind me at 6" or "every morning at 9"`
  No shelf and no time window is drawn under it.
- **memory**, headed `memory`:
  `what it has learned about you and this machine · /remember adds a line`
  If this build is not remembering anything at all, the rule under the page also says
  `memory is off for this session · turn it on under /settings`.
- **spend**, headed `spend`: `every chat and task is priced here as it runs`
- **search** with an empty box, headed `search`:
  `type a word · every conversation on this machine is searched`

On a narrow window the line wraps onto a second or third dim line under the first; it is
never cut, and never ends in `…`. The moment the first thing arrives the line goes and the list begins under
the same heading; nothing above it moves.
- **spend**, **search** and **memory** over `--host` each say one dim line where their rows
  would be — see *The places over --host* below for the exact words and why three of the
  seven still say them.

There is no "coming soon", no greyed-out list and no empty table with headings over it. A
page that draws the furniture of a feature it does not have looks like a bug rather than like
a plan.

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
session runs on.** Home lists the conversations under `~/.codeaf/v3`; tasks lists the work
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
| **sessions** | the far machine's work, out of the same reading |
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

## Why is home empty over ssh when I connect to another machine — Escape over --host

**It is not empty any more, and this is the answer if you have seen it be.**

`space` `space` over `--host` opens the home of the machine your session runs on: its
projects, its conversations, and what each of those ran. `enter` on a row opens that
conversation beside the one you are in — the engine gives it a connection of its own and
the chat you came from keeps running, the same door `codeaf resume` uses locally.

It used to draw **one dim line** where the rows would be —
`home shows this machine's projects, and this session is on another` — because the projects
it could reach were the laptop's while the work was on the server. Before that it refused to
open at all. If you press Escape over a connection and get one line, the machine you are
attached to is running an older codeaf than the one you are sitting at, and the fix is the
same as for any version mismatch: update the older one.

In the fraction of a second before the far machine's first answer arrives, home draws **no
rows and no sentence at all**. `nothing here yet` over a server full of work would be the one
wrong thing this screen can say about somebody else's disk, so nothing is said until there is
something true to say.

## Does the tasks page show the other machine's work over --host

**Yes — the far machine's work, and none of this one's.**

The sessions place reads its rows out of the same reading home lists, so the door that carried
home carried this too. `/history`, `ctrl+.` and the tabs all open the same page.

This was the worst of the seven before it crossed. The page walked *this* computer's
`~/.codeaf/v3` and drew what it found — a count and a total in dollars, `work codeaf ran on
its own. 8, $22.54 of it.` — under a conversation on a server that had run none of it. A page
that reads a real disk and names the wrong machine is worse than a page that says nothing.

## Can I search my old chats, or see spend and memory, over --host

**Yes. All three follow the machine the session runs on.** Spend reads that machine's priced
model calls, search asks its conversation index, and memory reads and writes its memory
store. `e` and `f` on a memory line therefore change the other machine's memory, not this
computer's. If the engine is an older build without one of these doors, the place keeps its
honest dim sentence instead of falling through to this computer's files.

## Can I put away a conversation on the other machine from home

Yes. `x close` on the row menu or `ctrl+e` writes the archive mark on the machine whose home you are viewing.
`enter` on a far conversation opens it in this window. `o open folder`, `p copy project`, and
starting a new conversation in that folder are absent on far rows because those paths do not
name folders on the computer holding your file manager and clipboard.

## Do the tab numbers follow the machine too

Yes, and the stamp behind them is kept per machine.

A tab's number is *what changed since you last looked at that place*, so it needs two facts:
what is in there now, which belongs to the machine the place describes, and when you last
looked, which belongs to the terminal you are sitting at. Those are two different machines
over a connection, so **the look stamps for a remote session are kept on this computer in a
folder of their own** — `~/.codeaf/v3/looks/<machine>` — beside the local ones rather than in
them. Glancing at the server's tasks does not clear the number over your laptop's sessions tab,
and your laptop's own windows do not overwrite the origin a remote one measures from.

## Why doesn't home say folder gone over --host

Because the folder is on the other machine, and this one cannot see it.

A local home stats every project directory it is about to draw and marks the missing ones
`that folder is gone · <path>`. Over a connection those paths are the far machine's —
`/srv/code/api` is almost certainly not on your laptop — so a stat here would mark **every**
remote row as deleted. It is not made at all, and nothing is claimed: a row whose folder was
never asked about is not a row with a missing folder.

## What does alt+w do now — folder preview, project selection moved to alt+p

`alt+w` (`opt+w` on a Mac) hides or shows the preview in the folder browser. When
walking the conversation's task roster it still widens that roster. Project selection
on home and in the task composer now uses `alt+p` (`opt+p`), and clicking home's
project path takes the same step through the same list. The selected project remains
set when you start a conversation and return home.

## Where is the model filter in Settings

When choosing a model for a setting, type to filter. The filter text appears above
the model list, and Enter applies the selected model.

## Where is the model filter in Settings

When choosing a model for a setting, type to filter. The filter text appears above
the model list, and Enter applies the selected model.

## Where the Settings search cursor appears

Settings draws its search field above the rows. The cursor follows your query there; model pickers use their own visible filter, and a connection key entry keeps its cursor inside the key field. The bottom row remains the navigation hints.
