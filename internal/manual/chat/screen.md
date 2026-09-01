# What is on the screen

## Why is there a line next to my task

After you send `/task <brief>`, the transcript tail temporarily shows one dim forming
block. Each row begins with the thin `▏ ` hairline: the word `task`, your own brief
verbatim in quotes, and the live `sizing it up…` or `shaping the brief…` spinner and
clock. Long briefs are fitted to about two rows. The phase changes inside that same
block. As soon as the task starts, the whole scaffold and hairline collapse into the
ordinary started-task row; if it fails, they collapse into the error line instead. No
forming block is drawn when a task command is not in flight.

## What the frame draws, top to bottom

aforge draws one screen in a fixed order every frame. From the top: the top bar, the
conversation, a breathing gap, the rule with the legend in it, the approval question, the
connect offer, the sub-harness offer, the steer guard, the follow-up row, any message waiting for
the answer to finish, another gap, the tray row above the box, the draft box where you type,
any open list (picker, menu, completion), and the status line last.

**The tray row** carries what the next message takes with it besides its words —
a picked sub-harness, an attached picture, an attached file — and, at its right end, the
**thinking chip** (`⠿ high`), which names how hard the model will think about your next
turn. `ctrl+v` walks that rung and clicking the chip opens the five-rung ladder; see
*The thinking chip above the message box* on the keys page. The tray is drawn only when
it has something on it: with nothing attached and thinking set to `off`, the row is not
there and the box sits straight under the gap.

Beside the conversation, on the right, the column — the right-hand bar, sidebar, task
panel, whatever you call it — takes 30 columns (24 on a narrower frame)
from the session's first keystroke, before any tasks exist. An untouched empty
conversation opens without it: no column, no doors, no rule, no telemetry, just the
centred greeting with the message box inside it (see *The empty screen* page); the
column stands the moment you type, or at once if a standing order or a task is already
here. It carries `tasks`, the
roster of work, and `standing`, the orders standing over this conversation. A section's
dim lowercase label appears only when that section has rows. Work fills the column
rather than raising it, and the work it fills with is **this conversation's alone**.
An empty column keeps only its typeable `+ /task` and `+ /standing` doors; where
the project has a record from earlier sessions, one dim line at the foot of the column
reads `ctrl+. earlier` and opens the task page. `ctrl+g` closes it and opens it again, remembered between
sessions, and the column's own last line says so: `❯ ctrl+g hide`. With it closed the
conversation is laid out at the full width of the terminal, and the legend's hint slot
reads `ctrl+g tasks` once the session has tasks to come back to.

**Seven places take the whole frame instead of sharing it**, at every width: home, tasks,
standing, memory, spend, search and settings. `tab` walks between them, `alt+1` … `alt+7`
(`⌥1` … `⌥7` on a Mac) jump straight to one from wherever you are standing — a place or a
conversation — and each
has commands of its own (`/home`, `/history`, `/standing`,
`/memory`, `/settings`). The rewind timeline (`/rewind`) takes the frame the same way and is
deliberately not one of the seven — it is something you do to this conversation rather than
a room in the machine.

While any of them is up nothing else is drawn — no conversation, no box, no status line —
and `esc` gives the frame back. **Only one is ever up:** opening any one closes the rest.

Every place is drawn in one frame, top to bottom: the machine's own top line, the tab bar
naming the seven, a dim rule, the place's body, a rule, the place's own count or note, the
**composer** with its scope chip (`here ~/aforge-v2`) at the right of the box row, and the
hint line last. See the **Places** page.

The status line is the last row of the frame, not the first. It sits at the bottom so
you read it in the same glance as the box above it.

## The bar at the top — where you are, and what is answering

One row at the top of the screen, above the conversation, with a hairline rule under it,
in every conversation and every task room alike. It carries the **slow facts** — things
true of the whole conversation that change only when you deliberately change them — and
it carries them in the same two clusters the status line uses: **where you are on the
left**, **what is answering on the right**.

The left cluster is a **breadcrumb**. In a conversation it reads:

```
· aforge-v2 › fix the parser
```

A two-cell lead glyph column first — the plain `·` at rest — then the project, then the
conversation's own name, joined by ` › `. **Parents are dim; the step you are standing in
is the last one and it is drawn bold, in its own hue** — ink in a conversation, the accent
in a task room. It is bold rather than a fourth colour because a crumb is read by finding
the end of it, and weight is spent on exactly three things across this surface, of which
where-you-are is the first.

**The conversation's step is there only when the conversation has a name of its own.** An
untitled one, and one whose name is simply the folder it stands in, would put the same
word on the crumb twice — so those draw the project step alone.

In a task room the crumb grows the parent tasks and then this task's title, with its
roster number riding dim after it, `#2`; the lead glyph becomes the task's state glyph,
the same mark the roster paints:

```
⠙ aforge-v2 › fix the parser › port the lexer #2 · working · 2m12s · $0.04   esc/← back · ✕
```

After the crumb, in a task only, come the room's own facts — the state word, the clock,
the spend — each dropped when nobody has published it, and the whole left cluster wears
the accent while the room is open. While a consent question is up the cluster drops to
dim, so nothing on the bar competes with the decision.

## The top bar's right cluster — the model, the branch, the host and YOLO

The right end of the bar reads `glm-5.3:high · main* · devbox · YOLO`, and in a task room
it gains ` · esc/← back · ✕` after that:

- **the model**, with its effort as a `:high` rider — the same basename the picker uses.
  In a room it is the task's own model, spelled `task glm-4.6`.
- **the branch**, with a `*` when the tree is dirty. There is no branch on a session
  running over `--host`: the git probe would read *this* machine's repository at the
  other one's path, so nothing is shown rather than something possibly wrong.
- **the host**, only on a `--host` session, beside its latency once a measurement has
  answered. A reconnect in progress outranks everything else in the cluster and may evict
  all of it.
- **`YOLO`**, only while the approval gate is set to `allow`, painted as the warning it
  is.

**The whole right cluster rests dim.** It is the quiet "about" voice, and that includes
the model's name even though pressing it opens a picker: pressability on this surface is
said by the pointer, not by loudness. Two things there are loud, and both are loud because
of what they **mean** rather than what they do — `YOLO` in the bad hue, and a reconnect
sentence in the accent.

Nothing that ticks ever goes up there — no cost, no context percentage, no counts, no
rates. Those live at the bottom, and a change in the top bar is a navigation event: the
bar doubling as confirmation that the navigation happened.

## Pressing the top bar — its doors, and what each one does

Every door on the bar lights **at the size of its own cells**, never as a band across the
row: the bar is one row, and a band would offer to go home over the cells that stop the
work. Under the pointer a door steps up one — to the accent in a conversation, to ink
inside a room, where the cluster is already accent. `YOLO` is the one exception: it keeps
its red and takes a background band round its four cells instead, because lifting it would
paint an open gate in the hue of something safe.

- **the project step** → home, the same thing `space space` does.
- **the conversation's step**, in a room → back to the conversation, scroll restored,
  **however deep the trail is**.
- **the model** → the model picker. Inside a room the pick lands on **that task**, not the
  conversation. Where the pick could not land — a task that has finished, failed, been
  stopped or needs your look, one that has not started, an adaptive run's page, a node
  inside a run — the name is drawn, records no cells at all, and simply does not react.
  `/model` is the keyboard path onto the same picker.
- **`YOLO`** → `/settings` on the **Safety** tab, because seeing the gate open must offer
  the way to close it.
- **`esc/← back`** → **exactly one crumb level up**, which is what `esc` and `←` do. From a
  sub-task that is the parent task's room, not the conversation. It is *not* a wider target
  for the crumb's conversation step: one level down the two land in the same place and two
  levels down they do not, and they light separately.
- **`✕`** → the card that asks to stop the work.

The step you are standing in is not a door — you are already there, and a door to nowhere
does not light. Neither are the branch, the host, the clock, the spend or the state word:
they are facts.

## What the top bar gives way to when the frame is narrow

In one order, and the crumb is the last thing on the left to yield:

1. **the task's trimmings**, in this order: the spend, then the clock, then the state
   word, then the lead glyph.
2. **the crumb's own ladder** — the middle folds to `…`, then the project goes, then the
   parent goes, and only then is the current title cut, to 12 columns. The step you are on
   is never dropped.
3. **the right cluster** — the host's latency detail first (the machine's name survives),
   then the branch, then the `:effort` rider, then the model, then the back word.

**`YOLO` and a reconnect sentence are never dropped**, at any width the bar is drawn at,
and the crumb yields before either of them: if both clusters still will not fit, the last
resort truncates the crumb and never the right cluster. The `✕` outlives the back word.

On a short frame the top bar is the first chrome to go — the transcript wins, and the
bottom two rows are the last. Below **60 columns** it is not drawn at all, and the phone
deck's top row carries the crumb instead (see *Does this work on my phone?* below).

Under the empty screen's greeting the bar is **the crumb alone**: no right cluster, and no
doors on it.

## Where is the task strip — the row of task chips above the transcript is gone

**It does not exist any more, and nothing replaces it.** aforge used to draw a strip of
chips across the top of the frame, one per running task, above the conversation. It said
only what the roster already said and it cost a row on every frame, so it was removed.

What answers the same questions now:

- **which page you are on** — the top bar's crumb, and its lead glyph, which carries the
  state of the task whose room you are standing in;
- **what is running** — the roster: the column on the right when it is open, and `ctrl+t`
  or `ctrl+g` to raise it when it is not;
- **at phone width** — the deck's two rows, whose row 1 is the crumb and whose row 2 ends
  with `⏺ N running` and the state word.

**The room's own pinned header is gone with it.** A task's page used to pin a rule at the
top carrying the task's trail, state, clock and spend, with dim `part of:` and `spawned:`
lines under it. Those facts are the top bar's task form now, and the parent is a step of
the crumb; nothing is drawn over the top of a task's transcript.

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

The same task is marked twice more while you are in it: its row in the roster wears
the same tint, and the top bar's lead glyph carries its state.

The rule above the input is the only horizontal line this surface draws. There are no
borders anywhere else. The draft box is inset one cell.

If the frame is taller than your terminal, rows are lost from the **top**, never from
the bottom. The chrome is the tail, and the tail survives.

## Blank rows, and the gap above the message box

Four rules decide every blank line in the conversation. One blank before a tool cluster
that follows text; none between the lines of a cluster; one blank after a cluster; one
blank before each of your own messages. A cluster that opens a turn — one that answers
your message directly — gets no gap of its own. A gap asked for twice is still one gap.

One thing only is ever drawn at the very top of the conversation: the dim
`· earlier · keep scrolling` marker, and only while a resumed conversation still has
older messages you have not scrolled back into yet. Nothing else goes there.

The resting whitespace between the conversation and the draft box is a **height**
ladder, not a width one:

| terminal height | breathing rows |
| --- | --- |
| 16 rows or more | 2 |
| 6 to 15 rows | 1 |
| under 6 rows | 0 |

Below 6 rows the rule, the gap and the top bar all go, leaving the conversation, the box
and the status line. The ladder steps down, never up.

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

## Scrolling up to older messages, and seeing the start of a conversation you came back to

Three ways up, and all three go through the same machinery:

- **`pgup` / `pgdown`** move a screenful at a time. These always work, whatever is in
  your message box, so they are the ones to reach for while you are part-way through
  writing something.
- **`↑` / `↓`** move one row — but only once the box is empty and there is no tool row
  left to select. With a sentence in the box, `↑` walks your own history instead. That
  is why `pgup` is the reliable one.
- **The mouse wheel**, if the pointer is switched on for aforge (`ui.mouse`). With it
  off, the wheel does nothing here — aforge runs on the alternate screen, so your
  terminal's own scrollback holds nothing to scroll.

**A conversation you came back to can be scrolled all the way to its first message.**
Reopening one draws its last **40** blocks so the first frame is fast rather than
re-rendering an hour of work you may not want. That is a starting position, not a
ceiling: when a scroll runs off the top of what is drawn, aforge reads the previous 40
out of the session file and puts them **above** what you are reading. The line under
your eye does not move; you simply carry on scrolling into it. Repeat and you reach the
first thing you ever said in that conversation.

**Scrolling runs straight through a compaction.** A long conversation gets shortened for
the model along the way — old tool results become one-line pointers, long runs of the
model's own work become a single line — and scrolling up used to read that shortened copy
back to you. It no longer does. The session file kept every original line, so above the
boundary you are handed the conversation **in the words it was said in**, and one dim line
is drawn where the two meet (below).

This works on a session compacted by **this** version of aforge or later. A conversation
compacted by an older one is drawn from the shortened copy exactly as it always was, and
picks the fuller history up the next time it compacts. Nothing is lost either way — the
file has always held it all.

While there is still more above you, the top row of the conversation reads:

```
· earlier · keep scrolling
```

dim, on its own line. When you reach the real beginning it is not drawn at all — so a
top row with no marker over it *is* the start of the conversation.

`/rewind` is the other way at the same thing, and it needs no scrolling: it opens the
whole conversation as a list, oldest first (see the sessions and rewind page).

## The dim line that says the model keeps a shortened record — reading what was summarized away

Scroll far enough up a long conversation and one dim line appears in the middle of it:

```
· above here the model keeps a shortened record — you can still read it all
```

That is the point where aforge shortened the conversation to keep it inside the model's
context. It stays where it happened, so you scroll past it and carry on reading upward.

Both halves of the line are true and neither one covers for the other:

- **You can read all of it.** Everything above the line is drawn from the session file, in
  the words it was said in — your messages, the replies, the tool calls and their whole
  output. Nothing was thrown away.
- **The model does not.** Above that line the model is working from a shortened version:
  old tool results became one-line pointers to the files that hold them, and long runs of
  its own earlier work became a single line saying how much went. So if you ask about
  something above the line, it may answer from something shorter than what you are looking
  at — ask it to `read` the file, or paste the part you mean back in.

**The shortened copy is never drawn.** aforge holds the same conversation twice above that
line — the original, and the version the model kept — and it always shows you the original.
So the seam is a statement about the model's memory, never about how much of your
conversation is on the screen.

The line is aforge talking, not part of the conversation: a rewind cannot cut it, an
export does not carry it, and it is drawn fresh each time you scroll back into it. A
conversation short enough never to have been shortened never shows one, and neither does
one compacted by a version of aforge older than this line.

If a compaction happens **while** you are reading, you see its own row instead — the
`compacted · …` line — and no second line beside it. The history above stays reachable.

## It jumps to the bottom when I scroll — does new output pull me back down?

No. A scroll away from the live edge is yours and it stays.

- A reply **streaming in** does not move you. Lines land below the frame and the
  conversation you are reading stays exactly where it is.
- Work **landing** while you read — a task finishing, a note, a tool call — does not
  move you either.
- **Typing** into the message box does not move you. Nor does deleting, pasting, or
  attaching a picture.

The one dim chip at the bottom right, `↓ latest · ctrl+l`, is the whole of aforge's
answer to being scrolled away: it offers the way back rather than taking it. Pressing
`ctrl+l`, clicking the chip, or scrolling down to the bottom yourself re-arms following,
and from then on new output keeps you at the edge again.

Three things do deliberately put you back at the bottom, because in each you asked for
it: sending a message, queueing one with `ctrl+q`, and leaving copy mode.

## The line above the message box (the legend)

The rule that separates the conversation from your own business carries the keys that work
now, like the legend on a fieldset:

```
─────────────────────────────────────────────── / commands ─
```

**Its left end is empty at rest.** The branch and the host used to sit there and are the
top bar's now, where the slow facts live — a legend that repeated them would be the frame
saying everything twice. Nothing takes their place: no name, no path, never "untitled".

The one thing that does appear there is the room's own word while a task room is open —
`room · esc/← back`, or `room · esc your line back` while a history walk is on, because in
those few keystrokes `esc` puts your own draft back before it leaves.

The right is a hint slot. It names the keys that work right now when a state has keys of
its own — for example `y allow · n deny · a always` while a question is up,
`esc interrupt` while a turn is running,
`enter steers it in · cmd+enter waits · shift+enter stops and sends` while a turn is
running and you have typed something, `esc stops and sends` while a message of yours is
waiting for the answer to finish, or `↑↓ · enter · esc` while a list is open.

It only ever names a key that **works right now**, and that includes the terminal: the
`shift+enter` and `cmd+enter` clauses are not drawn on a terminal that cannot tell those
chords apart from a plain `enter`, because a hint for a key that could never arrive would
be the surface lying to you — there the line keeps only `enter steers it in`. See the keys page,
"Interrupt and say something new in one key" and "Send a message into the running
answer".

**The key itself is drawn apart from the word beside it.** In `esc interrupt`, `esc`
wears the soft cyan every highlighted fact wears and `interrupt` stays at the border's
own dim — the thing you press reads at a glance and the explanation of it does not
compete. It is the same in every hint the slot carries, in home's foot hint, in the keys
legend at the bottom of a conversation's card on home, and on the task record's foot. See "Why is one word in a line brighter than the rest"
below.

**At rest it carries the doors out of the conversation** — every one that would actually
act, in the order you meet them. With an empty box it reads:

```
space space home · tab last · ctrl+k switch · / commands
```

Pressing the space bar twice on an empty box opens the home screen, and clicking those
words does the same; `/` opens the command list. `tab last` appears once there is a
conversation to flick back to, `ctrl+k switch` whenever the switcher would act. It is
there on a fresh machine from the first minute — an empty home is still a home — and over
`--host` too, where it opens the far machine's home. The whole slot gives way the moment
you type or a state above claims it. It costs no row either way — this line is on the
frame regardless.

**And the advertising decays with the earned tips.** This rest line is shown for as long as
the tips machinery still has a tip left to give you; once you have retired all of them the
slot is simply quiet. **Only the advertising decays, never the doors** — the crumb's
project step still opens home and the status row's `N open` still opens the conversations
list, whether or not the words are drawn.

**Inside a task's room the slot is the room's**, and it never says `esc interrupt` there
— in a room `esc` leaves the page rather than interrupting anything. It reads `x stop`
while there is work here to stop, `↑↓ history` while a history walk is on, and nothing
otherwise. The way back is named at the top of the frame instead: the top bar's task form
ends `esc/← back · ✕`, and pressing either word does what it says.

**Two lines in that slot are about the draft you are typing**, rather than about a state
the surface is in. `ctrl+enter keeps this true` appears while your sentence looks like a
rule (see the standing orders page), and `ctrl+r spell it out` while it looks like
something to build and still has room to grow (see the keys page). They share the one
slot and the standing line wins whenever both would show. While the spelling-out call is
out, the slot turns a small spinner in front of the same words. Neither ever moves the
message box: this line is on the frame in every state.

One line in that slot is not about the next keystroke: `ctrl+g tasks`, which appears
when you have closed the task column and this session has run something. It is the
whole of what the frame says about a roster that is not on screen, and it says nothing
at all when nothing has been run.

While a question is waiting, the status word at the bottom of the frame goes violet, so
the question is pointed at from below. The legend itself stays the grey it always is.

As the terminal narrows, the hint slot gives way, and below width **70** it is gone. The
conversation's name is in the top bar, not on this rule. With nothing true to put at its
right end the line is the plain rule it always was.

While a task room is open the legend's slot is the room's alone, and the way out is
named at the top of the frame: the top bar's task form ends `esc/← back · ✕`, and
pressing either word does what it says. Clicking the page itself does not leave a room —
a press on empty space does nothing here as it does everywhere.

## Why is one word in a line brighter than the rest — highlighted model names, keys and figures

Every line aforge writes about itself — a note in the conversation, the hint slot on the
legend, `/help`, `/status`, `/cost` — is drawn in a quiet grey, because none of it is the
conversation. **The facts inside those lines are not.** Each load-bearing word steps up
into a soft cyan of its own — a hue no other kind of thing on the screen wears — so the
answer reads at a glance while the sentence around it stays out of the way. It is a hue
rather than a brighter grey on purpose: brightness says how loud a thing is, hue says
what kind of thing it is, and a fact inside a quiet sentence is a different kind of
thing, not a louder one.

What steps up, in the lines you will see it in:

| Line | What is drawn brighter |
| --- | --- |
| the crew line after `/crew` | the three crew model ids and the model you are still talking to — `brain`, `hands`, `checks` and `you are still talking to` stay grey |
| `model · <id>` after `/model` | the model id |
| `harness · <name>` | the harness's name |
| `<mode> task <id> started · <title>` | the id and the title |
| `N standing orders here — /standing` | the count |
| the legend's hint slot | the key, never the verb beside it |
| `/help` | the key at the head of each row, never its explanation |
| `/status` and `/cost` | the figure in the second column, never its label |
| the opening `esc interrupts · ctrl+c twice quits` | the two keys |

Three rules hold it to one gesture, and they are worth knowing because they tell you what
a mark means:

- **A tinted background on your words is always a slash command that acts** — a command
  at the start or a live `/standing`, `/orders`, or `/task` tag later in the draft. Help
  rows chip their leading command too. Nothing else borrows the mark, so it never
  highlights a slash word the send path will ignore.
- **A key chord is brighter ink and never a background.** `ctrl+b`, `esc`, `↑↓` step up a
  tier; they do not get a chip.
- **Nothing here is ever drawn in the accent.** The accent marks the one live or chosen
  thing on a screen — your own `›`, the rail — and a line that appears and scrolls away is
  not that.

Nothing moves when a word is lifted: it is the same characters in the same columns, one
tier louder. And a line whose facts have not been named stays exactly as grey as it was —
the ones listed above are the ones that step up.

## Which folder am I in — where the workspace path and the git branch are shown

The line above the message box used to carry the folder. Its left end is empty now, so the
workspace path lives in two places, and both say it in full:

- **`/status`** (aliases `/info`, `/context`) prints a `place` line — the whole path,
  then ` · ` and the branch with its `*` if the tree is dirty. On a remote session the
  machine is in front of it: `devbox:/srv/code/app`.
- **The status sheet**, which is `/status`'s own list on screen: the same `place` row,
  with the path abbreviated fish-style (`~/s/aforge-v2`) because a sheet row is one line.

The **branch** is on the **top bar's right cluster** — `main*` — so a glance at the top of
the frame tells you which branch you are working on without opening anything. It used to
be on the legend above the message box and is not there any more. There is no branch on a
session running over `--host`: the git probe would read *this* machine's repository at
the other one's path, so nothing is shown rather than something possibly wrong.

If the answer is just the word `aforge`, this conversation has no project — it was
started somewhere with nothing to borrow, and works in a directory of its own. `/status`
prints where that actually is.

## The task name above an answer that appeared on its own

A reply that begins because a task finished has a dim task line immediately above it in
the transcript. The line uses the same identity mark and name as the task column and quotes
your original request. Several finished tasks answered by one turn make several lines in
arrival order. A reply to something you just typed has no task line, and a task with no
recorded request shows its name without an empty quote. These lines return with the reply
after `/resume`.

## The status line at the bottom

One row at the bottom of the frame, in two clusters. **A dim few facts on the left** —
work this screen is holding for you. **The ticking numbers on the right** — what it
costs, how full it is, whether it is alive. The gap between them is the only separator:
no pipe, no bracket, no rule.

The left cluster is dim and carries at most two segments: `2 jobs · 1 watch` — background
work this screen saw start and has not seen killed, a `bash` with `background:true` or a
`watch` call — and `◦ keeping an eye on 2`, the standing orders this project holds (the
keeping-an-eye page has the whole of it). Each draws nothing when it has nothing to say.

The right cluster is the ticking facts, right-aligned, in a fixed order:

```
2 open · 1 want you · $0.14 · 12% · compaction in ~3 turns · ⠹ working · 1m 4s
```

- **`N open · M want you`** — how many conversations **this terminal** is holding, and
  how many of them are stopped on a question. Absent whenever only one is open, which is
  the ordinary case; the `· M want you` clause is absent when none is waiting. It is a
  **door**: press it and the conversations list opens, the same list `tab` walks.
- **`$0.14`** — the session's running spend. **Zero renders as nothing**: a figure
  nobody measured is not drawn, and a session that has spent nothing has nothing to say
  here. It is a **door**: press it and the **Spending** tab of `/settings` opens, where
  every money limit is set; `/budget` is the keyboard door onto the same tab. It takes
  the warm ink once this conversation has spent four fifths of its own `per conversation`
  limit — a bound about to be reached is not a failure and does not wear the failure hue.
- **`12%`** — the context meter, how full the conversation is against the compaction
  threshold. **It is a percent and nothing else**, at every width; the full
  `12.4k/128k · 10%` form comes back on its own past 80% of the threshold, where the two
  absolute figures start to matter. There is **no sparkline on the row at any width** —
  that went to `/status` and the status sheet. Under 1% there is no segment at all. It is
  a **door**: press it and `/status` prints into the transcript, so the whole reading is
  where you can scroll and copy it. The meter keeps its own three-rung heat — calm,
  **near** past 80% of the threshold, **due** past it — which outranks its age.
- **`compaction in ~3 turns`** — a forecast from average growth. Empty when the
  conversation is not growing, when the answer is more than 5 turns out, or when
  compaction is already due.
- **the state word last** — what the screen is doing, and for how long. `⠹ working ·
  1m 4s` while a turn runs, `waiting · your call` while a question is open, `stopping`,
  `interrupted`, `COPY`. **`idle` is not one of the words**: an idle chat's row ends at
  the numbers, because a row that said nothing would be a row spent saying it.

The numbers are painted by how recently each **changed**: changed under 4s is ink, under
10s is muted, otherwise dim. At rest the whole cluster is one quiet grey, and the one
segment that moved is the only thing with weight. A segment's first appearance is not a
change, so a new segment starts at the bottom of the ramp. A segment that vanishes loses
its clock — the next thing of that kind is new, not recently changed. Four things override
the ramp: the state word owns its own paint; the context meter outranks its age with its
own heat; while you are being asked something the whole ramp collapses to dim, so no
number competes with your decision; and the `YOLO` badge — which lives in the top bar now,
  not here — is always the bad hue wherever it is drawn.

**The crew, the delta, the cache, the burn rate and the served rider are not on this
row any more.** They are still read in full by `/status`, and the phone deck's status
sheet carries them (see below). A row that ticks every frame is for what you act on;
the rest is a reading, and readings belong where you ask for them.

While a task **room** is open this row stays the **session's** — its spend, its context,
its state are the conversation's, because a room is a view over one region of the frame
and not a second session. The task's own spend and clock are on the **top bar**, after the
crumb. The model in the top bar retargets to that node: pressing it opens the same picker
aimed at **that task**, which switches from its next turn onward. Where the pick could not
land the name is drawn and simply does not react: a task that has finished, failed, been
stopped or needs your look, one that has not started, an adaptive run's page, or a node
inside a run. The tasks page says the whole of it under "Changing the model for one task
while it is running".

Below width **100** the ticking cluster may take a **row of its own**, still right-aligned,
with the dim presence cluster on the row above it — and only when the two would otherwise
collide, so a short session still fits on one row at 60 columns. If even that will not fit,
the numbers are what survive, and nothing on the row is pressable.

While the empty screen's greeting is up the row is empty and the top bar carries the
crumb alone — a conversation nobody has typed into has nothing to tick (see *The empty
screen* page).

## What each part of the status line means

The row's segments, right to left, joined by ` · ` in a fixed order — each one a fact
that ticks, each one a door where a door makes sense:

| # | segment | example | what the number is | when it is empty |
| --- | --- | --- | --- | --- |
| 1 | open | `2 open · 1 want you` | how many conversations **this terminal** is holding, and how many of them are stopped on a question. **A door**: press it and the conversations list opens | absent whenever only one is open, which is the ordinary case; the `· N want you` clause is absent when none is waiting |
| 2 | cost | `$0.14` | the session's running spend. **A door**: press it and the **Spending** tab of `/settings` opens, and it brightens under the pointer to say so. It takes the warm ink once this conversation has spent four fifths of its own `per conversation` limit — a bound about to be reached is not a failure and does not wear the failure hue | **zero renders as nothing** — a session that has spent nothing has nothing to say |
| 3 | context | `10%` | how full the conversation is — the percent alone at every width, and the full `12.4k/128k · 10%` form once it is past 80% of the compaction threshold. No sparkline on the row at all. **A door**: press it and `/status` prints into the transcript | empty when nobody has said what the window is, when tokens are 0, or under 1% |
| 4 | eta | `compaction in ~3 turns` | forecast from average growth | empty when the conversation is not growing, when the answer is more than 5 turns out, or when compaction is already due |
| 5 | state | `⠹ working · 4s` | what the screen is doing, and for how long | never empty while anything is happening; **an idle chat's row ends at the numbers** |

On the dim left end of the row sit the two facts that are about your work rather than
the conversation's: `2 jobs · 1 watch` — background work this screen saw start and has
not seen killed, a `bash` with `background:true`, a `watch` call — and `◦ keeping an eye
on 2`, which is a door onto the standing orders page. Zero of either draws nothing.

The `N jobs` figure means "what you started". A background job that exited on its own is
still counted, because nothing on the wire says otherwise. For the state of one job rather
than a tally, read the task column on the right: every job has a row there while it runs and
settles when it ends — the tasks page has it under *Background jobs on the column*.

The `open` count is read from the conversations themselves and not from the files other
terminals leave behind, so it never lags: a conversation that stops on a question while you
are looking at a different one is counted in `N want you` on the next frame. `tab` over an
empty box goes to the last one — see the keys page, and home's *Switch between projects
without leaving*.

The **crew preset, the session's line delta, the cache hit rate, the burn rate and the
served rider** are not on the row. `/status` prints every one of them, and the phone
deck's status sheet carries them; the row keeps only what ticks and what you act on.

## How fast is the connection — host latency and round-trip time in the top bar

For `aforge chat --host devbox`, the host segment in the top bar's right cluster begins
empty. Every few seconds the surface sends one empty call, off the drawing path, and folds
the reply into a rolling estimate. After the first answer it reads like `devbox · 3ms`.
A sub-millisecond reply is shown as `1ms`, never `0ms`; no answer means no segment.

The check is never sent per frame and is skipped while the link is reconnecting. During a
redial the existing sentence — `reconnecting to devbox — trying for up to 5 minutes` —
takes the segment, and it outranks everything else in the right cluster: it may evict
all of it. `/status` spells the healthy fact out as `the round trip to devbox is
about 3ms` under `connection`.

## Why the numbers on the status line fade

The telemetry cluster on the right of the status line is painted by how recently each
segment **changed**: changed under 4s is ink, under 10s is muted, otherwise dim. At rest
the whole cluster is one quiet grey, and the one segment that moved is the only thing
with weight.

A segment's first appearance is not a change, so a new segment starts at the bottom of
the ramp. A segment that vanishes loses its clock — the next thing of that kind is new,
not recently changed.

Four things override the ramp, in this order: the state word owns its own paint; the
context meter outranks its age with its own three-rung heat; while you are being asked
something the whole ramp collapses to dim, so no number competes with your decision;
and the `YOLO` badge — which lives in the top bar, not on this row — is always the bad
hue wherever it is drawn.

There is no clock running at rest to drive this. During a turn the frame clock is
already running, and when a turn settles exactly two one-shot ticks are scheduled so
the fresh tier can expire on time.

## Why the bottom rows of a long list look dimmer — faded, greyed out or washed out rows

The last three rows of a list that runs on past the bottom of its window are drawn a step
fainter each, fading toward the background. It happens on the task page (`/history`,
`ctrl+.`), on the task column, and on home's list of projects and conversations.

It means one thing: **there is more of this list below**. The head of the window is at
full strength, the tail steps back, so a long list reads as sharp where you are and quiet
where you are not. It is the same three-step ramp the thinking window uses while a model
works.

Four things about it:

- **A list that fits does not fade at all.** With the last row of the list already on
  screen there is nothing below to point at, so a short list is drawn exactly as it would
  have been with no such rule.
- **The row you are on is never faded**, wherever it has been scrolled to — including
  when it is the very last row before the fold. Neither is a row under the mouse.
- **Nothing you are reading fades.** The conversation and copy mode are untouched: a
  transcript is read line by line and every line of it is the content, not context.
- **Rows never alternate light and dark.** aforge draws no striped lists anywhere. Rows
  are told apart by spacing, and groups inside a list by a blank line — never by a rule,
  and never by a background that flips row to row.

On a terminal below 256 colours, and with `NO_COLOR` set, there is no ramp to fade along
and the rows are drawn plainly. Linear mode (`--linear`) drops it too, for the same reason
it drops the thinking window's gradient.

## What the `$` on the status line counts — the conversation and its tasks

The money segment is **this conversation and every task it started**, added up while the
work is still running. One figure, not two: the row is the most crowded thing on the
screen, and its segments must not grow and shrink under your eye.

It is also a **door** — pressing it opens the Spending tab — and it takes the warm ink at
four fifths of this conversation's own limit, measured on that same whole-tree figure.

`/cost` is where the figure is taken apart: it prints `conversation` and `tasks` under the
total, and they add up to it. See *Does the status line's money include what my tasks are
spending* on the models and cost page.

## Why there is no $0.00 on the status line — where the zero went

A figure nobody measured is not drawn. Zero jobs, zero watches, an unknown context
window, an unpriced cache — every one of them draws nothing rather than a zero, and
**the money segment now keeps the same law**. A conversation that has spent nothing has
no `$` on its status line at all; the segment arrives with the first priced turn.

It used to print `$0.00`, and the reason was stability: the status row was a live row
carrying the model, the branch and the host beside the money, and a segment that came
into existence mid-conversation shoved its neighbours sideways. Those slow facts moved
to the bar at the top, so the row no longer has anything to jostle, and the exception
died with the crowding that justified it.

`/status` and `/cost` have always kept the law: `/status` filters the spend line out when
the cost is zero, `/cost` only adds it above zero, and the two say `nothing spent yet —
this session has not sent a turn.` A landed task card refuses to print `$0.00` too.

Other honest silences: the context percentage is dropped below 1% rather than shown as
`0%`; the cache cash half appears only when there is a published price pair, never
"saved $0.00"; and the saved figure uses four decimals under a dollar, so a real
fraction of a cent is not rounded away to nothing.

## The state word: working, stopping, waiting — and why there is no idle

The last segment of the status line is the one thing true of the whole row. The exact
words:

| word | when | paint |
| --- | --- | --- |
| `⠹ working · 1m 4s` | a turn is running; spinner plus a count-up | accent |
| `waiting · your call` | a consent question or a task proposal is open | the question hue, bold |
| `stopping` | you pressed `esc` and the turn has not finished letting go yet | dim |
| `interrupted` | the last turn was stopped by hand and is over | the bad hue |
| `COPY` or `COPY · 12 lines` | copy mode | accent |

**`idle` is not one of the words.** A conversation that is doing nothing has a row that
ends at the numbers — no word sits there saying so, because a word that is always true
of a resting screen is a word nobody reads. The row's last segment is drawn only when
there is something happening, and the numbers in front of it are what tell you the rest.

`stopping` outranks `waiting · your call`, and `waiting · your call` outranks `working`.
Copy mode outranks everything, because it is the only state about the keyboard rather
than about the turn.

The spinner turns on the same 4-tick grid the tool rows use, so nothing on screen beats
against anything else. In the screen-reader tier the spinner is a still `*`.

## What the word stopping means in the status line, and why it is not interrupted yet

Because it has not finished stopping. `esc` cancels the turn instantly, but the turn does
not close instantly: a `bash` call whose command left something holding its output waits
up to three seconds before the pipes are forced shut, and a `jobs` kill spends two seconds
on a polite signal and two more on the one that is not polite. For those few seconds the
turn is being let go rather than gone, and the word says so.

Nothing moves during that window. The spinner is gone from the status line and from every
tool row, every running call already carries the time it ran until you stopped it, and
nothing new is drawn — a reply the model was still speaking and a call it was half-way
through asking for both stop where they were rather than landing under the `interrupted`
line. The word becomes `interrupted` the moment the turn is actually over.

**There is no second, harder stop, and there is no key to press.** `esc` again is the
rewind's door (see the sessions and rewind page) and `ctrl+c` is the quit arm, so neither
is free — and there would be nothing behind a third key anyway: the waits that make this
window long are inside a tool that has already been told to stop. What you have if it
will not let go is the program's own door, `ctrl+c` twice.

## What the status line drops when it is narrow

When the segments do not fit, they are removed one at a time in a fixed order, by how
actionable each one is:

```
jobs · watches → keeping an eye on → open · want you → compaction eta → context → cost
```

The work you started goes first because the task column holds the same facts in full,
and `keeping an eye on` follows for the same reason — the standing page is one `space
space` away. `open · want you` goes next because it is the one segment that is not
about the conversation in front: at forty columns what you need is what **this**
conversation is doing. The cost is the last number to go, because money is the one
fact you cannot read anywhere else on the frame.

The **state word is not in that list at all** — it is why you are looking at the line.

## The context meter, and where the sparkline went

How full the conversation is, measured against the **compaction threshold** rather than
the model's window — the threshold is the thing that actually happens to you.

**On the status row it is a percent and nothing else** — `9%` — at every width. Twenty
cells of fraction and trend on a row a person reads on every keystroke answered no
question at 9%: "how much of the window am I on" is the decision the meter forces, and the
percent *is* that answer. The workings come back exactly where they are needed: past 80%
of the threshold the row draws the full `82.5k/1M · 84%` on its own, because that is the
point where you are deciding whether to compact now or finish a thought first.

**Pressing it prints `/status` into the transcript**, where the whole reading — tokens,
window, threshold, cache — is prose you can scroll and copy.

Three rungs of heat: calm (dim), **near** (accent) past 80% of the threshold, and **due**
(the bad hue) past the threshold itself. It is the same one-bit question that brings the
fraction back.

**The sparkline is not on the status row at any width.** It was demoted to `/status`, the
phone's status sheet and the Spending tab — a trend is a thing you go and look at, not
something that earns six cells of the row you type under. Where it *is* drawn it is the
last **6** turn-end readings, one glyph each from `▁▂▃▄▅▆▇`, scaled to the threshold; none
is drawn with fewer than two readings, because one bar is not a trend, and none in the
ASCII glyph tier or the screen-reader tier. All of them keep the number, which is the fact.

Under 1% there is no segment on the row at all, and the percentage is dropped rather than
shown as `0%` — a figure nobody measured is not drawn.

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

Phone width **reshapes eight things and deletes none**. What will not fit is relocated,
not truncated away.

## What phone width reshapes: the eight

Under 60 columns, eight things change shape:

1. **The status line becomes a two-row deck**, with a fullscreen status sheet one tap
   away — and the deck's top row carries the top bar's crumb, because the top bar itself
   is not drawn at this width. The two geometries converge: what the wide frame splits
   between a top bar and a bottom row, the phone stacks into one deck.
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
7. **Home becomes an inbox, a sheet and an action bar.** The column stops being a
   directory of projects and becomes triage across all of them — `waiting on you`,
   `running`, `since you left`, three rows each and then `▸ …N more` — with this
   window's project open under them and every other project folded to one line.
   `enter`, or a **tap**, opens that row's card as a full-frame sheet whose top row
   reads `‹ back`; the card's answer chips become full-width answer bands, one per
   row, that a digit or a tap answers. The hint line under the box becomes one row of
   at most three wide targets: `open · new · ask here` on the inbox, `‹ back · open ·
   more` on a sheet. A tap **opens** — there is no second column to preview into, so
   there is no two-step — and mouse motion is ignored. Below width **24** the plain
   hint line is drawn instead of the bar.
8. **The roster becomes cards.** That page's rows become two-line cards a
   thumb goes into, and its foot becomes a `‹ back` bar in place of the key legend. See
   *Tasks on a phone* on the tasks page.

On top of those eight: preview blocks under a pending call are capped at 4 rows instead of
12; there is no task rail column (that already went at 100); and the legend has already
dropped its hint slot (that went at 70).

## Other width thresholds worth knowing

Beyond the four tiers, these are the exact points where parts of the screen give way:

| what | threshold |
| --- | --- |
| the top bar is not drawn at all — the deck's top row takes the crumb | below width 60 |
| telemetry may wrap to its own row | below width 100 |
| legend loses its hint slot | below width 70 |
| context meter is the percent alone at every width; the full `12.4k/128k · 10%` form returns | past 80% of the compaction threshold, not at any width |
| full task rail, 30 columns off the conversation | width 120 |
| slim task rail, 24 columns | width 100 |
| no rail column at all — `ctrl+t` overlays the roster instead | below width 100 |
| no rail column at any width — you closed it with `ctrl+g` | your choice, remembered |
| the empty screen's greeting (wordmark, model line, centred message box, try line, recent sessions) | not drawn below height 12 or width 40 |
| consent bottom sheet at phone width | width 16 |
| the size in a tool row's right column | dropped unless the target keeps 7 cells |
| tool preview and expansion | nothing below width 8 |
| phone code wrap | falls back to plain cut rendering below 8 content cells |
| opened-table columns | become stacked records below 8 |
| landed task card | nothing below width 8 |
| turn receipt | none below width 8 |

## The two-row status deck at phone width

Under 60 columns the status row becomes a deck of exactly two rows — never one, never
three:

```
 aforge-v2 › fix the parser     $0.31 · 12% ▸
 deepseek-v4-flash          ⏺ 2 running · ⠹ working
```

Row 1's left is **the crumb** — the same trail the top bar draws, walked down the same
give-way ladder until it fits: `project › chat` at rest, and in a room the room's own chip
(its state mark and its title). The top bar is not drawn at this width, so this row carries
it. Against it on the right is **what it has cost** — spend, and the context percent only,
because the fraction is what the sheet is for — with a `▸` on the end. Row 2 is **what is
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
fullscreen sheet listing every fact the wide row holds, plus the readings the row gave
up — the crew preset, the session's line delta, the cache hit rate, the burn rate, the
served rider — one per line, label then value.

Typing `/status` (aliases `/info` and `/context`) prints the list as a note in the
conversation. It also adds the complete session-file path and the build identity, because
both are facts meant to be copied rather than permanent rows in a phone-sized sheet. That
is the only door if you are on the keyboard with the mouse off.

The head is `status` on the left and `esc close` on the right. The foot names the keys:
`esc close · ↑↓ move`, plus ` · enter model` while the cursor is on the model line.
Rows carry a `▸` when a press acts on them. The row you are on has its label in the
accent and its ground one step up — the quieter of the two row backgrounds, the one that
means "the cursor is here". The keyboard and the pointer share it: this sheet has nothing
open on it, so there is no second, louder background to keep apart from the first.

The items, in order: `session`, `task` (in a room), `model` (the full routing address
with its `:level` — the one actable row), `task model` (in a room), `served`, then every
telemetry segment under its own word — `background`, `changes`, `spend`, `context`,
`cache`, `rate`, `compaction`, `approvals`, `connection`, `state` — then `tasks`, `place` (full path,
branch and dirty star) and `keys`.

A press selects a row; a second press on the already-selected row answers it. A press
outside the list — the title, the rules, the keys line, the empty rows under a short
list — closes the sheet. The wheel moves the cursor.

The sheet closes itself the moment the frame grows back out of phone width. A sheet
standing in for a row that is back on screen is a sheet nobody asked for.

## Which aforge build is running — version, commit, dirty build and restart notice

Type `/status` and read the one `build` line. It names the source revision, says `(dirty)`
when the build included uncommitted files, and gives the local build time:

```
build  1265feda (dirty) built 2026-08-27 13:28
```

`aforge --version`, `aforge version` and `aforge -v` print the same identity without
opening a conversation. On a session opened with `--host`, `/status` names the build on
the machine holding the conversation, not the surface machine's build.

If the `aforge` file is rebuilt while this process is still open, aforge writes one quiet
line after the current turn:

```
a newer aforge was built at 13:28 — restart to use it
```

It says this once for that newer file, not after every turn. Rebuilding again produces
one new line. The running conversation is not silently changed underneath you; restart
aforge to use what was built.

## Markdown: what aforge renders

The model's reply is rendered as markdown, parsed with goldmark and painted through
aforge's own token layer — there is no HTML renderer involved. **Every place on this
surface that draws an answer draws it through this one renderer**, including the `ask here`
pane on home, so the same words never read as prose in one place and as `**source**` in
another. What is supported:

- **Headings** — promoted by tier, never by size. h1 is primary ink and bold, h2 is
  primary, h3 and below step back down the grey ramp. Loudness is position on the ramp
  plus the whitespace around it.
- **Paragraphs** — wrapped to a measure of **88** cells, clamped to the pane width, not
  to your terminal width. A 200-column window is a wide window, not a wide sentence.
- **Emphasis** — CommonMark by delimiter count: one `*` is italic, two is bold, three is
  both. Nesting composes, so it cannot leak.
- **Strikethrough** — GFM `~~x~~`.
- **Inline code** — drawn on the one raised plane, except when the span names a real
  path: a linked path keeps its underline and drops the plane so one token never wears
  two visible marks. Where the terminal has no raised plane (16 colours and below), the
  **backticks come back** rather than ordinary code reading as prose.
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

An `ask here` answer on home keeps the same law with one step instead of two: it is plain
while it arrives and formats when the turn ends. There is no 1500ms promotion there, because
the pane's answers are short enough that the settle is the catch-up.

## The reply dims when it finishes — brighter while streaming, calmer when done

That is deliberate, and it is the only thing that says the turn is over. There is no
spinner at the end of an answer and no tick mark.

While a reply is arriving, its plain tail — the part below the last promoted line, which
is the part still growing — is painted **one step brighter** than the body. The moment
the turn finishes the whole block is re-rendered as markdown at the ordinary body ink,
and the brightness drains away. Nothing is added to the screen and nothing is taken off
it; the ink dries.

Only the growing tail is brighter. The prefix already promoted to markdown carries its
own styling and is left as it is, so the calm part of an answer is the part this screen
has already decided is final.

The whole effect needs 256 colours. On a terminal with sixteen, and with `NO_COLOR` set,
a streaming reply is drawn exactly like a settled one — bolding it instead would make it
look like a reply that opened in bold, which is a different thing.

## Why is part of the reply grey, and where is the actual answer

Because that part was never the answer. It was aforge saying what it was about to do.

A turn is usually prose, then tool calls, then more prose. **Any paragraph that had more
work start under it in the same turn is narration** — "let me check the config first" —
and the moment the next tool call opens, that paragraph visibly steps back: it moves into
the same two-column gutter the tool rows use, and drops one shade below the body text.

**The answer is the last thing the turn says, and it is the only flush-left, full-ink
block in it.** So: scan down the left edge. Text that starts at the margin was said to
you. Text that starts two columns in was done for you. There is one blank row above the
answer whenever the turn did any work, so it stands away from the machinery.

Grey narration carries **no markdown** — no bold, no headings, no code colouring. That is
deliberate: a bold heading inside working notes would be heavier than the answer under it,
and the loudest thing on screen would be the part you did not ask for.

Nothing here reads what the model wrote. It is decided entirely by the shape of the turn —
what came after what — so it is the same on a conversation you resume as it was live, and
the same on a task's own page.

Below 60 columns the gutter is dropped and the shading alone carries the difference. With
no colour at all, the gutter alone does.

## I pressed esc and the reply stayed grey — why nothing became the answer

That is the screen telling you the truth: **an interrupted turn never reached an answer.**

Press `esc` while a turn is running and whatever had been written stays on screen,
because the session keeps it — but it stays at the working shade, in the working column, for good. The missing
flush-left paragraph *is* the statement that you did not get an answer, so nothing has to
be added to say it. Asking something else afterwards does not promote it later.

The turn also collapses to a chip that says who stopped it —
`▸ stopped by you at 40s · 4 tool calls · ctrl+e` — with nothing left standing under it.
`ctrl+e` over an empty message box, or a click on the chip, opens it again. aforge's own
lines about the stop, `· stopped` and anything it dropped from the queue, stay outside
the chip.

One limit worth knowing: the session file keeps the words a stopped turn managed to say
and keeps no mark saying it was stopped. So if you close aforge and **resume** that
conversation later, that turn is rebuilt from its shape alone and its last paragraph reads
as an answer again.

## My message appeared in the middle of the reply — a message never lands mid-stream

It cannot any more. A message of yours is never drawn inside a streaming answer, never
splits a reply into two blocks, and is never interleaved with the paragraph being
written. The rule holds in the conversation and in a task room's own page alike. The
partial reply stays one contiguous block, the provider request stops, and your line goes
**after** the partial before the turn continues.

There was a defect here. A steer once waited for a model request or a long tool to
finish, so the correction looked inert. Plain `enter` now stops the current model
generation, keeps its partial reply, and draws your correction immediately beneath it.

A sentence you deliberately steer into the running turn — plain `enter`, or `→`
over a message that is already waiting — never lands inside the answer's block.

## A message you typed while the answer was still coming (the waiting block)

Press `cmd+enter` while a turn is running and your message is **held**, not sent. It is
drawn in its own block directly above the message box — under everything that has
happened, above the box you typed it in — in your own accent hue, with the same `›`
glyph your messages wear in the conversation. Under it sits one dim line:

```
› do much more of a deep research please
  waits for this answer · esc stops and sends · → steers it in · ↑ or click to edit
```

The dim line trims from the right on a narrow terminal: the last piece goes first, then
the next, and the narrowest frame keeps `waits for this answer` alone. With more than
one message waiting the first piece is counted — `2 wait for this answer` — and with
exactly one it is not counted at all.

`→ steers it in` is there only while the message can go into the running answer: a turn
still running, and a message of words alone. A waiting message that carries pictures, or
one marked with `ctrl+enter`, cannot be sent in and the clause is absent for it. `esc
stops and sends` and `→ steers it in` both go while a turn you stopped is winding down —
for those seconds neither key does anything, and what is left of the line is still true.

## What happens to a message waiting above the box

What happens to it:

- **When the answer finishes**, it sends itself as an ordinary new turn and appears in
  the conversation as a normal message of yours. Several waiting messages go **one per
  finished turn**, oldest first, in the order you typed them.
- **`esc`** stops the answer and sends it immediately.
- **`→` over an empty box**, or a **click on the words `→ steers it in`**, sends it
  **into** the running answer instead of leaving it to wait. A streaming generation
  stops and keeps its partial; a long bash is kept as a job. With several waiting it
  is the one at the front of the queue that goes.
- **`↑` over an empty box**, or a **click on the block**, takes it back into the box to
  be edited. `cmd+enter` then holds the edited sentence again.
- The box is cleared the moment you press `cmd+enter`, so you can keep typing. Attachments
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

## Click a file path to open it — open a file from the chat

**Every file path on this screen that names a file that really exists is a real
hyperlink.** Click it and the file opens the way your desktop would open it. In most
terminals that is **cmd+click** on a Mac and **ctrl+click** on Linux; a few open on a
plain click, and most will offer it on the right-click menu as well.

This works everywhere a path appears in aforge's own text:

- **anywhere in a reply** — in a sentence, inside `` `backticks` ``, inside a fenced
  code block, in a list, in a table cell.
- **in your own message**, including the `[shot.png]` markers under a message you
  attached a picture to, and the `Transcript: file:///…` line an `@task` mention
  leaves behind.
- **in aforge's dim `·` notes** — `/status`'s `file` row, `/help`'s `session · …`,
  `exported · …`, `resumed …`, `new session · …`.
- **on a tool row** — the target of a `read`, an `edit` or a `write`, and the file name
  above an edit's diff, even when the row was too narrow to show the whole path.
- **the dim line under a picture**, which is the picture's whole absolute path.
- **on the home screen** — the dim `project · path` line under a conversation's name,
  which opens that folder. (There used to be an `elsewhere · …` line beside it, naming the
  folder you had to go and start aforge in to open another project's conversation. It is
  gone: `enter` on home opens any project's row now.)

**It survives wrapping.** A path too long for the pane goes down across several rows,
and every row of it opens the same file — the terminal is told the target
separately from the text, so there is no fragment to grab by mistake. This is the whole
reason aforge writes the links itself rather than leaving your terminal to guess where a
word starts and stops.

**A line and column come along.** `internal/tui3/app.go:412:9` — the sort of thing a
compiler prints — is one target, and the click opens the file.

**A bare file name counts** when it really is a file in the workspace: `go.mod`,
`README.md`, `main.py`. So does one written against your home directory, `~/notes.md`,
and one written out as a `file:///…` URI.

**In `/files`, `enter` opens the row** the same way. Those rows are not hyperlinks
because the key is already there.

## Why is a path underlined

The underline is how you can tell there is something to click. Terminals differ wildly
about whether they show a hyperlink at all until you are already holding the modifier
down, so aforge draws the affordance itself: a path that is a working link is
underlined, and a path that is not is plain.

When a working path is written as inline code, the underline is its one visible mark;
it does not also wear inline code's raised plane. Non-path inline code keeps the plane
and has no underline. Fenced code blocks are unchanged.

It is the same underline a path wears inside a highlighted `bash` command, because they
mean the same thing — this is a location.

The underline is drawn with colour, so a terminal set to draw **no colour at all**
(`NO_COLOR`, or `TERM=dumb`) shows no underline. Where colour is off but the terminal is
real, the link is still there and still clickable; you just cannot see it in advance.

## Which terminals can open a path, and which cannot

**They open:** iTerm2, Kitty, WezTerm, Ghostty, Windows Terminal, and the terminal
built into VS Code. Inside tmux they open too, on tmux 3.4 and later.

**macOS Terminal.app does not.** It has no support for terminal hyperlinks, so a path
there is underlined text that does nothing. What it does instead is its own
guess-at-the-word — cmd+double-click — which is exactly the behaviour that breaks on a
wrapped path, and is why this exists everywhere else.

Nothing breaks in a terminal that cannot open them: the link is written in a form an
unknowing terminal ignores, and the path reads and copies as the plain path it always
was. If `TERM` is unset or `dumb` — a pipe, a file, a cron job — aforge writes no path
links at all.

## What is not a link: a path that does not open

A path only becomes a link **after aforge has gone and looked for the file**. Everything
below is drawn as plain text on purpose:

- **A file that is not there.** A name the model invented, a file deleted since, a path
  with a typo in it. A link that opens nothing is worse than no link.
- **Anything that merely looks like a path.** A diff's `a/main.go` and `b/main.go`,
  a version like `v2.0`, an aspect ratio like `4:3`, a fraction like `1/2`, an import
  path like `github.com/…`. None of them is a special case; none of them exists.
- **A web address.** Only files are linked here. (A sign-in link from `/connect` is
  separately clickable — that is its own machinery.)
- **A relative name that climbs out of the workspace** with `../..`. Inside a reply a
  relative name means "in the workspace", and one that leaves it has stopped meaning
  that.
- **Anything a command printed.** A `bash` call's output, a `grep` or `find` result, the
  body of a `read` — those are another program's words and aforge draws them exactly as
  they arrived. Sweeping them for pathish words would underline half a test log. What
  **is** linked on a tool card is the part aforge wrote itself: the call's own target.
- **Everything on a task's page and inside a task's room** — that is the `/history`
  page and a room both. A task works in its own git worktree, so `internal/tui3/app.go`
  on one of those rows means THAT tree's copy and not this one's, and a link built
  against the wrong tree opens a file with the right name and the wrong contents. The
  `/history` page names no file paths of its own anyway: a row says what a task did and
  which branch it left behind, never where its journal is.
- **A path the other machine has not confirmed, over a connection.** On a session started
  with `--host` the files are on that machine, so the check is made **there** and only a
  path it confirms becomes a link. A word it has not answered about yet, or one it says is
  not a file, is plain text exactly as it would be at home. What a confirmed path links to
  is not `file://` — that would mean this machine's disk — but a small door this window
  owns; the whole of it is on *Opening files from that machine*. A confirmed **folder** is
  not a link over a connection either.

## Copying a path, and why a reply cannot make its own link

**What you copy is the plain path.** Copy mode (`ctrl+b`, or `/copy`) and `/export` strip the
escape sequences, so a path leaves this conversation as the characters you can read, and
an exported `.md` has no terminal machinery in it. Your terminal's own
select-and-copy takes the visible characters too.

**A reply cannot make its own link.** Everything the model and the tools write is
cleaned on the way in, and that strips terminal hyperlinks along with everything else a
stream of text could use to make your terminal do something on its own — set the window
title, write your clipboard. So a reply that writes out a hyperlink to somewhere else
gets no link, only its visible text.

Every link on this screen was therefore made by aforge, points at a file, and points at
a file that was there when the row was drawn.

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
├─▶ read internal/session/session.go              189 lines · 0.4s
├─▶ edit internal/session/loop.go                     +3 −1 · 0.2s
│ internal/session/loop.go
│ @@ -1,4 +1,4 @@
│ -const argsLimit = 8192
│ +const argsLimit = 32768
╰─▶ bash go test ./internal/session              ✗ exit 1 · 1m02s
```

`├─▶ ` for every call above the last, `╰─▶ ` for the last, `│ ` for an opened call's
detail rows. Every rail form is exactly 4 cells wide, so a cluster's names all start in
one column. A terminal that cannot draw those glyphs gets `+-> ` and `| ` at the same
widths.

Each state has its own mark:

| state | mark | the row |
| --- | --- | --- |
| arriving on the wire | `◌` pulsing dim | dim whole, e.g. `receiving · 1.2 KB`; a `write` also hangs the file it is typing |
| queued | `◌` dim | ordinary row, quiet |
| waiting on you | `?` in the question hue, bold | the whole row is the question hue |
| running | braille spinner, muted | the spinner leads the right column and the count-up follows it: `⠋ 4s` |
| done, success | **nothing** | a quiet line is the success; the right column is the size and the duration |
| done, failed | `✗` in the bad hue | the `✗` leads the right column: `✗ exit 1 · 1.2s` |
| unresolved when the turn ended | `·` dim | frozen; the clock stops |

The spinner means one thing only: something is turning. On success there is no glyph,
ever — a column of ticks is a column you must read to learn nothing. In the
screen-reader tier the marks are `o` queued, `*` running, `x` failed, `.` idle; `?` is
already ASCII.

At most **3** calls of a turn stay on screen. The rest fold into one line reading
`↳ 1 earlier tool call · ctrl+o` or `↳ N earlier tool calls · ctrl+o`. Press `ctrl+o`
or click the line to unfold. Three is the number you can hold without reading: the call
that is running and the two it followed.

A **task's page** keeps more — as many calls as the window is tall — and its fold line
reads `↳ N earlier tool calls · scroll up or ctrl+o`, because there scrolling up at the
top of the page opens it too. The conversation's fold only ever opens with `ctrl+o` or a
click.

## What a tool row says, part by part

A row is a **sentence** — rail, name, target — that starts at the rail, and a **right
column** that ends at the frame's edge. The sentence says what the call is pointed at;
the column says how it is going and what it cost.

The **name** is chrome, so it is muted. The **target** is what you are reading, so it
leads in primary ink — and it is split in two within itself: what the call is about
stays ink, what merely qualifies it recedes to dim.

- `bash` — in an `&&` chain, everything before the last command is context and goes dim.
  The last command leads in ordinary ink; flags, operators, numbers and comments recede.
- `read`, `edit`, `write` — the path is ink, a trailing `120-240` line range is dim.
- `grep`, `find` — the **pattern is ink** like every other target, and the place it
  searched is dim. The pattern is never the accent: a tool row is telemetry, and the
  accent is spent on the one live or chosen thing on the screen.
- `web_fetch` — the URL, whole, off the call's own arguments.

A target too long for the row is **cut in the middle** when it is a path or a URL, and at
the **end** when it is a command or a pattern. A URL's two ends are the two you read it
by — the host says whose page it is, the tail says which page — so
`https://www.reuters.com/world/us/us-treasury-double-sizes-…-2026-08-20/` keeps both and
spends one cell on the `…` between them. Three fetches of one news site cut at the end
would be three identical rows. A command is read left to right and its first words are
what it does, so a command keeps its head.

The **size** is the dim figure in the right column of a finished call, derived from the
call's own arguments and output:

| tool | size |
| --- | --- |
| `edit` | `+3 −1` |
| `write` | `+42 lines` |
| `read` | `189 lines` |
| `bash` | `exit 1`, **only on failure** — a zero exit says nothing |
| `grep` | `12 matches` |
| `find`, `ls` | `8 entries` / `1 entry` |
| `web_fetch` | `12.4 KB` — how much page came back, the whole page and not the shortened copy |
| anything else | nothing |

A `+` is appended to a count whose output — or whose arguments — were shortened by the
display cap, meaning "at least this many": a huge write reads `+412+ lines` and a huge
edit `+37+ −12+`. See *Why is a big write or edit cut off*. If you answered a consent question for the call, the word `allowed` or
`denied` rides the same slot, after a ` · ` on a wide row, and replaces the stat
entirely at phone width. **A command you sent to the background with `ctrl+g` rides
it too**, as `job 3`, and it is drawn last because it is the most recent thing to
have happened to the row.

A stat is only ever drawn for a finished call. An edit's `+N −M` is knowable early and
is deliberately withheld — it is the shape the preview collapses into when the change
lands. A result that never arrived draws no stat at all, because "0 matches" is a claim
about a search nobody made.

When the row is too narrow, the right column gives up **whole segments** rather than
clipping characters — see *What the numbers on the right of a tool row mean* — and the
target is truncated last, because the target is the substance and a figure nobody has
room for is a number about a line nobody can read. The target always keeps at least
**7** cells: the last few characters of a name and the `…` that says the rest was cut.

## What the numbers on the right of a tool row mean

Everything at the right-hand end of a tool row — the spinner, the time, the size — is one
column, dim, flush against the frame's edge so the figures line up down a cluster. What
it holds depends on the state:

| state | the right column |
| --- | --- |
| queued | `◌` |
| waiting on you | `?` |
| running | `⠋ 4s` — the spinner, then how long this call has been going |
| done | `189 lines · 0.4s` — what it came to, then how long it took |
| failed | `✗ exit 1 · 1.2s` |
| unresolved when the turn ended | `·` |

So `12.4 KB · 0.8s` on a `web_fetch` row means the page was 12.4 KB and took 0.8s;
`⠋ 4s` means it is still going and has been for four seconds. The time is always **this
call's own**, never the turn's.

**When the row is too narrow it drops whole segments, in a fixed order**: the size goes
first, then the duration, and the mark — the spinner, the `◌`, the `?`, the `✗` — is the
last thing given up. Half a figure is worse than no figure: `12.4 K` is a number you have
to distrust. Below the room for the mark alone the column is not drawn at all, rather
than drawn as a stub.

Nothing here is ever cut mid-figure. If you see a tool row ending in a stray `…`, or a
running call with no spinner, that is a bug and not the design.

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
missing, null or zero counts down against the 600-second ceiling the command is really
bounded by, never a number nothing is going to enforce. A background `bash` has no bound,
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
nobody watched what became of it. A request the session **cut and sent again** — a model
that went quiet, a reply that came apart — settles its rows the same way, in the same
breath as it drops that attempt's text.

If a row is spinning and the bottom row ends at the numbers — no state word, nothing
running — that is a bug worth reporting: nothing spins on a resting session.

## What happens when the countdown runs out

Not a kill. A foreground `bash` command that reaches its bound is handed to the
background and **keeps running**: the row finishes normally, its result opens with
`still running as job 3; log at …` and then carries whatever the command had
already printed, and the turn goes straight on.

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
| `write` | the content, syntax-coloured | 20 rows |
| `read` | the returned chunk, syntax-coloured | 30 rows |
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

A `write`'s content and a `read`'s returned text are drawn as **source**, not as flat
text: see *Syntax colours in a tool call* below.

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

## Watching a file being written — the live content under a `write` that is still arriving

A long `write` takes seconds to arrive over the wire, and while it does you can **read
the file as it is typed**. The row says how much has landed — `write notes.go ·
receiving · 12.4 KB`, with a dim pulsing `◌` — and underneath it hangs the **last lines
of the file so far**, dim and syntax-coloured, following the text downward as it grows.

It is a **tail**, not the beginning: at most **12** rows (**4** at phone width), always
the most recent ones, so what you are watching is where the model is writing. There is no
header above it and no `… N more lines` foot under it — there is no remainder to offer,
because the file is not finished. The honest figure for the size is the `12.4 KB` on the
row itself.

**Only `write` does this.** `edit` deliberately does not: an edit's block is a unified
diff, and a diff needs both sides of the change whole — half of what is being replaced
against nothing at all is not a change, it is a guess. The whole diff appears the moment
the call is announced, which is the moment it becomes true. Every other tool hangs
nothing while it forms.

A row that is still arriving **answers no pointer and cannot be opened**: nothing has
been asked for yet, so there is no payload to expand and no result coming. It becomes an
ordinary clickable row the instant the call is whole, and the live tail is dropped then —
from that point the preview under the row is drawn from the call's own arguments.

## Syntax colours in a tool call — code in an opened `write` or `read`

Open a `write` and you get the file's content; open a `read` and you get the text it
returned. Both are drawn as **source**: lexed by the file's own name — `.go`, `.py`,
`.ts`, `Dockerfile`, whatever the path says — and painted on the same quiet ramp a fenced
code block in a reply uses. Keywords, strings, numbers, comments and function names take
their tint; everything else stays at the block's dim tier. It is meant to read as
evidence you can skim, not as an editor window: **colour where there is meaning, dim
everywhere else.**

It falls back to the flat dim block it always drew in three cases, and nothing is lost in
any of them:

- **the file's name matches no language** — a log, a `.txt`, a path the call never
  carried. Guessing would mean painting a log file as if it were code.
- **the screen-reader tier** (`--linear`). Syntax colour is a claim made by hue alone,
  and a surface being read aloud does not receive one. Every other colour on this surface
  stays in linear mode; this one has nothing to say there.
- **16 colours and below**, where there is no code ramp at all — the same rung at which a
  fenced block in a reply stops being highlighted.

Long lines still **truncate and never wrap**, because indentation is how source is read
and a continuation at column zero lies about the nesting. The live tail under a `write`
that is still arriving is coloured the same way.

## Why is a big write or edit cut off — `… (12345 more bytes)`

The copy of a call's arguments that reaches the screen is capped at **32 KB** — about
eight hundred lines of source. It is a display cap and nothing more: the tool ran on
everything the model sent, and **the file on disk has the whole of it**.

When a call is over that, the shortening happens **inside the long fields** rather than
by cutting the payload, and each shortened field ends by saying what it cost:

```
│ func lastLineThatFitted() {
│ … (12345 more bytes)
```

So an opened `write` shows the beginning of the file and that marker at the end of it,
and an `edit` shows as much of each replacement block as fits with the same marker on
each. Every replacement gets the **same size window**, so one enormous block cannot spend
the budget the others needed.

Anything counted from a shortened field becomes "at least this many", spelled with a
trailing `+` — a write reads `+412+ lines` and an edit `+37+ −12+`, the same `+` a
capped `read` or `grep` count wears. A call that fits is unmarked and its numbers are
exact.

This is why very large calls are worth opening now: they used to arrive here as JSON cut
mid-string, which nothing could read, so opening one drew a dim `—` and nothing else.

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

## When the image preview is not drawn — you only gave me text, it only gave me text, why don't I see the image, and where did my generated picture go?

If a row shows only text — something like
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
- **The session is over `--host`.** The generated file is on the other machine, so this
  terminal keeps the full result path instead of reading the same path on this machine.
  Open the linked path to fetch it and use your desktop viewer.
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

Backgrounds — the hover band, the selection band, the stronger band under a copy-mode
or drag selection, and the chip behind a recognized slash command — are drawn only at
ANSI256 and above. There is no weight that means "this row", so a slash command falls
back to bold and a hovered row to nothing.

There are three background steps and no more, each one a shade above the last: the row
under the pointer or the cursor, the row that is the chosen one, and the span you have
selected to copy. A row that is none of those has no background at all.

If a colour on this surface could be described as "bright", it is wrong.

## The font aforge is drawn for — JetBrains Mono, and how to set it in your terminal

**aforge is drawn for JetBrains Mono, regular and bold. Any monospace font with the block
and box-drawing ranges works.** A terminal program cannot set your font — it draws
characters and your terminal chooses the shapes — so this is a recommendation and never a
requirement, and nothing here breaks on another face.

What the design actually assumes is two weights and no more: **regular for everything, bold
for one tier.** There is no third weight and no second size, because a terminal has
neither. Where something needs to stand out past bold, aforge uses brightness, case, indent
or a blank line instead.

Every character aforge draws on home and the places is from a standard Unicode range —
`?` `◐` `○` `✓` `✕` `▸` `›` `·`, the box-drawing rail, the block characters in a bar chart.
**Nothing is from a nerd-font private-use area**, so no patched font is needed anywhere.

Where the font setting lives, per terminal:

| Terminal | Where |
| --- | --- |
| iTerm2 | Settings → Profiles → Text → Font |
| Terminal.app | Settings → Profiles → Text → Font → Change… |
| kitty | `font_family JetBrains Mono` in `~/.config/kitty/kitty.conf` |
| alacritty | `[font.normal] family = "JetBrains Mono"` in `~/.config/alacritty/alacritty.toml` |
| ghostty | `font-family = JetBrains Mono` in `~/.config/ghostty/config` |
| WezTerm | `font = wezterm.font("JetBrains Mono")` in `~/.wezterm.lua` |

If characters come out as boxes or as `?`, the font is missing those ranges — pick another
monospace, or start aforge with `NO_COLOR=1` and a non-UTF-8 locale, where every mark falls
back to plain ASCII (`!` `*` `o` `-` `+`) and the screen still reads.

## alt or option or ⌥ — how the chords are spelled on a Mac, on Linux and on Windows

**It is one key and two spellings, and aforge picks the spelling from the platform it is
running on.** On macOS every chord is drawn with `⌥` — `⌥1`…`⌥7`, `⌥.`, `⌥enter`, `⌥g`, `⌥q`,
`⌥s`, `⌥w`, `⌥o` — because that is what the keycap says. On Linux, on Windows, and everywhere
else the same chords are drawn `alt+1`…`alt+7`, `alt+.`, `alt+enter` and so on. Every hint
line, the key map, the composer layer's rows and the `/keys` sheet read that one spelling, so
what is on your screen is what is on your keyboard.

The manual names both spellings together — `alt+1` (`⌥1` on a Mac) — because it is one book
for both platforms. If a page here says `alt+` and your screen says `⌥`, they are the same
chord.

**On Windows and on Linux, Alt is already meta and there is nothing to set.** Windows
Terminal, conhost, the WSL consoles and every Linux terminal send `alt`+key the way aforge
expects. There is no `option` key and no setting; the chords simply work.

## Why my option key types ¡ ™ £ instead of jumping — "use option as meta" on macOS

**On macOS most terminals send Option as an accent-composing key rather than as meta until
you turn that on.** Until you do, `⌥1` types `¡`, `⌥2` types `™`, `⌥.` types `≥` and
`⌥enter` opens a line in the box instead of sending a task off.

aforge notices. The first time one of those characters arrives on a place, one dim line
appears under the list:

    your terminal sends ⌥ as a letter — turn on "use option as meta" in iTerm2: Profiles › Keys › Left Option: Esc+

It names the terminal you are actually in, it is said once, and the first real chord that
arrives retires it for the rest of the session. The first-run setup says the same thing ahead
of time, as a condition rather than a diagnosis: `the seven places answer ⌥1…⌥7 · if ⌥ types
a character instead, turn on "use option as meta" in …`.

**`alt+b` and `alt+f` do not retire it, and that is deliberate.** iTerm2's Natural Text
Editing preset maps `⌥←` and `⌥→` to the escape sequences `esc b` and `esc f`, so those two
chords arrive perfectly on a profile where Option is still composing accents — the mapping was
written for the two arrows and not for the digits. On such a profile the **word jumps work and
the place jumps do not**, which is exactly the case the line has to survive to explain. Every
other `alt+` chord still settles the question on its first arrival.

Where the setting lives:

| Terminal | Setting |
| --- | --- |
| iTerm2 | Settings → Profiles → Keys → **Left Option key: Esc+** (and Right Option, if you use it) |
| Terminal.app | Settings → Profiles → Keyboard → **Use Option as Meta key** |
| kitty | `macos_option_as_alt yes` in `~/.config/kitty/kitty.conf` |
| alacritty | `[keyboard] option_as_alt = "Both"` in `~/.config/alacritty/alacritty.toml` |
| ghostty | `macos-option-as-alt = true` in `~/.config/ghostty/config` |
| WezTerm | `send_composed_key_when_left_alt_is_pressed = false` in `~/.wezterm.lua` |

**What "on" looks like:** `⌥1` arrives as the escape character followed by `1` — which is how
meta has been sent for forty years, and is why aforge puts the place numbers on Option rather
than on Control.

**What "off" looks like:** the chord either does nothing or types a symbol. Nothing is broken
and nothing is lost — every chord has a drawn way to the same place: `tab` walks the places in
order, and the composer's own foot line names what `enter` does. But the map and the jump keys
are worth the one setting.

**And on kitty, ghostty and WezTerm there is a way in that needs no setting at all.** Those
terminals run the kitty keyboard protocol and report it, and where that report arrives aforge
binds `ctrl+1` … `ctrl+7` as a second spelling of the jump and `ctrl+.` as a second spelling of
the map. The map's own line says `alt+1…7 or ctrl+1…7 go to a place` exactly when the alias is
live, so you never have to guess. `ctrl+<digit>` has no encoding in the older scheme, which is
why it can only ever be the second spelling and never the first — a terminal that has said
nothing is never promised it.

## What each colour means

Every colour aforge draws is a role, and each role has one job.

The roles: ink for the reply's body and every tool's target; accent for your own `› `
glyph, the tool rail, and the one live or chosen thing on the screen; muted — a soft
blue one full step calmer than the accent — for your message's words, tool names, the
spinner, and headings; a neutral narration grey for the reply's working prose, one shade
under the body; dim for everything the surface says about itself — stats, notes, hunk
markers, the status line; add and del for a diff's `+` and `−`; bad for `✗`, `exit N`
and an overdue context meter; amber for a bound about to be reached **and for anything
waiting on you**; a mint green for **money and only money**; and violet for **the
question hue inside a conversation**.

**The accent budget is one thing per screen, and it is always the live one.** Whatever is
running, selected, hovered, or waiting on you takes the accent — the row under the
cursor, the room you are standing in, the spinner, `waiting on you`, the tab you are on.
Headings and section labels do not: the `openaf` wordmark in the welcome box, the name on
home's top line, and every band heading on a card are **structure**, and
structure wears muted or dim. So a screen with nothing waiting on you has no accent on it
at all, and the moment something does want you there is exactly one place your eye goes.

Violet is spent on the moment aforge is waiting for you **inside this conversation** and
on nothing else: the consent question, its glyph, its choices, the status word, and the
legend while one is up. Its whole value is that seeing it anywhere means one thing.

## Home and the places use the same colours as the chat — and their own background, none

**Home and the six places beside it — tasks, standing, memory, spend, search, settings —
paint from the table you just read.** Same inks, same three background steps, same
terminal background showing through. A place is the chat's palette applied to a list.

For a while they were not. A wave painted a page of their own — a near-black `#12121A`
under every cell, with the design's own brighter greys on top of it — and the owner tried
it and asked for it back: *"i want bg color and text color to be same as in inside chat…
this new bg looks weird."* So there is one palette again.

| What a place is saying | The colour it uses | Where you have seen it |
| --- | --- | --- |
| the subject — a conversation's title, a belief's sentence, a model's id, the place you are standing in on the tab bar | ink, **bold** for the subject | the reply's body |
| what is true about the subject — the note beside it, a section heading | muted | tool names, headings |
| the reply's working prose on a card | the narration grey | a reply's grey working-out |
| the margin — an age, a count, the hint line | dim | the status line |
| **needs a human** | **amber** | a bound about to be reached |
| **alive** — work in flight this instant | **the accent** | the one live or chosen thing |
| **money** | **mint green** | a figure in dollars anywhere |

**The background is your terminal's, on a place exactly as in a conversation.** Nothing
paints a page. The only rows lifted off your own background are the row your pointer is
over, the row your cursor is on, and a span you have selected to copy — the same three
steps, the same shades, everywhere in aforge.

**A place spends colour on two meanings and no more: amber means a person is being waited
on, the accent means something is moving.** Green is a unit rather than a signal. Every
hierarchy step past that is made with **bold, case, indent or a blank line** — bold marks
the subject and nothing else, and **nothing on a place is ever italic**.

**Three of the chat's colours do not appear on a place**, and that is the one thing about
a place's palette that is not simply the chat's:

- **The question's violet.** In a conversation, violet means aforge is waiting on you —
  the consent block, its glyph, its choices. On a place that reading is the amber, so a
  list says "this stopped on somebody" in one colour rather than two.
- **The payload cyan** that lifts a model id or a figure out of a quiet line. On a place
  a datum lifts by being the subject: it takes the body ink.
- **The finished tick's olive green.** The `✓` already says the work landed; a place draws
  it in muted so the one green on the screen stays money.

In both surfaces money's green is never the green of a finished tick: landing and paying
are two different events, and a table where they share a colour is a table that says they
are one.

## What the task column looks like: quiet rows, its footer lines and its one door

The right-hand task column is read at a glance, so it is drawn as one bright thing and a
lot of quiet ones. (This is its `tasks` section. The same column's other section,
`standing`, is described under *What is that column on the right*.)

- A **running** task's name is in ink, the body colour. **Idle, parked and finished**
  names are muted — a step quieter — and the room you are standing in is the one name in
  the accent and bold, with a colour band across its whole row.
- The tree connectors (`├─ `, `└─ `, `│  `), the id at the end of a row (`#7`), every
  detail line under a title, and every footer line are dim. The one loud exception is a
  branch that did not merge: `conflicted · task/fix-nil` is in the bad hue.
- The state glyph at the head of a row takes its own identity ring hue and no role
  colour, so a glyph can never be misread as a state paint.

**Rows that are running never scroll off**, however long the list gets: they are pinned to
the top of the column and everything under them scrolls. The `tasks` label above them is
pinned too, so the heading is still there when you have scrolled a long way down.

**A task that has finished is one line.** While a task is running, waiting on you, or
held, its row carries the detail lines under its title — what it is doing, what it is
costing, what it waits on, where a job's log is going. Once it is over, the row is its
glyph, its name and its `#7` and nothing else, so the column's height goes to what is
still moving rather than to a day's history. Queued work is not finished and keeps its
`waits: Collect sources` line; a finished task that still **needs you** — `conflicted ·
task/fix-nil`, `stopped — branch kept · task/fix-nil`, `finished — look it over` — keeps
its line too, because it is something to act on rather than something that is over.
Nothing is thrown away: see *Opening a finished row on the task column*.

**The roster is this conversation's work and nothing else.** No rows of the project's
record are drawn under it. It used to carry a dulled footnote of up to six of them — a
sample of two thousand, standing where this conversation's task rows go, with the
cursor walking out of this conversation into another one without the column saying so.
What stands in their place is **one dim door** at the foot of the column: `ctrl+. earlier`.

**Everything earlier lives one press away.** `ctrl+. earlier` opens the full-screen task
page (`ctrl+.`, `/history`), which holds every task the project has ever run, across every
session, with the filter, the cards and the mention. Home (`/home`, or space twice on an
empty box) is the other place old work is listed. Running work belonging to *other*
windows is not on the column at all, and never was; `/history` carries that too.

The footer is up to three dim lines of totals — `Σ $1.42 · 312k tok`, `3 running · 1 needs
you` — and then up to three more dim lines, each of which is a button as well as a key:

```
ctrl+. earlier
w · click seam — widen
❯ ctrl+g hide
```

**The first of them is one door with two spellings, never two doors.** It is drawn only
when the full-screen task page (`/history`) has something this column cannot give, and the
words on it say which: `ctrl+. earlier` when the project's record holds work this
session never ran, and `ctrl+. view more` when the only thing held back is a family the
column has folded. There is never more than one such line.
`w · click seam — widen` appears only while a title is actually being cut by its own
indent. `❯ ctrl+g hide` is always there, and its `❯` is drawn in ink rather than dim
because it is the control the pointer presses — the words beside it are the label for the
hand that types chords.

## Opening a finished row on the task column: how to see the log path of a job that already finished, and where the merge word, price or branch went

A finished task's row is one line, and the line it used to carry underneath is **folded,
not deleted**. It is the same fold a family of tasks uses, on the same keys and the same
cell:

- **From the keyboard:** `ctrl+t` hands the column the keyboard, `↑` and `↓` walk to the
  row, `→` opens it, `←` folds it away again. `esc` gives the keyboard back.
- **With the pointer:** hover the row and its state glyph turns into `▸`; click that one
  cell to open it, and `▾` in the same cell to close it. Clicking anywhere else on the
  row opens that task's room, as it always did.
- The fold is remembered per row, exactly as a family's fold is, and it lasts as long as
  the conversation does.

What comes back is that task's own last word: `merged · $0.42` for work that came home
clean, and `job 3 · log /tmp/aforge/jobs/3.log` for a background job, which is the handle
`jobs output` and the `read` tool take. A row with nothing to say — a task that ended with
no merge word and no price — offers no `▸` at all, because a mark that answered a press
with silence would be a lie. The full record of any task is on the task page (`ctrl+.`,
`/history`) whether the row is folded or not.

## Scrolling the task column: the mouse wheel over the sidebar, and the keys that walk it

**Turn the wheel with the pointer over the column and the column scrolls.** It moves the
column's own window and leaves the conversation beside it exactly where it was; a wheel
turned over the conversation still scrolls the conversation. Three rows a notch, the same
as everywhere else on this screen. Work that is running is pinned to the top and does not
scroll away, and the `tasks` label stays with it.

From the keyboard it is `ctrl+t` to take the column, then `↑` `↓` to walk it — the window
follows the cursor — `→` `←` to open and fold, `enter` to walk into a task's room, `alt+w` to
widen the column, and `esc` to give the keyboard back. The column's hint line says the
same: `↑↓ move · →← tree · enter open · alt+w wide · esc`, and it gains `ctrl+v think harder`
before the `esc` while the row under the cursor is work that has not finished — that chord
moves the task's own thinking rung. And on a row that **needs your look** the whole slot
becomes `a accept · l look again · n not right · esc`, and those three letters answer that
landing from the column without opening its room. While the column holds the keyboard
the wheel walks that cursor instead of the window, so the two never fight.

Widen is `alt+w` and not the bare letter `w`: the roster is read before the message box, so
a bare `w` there ate the `w` out of every sentence somebody typed with the column still
holding the keyboard. The bare letter is kept only on the full-frame roster, where there is
no message box on the screen at all.

There is **one scrollbar-less window and no second one**: the wheel, the arrow keys and a
landing task all move the same offset. What the window cannot show is said at the foot of
the column — the totals, and `ctrl+. earlier` onto the full task page.

## What is that column on the right — tasks, standing and an empty rail

The column beside the conversation carries the two things that govern a conversation.
A section with rows earns a dim lowercase label; an empty section keeps only its dim `+`
door. (An untouched empty conversation has no column at all until the first keystroke,
a task, or a standing order — *The empty screen* page says why.) Once it stands:

```
tasks
⠙ ◆ Fix the nil-map                                                     #7
+ /task

standing
◦ keep the tests green
◦ never touch the public API                                    everywhere
+ /standing

Σ $1.42 · 312k tok
ctrl+. earlier
❯ ctrl+g hide
```

- **`tasks`** is the roster — this conversation's work, one line per task, a click on a row
  opening that task's room. Workers under a task are rows of that roster too — a task's
  parts, an adaptive run's workers — hung under their parent with connectors, folded and
  walked like any other row; there is no separate preview list beneath a row. When more
  than one worker is running, the label itself says the count, such as `tasks · 4 working`;
  at zero or one it remains simply `tasks`.
- **`standing`** is the standing orders reaching this conversation, one line each: a mark,
  what the order is called, and a dim tail naming its reach **only when that reach is not
  the ordinary one** — `everywhere` for machine-wide, `just here` for this conversation
  only, and nothing at all for an order governing this project. A row's mark becomes the
  spinner while that order is being checked or fired right now. Clicking one opens
  `/standing` with the cursor already on it.
- **The `standing` section keeps its rows however long the roster gets.** The two sections
  do not compete for the column: the roster is given what is left over after the label,
  the doors and the standing rows have been reserved, and it is the roster that scrolls.
  A session with forty tasks in it still shows the orders standing over it, at the foot of
  the column, without scrolling.
- **At most three orders are drawn, and the label counts the rest** — `standing · 7 more`,
  in the same shape as `tasks · 4 working`. Past a handful the rows stop being read one at
  a time; `+ /standing` (or `/standing`) opens the page that lists them all. A section
  showing every order it has says nothing extra: the label is simply `standing`.
- On a **short terminal** the standing rows give way one at a time so that the roster
  keeps at least six rows, and under that the whole section stands down rather than
  drawing a label over nothing.
- **An empty section has no label and no absence sentence.** When both are empty, only
  `+ /task` and `+ /standing` remain as the discoverable doors.
- **The `+` rows type, they do not arm.** Pressing `+ /task` or `+ /standing` puts that
  command and a space at the head of your message box and hands the keyboard back — plain
  text you can edit or delete, no mode, no form.

Under both sections come the column's dim totals and its door lines. The separation between
the sections is one blank line: this surface separates with whitespace and never with a
rule. On a build with no ambient side the `standing` section is absent entirely.

## The right edge: the chevron that opens and closes the task column

**The right edge always carries one chevron, and clicking it goes both ways.**

- While the column **stands**, its last footer line reads `❯ ctrl+g hide`. The `❯` is
  drawn in ordinary ink, not dim, because it is a control and not a reading; the words
  beside it stay dim. Click the line and the column closes.
- While the column is **away**, what is left is **two columns down the right of the frame**
  with a `❮` handle at the middle of them, also in ink. The whole strip is a door: click
  anywhere on it and the column comes back.

So one control in two states — `❯` to close, `❮` to open — and the pointer can go round the
whole cycle without touching the keyboard. `ctrl+g` does the same thing from the keyboard
and still works either way. Under the pointer the chevron brightens further and its line
takes a background, which is how everything pressable on this screen says so. On a terminal
that cannot draw them the two chevrons are `>` and `<`.

One cell above the closed edge's handle carries what the work is doing while there is
anything to carry — `?` in the question colour for a task waiting on you, `◐` in the accent
for something running, and nothing at all otherwise. Those keep their own colours; they are
about the work, not about the door. The edge costs the conversation its two columns, so the
text re-wraps around it and nothing is ever drawn underneath. Under 100 columns there is no
edge, because at that width there is no column to bring back.

## Light terminals, and why there is no theme setting

There are two fully authored palettes. The dark one is invisible on a white page, so a
light one exists with the same law and the opposite move: what carried by being lighter
than the background now carries by being darker. Ink darkens, accent darkens, and **dim
goes lighter** — the meta tier recedes toward the page. The question hue is inverted
rather than dimmed, because a question has to lead on a page too. No two roles resolve
to the same 256 index.

**Which of the two you get is detected, not configured. There is no theme setting to
change.** The best answer comes from the terminal itself: on the first frame aforge asks
what colour its background is, and a terminal that answers picks the ladder outright —
a light background gets the light palette, whatever anything else says. See *How the
colors tune themselves to your terminal's background*.

Where the terminal does not answer, `COLORFGBG` is read instead: its last field is taken
as an ANSI index, where 0–6 and 8 mean dark and everything else means light. **Unset
means dark**, which is what most terminals are.

The code can accept `dark`, `light` or auto, but the settings row that would carry your
choice does not exist yet. It is a stated seam, one call away from being wired, and until
it is wired the answer comes from your terminal and its environment. A pin, when it
lands, will outrank the terminal's own answer.

## How the colors tune themselves to your terminal's background

**aforge asks your terminal what colour its background is, and tunes the palette to the
answer.** The question goes out on the first frame and nothing waits for it. If your
terminal answers — and many do — the reply arrives like any other event and the screen
repaints in colours measured against your real background rather than an assumed one.

Four things are re-aimed when it lands:

- **The three background bands** — the row under the pointer, the chosen row, and a
  copy-mode selection — are built out of your own background colour, moved away from
  itself by a fixed amount. They inherit your terminal's tint, and on a 256-colour
  terminal they still land on greys, never on a hue.
- **The reading tiers** — ink, muted, dim — are checked against the real background and
  moved only if they are outside the range they were aimed at. **A value already in range
  is left exactly as authored**, so on an ordinary terminal you will not see a
  difference. On a pure black screen the body steps back from the glare it had; on a
  tinted page the tiers that were sliding out of sight come back. **The answers
  themselves move with the body ink**, and so does the brighter step a streaming reply
  wears: the growing edge stays one step above the settled text.
- **The role colours** — accent, add, del, bad, warn, the question hue and the datum
  cyan — are checked for legibility and, if the background has crowded them, all of them
  move together by the same step, never one on its own.
- **The light or dark ladder itself**, which is the exact answer described above.

**A terminal that does not report a background gets the built-in palette, and that is
not a degraded mode.** It is the same palette every earlier version shipped, authored by
hand against the range real terminals sit in. There is no timer, no waiting and no
fallback path to take: silence simply means the built-in colours stand. Pipes,
recordings and terminals without the query all take that road, and so does every frame
drawn before a reply arrives.

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
reader loses nothing), the **chosen row's background** — the model in use, the
conversation you are in, the room you are standing in — and the **copy span's**. Those
are facts about the session rather than about a pointer, and they are true whoever is
reading. What is dropped is the quieter background the pointer and the cursor share.

## Two other things that move on screen

**The pulsing ellipsis, and the `still working` fallback.** When a turn is running and
nothing else on screen is moving, two spaces then a pulsing ellipsis cycles `·` → `··` →
`···` in accent, about 300ms a step. It is suppressed entirely while text is actively
streaming, while any tool call is spinning, and while a sub-harness run has a step on the
row under it (see *Saved shapes of work*) — two answers to "is this alive?" is one too
many.

Beside it, aforge says as much about the wait as it honestly can, and **the most specific
of three answers wins**:

```
  ··· thinking · 12s · friendli 38 t/s   the connection's own account of itself
  ··· waiting for kimi-k3 · 12s          a request is out, and that is all anyone knows
  ··· still working                      the stream has simply gone quiet
```

` · still working` is appended in dim after the stream has said nothing for **10
seconds**, and it is the **last** of the three rather than the normal state: it is what is
left when neither of the lines above knows anything. It says "still working" and never
"trying again" — a silence is only a silence to this suffix, and the words change to
`trying again` solely when the request really was cut and re-sent, which is said outright.

**The compaction mark.** A compaction is drawn while it runs and left as a rule once it
lands, so the conversation never silently loses its middle. Running, it reads
`⠙ compacting ~84k tokens · 6s` — a braille spinner on the same grid as the tool
spinners, dim, with a count-up. Settled, it becomes a centred rule:
`───── ⚭ compacted from ~84k tokens · took 6s ─────`. The duration is dropped under one
second. It is never painted the question hue, because nobody is being asked anything.

## What is it doing right now — connecting, first word, thinking, writing, paced, trying again

While a turn is running, the working line and the status line say what the connection to
the model is **actually** doing, in one of a small set of words. They are reported by the
layer holding the wire, never guessed by the screen:

| The word | What is happening |
|---|---|
| `connecting` | the handshake — nothing has been accepted yet |
| `first word` | the request was accepted and nothing has been written back: a queue, a cold model, or the router still walking its own endpoints |
| `thinking` | the model **is** writing, and none of it is answer — it is reasoning, which is billed and streamed and shows nothing |
| `writing` | the answer itself is arriving |
| `paced` | the provider asked aforge to slow down, and it is waiting |
| `trying again` | the same question is being asked again with one field dropped from it |
| `switching` | a second machine is being asked the same question while the first is still live |

They read like this, with a clock counting up from the moment that phase began:

```
  ··· connecting · 1.2s
  ··· first word · 3.1s → parasail at 4.4s
  ··· thinking · 12s · friendli 38 t/s
  ··· writing · 4s · friendli 61 t/s
  ··· paced · retry in 6s
  ··· trying again · 2 of 6
  ··· stalled 9s · switching to parasail
```

**Every part is dropped when it is not known** — no machine name, no machine name; no
measured rate, no rate. The two waiting words, `connecting` and `first word`, are read in
tenths, because the difference between 1.2s and 3.1s is the whole of what those seconds
tell you; everything else is read in whole seconds.

The same words also ride the wait line above the box, where they take the place of the
`via <machine>` reading for as long as the turn is running (see *Models, context, and
what it costs*).

**A turn also has waits of its own, between requests**, and they use the same line and the
same clock:

| The word | What is happening |
|---|---|
| `running <tool>` | one call on the belt is executing |
| `checking` | a reader is deciding whether the answer finished the ask, or whether it should have been work |
| `taking stock` | the work stopped mid-round and a second model is being shown an account of it and asked what is left of what you asked for — ten to thirty seconds |
| `tidying` | the conversation is being compacted |
| `briefing a worker` | your turn is being handed to a task, and the instruction it opens on is being written — fifteen to thirty seconds is normal (see *How tasks run*) |

Each of them is taken off the screen the moment the wait ends.

## A stage that lasts minutes keeps drawing — the phase went blank, the status line disappeared while it was still working, does a slow stage stop being shown

**No stage ever goes dark while the work behind it is alive**, however many minutes it
lasts. A phase says
itself again while it lasts — a request off its own stream, a turn's own stage off a timer
— and the screen keeps drawing one it has heard from in the last fifteen seconds. A reading
that takes a quarter of an hour draws a clock for the whole quarter of an hour.

That fifteen seconds is the one thing that can take a line off the screen early, and it is
deliberate: it is what stops a clock running forever when the layer behind it was killed
without saying so. If a phase disappears and the work has **not** finished, what you are
looking at is a layer that stopped reporting, and `still working` — the vaguest true
sentence aforge has — is what takes its place.

This was not always true. Long stages used to be drawn for fifteen seconds and then vanish
while they carried on, and one of them worked around it by announcing itself twice. Neither
is the case now: every stage is said once and kept alive until it ends.

## It says thinking and nothing is on the screen — is it stuck, is it frozen, why is it slow, and what still working means

`thinking` means the model is writing and none of what it writes is for you. Reasoning
models produce a run of thought before the answer, on the same connection, billed the same
way, and on a big conversation it can run for a minute before a word of answer appears.
Nothing is wrong. The clock beside the word counts up, so a number that is moving is a
program that is alive and painting; a clock that has **stopped** is the thing to worry
about. `esc` interrupts at any point.

`first word` is the other slow one, and it means something different: aforge's request was
accepted and the endpoint has written nothing at all — a queue, a cold model loading, or a
router still choosing between its own machines.

**`still working` is not the normal state.** It is the vaguest true sentence aforge has,
and it appears only when the two more specific lines know nothing: a phase that stopped
being refreshed, a request that has already returned, a build with nothing reporting. If
you are reading it, the layers that would say more have nothing to say.

None of these lines ever claims the network is slow or the model is confused. The screen
sees only what it is told about the wire, and it does not pretend otherwise.

## How long until it gives up on this one — the countdown after the arrow, and when there is none

Sometimes the waiting line carries an arrow:

```
  ··· first word · 3.1s → parasail at 4.4s
```

Read it as: **this request has been waiting 3.1 seconds, and if it has not started
answering by 4.4 seconds, aforge asks parasail the same question as well.** Both figures
are seconds since the phase began, on one ruler, so the gap is readable without
arithmetic. The moment is not a round number somebody picked: it is the deadline aforge
already worked out from what it believes about the machine answering you, and it lands
between **0.7 and 8 seconds**.

`paced · retry in 6s` is the same shape of promise for a rate limit, and those six seconds
are the provider's own `Retry-After` rather than anything aforge chose.

**No arrow is drawn unless both halves are real** — a moment, and something that really
happens at it. So there is no countdown when there is no other machine to go to: a
conversation pinned to one lane, `routing` set to `off`, an endpoint that is not a router,
or a model with only one machine behind it. You still get the phase and the count-up,
which are true, and no promise, which is the point — aforge would rather show you nothing
than a countdown that expires and does nothing.

Turning the **speed guard** off stops the second request being bought at all; see *Models,
context, and what it costs*.

## Why the reply is slow to start, why it says "waiting for" a model, and whether it is stuck

Between you pressing enter and the model's first word there is a gap, and it is sometimes
long — twenty seconds, a minute. A pulsing ellipsis claims exactly as much at second one
as at second fifty, so past a few seconds it starts saying what it is waiting on.

**This line is the second-best answer.** Where the connection itself is reporting — which
is most of the time on a router — you get the phase instead: `first word · 3.1s`,
`thinking · 12s · friendli 38 t/s`. See "what is it doing" above. The `waiting for` line
below is what is drawn when nothing on the wire has said anything at all.

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
stream has said nothing since. It never says "the network is slow" or "the model is
thinking" — this screen cannot see the wire and does not pretend to. So
`waiting for kimi-k3 · 47s` is not a report that anything is broken. It is aforge saying
it is still there and still waiting, which is the one thing a bare ellipsis could not tell
you apart from a hung program.

**Is it stuck? Is it frozen?** A clock that is counting up means the program is alive and
painting; a clock that has stopped means it is not. `esc` interrupts the turn at any point.

**It never runs under a tool call.** A tool that is executing has its own spinner and its
own count-up, and the ellipsis stands down for it entirely. This clock is only for the
window between a request going out and the stream first speaking, so after a three-minute
`go test` the request that follows starts the clock at zero rather than inheriting the
call's runtime.

## Does it ever ask a second time in parallel, and does that spend twice

Yes, once, and only when a slow answer can be moved to a **different machine**. The status
line spells it:

```
  stalled 9s · switching to coreweave
```

One model id is served by many machines. When the one answering goes quiet — before the
first word, or in the middle of its thinking, or mid-answer — aforge sends the same
question to another one of them and lets the two race. Whichever writes first owns the
reply and the other is cancelled, which is what stops the bill. Anything the loser wrote
while the race was undecided is thrown away, so its text, its thought and its half-formed
tool calls never reach the screen or the conversation. If text was already on the screen
when the switch happened, a line says so:

```
  that lane went quiet — this answer is coming from another one
```

The moment it acts at is not a fixed number of seconds. It is worked out per request from
what the machine answering is believed to do, and it sits between **0.7 and 8 seconds** —
see "how long until it gives up on this one" above for the countdown the status line draws
while it is waiting.

**There is no second request that asks the SAME machine again in parallel.** aforge used to
do that on a flat eight-second wait, with no machine named and no budget, and it is gone:
one silence now has one answer. What is left, and is a different thing, is *retrying* —
asking again after a request has been cut or refused, one at a time. See the next section.

Only work somebody is reading is rescued this way, and it is paid for out of a budget: at
most a couple of rescues in any twenty requests, and never more than about a tenth of what
the session has spent. Turning the speed guard off in settings sets that budget to nothing,
and then a slow answer is simply waited out — the status line still says what is happening
and how long it has been, and draws no countdown, because nothing is going to happen at
the end of one.

## Why did the reply restart, what does "trying again" mean, and where did the text that was on screen go

Sometimes the wait line stops naming a model and reads this instead:

```
  ··· trying again · 12s
```

That is the session asking the model again, and it is the one thing this line ever says
that it did not work out for itself — it is reported, never guessed. It is asked one at a
time: the rescue in the section above is the only thing that ever puts two requests on the
wire at once, and it asks a different machine rather than the same one. After a stream
starts, two things get its request cut and replaced: the model stopped writing
(see *Models, context, and what it costs* for the exact clocks), or the reply came apart
into repetition or jumbled text. A dim line lands in the conversation saying which:

```
  nothing came back from the model — asking again
  the model went quiet mid-reply — asking again
  the reply lost its thread — that text was dropped, asking again
```

The model is **not** named on `trying again`. The name was on the line that was just cut,
and the retry still asks that same model. When the router identified an endpoint that went
quiet, the retry avoids that endpoint and may reach another one serving the model. There is
no grace on this one either — the plain wait hides its clock for four seconds, and this
appears at once, because you have just watched something disappear and are owed the reason.

**The one dim line that does name a model** is the last of them: when asking again has run
out, aforge finishes the reply on a different model, and that is said before it happens.

```
  nothing kept coming back from the model — finishing this one on openai/gpt-5-mini
  the model kept going quiet mid-reply — finishing this one on openai/gpt-5-mini
  the reply kept losing its thread — finishing this one on openai/gpt-5-mini
```

The rest of the answer arrives from that model, at that model's price, and the wait line
above it names it from then on. Your own model is unchanged and your next message goes back
to it. See *Models, context, and what it costs* for which model it moves to and when.

**Where the text went after a cut.** If the reply had started, what you were reading is
**removed from the screen**, and it is removed because it was removed everywhere: none of
it is in the conversation, none of it is in the session file, and none of it is sent back
to the model on the retry. Any tool call that was still arriving when the cut happened
stops where it is and keeps its row. A rescue is different: only one of the two machines is
ever the one you are hearing, so when it wins nothing is taken away, and when the other one
wins you are told in a line of its own that the answer changed machines.

This is the one place aforge takes something off the page that you watched arrive, and the
difference from an interrupt is exactly that. When **you** press `esc`, the half-written
reply is kept — it is real work you watched happen, and it stays in the conversation. When
the session cuts a request, nothing of that attempt exists anywhere, so leaving it on
screen would show you an answer the model never gave and will never read.

**Anything you typed is untouched.** A message you sent while the reply was coming is held
above the message box exactly as before (see *Keys, typing, and the mouse*); the retry has
no opinion about it.

## aforge's own lines, and why the same answer is not repeated when you ask twice

Some lines in the conversation are not the model's. They lead with a dim `· ` and are
aforge answering you directly: the tables `/status`, `/cost` and `/help` print, an
`exported · …` receipt, a refusal, and the one-line answers a command gives when there is
nothing for it to open — `nothing made yet.` from `/files`, or
`no subharnesses here yet — a subharness is a saved program for work that comes round
again.` from `/subharness`. None of them is ever sent to the model.

The **seven places** are not among them: `/standing`, `/history` and `/memory` open their
page whatever is in it and let the page say so, rather than writing a line here (the Places
page states the law).

**The same line twice running is one line.** Press a command four times because the first
press looked like it did nothing, and you get one copy of its answer rather than four
stacked identical lines; the conversation scrolls back down to the line that is already
there. If anything at all lands in between — a reply, a tool call, a different line of
aforge's own — the answer is written again, in its new place.

## I typed something while it was working and it disappeared — the `└` correction stays where I said it

A sentence you send while an answer is still being worked on is **part of that same
question**, not a new one. It is drawn where you sent it, between the work already shown
and the work that follows:

```
› port the parser to the new lexer
  ├─▶ read lexer.go
└ use the staging bucket, not production
  ╰─▶ read parse.go
```

The `└` is furniture, drawn dim like every other mark aforge uses about its own
structure. The words after it are yours, painted in your `narr` ink tier. The row is
flush left in your column, while tool rows stay padded two columns in. The dim `└` says
this continues the same question rather than opening a new one. Reloading the
conversation keeps the row in the same journal order.

A correction wider than the frame wraps onto the next row, hung under its own first
character rather than being cut. A file path inside one is a link, exactly as a path in
any message of yours is.

On a terminal with no box-drawing glyphs, and in the plain screen-reader mode, the `└`
is drawn as `+`.

## What `steering` or `stopped the reply here` next to my correction means — where did the steer land?

A correction does not reach the model the instant you send it. It is handed over at the
running turn's next step — the moment between one batch of tool results and the next
request — so there is a gap, and the row says which side of it you are on. While it
waits, a dim clause sits on the end of the row after a `·`, with the spinner every live
row on this surface turns:

```
└ use the staging bucket, not production · ⠹ stopped the reply here
```

The clause is aforge's own account of what it did to make the next boundary for your
words, and it is one of:

| Clause | What it means |
|---|---|
| `stopped the reply here` | the reply that was streaming was cancelled for you, and the text it had already sent is kept |
| `stopped the running command` | your words plainly told a long-running command to stop, and it was stopped |
| `kept bash running as job 3`, or `kept bash running as jobs 3, 4` | a bash call running for more than 3 seconds was moved to the background so your correction could land now |
| `waiting for the running step` | a short tool is being allowed to finish first |
| `steering` | the plain working word, used when aforge sent no account at all |

On a narrow frame the clause goes on a row of its own under the sentence rather than
being cut.

The clause means **the model has not been given these words yet**. It comes off the row
the moment it actually has. Nothing on this surface claims your correction landed before
it did.

When it lands, the words light up for a moment and settle back down on their own: full
ink for the first 4 seconds, the calmer tier until 10, and the quiet resting tier after
that. No glyph is added and none taken away. From then on it is the row's **position** —
between the work before it and the work after it — that says where the correction went.

A conversation opened from disk draws its corrections already settled — a correction made
an hour ago is a fact and not news, so it never flashes on reload.

## Several corrections on one question — each one stays where you said it

A steer is an ordinary user message inside the turn that is already running, so several
corrections stay several rows, each in the place it was sent, in the order the model read
them:

```
› port the parser to the new lexer
  ├─▶ read lexer.go
└ and skip the cache while you are in there
  ╰─▶ read parse.go
└ actually leave the cache alone entirely
```

They are never gathered up under the question, and there is no `…2 more steers` fold to
open: that line belonged to the older shape, where corrections hung under the question
rather than standing where they were said. Folding a finished turn's work into its
`▸ worked · …` chip does not hide them either — a turn collapsed to one line still reads
back as everything you asked for.

## Where did my correction go — it arrived after the answer had finished

A turn whose last request has already gone out has no next step left, so a correction
sent in the last seconds of one can miss it. It is never dropped and it is never
pretended about. It **leaves the transcript at that point** — it was not part of that
turn, and a row left standing in the middle of it would say the model had read something
it never saw, and would say it twice once the words come back as a question — and aforge
says so in the dim line it uses for everything it says on its own account:

```
· your correction came after the answer finished — asking it as a new question
```

The words then start a turn of their own, and appear in the conversation as an ordinary
message of yours with the usual `›`. Nothing is retyped and nothing is lost.

The one exception is a turn **you stopped**. Pressing stop stops everything you had said
to that turn, corrections included, so a correction that had not reached the model when
you pressed it goes with the turn. Its row leaves the page the moment the turn ends, and
nothing new is drawn after the stop — which is what stop means everywhere in aforge.

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

Everything you can press answers the pointer before you press it, and nothing else
reacts. That is the whole rule: **if it lights, clicking it does something.**

**Things that own a whole row take a background band** across the width — a tool call and
its expansion, the `N earlier tool calls` fold, a thinking block, a proposal or a landed
card, a sign-in still waiting for the browser, a roster row, a message parked above the
box, a row of any open list or page, the task record card's title
and its keys line, and either row of the phone status deck.

**Things that share a line light only their own cells.** A segment of the top bar's
crumb, a
picture on the tray above the box, one of the four answers on a landed card, one of the
two answers on the stop card, a chip or a link on an adaptive run's page: the one under
the pointer lights and its neighbours stay dark. The gap between two chips lights nothing
— it is a place to miss, not a door.

**The tab bar of the places is one of them.** Each place's word is its own chip, clicking
it goes there, and the gap between two words is a place to miss. Under the bar, every
place answers the pointer the same way: the row you are hovering takes the band, and the
wheel walks the list three rows a turn.

**A few words inside a sentence brighten instead.** A task reference in a reply goes from
accent to ink and keeps its underline; a cut table's foot, the jump-to-latest chip, and
every door on the top bar and the status row go one step up. A highlighted rectangle
mid-paragraph would be the one boxed thing on a surface with no boxes.

**On the top bar each door lights over its own span**, never the whole row: the project
step, the conversation's step, the model's name, `esc/← back` and the `✕`. The step is one
rung — to the accent in a conversation, to ink inside a room, where the bar is already
accent. **`YOLO` is the only exception on the bar**: it keeps the bad hue and takes a
background band round its four cells instead, because lifting it would paint an open gate
in the hue of something safe.

**On the status row four figures are doors and each brightens over its own cells**:
`◦ keeping an eye on N`, `N open · M want you`, the context percent, and the money figure.
The rest of the row is figures, not controls, and does not react.

**The pointer and the cursor share one background; the chosen thing gets the louder
one.** Whether you reached a row with the mouse or with `↓`, the row you are on looks the
same — it does not change appearance depending on which hand you used. What tells the two
apart is the mark in front: `›` where enter would act, `·` where the pointer is.

The step above that is for the thing you have actually **chosen**, and it stays drawn
when nobody is touching the list: the roster row of the room you are
standing in, the model in use in `/model`, the conversation you are in on home and in
`/resume`, the crew in force in `/crew`, the tab you are on in `/settings`. Both can be
on screen at once — that is what two steps are for — and the roster is where you will
see it: the room you walked into on the louder ground, the row `↑↓` has reached on the
quieter one. Where a cursor lands on the chosen row itself, the louder ground wins, so a
row never gets quieter for being arrived at, and the `›` still says where enter is aimed.

Nothing else lights: empty space, a paragraph, a dim telemetry line, the hint beside a
picked harness, the body of the task record card, and the phone's tool detail sheet,
which has no pointer targets at all.

There is no hover at all in the screen-reader tier. Terminals below ANSI256 get no hover
background either, because there is no weight that means "under the pointer" — the ink
steps above still show there.

The mouse is aforge's by default for the whole session, which is what makes the click
targets on this screen work. `ctrl+s` hands the pointer back to the terminal so you can
drag-to-select with it, and takes those targets away until you take the mouse back.

## The terminal tab and window title — why my tab is renamed after my project

aforge sets the terminal's window title, which is what your terminal shows on the tab,
in the cmd-tab switcher, and in a tmux or screen window name. It says which aforge this
is: the project folder first, then the conversation's own name once it has one, joined
with a dot — `myproject · porting the parser`. Before the conversation names itself the
tab is just the project, and with no workspace at all it says `openaf`.

The project comes first on purpose: tabs truncate from the right, so when the bar is
narrow the part that tells your aforge windows apart is the part that survives. The
title only changes when a fact changes — a conversation naming itself, a question
coming up, you switching conversations — never on a clock, so an idle window's tab
never flickers.

There is no setting to turn this off. If your tmux windows keep their own names, that
is tmux's `allow-rename` setting refusing outside renames, and aforge respects the
refusal by simply being refused. When aforge exits, your shell's next prompt sets the
title back the way your shell normally does.

## The ? and ✓ on the terminal tab — does it need me, did something finish while I was away

The tab can carry one glyph ahead of the name, from the same vocabulary the rest of the
surface uses:

- `?` — something is waiting on you: a permission question in this conversation, or in
  any conversation this window is keeping in the background. It stays until the
  question is answered. This is the one to come back for.
- `✓` — a turn finished while you were looking at another window, and you have not been
  back since. Clicking back into the window clears it; the answer itself is on screen.

A question outranks a tick: if both are true you see `?`. While work is simply running
there is no glyph and no spinner in the tab — a window you walked away from is assumed
to be working, and the tab only speaks when something changed that is worth a glance
from outside. No glyph at all means nothing is waiting and nothing landed unseen.

In the screen-reader tier the same two facts are spelled `!` and `+`. The glyphs match
the home screen's rows, so a `?` on a tab and a `?` on home are the same statement
about the same conversation.

## The dim thought row above a reply — and models that think between their words

Some models put their working on the wire. While it streams you see a dim three-line
window under a `thinking · N tok` header; the moment the first word of the reply lands it
collapses to one row — `thought for 6s · 148 tok · ctrl+e` — and `ctrl+e` or a click
reopens it. The count of tokens on the row is how much working the model wrote, and the
seconds are how long it spent.

Some models keep thinking in between the words of their own answer, a few tokens at a
time. That does not split the reply and does not stack up extra rows: the one thought row
above the answer keeps its place and its numbers grow, while the reply below streams on
unbroken. A new thought row only appears after a real boundary — a tool call, or the next
turn — because that is a genuinely new stretch of thinking.

Thinking is never saved into the conversation's record. A reopened session shows the
answers, not the working behind them — so a `thought for` row you can see now will not be
there after a restart, and that is deliberate.
