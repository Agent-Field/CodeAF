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

aforge draws one screen in a fixed order every frame. From the top: the row of
conversation tabs, the pinned room header (only while a task room is open), the task
strip, the conversation, a breathing
gap, the rule with the legend in it, the approval question, the connect offer, the
sub-harness offer, the steer guard, the follow-up row, any message waiting for the
answer to finish, another gap, the tray row above the box, the draft box where you type,
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
reads `ctrl+. earlier` and opens the task page. With no foreground command that can be
kept, `ctrl+g` closes the column and opens it again, remembered between sessions, and the
column's own last line says so: `❯ ctrl+g hide`. While a command can be kept, that
command takes the key and the column stays where it was. With the column closed the
conversation is laid out at the full width of the terminal, running work still draws
the strip along the top, and the legend's hint slot reads `ctrl+g tasks` once the
session has tasks to come back to and no running-turn line owns that slot.

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

## Reading padding — text against the left edge

Conversation text and task transcripts have a two-cell left gutter where the body
column has room. On very narrow layouts the gutter collapses to preserve reading
width. Links and buttons move with their text. Copying removes the layout gutter
while preserving the content's indentation.

## Conversation tabs — switching conversations by clicking, the tab strip over a chat, clicking a chat name

**The header begins with Home and the conversations this window has been in**, drawn as
tabs in a row of their own, with a thin rule separating navigation from reading:

```
  Home    openrouter price scrape    Refactor the rail sco…    [Shipping the parser] ×  +  Chats ▾
  ────────────────────────────────────────────────────────────────────────────────────────
```

Each tab is a **padded target** separated by quiet space. The filled surface includes
one blank cell before its status icon and after its close mark; the leading inset
selects the tab and the trailing inset belongs to the close target. The gaps do nothing.
Every tab has a filled background. The active tab reverses the surface contrast
and has stronger text; brackets identify it on
terminals without background color. **Every tab reacts to the pointer**,
including the one you are already in, and the highlight it wears as the *chosen* tab stays
put when the pointer leaves. Without color, hovering adds a dot beside the tab’s
close mark; Home, `+`, and the scroll arrows gain a pointer dot, and Chats changes to uppercase.

**Clicking a tab goes to that conversation** — the same switch `ctrl+k` makes. Clicking the
tab you are already in does nothing while you are in the conversation itself, and takes you
back out to it from a task page.

## Many open conversation tabs — horizontal scrolling, overflow, and readable names

**The order never changes as you switch.** Tabs sit in the order this window first entered
them, so the one you reached for a minute ago is still in the same place. At most 32 are
remembered; past that the one you have not been in for longest falls off, and the tab you
are in never does. This is a presentation limit, not a limit on running work or history.

**Many tabs scroll horizontally instead of shrinking their names.** On a roomy strip,
`‹` and `›` appear at the edges when more tabs exist in that direction (`<` and `>` in
ASCII). Click an arrow, or wheel vertically or horizontally over the header, to browse
the names. This changes neither the conversation, its draft nor the transcript position.
The selected tab may leave view while you browse; choosing a conversation or closing a
tab brings the selection back. Narrow frames keep the selected tab and `Chats` fallback
without spending its name on arrows.

**`+` and `Chats ▾` follow the last visible tab**, with small gaps between their
targets. They stay beside a short row of tabs; when the row fills, the tabs scroll
and the controls remain at its edge. **`Chats ▾` opens the switcher** — the same card `ctrl+k` opens, with
every conversation on this machine in it, its fold already open. Where the row is too
narrow for every tab the control reads `Chats +3 ▾`, counting the tabs that did not fit.
On a narrower frame it drops the `▾`, then the count, and keeps the word `Chats`: the
word is what says it is a door. Where the switcher cannot open at all, the count is drawn
alone (`+3`) and does nothing, because it is still true.

## What stays alive when switching tabs — running two or three chats at the same time, saved drafts, and Home

**Switching tabs stops nothing.** Every conversation this window holds keeps running
while you are somewhere else — its turn finishes, its tasks go on, its output accumulates
and is all there when you come back. That is true over the ordinary socket onto this
machine's engine, over `--host`, over `--at` and with `aforge chat --no-host`: each tab
holds its own connection and its own conversation, so nothing you do to one reaches
another. Going Home stops nothing either, and neither does opening a fourth chat.

The one door that cannot do this is one that has no way to dial a second connection. There
the surface says `closed · <name> — a connection holds one conversation at a time` as it
switches, so you are never told work continued when it did not. No door shipped today is
in that state.

**Your unsent words and caret are kept either way.** A half-written message goes down under
the conversation it was written for and comes back when you return to it, including on a
door that had to close the conversation to leave it.

**It stands down on a small frame** — under 12 columns wide, or on a terminal too short
for a blank row above the message box — for the same reason the room header does. The
rule under the tabs goes first, on a terminal shorter than twenty rows: it is a seam, and
a seam is the cheapest thing on the frame to give up.

On frames at least 48 columns wide, a blank row above the tabs appears at 32 rows,
one below at 36 rows, and one after task metadata at 40 rows. These separate steps
keep the reading area from shrinking as the window grows. Smaller terminals collapse
the vertical padding. Blank tab rows and gaps cannot activate the content beneath them.

**Home at the left opens the home page**, keeping your conversation and unsent words.
It is separate from the tabs and breadcrumbs. Space twice on an empty composer still
opens Home. Home disappears when the connection cannot open conversations, and on
very narrow frames the current tab takes priority.

The switcher floats on a separate background inside a rounded outline, with space
above and below its contents when the window is tall enough. `>` marks the keyboard
choice; the pointer has a separate dot, so hovering another conversation does not
change what Enter opens. Without color both markers remain visible; ASCII mode uses
straight corners and a plain dot. Short windows give up inner vertical space before
the selected row.

## Why does my tab say Untitled — when does a chat get its name, my new chat has no title, the tab says Untitled instead of the conversation name

**Naming starts when your first message is accepted.** The small model on the `title`
role works in the background alongside the answer. Each naming ask has twenty seconds to
reach an answer or its existing fallback. The answer does not wait for a title, and the
title does not wait for the answer to finish.

1. `+` opens the `New chat` page. A newly created conversation starts as `Untitled`.
2. Sending your first message starts both the conversation and background naming.
3. One response supplies a full conversation title and a one- or two-word tab label. The tab strip
   uses the compact label; breadcrumbs, the status line, Home, the switcher, recent sessions,
   and the terminal window title keep the full title. This also works after the answer has
   finished or you have switched to another tab.

Hover over a tab to reveal its full title beneath it. Long titles wrap; the tab and
conversation stay in place, and the preview disappears when the pointer leaves.

**Temporary failures and unusable names retry automatically.** There are up to three naming attempts, with
short increasing delays, within a two-minute overall window. You do not need to send
another message. A failed title never interrupts the answer or changes its working state.
Empty answers, instruction echoes and placeholders are rejected, and the next configured
naming model can answer within the same budget. If those attempts fail, the tab remains
`Untitled`; an unnamed saved conversation can try again on its next message after reopening.

**An existing name wins.** Naming runs once per session lifetime, and a chat that already
has a name is not named again. Older saved names with a leaked `Full:` label are cleaned
when read, and saved tab labels are limited to two words. Closing the session cancels unfinished naming. There is no
command or tab action to rename a conversation manually.

**`Untitled` labels an unnamed tab and its breadcrumb root.** Elsewhere it is named
after the folder it is in: the status line and the window title say the project, home and
the `ctrl+k` switcher say `new conversation`. And `Untitled` is not `main` — `main` is
where you are, the conversation you get back to from a task page, which is what `esc/←
main` and `say it to main` both mean.

## Closing a tab — the × on a tab, Ctrl+W, where do I go next

**`×` or `ctrl+w` closes the tab in front.** Closing another tab leaves the
current chat selected. Closing the current tab selects the most recently used
remaining tab; only closing the last tab takes you to Home. A destination that
cannot be opened leaves the current tab and draft in place and explains why.

The close mark appears on the selected tab and on a hovered tab. Its padded
cells stay reserved on every tab, so hovering cannot move the targets. The close
cells dismiss; the label beside them selects. With color disabled the newly
visible `×` also identifies pointer hover.

Unsent drafts, carets and attachments stay with their conversation. Reopen a
closed tab from Chats, Home or `ctrl+shift+t` to retrieve them. The keyboard
shortcut needs a terminal that distinguishes Ctrl+Shift+T from Ctrl+T.

**A closed tab's conversation keeps running unless you asked for it to stop**, over
every door — the ordinary socket, `--host`, `--at` and `--no-host` alike. Selecting the
next tab replaces nothing: that chat has its own connection and carries on. Closing the
last tab to Home leaves its connection in place.

**Quitting is a different act from closing a tab.** `ctrl+w` and `×` take a view off the
row, and a working one is answered by the card below. `/quit` and `ctrl+c` end the whole
program, ask
their own question about work in flight, and act on every conversation this window holds
at once. Closing a tab never quits aforge, and quitting is not what any of the card's
three answers does.

On the switcher card, `ctrl+w` dismisses a selected background tab while keeping
its work running. For the current conversation it uses the same close card when
work is active. On New chat it closes
the start page and parks its unfinished first message. Stop on a task page
ends that task; `/quit` ends the program.

## Keep running, stop work or cancel — closing a tab on a chat that is still working

A tool permission question does not trap you in its tab. `ctrl+w` offers the same
close actions while leaving the question unanswered; `ctrl+k` opens Chats and
`ctrl+t` opens another chat. A hidden chat waiting on your answer is marked
with `?`; Chats says `asking you something`. Reopen it to answer the original question. Cancel on the close card
leaves both the tab and permission untouched; `stop work` cancels that reply.


**Closing a tab with work in it asks first.** A card appears above the message box
naming that conversation and what it is doing — `Close this tab? the tree walk is
working · 2 tasks running` — with three answers under it and a dim line saying what the
answer the cursor is on will actually do:

```
  ? Close this tab? the tree walk is working · 2 tasks running
    ▌[keep running]   [stop work]   [cancel]
    it keeps going here; find it under Chats, and ctrl+shift+t brings the tab back
```

| Answer | What it does |
| --- | --- |
| `keep running` | The tab goes; the conversation does not. It keeps writing, its tasks keep running, and you find it again under `Chats`, on Home, or with `ctrl+shift+t`. Reopening it shows everything it did while it was out of sight — the same conversation, not a second run of it |
| `stop work` | Ends the turn and cancels this conversation’s queued/running tasks, adaptive runs and jobs, then closes the tab. Nothing in any other chat is touched, and nothing is deleted |
| `cancel` | Nothing happens. The tab stays, the work stays, your draft stays |

Cancellation news cannot start another reply after `stop work`. A fresh message
starts work again once cancellation finishes. If work is still stopping, the message
is refused with “this conversation is stopping; wait for its work to finish stopping before sending a new message”.
Keeping or reopening a tab does not restart stopped work.

Chats marks a hidden reply or background job `working` even when it has no tasks. A reply that
finishes while held there says `it finished while you were away`.

Narrow terminals shorten the answers to `keep`, `stop`, and `cancel`; the smallest
frames show their keys `k`, `s`, and `esc`. Each visible answer remains clickable.

The cursor opens on **keep running**, so `enter` is the safe answer. `←` and `→` walk the
three and stop at the ends rather than wrapping; `k` keeps running, `esc` is `cancel`, `s` is `stop work`,
and clicking an answer takes it. `ctrl+c` puts the card away and goes on to do what it
normally does — leaving is never something you get stuck inside.

**The difference between `keep running` and `stop work` is only whether that chat is
still working afterwards** — both take the tab off the row, and neither deletes anything.
`stop work` is how you stop the work in one chat and leave every other chat alone.

**An idle tab closes with no card**, because there is nothing to decide; the card is
raised only for a chat writing a reply, running tasks or holding a question. It does not
go away when that finishes underneath it, so an answer arriving a moment before your press
cannot turn `stop work` into a press that lands on nothing.

The selected answer is marked `▌` — `>` where there are no box characters. Where the frame
is too narrow for all three the row is cut at the right and the keys still answer it;
under four columns the card draws nothing.


Reopening a running chat through this machine’s engine restores the reply so far
and follows its live output without sending your message again. Background
status and the open reply each have their own reader. If the connection drops,
that live view ends; reopen the chat after reconnecting to recover its current
record and live output. This does not promise that a hidden tab’s status stays
live across a broken connection.

## Starting a new chat with Ctrl+T or the `+` plus button beside the tabs

**`ctrl+t` or `+` at the tab strip opens a start page. It does not create anything.** No session, no
agent, no file, nothing on the switcher — pressing it three times and escaping three times
leaves you exactly where you began. The conversation is made when you **submit the first message or command**.

The page is the launch screen drawn inside the frame you are already in: the wordmark, the
model and crew line, a blank message box with the caret in it, and this project's recent
conversations under it. A selected **New chat** tab labels this page. The other chat tabs remain available, with overflow in Chats. The footer belongs to the start page and shows no previous conversation costs. The previous chat’s sidebar and compact task strip are hidden.

| Key or click | What it does |
| --- | --- |
| type, then `enter` | Starts the conversation and sends that as its first message |
| `esc` | Cancels — back to the chat or task page you pressed `+` from, with your draft |
| `↑` / `↓` | Walk the recent conversations, while the box is empty |
| `enter` on a chosen row | Opens that conversation. It sends nothing |
| click a recent row | The same |
| `ctrl+t` or `+` again | Reuses the page you already have |

**A picture on its own is a message.** Drop or paste one and press `enter` with nothing
typed and the conversation starts on the picture.

## What happens to your draft when you press `+`

**Nothing. Your unsent message stays in the chat you were in.** Pressing plus does not take
your draft anywhere: the new chat start page opens with an empty box, an empty attachment
tray and none of the pasted documents from the chat behind it. Nothing you had attached
there rides out on the first message you send from the start page.

**`esc` gives the draft back** — the words, the caret where you left it, the files
and pictures on the tray, the compact pastes, where you were reading in the transcript, and
the task page you had open if you pressed `+` from one. A task read from another conversation is reopened through a fresh connection to that exact owner; if that connection is unavailable, the existing task card explains why.

**A half-written first message on the start page is parked, not thrown away.** Press `esc`,
do something else, press `+` again, and it is still in the box. It is kept for as long as
this window lives; it is **not** written to disk, so a crash loses that one. Your
conversation's own draft is written down as it always was and comes back after a crash.

**A delivery refusal keeps your words in the new conversation** after it has been created. They are never sent to the chat you came from. Enter on a selected compact paste opens its editor; move beyond the token to send the message.

**If the conversation cannot be made, the words stay.** The reason is said on the page
itself — `new session failed: …`, or `/new is unavailable here` where this window has no
door onto new conversations at all — nothing is sent into the chat you came from, and that
chat is still running behind the page.

**What happens to the chat you were in.** It goes on running and keeps its tab, its draft
and its work. An unused chat with no draft may be replaced; a conversation with unsent words retains its own draft. If the old conversation has no saved identity yet, first send refuses with `finish or clear the draft in the current chat before starting another`; Escape restores that draft. Sending the first message opens the new conversation on a
connection of its own, over every door, so the chat you came from is still running and
still on the tab row. Opening and cancelling the page never touches it either.

**On a window too small for the page** — under 40 columns or 12 rows — the box stays at the
foot of the frame where it always is and one row reads `new chat · esc keeps the chat you
were in`. Typing and `enter` still start the conversation.

`/new` is the same act without the page: it starts a conversation immediately and keeps
your draft with you.

## What the `?` and `◐` marks on a tab mean

A tab can carry one small symbol in front of its name — a question mark, or a half-filled
circle — and it carries at most one:

| Mark | Means |
| --- | --- |
| `?` | That conversation is **waiting on you** — an approval, a sign-in, a proposal with no clock on it, or work out of fuel |
| `◐` | A turn or task is **running** in it |
| nothing | At rest, or nothing is known about it |

**`?` outranks `◐`** when both are true, because it is the one you can act on. The cell is
the same width in all three states, so a name never moves sideways when a turn starts. On a
terminal with no box characters `◐` is drawn `*`; `?` is already plain text, so the three
stay apart with color off.

**A countdown is not a question.** A task proposal that will go ahead on its own wears the
working mark or none — only something that will wait forever for your answer gets `?`.
Internal waiting, a tool checking something, a dependency, a provider being retried: all of
those are work.

**Nothing is a mark of its own.** An idle conversation gets no dot and no badge, and a
conversation this window only remembers rather than holds claims nothing at all rather
than guessing that it is still live. A conversation you left with `keep running` is held
rather than merely remembered: its tab is off the row, but it is still on the switcher and
on Home with `working` or `needs you` against it, and taking it back puts its tab up with
everything it wrote while it was out of sight.
The marks change when something actually happens; there is no clock behind them and nothing
on the strip animates.

## Which tab is waiting on you

**The `?` in front of a tab's name is that conversation asking you something** and waiting for your answer: an approval it needs, a sign-in, a proposal with no clock
running on it, or an adaptive run that has stopped because it is out of fuel. Click the tab
to go there and the question is on screen.

**`◐` is the other mark and it is not asking you anything** — that conversation has a turn or task in progress. A proposal that is counting down wears this or
nothing, because it will go ahead whether or not you look at it.

A tab with neither mark is at rest or is one this window can no longer say anything about.
For the same question asked about everything on the machine rather than about this window's
tabs, the switcher's card (`ctrl+k`) and the home page both carry it.

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

**And the words in it are that recipient's own.** The segment names who is listening, and
what is under it is what you last typed to them: the conversation's unsent sentence while
no room is open, and that task's while one is. Opening a room, leaving it with `esc` and
going straight from one room to another never carry a word between them (see the task
page and keys pages).

The same task is marked twice more while you are in it: its row in the roster wears the
same tint, and its chip on the task strip does too.

The input rule and header divider separate navigation, reading, and writing. There are no
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

Below 6 rows the rule, the gap, the pinned room header and the task strip all go,
leaving the conversation, the box and the status line. The ladder steps down, never up.

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

## Reopening a conversation — only my question and the final answer

Completed work starts folded under one `▸ worked` chip, leaving your question and
the final answer standing. This also applies to long turns whose earlier steps
load as you scroll up. Open the chip with `ctrl+e` or a click to see its outline,
then open a caption to read its calls. Set `ui.work` to `open` to start with the
work expanded instead.

The internal `[carry on]` continuation is guidance to the model, not a message
you sent. It is omitted when reopening saved conversations, including older
records that stored it as a user message.

## Scrolling a reopened conversation back to its first message

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

The rule that separates the conversation from your own business carries the branch and
the keys that work now, like the legend on a fieldset:

```
─ chat-v3-task* ─────────────────────────────── / commands ─
```

The left is the git branch, with a `*` when the tree has uncommitted work. On a session
running over `--host` it is the machine instead. The conversation name belongs to the
status line directly below, so the two adjacent lines do not repeat it.

In a directory that is not a git repository there is no branch either, and the left end
is simply blank rule. It never says "untitled" and never invents a placeholder.

The right is a hint slot. It names the keys that work right now when a state has keys of
its own — for example `y allow · n deny · a always` while a question is up,
`esc interrupt` while a turn is running,
`enter steers it in · shift+enter stops and sends · esc interrupt` while a turn is
running and you have typed words on a terminal that can deliver the secondary key,
`enter waits · shift+enter stops and sends · esc interrupt` while an otherwise empty
box has a picture on its tray on that terminal,
`enter steers it in · shift+enter stops and sends · ctrl+g backgrounds · esc interrupt`
when that turn also has a foreground command that can be kept, or `↑↓ · enter · esc`
while a list is open. A waiting message changes the final clause to
`esc stops and drops`; with neither words nor a picture the send clauses are absent.

**A question that cannot remember its answer loses the `a always` clause**, on this line and
on the offer above it: a stuck turn is asked about with a scope aforge cannot save, so the
key would do nothing and neither line names it. The slot reads `y allow · n deny` there.

It only ever names a key that **works right now**, and that includes the terminal: the
`shift+enter` clause is not drawn on a terminal that cannot tell that chord apart from a
plain `enter`, because a hint for a key that could never arrive would be the surface lying
to you — there the send half keeps only `enter steers it in`. `cmd+enter` still waits on
terminals that can deliver it, but is not part of this one-line slot. See the keys page,
"Interrupt and say something new in one key" and "Send a message into the running
answer".

The running-turn clauses always have this order: send, `shift+enter`, background, stop.
When the right side is tight, aforge removes whole clauses from the right until the line
fits. At 70 columns at least the first fitting clause remains; a running turn never loses
the slot merely because every clause would not fit.

**Under 70 columns the LEFT end gives up the branch and the slot keeps its keys.** A branch
is on the shell prompt in the pane behind this one; `/ commands` is written nowhere else on
a frame that narrow, and this line used to go blank at both ends there — a rule with nothing
on it, at the width where being told about `/` matters most. Only a frame with no room for a
label at either end falls back to the plain rule.

**The key itself is drawn apart from the word beside it.** In `esc interrupt`, `esc`
wears the soft cyan every highlighted fact wears and `interrupt` stays at the border's
own dim — the thing you press reads at a glance and the explanation of it does not
compete. It is the same in every hint the slot carries, in home's foot hint, in the keys
legend at the bottom of a conversation's card on home, and on the task record's foot. See "Why is one word in a line brighter than the rest"
below.

**At rest it carries the two doors out of the conversation.** With an empty box, it reads
exactly:

```
space space home · / commands
```

Pressing the space bar twice on an empty box opens the home screen, and clicking those
words does the same; `/` opens the command list. It is there on a fresh machine from the
first minute — an empty home is still a home — and over `--host` too, where it opens the far
machine's home. The whole slot gives way the moment you type or a state
above claims it. It costs no row either way — this line is on the frame regardless.

**Inside a task's room the slot is the room's**, and it never says `esc interrupt` there
— in a room `esc` leaves the page rather than interrupting anything. It reads `x stop`
while there is work here to stop, `↑↓ history` while a history walk is on, and nothing
otherwise. The left end of that legend is the room too: `room · esc/←← main`, or
`room · esc your line back` for as long as a walk is on, because that is the key's real
meaning until the walk ends.

**Two lines in that slot are about the draft you are typing**, rather than about a state
the surface is in. `ctrl+enter keeps this true` appears while your sentence looks like a
rule (see the standing orders page), and `ctrl+r spell it out` while it looks like
something to build and still has room to grow (see the keys page). They share the one
slot and the standing line wins whenever both would show. While the spelling-out call is
out, the slot turns a small spinner in front of the same words. Neither ever moves the
message box: this line is on the frame in every state.

One line in that slot is not about the next keystroke: `ctrl+g tasks`, which appears
when you have closed the task column, this session has run something, and no active
running-turn hint has the slot. It is the whole of what the frame says about a roster
that is not on screen, and it says nothing at all when nothing has been run.

While a question is waiting, the legend's left label — the branch, or the machine over
`--host` — goes violet along with the status word below it, so the question is pointed at
from both sides. On a session with neither there is no label to turn, and the rule stays
the grey it always is.

As the terminal narrows, the hint slot gives way and below width **70** the branch is
dropped too. The conversation name remains on the status line. With nothing true to put
at either end the line is the plain rule it always was.

While a task room is open the left says exactly `room · esc/←← main` — and exactly
`room · esc your line back` while a history walk is on, since for those keystrokes `esc`
gives your own draft back before the room's own `esc` gets the key. The name and the
branch are not drawn either way; the task's own title is on the status line below, which
a room renames. The pinned header at the top of the frame says the same
thing in its own words, `esc/← main`, and unlike the legend it answers to a press: click it and
you are back in the conversation. Clicking the page itself does not leave a room — a
press on empty space does nothing here as it does everywhere.

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
| the opening `esc interrupts · ctrl+c twice quits · ? for help` | the three keys |

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

The line above the message box used to carry the folder. It carries this conversation's
name now, so the workspace path lives in two places, and both say it in full:

- **`/status`** (aliases `/info`, `/context`) prints a `place` line — the whole path,
  then ` · ` and the branch with its `*` if the tree is dirty. On a remote session the
  machine is in front of it: `devbox:/srv/code/app`.
- **The status sheet**, which is `/status`'s own list on screen: the same `place` row,
  with the path abbreviated fish-style (`~/s/aforge-v2`) because a sheet row is one line.

The **branch** is also on the legend — `chat-v3-task*` — so a glance above the box tells you
which branch you are working on without opening anything. There is no branch on a
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
after `/resume`; the finished-task strip above the input is unchanged.

## The status line at the bottom

One row at the bottom of the frame, in two clusters. **Identity on the left** — which
conversation, and what is answering it. **Telemetry on the right** — what it costs, how
full it is, whether it is alive. The gap between them is the only separator: no pipe,
no bracket, no rule.

Nothing crosses. A number never appears on the left; a name never appears on the right.

The identity cluster reads like `porting the parser · gpt-4.1-mini:high`. The name is
the one the session chose for itself, falling back to the workspace's base name until
it has named itself, so it is never empty. The model is its **basename** — everything
after the last `/` — because the vendor prefix is nine identical cells on the row where
width is scarcest. The `:high` is the reasoning level, which is how the model is being
run, not part of its address. The full routing address stays in the picker, in the
`/model` note, in the session file and on the phone status sheet.

When the endpoint that answered is not already named by the model id, the segment gains
` · via deepinfra`, and **while a turn is running** the measured rate rides beside it —
` · via deepinfra · 92 tok/s`. The rate clears the moment the turn ends, because a rate
is a claim about now; the attribution alone goes silent after **10 minutes**.

Where aforge has timed the lane itself the same segment reads `via cloudflare · 0.6s ·
61 t/s` — the wait before the first word, then the rate. Four more readings replace it for
one answer at a time: `slow · trying coreweave…` while a slow answer is being asked of a
second lane, `refused · trying coreweave…` when the first machine said it will not serve
this model at all, `coreweave refused` when that second machine refused as well — the
promise is withdrawn rather than left standing — and `via coreweave · rescued` when the
second one won. `slow` is the only time this row calls anything slow, and it says it while
something is already being done about it. The models page has the whole of it under "what
rescued means".

Pressing the model segment opens the model picker, and it brightens under the pointer to
say so. The **money figure** further along the row is a door in the same way: pressing
`$0.14` opens the **Spending** tab of `/settings`, where every money limit is set. `/budget`
is the keyboard door onto the same tab.

Across the gap, the telemetry begins with the **crew** word — `crew max`, `crew balanced`,
`crew frugal`, or `crew custom` when you pinned one of the five yourself — so the two model
dials sit side by side: the model you talk to on the left, the preset the five models aforge
uses on its own behalf are on to its right. It is absent only on a **remote** session
opened with `--host`, whose crew lives on the other machine, and it is one of the first
segments a narrow row drops.

While a task **room** is open the cluster renames itself to the room chip and the room's
model — `task <model>` — and **pressing it moves that task**, not the conversation: the
same picker opens aimed at that node, and the task switches from its next turn onward. One
`esc` restores both the name and the door. Where the pick could not land the name is drawn
and simply does not react: a task that has finished, failed, been stopped or needs your
look, one that has not started, an adaptive run's page, or a node inside a run. The tasks
page says the whole of it under "Changing the model for one task while it is running".

Below width **100** the telemetry may take a row of its own, still right-aligned, and
only when the two clusters would otherwise collide — a short session still fits on one
row at 60 columns. If even the emptied line will not fit, the telemetry is what
survives, the identity is not drawn, and nothing on the row is pressable.

## What each part of the status line means

Twelve segments, right to left of the identity, joined by ` · ` in a fixed order:

| # | segment | example | what the number is | when it is empty |
| --- | --- | --- | --- | --- |
| 0 | crew | `crew max` | which preset the five models aforge uses on its own behalf are on — `frugal`, `balanced`, `max`, or `custom` when you pinned one yourself; a setting, not a measurement, and the other dial beside the model on the left | absent only on a remote (`--host`) session, whose crew is the other machine's |
| 1 | open | `2 open · 1 waiting` | how many conversations **this terminal** is holding, and how many of them are stopped on a question | absent whenever only one is open, which is the ordinary case; the `· N waiting` clause is absent when none is waiting |
| 2 | ambient | `2 jobs · 1 watch` | background work this screen saw start and has not seen killed — a `bash` with `background:true`, a `watch` call | zero of both draws nothing |
| 3 | delta | `Σ +128 −14` | lines added and removed by this whole session | only at width 120 or more; empty when both are 0 |
| 4 | cost | `$0.14` | the session's running spend. **It is a door**: press it and the **Spending** tab of `/settings` opens, and it brightens under the pointer to say so. It takes the warm ink once this conversation has spent four fifths of its own `per conversation` limit — a bound about to be reached is not a failure and does not wear the failure hue | never empty |
| 5 | context | `12.4k/128k · 10% ▁▂▃▅` | tokens the conversation is carrying, the model's window, the percentage, then a 6-reading sparkline | empty when nobody has said what the window is, or tokens are 0 |
| 6 | cache | `⟲ saved $0.02 · 89% cached` | the session's cache hit rate, and what that share was worth in cash | empty until there is a cached share; on an unpriced model the cash half goes, leaving `⟲ 89% cached` |
| 7 | burn | `1.2k tok/s avg` | output tokens over the wall time of **this** turn, including tool and model waiting time; the separate `via` provider rate measures generation | empty unless a turn is running and has run for at least 1 second |
| 8 | eta | `compaction in ~3 turns` | forecast from average growth | empty when the conversation is not growing, when the answer is more than 5 turns out, or when compaction is already due |
| 9 | yolo | `YOLO` | the `tools.approvalMode` row in your profile is `allow`, or the session was launched with `--yolo`, which forces that posture for the session without writing the row — over `--host` it is the far machine's row, carried once when the connection opens | empty in every other posture — absence is the safe state |
| 10 | connection | `devbox · 3ms` | a rolling estimate of one empty round trip to the machine a `--host` conversation runs on; while the link is down this is replaced by `reconnecting to devbox — trying for up to 5 minutes` | empty on every local session and on a hosted one until the first measurement answers; never `0ms` |
| 11 | state | `⠹ working · 4s` | what the screen is doing, and for how long | never empty |

The `N jobs` figure means "what you started". A background job that exited on its own is
still counted, because nothing on the wire says otherwise. For the state of one job rather
than a tally, read the `jobs` section on the column — collapsed it is one line of counts,
expanded it is the rows. The tasks page has it under *Background jobs on the column*.

The `open` count is read from the conversations themselves and not from the files other
terminals leave behind, so it never lags: a conversation that stops on a question while you
are looking at a different one is counted in `N waiting` on the next frame. `tab` over an
empty box goes to the last one — see the keys page, and home's *Switch between projects
without leaving*.

## How fast is the connection — host latency and round-trip time in the status line

For `aforge chat --host devbox`, the connection segment begins empty. Every few seconds
the surface sends one empty call, off the drawing path, and folds the reply into a rolling
estimate. After the first answer it reads like `devbox · 3ms`. A sub-millisecond reply is
shown as `1ms`, never `0ms`; no answer means no segment.

The check is never sent per frame and is skipped while the link is reconnecting. During a
redial the existing sentence — `reconnecting to devbox — trying for up to 5 minutes` —
takes the segment. `/status` spells the healthy fact out as `the round trip to devbox is
about 3ms` under `connection`.

## Why the numbers on the status line fade

The telemetry cluster on the right of the status line is painted by how recently each
segment **changed**: changed under 4s is ink, under 10s is muted, otherwise dim. At rest
the whole cluster is one quiet grey, and the one segment that moved is the only thing
with weight.

A segment's first appearance is not a change, so a new segment starts at the bottom of
the ramp. A segment that vanishes loses its clock — the next thing of that kind is new,
not recently changed.

Four things override the ramp, in this order: the state word owns its own paint; `YOLO`
is always the bad hue, loud for what it means rather than for when it changed; the
context meter outranks its age with its own three-rung heat; and while you are being
asked something the whole ramp collapses to dim, so no number competes with your
decision.

There is no idle ticker driving this. During a turn the frame clock is already running,
and when a turn settles exactly two one-shot ticks are scheduled so the fresh tier can
expire on time.

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

## Why the status line says $0.00

A figure nobody measured is not drawn. Zero jobs, zero watches, an unknown context
window, an unpriced cache — every one of them draws nothing rather than a zero.

The spend segment is the one deliberate exception. The status line and the phone status
deck **do print `$0.00`** on a session that has sent a turn and spent nothing. The reason
is that the status row is a live row: a segment that came into existence on the first
priced turn would shove every segment beside it sideways. **While the empty screen's
greeting is up there is no spend segment and no context meter at all** — the row is
the name, the model and `idle`, and the numbers arrive with your first keystroke (see
*The empty screen* page).

The commands keep the law instead. `/status` filters the spend line out when the cost is
zero, and `/cost` only adds it when the cost is above zero — so the two commands say
`nothing spent yet — this session has not sent a turn.` where the row says `$0.00`. A
landed task card also refuses to print `$0.00`.

**A resumed conversation is not a $0.00 conversation.** Reopening a transcript with
`--session`, `aforge resume` or `/resume` puts what that conversation has already spent on
the spend segment on its first frame — the sum of every cost line in its file, the
errands aforge ran beside your turns included — and the work it started is on the same
first frame, read off the spending ledger on the way in rather than waiting for the task
column to come up. New turns add to it. If you resume a
session that spent real money and the row still says `$0.00`, the file has no cost lines
in it (an older build wrote none), not that the money was forgotten.

**And nothing carries over from the conversation you left.** Every figure on the row —
the money, the tokens, the cache, the context meter — is a fact about one conversation,
so switching zeroes them all and then asks the arriving conversation for its own. The
first turn you send after reopening a transcript is charged only what that turn spent:
its receipt is its own price, not the bill the conversation opened with.

Other honest silences: the context percentage is dropped below 1% rather than shown as
`0%`; the cache cash half appears only when there is a published price pair, never
"saved $0.00"; and the saved figure uses four decimals under a dollar, so a real
fraction of a cent is not rounded away to nothing.

**A real amount is never drawn as zeros.** Every price on this screen is written to cents
above a cent (`$1.63`), to four decimals under one (`$0.0052`), and as `<$0.0001` under a
hundredth of a cent — because `$0.0000` is four zeros on a screen that has taught you a
zero means nothing happened, and a turn that spent six millionths of a dollar spent
something. The limits on the Spending tab are written by the same rule, with whole dollars
where the figure a person typed was whole (`$500`).

## The state word: idle, working, stopping, waiting

The last segment of the status line is the one thing true of the whole row. The exact
words:

| word | when | paint |
| --- | --- | --- |
| `idle` | nothing is running | dim |
| `⠹ working · 1m 4s` | a turn is running; spinner plus a count-up | accent |
| `starting task` | a task proposal has a countdown and will start automatically | accent |
| `waiting · your call` | an approval, standing or saved-program question requires an answer, or a task proposal has no countdown | the question hue, bold |
| `stopping · detaching in 7s` | you pressed `esc` and the turn has not finished letting go yet; the count is what is left of the 10-second bound before aforge detaches | dim |
| `interrupted` | the last turn was stopped by hand and is over | the bad hue |
| `COPY` or `COPY · 12 lines` | copy mode | accent |

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

**The second stop is a clock, not a key.** There is no key to press, and there does not
need to be: `esc` again is the rewind's door (see the sessions and rewind page) and
`ctrl+c` is the quit arm, so neither is free, and a stop you have to ask for twice is a
stop that did not work the first time. So the window is bounded at 10 seconds from the
key you already pressed. The status line counts it down — `stopping · detaching in 7s` —
and at the bound aforge detaches: the waits inside the tool are ended, whatever request
was still in flight is aborted, the conversation reads `detached — the turn was let go of
and nothing is waiting for it`, and the turn is written to the journal as abandoned with
what it had spent. You get the next prompt straight away.

## What the status line drops when it is narrow

When the segments do not fit, they are removed one at a time in a fixed order, by how
actionable each one is:

```
delta → crew → open → cache → eta → burn → ambient → cost → context
```

`crew` goes second because it is a setting rather than a measurement — it changes only when
you change it, and `/status`, bare `/crew` and the hint under the model picker all say it in
full. `open` goes early because it is the one segment that is not about the conversation in
front: at forty columns what you need is what **this** conversation is doing.

The **state word, the `YOLO` badge and the connection are not in that list at all**. One
is why you are looking at the line, the second is why you should be, and the third is the
reason none of the numbers beside it are moving.

The burn rate is damped on purpose. It is recomputed every frame, but the string is
held: the rate is rounded (steps of 5 under 100, two significant figures above) and the
shown value is only replaced every **500ms**. Between replacements the segment is
byte-identical. When a turn ends the empty value takes effect at once. The reason, in
the code's own words: a number that moves faster than it can be read is not
information, it is motion.

## The context meter and its sparkline

How full the conversation is, measured against the **compaction threshold** rather than
the model's window — the threshold is the thing that actually happens to you.

Three rungs: calm (dim), **near** (accent) past 80% of the threshold, and **due** (the
bad hue) past the threshold itself.

The sparkline is the last **6** turn-end readings, one glyph each from `▁▂▃▄▅▆▇`, also
scaled to the threshold.

No sparkline is drawn with fewer than two readings — one bar is not a trend, it is a
bar. There is no sparkline in the ASCII glyph tier or the screen-reader tier, and none
below width **70**. All three keep the number, which is the fact.

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
   away.
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
8. **The task strip becomes one door, and the roster becomes cards.** The strip stops
   being a row of chips and becomes a single full-width door — `▸ 3 tasks · 1 running`
   — that a tap opens into the roster page; that page's rows become two-line cards a
   thumb goes into, and its foot becomes a `‹ back` bar in place of the key legend. See
   *Tasks on a phone* on the tasks page.

On top of those eight: preview blocks under a pending call are capped at 4 rows instead of
12; there is no task rail column (that already went at 100); and the legend has already
dropped its hint slot and its branch (that went at 70).

## What the top line of home drops when it is narrow — the clock goes first

Home's top line is the program's name on the left and the machine's vital signs on the
right:

```
 aforge          2 want you · 4 moving · $0.55 / $20.00 · thu 1:11pm
```

When there is not room for all of it, the segments give way **one at a time, in a fixed
order**, exactly the way the status line's do:

```
clock → the allowance ($20.00) → the day's spend → moving → want you
```

So the clock is the first thing off the line and `2 want you` is the last. The reason is
one sentence: the terminal's own bar, the window and the wall clock all say what time it
is, and nothing anywhere else says that two things have stopped and will not move until
you look — a cell that could carry either carries the one you can only get here. Within
that, `want you` outranks `moving` because a stopped thing needs you and a moving one does
not, and the day's spend outranks the allowance because a figure is a fact and a fraction
is that fact plus a bound.

**The allowance goes by respelling, not by slicing.** `$0.55 / $20.00` becomes `$0.55` —
never `$0.55 /` and never a bound with nothing in front of it. And when the money segment
goes entirely there is **no `$` left on the line at all**: a narrow top line never says
`$0.00`, because that would be the line reporting a figure it had actually given up on.
(The one `$0.00` on the whole surface is the live status line of a conversation, so its
segments do not jump sideways as the first money arrives. It is a different line.)

**The name never gives way.** A window too narrow even for `2 want you` beside it draws
` aforge` alone. This used to be all-or-nothing — everything, or the name by itself — so a
sixty-column window spent twelve cells on `thu 12:01am` and then, one segment later, said
nothing about the machine whatsoever.

## Other width thresholds worth knowing

Beyond the four tiers, these are the exact points where parts of the screen give way:

| what | threshold |
| --- | --- |
| session delta (`Σ +128 −14`) drawn at all | width 120 |
| telemetry may wrap to its own row | below width 100 |
| legend loses branch and hint slot; status keeps the conversation's name; no context sparkline | below width 70 |
| full task rail, 30 columns off the conversation | width 120 |
| slim task rail, 24 columns | width 100 |
| no rail column at all — `alt+t` overlays the roster instead | below width 100 |
| no rail column at any width — you closed it with `ctrl+g` | your choice, remembered |
| task strip | width 24 **and** height 6 |
| a room's pinned header | width 12 and a non-zero breathing gap |
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
 Fix the nil-map crash          $0.31 · 12%
 deepseek-v4-flash        ⏺ 2 running ▸
```

Row 1 is **what this is** (the session name, or the workspace place if it has not named
itself) against **what it has cost** (spend, and the context percent only — the
fraction is what the sheet is for), with a `▸` on the end. Row 2 is **what is
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
fullscreen sheet listing every fact the status line can hold, one per line, label then
value.

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
opening a conversation, on one line, with the Go toolchain and the platform after it —
`aforge 1265feda (dirty) built 2026-08-27 13:28 · go1.26.5 darwin/arm64` — which is what a
defect report needs. A binary built with a bare `go build` rather than `make build` carries
no revision at all, and says so: `aforge dev (no revision stamped — built without `make
build`) · go1.26.5 darwin/arm64`. On a session opened with `--host`, `/status` names the build on
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
  along. **A line too long for the frame wraps rather than being cut**, at every width:
  there is no horizontal scroll anywhere on this screen, so a cut line was a line that
  could not be read, copied or trusted. See "Long lines inside a fence" below.
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

## The reply pops in as a block instead of streaming smoothly — why it writes in

A model that is writing to you a few characters at a time is drawn a few characters at a
time: whatever the connection hands over, you see, as it lands. Nothing is held back and
no delay is added.

What used to pop is the other case — a **paragraph that arrives in one piece**, because
the endpoint buffered it or the connection stalled and then dumped what it had been
holding. Those bytes are kept; what you see is the growing edge writing them in over a
fraction of a second, fast at first and finer at the end. A few characters land on the
instant so the edge is already moving.

**A finished or stopped answer is always whole.** The moment the turn ends — it finished,
you pressed `esc`, a call started under it — anything still being written in appears at
once. There is no state in which a reply is left half-drawn.

The thinking window and an `ask here` reply on home behave the same way. A screen-reader
session never paces: every byte lands the moment it is known.

## Why the reply still jumps — a blob of text arrives all at once

That is the connection, not the screen, and the screen softens it. A stalled wire that
then dumps a paragraph is drawn as the growing edge writing that paragraph in over a
fraction of a second rather than as one block appearing. A stream that is genuinely
arriving word by word is drawn word by word, with nothing held back — the surface never
slows down text the connection delivered quickly.

If a reply seems to hang and then land whole, that gap is the model or the network. The
status line's `working` word and the elapsed clock are what to read for it.

## Markdown while a reply is still arriving

Within expanded work, the live tail of streaming prose is plain wrapped text, not markdown.

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

## The answer stayed raw until I asked the next question

It cannot any more. **The end of a turn is the only thing that settles it**, and it settles
the whole turn: every block of it is a finished document from that moment, and nothing that
arrives afterwards can leave a paragraph half-drawn for the next question to tidy up.

What used to happen: a turn ends twice — the session finishing it, and the connection
closing behind it — and in the gap a provider could still deliver the tail of a reply it had
already buffered, or a straggler behind an `esc`. Those words opened a **second** block that
nothing was left to settle, so it drew as the characters you typed rather than as markdown,
and its mere presence pushed the real answer above it into the grey working column. Both
went away the moment you asked something else, which is why it looked like the next question
was fixing the last one.

Late words now join the answer they belong to, already settled. If the turn's last block was
a tool call rather than a reply, they land in a block of their own — also settled — because
words that came after a call belong after it.

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
and the moment the next tool call opens, that paragraph becomes work. In the compact
conversation it supplies a step description; inside the opened outline it uses the same
two-column gutter as the tool rows and drops one shade below the body text.

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

One thing that is **not** work, and so never greys the paragraph above it: a line aforge
writes about the turn itself. What the turn changed, what it cost, a notice that a request
had to be reshaped — those land under the reply they are about, and a reply that had one
written through the middle of it stays one answer rather than breaking into a grey half and
a flush half.

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
  waits for this answer · esc stops and drops · → steers it in · ↑ or click to edit
```

The dim line trims from the right on a narrow terminal: the last piece goes first, then
the next, and the narrowest frame keeps `waits for this answer` alone. With more than
one message waiting the first piece is counted — `2 wait for this answer` — and with
exactly one it is not counted at all.

`→ steers it in` is there only while the message can go into the running answer: a turn
still running, and a message of words alone. A waiting message that carries pictures, or
one marked with `ctrl+enter`, cannot be sent in and the clause is absent for it. Pressing
`esc` removes the waiting block at once; `→ steers it in` is absent while a stopped turn
is winding down because that turn has no boundary left to take the words.

## What happens to a message waiting above the box

What happens to it:

- **When the answer finishes**, it sends itself as an ordinary new turn and appears in
  the conversation as a normal message of yours. Several waiting messages go **one per
  finished turn**, oldest first, in the order you typed them.
- **`esc`** stops the answer and drops every parked message and queued follow-up. None
  starts a turn when the interrupted stream closes.
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

While something is waiting, the hint slot in the legend ends with
`esc stops and drops` instead of `esc interrupt`.

## Long lines inside a fence — code cut off at the edge, the tail of a line missing, `↳`

**A code line longer than the frame wraps, at every width.** It is rendered two cells
narrower than the column, and those two cells hold a dim `↳ ` on every row that continues
a source line. The marker sits outside the code plane, so it can never be mistaken for
something the code said. Breaks prefer a space in the back half of the row and go
mid-token when there is none — a 40-cell URL in a 30-cell column has no break in it.

Copying takes the block whole: `a` in copy mode selects the run of code rows around the
cursor, wrapped rows included, and the paste carries neither the hairline nor the `↳`.

This used to be true only under 60 columns. Above it a long line was **cut** — with an
ellipsis at some widths and with nothing at all at others — so the same answer was whole
in one window and truncated in the next. There is no key that pans a block sideways and
never was, so the cut simply lost the bytes.

A URL or a path is not broken at the reading measure. A link is something you copy
whole, not something you read along, so one too long for the measure is left on a row of
its own and given the width of the whole column. Only a token longer than the column
itself is broken, and then at the column, flush against the divider — the same place a
fence, a table and a quoted passage already break.

## Markdown at phone width

Phone width (under 60 columns) is the tier where a **table** is not the prose renderer's
byte-for-byte output. Every tier above it renders one as it always has.

The document is scanned for two shapes, **at column zero only** — a top-level fenced
code block and a top-level GFM table. The fence is re-laid-out at every width (above);
the table only here; everything else goes straight to the ordinary renderer.
- **Tables stack** as one `key: value` line per cell, with one blank row between
  records. The header travels with each cell rather than standing once at the top. Empty
  cells are dropped. The key is bolded unless the header already carries markup. Each
  line goes back through the prose renderer, so cells keep their bold, their code spans
  and their links.

Limits: a fence indented inside a list item or a blockquote is **not** pulled out. It
stays in its prose segment and is cut. This is a deliberate gap, and it is the one place a
code line is still truncated. Below 8 content cells the wrap is abandoned and the fence is rendered
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

## Click a file path to open it — open a file from the chat, why is this file path not clickable?

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
  page and a room both. A task works in its own copy of your folder, so `internal/tui3/app.go`
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

## The three live steps under my question — compact progress, opening the work

While the main conversation works, recent step descriptions occupy a compact
window below your question. Older steps are fainter; the newest step shimmers
while its calls run. A soft highlight sweeps across the text every two seconds,
reaching ordinary reading brightness; the letters stay still. Thinking, raw tool calls, arguments and call counts stay
behind this view. The window changes when a new step arrives. Before the first step, a clickable `▸ Working · ctrl+e` door carries the shimmer.
Between finished calls, the latest description stays readable and still, with a
softly animated dot beside it. There is no extra Working row. After 10 seconds
of a known response wait, a dim `awaiting response · 12s` suffix appears when it
fits. A known connection loss says `waiting for connection` immediately when
the suffix fits, instead of describing it as a slow model response. When the
turn is still working after the response has begun, the same place says only
`still working · 1m 3s`; that clock measures the turn, not the completed step
whose description remains beside it. Detailed
phase and retry information stays in the footer and expanded view.

Click a step, or press `ctrl+e` with an empty message box, to open the full
outline. Each caption then opens its own calls. The live caption keeps its
existing open default. Click `▾ working · ctrl+e`, or press `ctrl+e` again, to
return to the compact view. `ctrl+o` can still show all calls.

The window budgets **3 wrapped rows**, admitting whole captions newest first.
On a narrow screen, a caption that needs two rows leaves room for fewer steps.
If the current caption alone needs more than three rows, it stays whole rather
than losing words. Between calls, the latest caption also stays whole. Waiting text uses only spare
space; when the dot cannot fit after the caption, it occupies the existing icon
gutter. The description never moves to make room for a timer. Screen-reader and lower-colour terminals (including 256 colours) draw the
descriptions without motion. Expanding or collapsing the work is immediate.

Your messages, corrections, answers, approval questions and notices remain
outside the compact work. A failed step keeps its ordinary outline and controls.
A correction can separate two compact blocks, preserving where you said it. When
what is above the split is only the model's reasoning, it draws no second
`▸ Work · ctrl+e` — it goes behind the ordinary `⠿ thought for 1s · ctrl+e` row,
so there is one working door per turn.
Running task pages use the compact treatment too; expanding it restores their
phase outline and individual tool details.

## A step is taking a while — elapsed time beside a running step

After a tool step has run for 10 seconds, its compact caption shows a dim elapsed
count such as `12s` or `1m 3s`. It measures time since that batch began, including
when its caption is renamed. A new step starts its own clock. Completion removes
the live timer. This counts up; the finish time is unknown.

The time uses spare space after the caption's last line. It never moves the
words or adds a row; on a narrow terminal with no spare room, it stays hidden.
The icon stays still and only the step's words shimmer while its tools run.
Between calls the separate waiting dot moves; its response clock starts with
the request, not with the preceding tool. Task pages never borrow this clock
from the main conversation.

## The two figures on the right while it works — the up arrow and down arrow, upload and download tokens, how many tokens is it using right now, is anything actually happening

While a turn is running, the right edge of the working block carries two figures:

```
▸ Working · ctrl+e                                        ↑ 63.6k  ↓ 12
  reading 2 files in internal/tui3
· running go test ./internal/session · 41s               ↑ 78.2k  ↓ 486
```

`↑` is what this turn has **sent** to the model, and `↓` is what has **come
back** from it, both in tokens. Between them they answer the question the
shimmer cannot: not "is this alive" but "is anything moving, and how fast". A
`↓` climbing steadily is a model writing; a `↓` that has stopped is a stream
that has gone quiet, and the line beside it will say so within ten seconds.

**They count up rather than jumping.** Both figures walk toward each new reading
over a couple of tenths of a second — the same ease as the reply writing itself
in. The books behind them stay exact; only what is painted is in motion.

**They belong to this turn and they leave with it.** They open at nothing when
you send a message and they are gone the moment the answer settles, because they
are a sign that something is moving rather than a total. The session's running
totals stay on the status line, where they never go away.

**Only one row carries them.** The row that stands for the whole turn — the
compact block's newest line, or `▾ working · ctrl+e` once you have opened the
work — is the one with both figures on it. A step that has finished carries
nothing.

**An opened step shows its own `↓`.** Press `ctrl+e` and each running step's
caption carries what the model wrote inside it, after its clock: `2s · ↓ 486`.
There is no `↑` on a step: one request carries the whole conversation rather
than the step it happens to be in, so a share of it per step would be arithmetic
nobody performed.

**A figure nobody has earned yet is not drawn.** Before anything comes back
there is no `↓`, not a zero. On a narrow terminal the words win and the figures
are dropped — `↑` first, because `↓` is the one that says something is arriving.

**On a screen-reader or plain terminal** the arrows are spelled `^` and `v` and
nothing eases: the exact figure is drawn each time.

**They are quiet on purpose.** The figures wear the same dim grey as every other
fact at the right edge of a row — the step clock, `189 lines`, `⠋ 2s / 30s` — and
the arrow is fainter still. A number that moves does not also need to be bright;
the words on the row are what it is for.

**A turn that was split** — a correction you typed into it, or a step kept out of
the compact view because a call in it failed — keeps the thing that split it
standing where it happened, with the working block under it. There is one
working door per turn: a run above the split that holds nothing but the model's
reasoning draws no `▸ Work · ctrl+e` of its own, because two of those on one page
read as the same turn running twice. That reasoning is behind the ordinary
`⠿ thought for 1s · ctrl+e` row instead, the same row a finished turn draws. The
figures ride the working block, the one where the work is now.

## Do the up and down token figures show inside a task room — tokens on a task's own page

Yes. A task's page (a room, opened from the task column) draws the same column on
its own live work, counted from the task's own lane: `↑` and `↓` are what THAT
task has sent and received, summed over the steps its page has heard, never the
conversation's figures. A room opened on a task that was already running shows
`↓` from the moment the task writes anything and `↑` from its next finished step
— the page cannot know what it did not hear, so it draws nothing rather than a
guess. The column leaves when the task finishes, as it does in the conversation.

A run's read-only transcript inside an adaptive run's page draws no column: it
is a journal being read back, not work being watched.

## How exact are the up and down token figures — is the upload figure what I am billed for

`↓` is the provider's own count as soon as a step reports one. In between, while
the model is still writing, it is estimated from the text already on your screen
at about four bytes to the token — the same estimate aforge uses everywhere else
it has to guess — and the exact figure takes over the moment it lands.

`↑` is the same shape the other way round: the tokens this turn has actually
been billed for, and, before the first step of the turn has reported anything,
the weight of the conversation being sent. It steps up rather than climbing
smoothly, because that is what really happens — a request goes out whole each
time a tool result joins the conversation.

Neither figure changes what you are charged, and neither is what `/cost` prints.
`/cost` and `/status` print the exact books.

## The symbol beside each step — the little icons in the working block, what the mark in front of a step means

Each compact caption carries **one small mark** on its first line,
in a gutter two columns wide. The mark says what **kind** of work that step is —
searching, editing, running a command — so you can tell at a glance what is
happening before you have read which file it is happening to.

| kind | normal icon | plain fallback | what it conveys |
| --- | --- | --- | --- |
| **search** | magnifying glass | `⌕` | looking for something |
| **read** | text document | `▤` | opening or listing information |
| **edit** | pencil | `✎` | changing content |
| **create** | plus | `+` | writing content or generating media |
| **run** | terminal | `$` | running a command |
| **test** | flask | `◎` | checking work |
| **browse** | globe | `↗` | visiting a page or service |
| **transfer** | exchange arrows | `⇄` | moving files or state |
| **communicate** | speech bubble | `»` | sending a message or speaking |
| **coordinate** | branching paths | `⇉` | handing work to tasks or forks |
| **plan** | list | `≡` | keeping track of the work |
| **wait** | clock | `◷` | waiting for something outside the turn |
| **work** | gear | `▪` | other work, including unfamiliar connected tools |

## The step marks never move and never say whether a step passed — no tick, no cross, still icons

**The marks never move.** The newest step's *words* shimmer while its calls run;
its mark holds still. Between tool calls, only a separate dot beside the latest
finished caption animates. On very narrow lines the dot uses the icon gutter
instead. Before any caption exists, the Working door carries the shimmer.
There is only one animated indication.

**They never say how a step went.** There is no tick, no cross and no warning
mark here. A step that failed is not folded into this block at all — it keeps its
ordinary rows — so a mark here could only ever mean "nothing has gone wrong yet",
which is not worth a column. `test` draws a flask (a target in plain mode), not a checkmark, because the
mark names the *act* of checking and not its result.

**The gutter is a fixed two columns.** Every step spends the same width whatever
its kind, and a description that wraps onto a second line leaves those two
columns blank, so all three sentences start in one column and the block does not
shift as steps arrive.

**The mark fades with its own line.** The oldest step is a faint mark and faint
words; the newest is at ordinary reading strength. The gutter is never brighter
than the sentence it belongs to.

## Proper icons, missing icons, empty boxes, Nerd Font and the step icons setting

The normal view uses the Font Awesome icons included in Nerd Fonts. Under
`/settings` → Display → **step icons** (`ui.icons`), `auto` chooses those icons
unless terminal detection calls for plain symbols. Known console and locale
limitations fall back; colour depth alone does not remove icons.

A terminal cannot report which font it uses. If you see empty boxes, select
`plain`, or select a Nerd Font in your terminal. Choose `rich` to use a patched
font on a conservatively detected terminal. The setting takes effect immediately.
No font is installed or changed automatically. Screen-reader and ASCII modes
keep simple one-character marks with the same fixed gutter.

## Where the marks come from — can the model choose the wrong icon

The kind is named by the same cheap one-line narrator that writes the step
description. It answers in the form `run | starting the local server`: one word
from the list above, then the sentence. **It costs no extra call** — the word
rides the sentence that was already being written, on the same budget of at most
three narrations per turn.

**A mark is drawn before any narration arrives**, and it is derived from the
tools the step actually called — `grep` is a search, `edit` is an edit, `bash` is
a run. When the narrator's answer lands it replaces the description and the mark
together, in the same repaint, so the two are never out of step. If the narrator
says nothing, says a word that is not on the list, or the answer arrives after
the step has finished, the tool-derived mark stands and nothing is retried.

A tool that came from a **connected account** has a name aforge has never seen,
so its steps draw the generic gear (`▪` in plain mode) rather than a guess.

**A reopened conversation retains saved descriptions and categories.** Older
conversations without that information derive their icons from the saved tool names. Scrolling a finished turn back into view never changes a
mark.

## The live work collapses when the answer finishes

When the turn finishes, its work collapses even if you opened it while it ran.
Your question and the answer remain visible. Open the finished `▸ worked` chip
to inspect the steps again. Reopening the conversation also starts with completed
work folded, including long turns whose older history loads as you scroll.
`ui.work = open` remains the explicit preference for expanded work.

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

Inside the opened work, calls in one step fold under a short **caption** — one sentence of about 5 to
10 words saying what that step is doing and where, with the honest call count
at the right. On a narrow window the caption wraps onto the next line; it is
never cut with an ellipsis mid-sentence. Press `ctrl+o` on the live caption or
click it to show the tool rows. A caption can be wrong; the rows beneath it are
the truth. Before a long live run has a caption, at most **3** calls stay on
screen and the rest use the fallback `↳ 1 earlier tool call · ctrl+o` or
`↳ N earlier tool calls · ctrl+o`.

A **task's page** keeps more in that no-caption fallback — as many calls as the window is
tall — and its line reads `↳ N earlier tool calls · scroll up or ctrl+o`, because there
scrolling up at the top of the page opens it too. The conversation's fallback only ever
opens with `ctrl+o` or a click.

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
entirely at phone width. **Every foreground command kept as a background job rides it
too**, as `job 3`, whether the background-after clock, its timeout, `ctrl+g`, or the
row's `click to background` offer did the handoff. On a wide row it is drawn last because
it is the most recent thing to have happened; at phone width it replaces the stat in
that same slot.

A stat is only ever drawn for a finished call. An edit's `+N −M` is knowable early and
is deliberately withheld — it is the shape the preview collapses into when the change
lands. A result that never arrived draws no stat at all, because "0 matches" is a claim
about a search nobody made.

When the row is too narrow, the right column gives up **whole segments** rather than
clipping characters — see *What the numbers on the right of a tool row mean* — and the
target is truncated last, because the target is the substance and a figure nobody has
room for is a number about a line nobody can read. The target always keeps at least
**7** cells: the last few characters of a name and the `…` that says the rest was cut.

## A long tool call is one row — it does not wrap or spill past the frame's edge

**A tool call is exactly one row, however long the command is.** A `gh api` with a
hundred-and-thirty-character query in it takes the same single row a `read` takes: the
command is cut to the width you have and the `…` says the rest is there. Nothing about a
tool call ever wraps onto a second row, and nothing it draws goes past the frame's right
edge.

That is true of what a call **opens** as well. The command shown whole, the diff, the
output, the `… N more lines` foot — every one of those rows is cut to the frame too, and
the two columns the machinery is indented by are subtracted before the cut rather than
after it. A row laid out to the whole frame and then moved two columns right is two
columns too wide, and a terminal answers that by folding it onto a second row: one open
`bash` call could eat four or five rows that way, half of them the tail-ends of the row
above. It does not any more.

**To see the whole command, open the call**: click the row anywhere along its length, or
select it with `↑`/`↓` and press `enter`. The same gesture closes it again. That is per
call — opening one leaves its neighbours alone — and it is different from `ctrl+e`, which
opens the newest work chip onto its caption outline. See *Seeing more of a tool call*.

Resizing the terminal re-cuts every row to the new width. Widen the frame past the length
of the command and the `…` goes away on its own.

## Why a tool's colours, tabs and progress bars do not show

What a command writes back is **somebody else's bytes**, and it is cleaned before it is
drawn: the colour is stripped, a tab becomes four spaces, and a carriage return or any
other control byte is dropped. The same cleaning is done to the command itself, and to a
file's content in a `read` or a `write`.

This is not tidiness. A tab and a carriage return both measure as nothing and draw as
something, so a row containing either is a row whose fit to the width was a fiction — a
`go test` line with four tabs in it was laid out to the frame and then wrapped anyway,
and a progress bar's `\r` sent the cursor back to column one so the end of the line
overwrote its own beginning. An escape sequence is worse than either: drawn into the
frame, it repaints rows that belong to this surface. A reply is sanitised for the same
reason.

The syntax colouring you see on a `read`, a `write` or a `bash` command is **aforge's
own**, applied after the cleaning — so source still reads as source.

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
second, and a rung whose remainder is zero is dropped rather than padded — `6m`, never
`6m 0s`.

**Countdown (a bounded call)** — only `bash`, and only a foreground `bash`, is bounded.
More than 10 seconds out, the bound is stated beside the age as `1m 12s / 2m`, dim.
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

A call with no bound gets no countdown and no colour — chrome implying a deadline would
be inventing one. A foreground `bash` always has one: normally it counts down against
the clock armed from `background after` when the session launched, 30 seconds by default.
Over `--host`, that exact armed clock comes from the engine machine rather than this
surface's profile. With the setting at 0, a `timeout` that is
missing, null or zero counts down against the 600-second ceiling. An earlier explicit
timeout wins. A background `bash` has no bound because it runs as a job. A turn that ended
with a call unresolved stops every clock, and it stays stopped:
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

If a row is spinning and the state word at the bottom says `idle`, that is a bug worth
reporting: nothing spins on an idle session.

## What happens when the countdown runs out

Not a kill. A foreground `bash` command that reaches its bound is handed to the
background and **keeps running**: the row finishes normally, its result opens with
`still running as job 3; log at …` and then carries whatever the command had
already printed, and the turn goes straight on.

So the coloured last seconds are a warning that the command is about to leave the
turn, not that it is about to be destroyed. Nothing is thrown away and nothing is
run twice.

`ctrl+g` or the row's `click to background` offer does the same thing early, on purpose
— see the keys page. Whichever door did it, the row keeps its spinner until its result
lands and gains a dim `job 3` beside its other trailing marks. The full account is on
what-i-can-do.

## Seeing more of a tool call

Click the row, or select it with `↑`/`↓` and press `enter`. `ctrl+o` on a capped block
lifts the cap.

**The whole line is the door** — anywhere along it, from the rail to the frame's right
edge, and the whole line lights up under the pointer to say so. `↑`/`↓` and `enter` reach
the same door with no pointer at all, and `enter` again closes the call. This opens **one
call**; `ctrl+e` opens the newest work chip onto its caption outline, and `ctrl+o` opens
the live caption's rows or the no-caption fallback of earlier calls.

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
it means nothing, and that is measured against the frame **after** the machinery's
two-column indent, so nothing an open call draws overhangs the edge (see *A long tool
call is one row*). The dropped remainder becomes a clickable `… N more lines` row, which
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
finishes, the picture is drawn under its row, in colour — no click, no key, no flag. A
picture you attach is drawn the same way under your own line after you send it, while its
numbered `[#1 shot.png]` marker stays above it. Both forms are there in the conversation
as you read it and in a task's room too.

It is drawn out of **half-block characters**: one cell carries two stacked pixels, its
top colour and its bottom one, which is how a terminal shows a photograph with nothing
but colour codes. No image protocol is involved and nothing is written outside the
frame, so the picture survives every repaint, scrolls with the conversation, and works
over ssh and inside tmux the same as anywhere else.

The picture under a row or your own message is a **thumbnail**: at most **12 rows** tall, or **4** at phone
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
and a row that repaints ten times a second all cost the same after the first frame. Tool
picture rows check the file's modification time when they repaint. Your own settled
message keeps its ordinary transcript-row cache and refreshes its thumbnail on a resize,
a theme repaint, or when a mirrored file lands; until one of those, replacing the file at
the same path may leave the earlier thumbnail on screen.

## When the image preview is not drawn — you only gave me text, it only gave me text, why don't I see the image, and where did my generated picture go?

If a tool row shows only text — something like
`/home/you/book/cover.jpg — 768×1376 jpeg, 776.9KB, generated on <model>` — then no
picture could be drawn, and **that line is the answer instead**: it names the file
**whole and absolute**, so you can open it yourself from anywhere.

For a picture you attached, the fallback is the message's existing `[#1 shot.png]`
marker. No error or empty picture block is added, and an ordinary file marker beside it
is unchanged.

The reasons, in the order they are worth checking:

- **Your terminal is below 256 colours**, or colour is off. The sixteen ANSI colours are
  your own theme, and a photograph painted out of them would be a lie about both.
- **Your terminal cannot draw box-drawing characters** — no UTF-8 locale, or no `TERM`
  at all. The half block is the whole technique.
- **Screen-reader mode**, where rows of block characters read aloud are rows of nothing.
- **The row is under 8 columns wide.**
- **The call has not finished.** A picture is drawn when the file exists, and
  `generate_image` writes the file last.
- **The session is over `--host` and the mirror does not hold the bytes yet.** Generated
  pictures are fetched from the other machine into the local mirror. A picture you have
  just attached is already read from this machine; after a restart its journal path is on
  the far machine and the thumbnail appears once an optional mirror fetch lands.
- **The file is missing, unreadable, over 24MB, over 64 megapixels, or not one of the
  four types** — a `svg`, a `tiff`, a `pdf`.

In every one of those the tool row is **exactly what it would have been** — its result
line, or what the looking model said — and never an error. Your own message keeps its
marker exactly. Opening a tool row in those cases gives you the file's whole absolute
path on its own rows, wrapped rather than cut, because a path with an ellipsis in it
cannot be clicked, copied or pasted.

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
line, the key map, the composer layer's rows and the key sheet `/help` draws read that one
spelling, so what is on your screen is what is on your keyboard.

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
Headings and section labels do not: the `aforge` wordmark in the welcome box, the name on
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
alt+w widen · click seam
❯ ctrl+g hide
```

**The first of them is one door with two spellings, never two doors.** It is drawn only
when the full-screen task page (`/history`) has something this column cannot give, and the
words on it say which: `ctrl+. earlier` when the project's record holds work this
session never ran, and `ctrl+. view more` when the only thing held back is a family the
column has folded. There is never more than one such line.
`alt+w widen · click seam` appears only while a title is actually being cut by its own
indent, **and only from 120 columns up** — that is the only width with a wider tier to
offer, so on a 100-to-119 column frame there is no line and the column's two leftmost
cells are part of the row rather than a handle. The `❯` door is always there, and its `❯` is drawn in ink rather than dim
because it is the control the pointer presses. With no foreground command to keep it
reads `❯ ctrl+g hide`; while a command owns that chord it reads only `❯ hide`, because
a hint may name only a key that works on that frame.

## Opening a finished row on the task column: how to see the log path of a job that already finished, and where the merge word, price or branch went

A finished task's row is one line, and the line it used to carry underneath is **folded,
not deleted**. It is the same fold a family of tasks uses, on the same keys and the same
cell:

- **From the keyboard:** `alt+t` hands the column the keyboard, `↑` and `↓` walk to the
  row, `→` opens it, `←` folds it away again. `esc` gives the keyboard back.
- **With the pointer:** hover the row and its state glyph turns into `▸`; click that one
  cell to open it, and `▾` in the same cell to close it. Clicking anywhere else on the
  row opens that task's room, as it always did — and so does that same cell on any frame
  where the triangle is **not** drawn in it, because then it is holding the row's state
  and a state is not a control.
- The fold is remembered per row, exactly as a family's fold is, and it lasts as long as
  the conversation does.

What comes back is that task's own last word: `merged · $0.42` for work that came home
clean. A background job is not a finished task row: it lives in the `jobs` section, and
its log path is on the job's page, not folded under a roster line. A row with nothing to
say — a task that ended with no merge word and no price — offers no `▸` at all, because a
mark that answered a press with silence would be a lie. The full record of any task is on
the task page (`ctrl+.`, `/history`) whether the row is folded or not.

## Scrolling the task column: the mouse wheel over the sidebar, and the keys that walk it

**Turn the wheel with the pointer over the column and the column scrolls.** It moves the
column's own window and leaves the conversation beside it exactly where it was; a wheel
turned over the conversation still scrolls the conversation. Three rows a notch, the same
as everywhere else on this screen. Work that is running is pinned to the top and does not
scroll away, and the `tasks` label stays with it.

From the keyboard it is `alt+t` to take the column, then `↑` `↓` to walk it — the window
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

## What is that column on the right — tasks, standing, jobs and an empty rail

The column beside the conversation carries the two things that govern a conversation, and
a third for the background work that conversation started.
A section with rows earns a dim lowercase label; an empty section keeps only its dim `+`
door. (An untouched empty conversation has no column at all until the first keystroke,
a task, or a standing order — *The empty screen* page says why.) Once it stands:

```
tasks
⠙ Fix the nil-map                                                       #7
+ /task

standing
◦ keep the tests green
◦ never touch the public API                                    everywhere
+ /standing

▸ jobs · 1 running · 4m12s

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
- **`jobs`** is this conversation's background work — a server, a build, a watch, a render —
  as a third section under the other two, collapsed by default to one line (`jobs · 2
  running`, `jobs · 1 running · 4m12s` when exactly one is live, `jobs · 6 ran` when
  nothing is). Enter or a click toggles it. Expanded, every running job draws, then
  finished ones fill whatever room is left, newest first, with `▸ N earlier` counting the
  rest. Zero jobs draws nothing at all — no label, no empty row, no `0 jobs`. A click on a
  job opens its page, not a chat. The tasks page has the whole of it under *Background
  jobs on the column*.
- **The `standing` section keeps its rows however long the roster gets.** The sections
  do not compete for the column: the roster is given what is left over after the label,
  the doors, the standing rows and the jobs section have been reserved, and it is the
  roster that scrolls. A session with forty tasks in it still shows the orders standing
  over it, and the jobs that are running, without scrolling.
- **At most three orders are drawn, and the label counts the rest** — `standing · 7 more`,
  in the same shape as `tasks · 4 working`. Past a handful the rows stop being read one at
  a time; `+ /standing` (or `/standing`) opens the page that lists them all. A section
  showing every order it has says nothing extra: the label is simply `standing`.
- On a **short terminal** the standing rows give way one at a time so that the roster
  keeps at least six rows, and under that the whole standing section stands down rather
  than drawing a label over nothing. Live jobs are reserved before standing spends: a
  running job is not a thing this column hides to make room for furniture.
- **An empty section has no label and no absence sentence.** When tasks and standing are
  both empty, only `+ /task` and `+ /standing` remain as the discoverable doors. Jobs add
  no `+` door — a job is started by a tool, not typed.
- **The `+` rows type, they do not arm.** Pressing `+ /task` or `+ /standing` puts that
  command and a space at the head of your message box and hands the keyboard back — plain
  text you can edit or delete, no mode, no form.

Under the sections come the column's dim totals and its door lines. The separation between
the sections is one blank line: this surface separates with whitespace and never with a
rule. On a build with no ambient side the `standing` section is absent entirely.

## The right edge: the chevron that opens and closes the task column

**The right edge always carries one chevron, and clicking it goes both ways.**

- While the column **stands**, its last footer line reads `❯ ctrl+g hide` when no
  foreground command can be kept, and `❯ hide` while a command owns that key. The
  `❯` is drawn in ordinary ink, not dim, because it is a control and not a reading;
  the words beside it stay dim. Click the line and the column closes either way.
- While the column is **away**, what is left is **two columns down the right of the frame**
  with a `❮` handle at the middle of them, also in ink. The whole strip is a door: click
  anywhere on it and the column comes back.

So one control in two states — `❯` to close, `❮` to open — and the pointer can go round the
whole cycle without touching the keyboard. With no foreground command to keep, `ctrl+g`
does the same thing from the keyboard; while one can be kept, the key backgrounds it.
Under the pointer the chevron brightens further and its line
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
tool row stops pulsing. The forming proposal card's mark becomes a still `*` too, while
only its clock climbs.

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

They read like this, with a clock counting up from the moment **the wait** began — not
from the moment the phase changed. A handshake, the queue before the first word, a pacing
wait, a retry, a rescue and a fallback model are one wait wearing different words, and the
number goes on climbing across all of them. It never counts backwards. A stage of WORK —
`thinking`, `writing`, `running go test`, `checking`, `tidying` — keeps a clock of its own,
because that number answers a different question: how long that stage has been going.

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

**The phase is on exactly one row at a time, and never on two.** While the working line is
drawn it owns the words; the moment it goes — an answer is streaming, a call is spinning,
the turn has ended — the status line beside your model takes them up, where they stand in
place of `via <machine>` (see *Models, context, and what it costs*). Before this the same
sentence was drawn twice on one screen, verbatim, two rows apart, and the second copy was
spending the cells the bill, the context meter and the watch count needed.

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
happens at it. So there is no countdown when there is no other machine to go to: a home
whose lane row pins one machine, `routing` set to `off`, an endpoint that is not a router,
or a model with only one machine behind it. You still get the phase and the count-up,
which are true, and no promise, which is the point — aforge would rather show you nothing
than a countdown that expires and does nothing.

Turning the **speed guard** off stops the second request being bought at all; see *Models,
context, and what it costs*.

## Why the reply is slow to start, why it says "waiting for" a model, and whether it is stuck

The ellipsis and model-name timings below describe the expanded transcript.
Before any caption exists, the compact Working door can carry the same details.
After a caption exists, compact progress keeps that description still, animates
only a separate dot, and adds `awaiting response` after 10 seconds when space
permits. A known outage says `waiting for connection` immediately.

A reported connection phase takes priority over the generic wait: for example,
`first word · 3.1s` or `thinking · 12s · friendli 38 t/s`. Without that information,
the expanded view shows a bare ellipsis for the first **4 seconds**, then a dim
model name and elapsed time:

```
  ··· waiting for kimi-k3 · 12s
```

After **30 seconds**, it adds `nothing has come back yet`:

```
  ··· waiting for kimi-k3 · 47s · nothing has come back yet
```

The name is the model's basename (`kimi-k3`). Without a name, it says
`waiting · 12s`. This generic label means a request is outstanding and no stream
content has arrived; it does not diagnose a slow network or claim the model is
thinking. A known phase is shown separately because it has better information.

An advancing clock confirms the view is repainting. A still indicator alone
does not prove a freeze: reduced-motion views use static marks, and narrow rows
can omit the clock. `esc` interrupts the turn.

The waiting clock stops when the stream speaks or tools run. A tool uses its own
activity and elapsed time. The request after a three-minute `go test` starts a
new response clock rather than inheriting those three minutes.

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

## Why did the reply restart, what does "trying again" mean, and where did the text that was on screen go — the text it was writing disappeared

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

A bash command that was still younger than 3 seconds when you steered gets
`waiting for the running step`, and it is a wait of at most those few seconds: if the
command is still running when they are up it is moved to the background exactly as an
older one is, and your words go to the model then. Those seconds bound the handoff and
not the reply — the step may hold other tools, and the model still has to answer. The
clause you were shown is not rewritten, because it was true when it was sent, but the
transcript's own record of the correction says which of the two actually happened.

`stopped the running command` can appear at any age. A plain stop is never held for the
three seconds; it reaches the command as soon as you send it.
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
· 14:02 · 2m12s · 3 tool calls · $0.04 ·
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
box, a room's pinned header, a row of any open list or page, the task record card's title
and its keys line, and either row of the phone status deck.

**Things that share a line light only their own cells.** A chip on the task strip, a
picture on the tray above the box, one of the four answers on a landed card, one of the
two answers on the stop card, a chip or a link on an adaptive run's page: the one under
the pointer lights and its neighbours stay dark. The gap between two chips lights nothing
— it is a place to miss, not a door.

**A running foreground command has one narrow target inside its row.** Moving over the
row reveals `click to background` in the right-hand stat slot only while that exact bash
call can be kept. Moving onto those words lights only the clause, not the row; clicking
it keeps that command as a job and does not open the expansion. Moving elsewhere on the
same row gives the whole row its ordinary band, and clicking there opens it. The offer is
absent for other tools, a command already in the background, a forming or replayed row,
a row already marked `job N`, and a session over `--host`. It is not drawn at phone width,
where the full-frame tool sheet has no pointer targets.

**The tab bar of the places is one of them.** Each place's word is its own chip, clicking
it goes there, and the gap between two words is a place to miss. Under the bar, every
place answers the pointer the same way: the row you are hovering takes the band, and the
wheel walks the list three rows a turn.

**A few words inside a sentence brighten instead.** A task reference in a reply goes from
accent to ink and keeps its underline; the `+N` at the end of the task strip, a cut
table's foot, the jump-to-latest chip, the `Stop` on a room's facts row and the model's name at
the foot of the frame all go one step up in ink. A highlighted rectangle mid-paragraph
would be the one boxed thing on a surface with no boxes.

**The pointer and the cursor share one background; the chosen thing gets the louder
one.** Whether you reached a row with the mouse or with `↓`, the row you are on looks the
same — it does not change appearance depending on which hand you used. What tells the two
apart is the mark in front: `›` where enter would act, `·` where the pointer is.

The step above that is for the thing you have actually **chosen**, and it stays drawn
when nobody is touching the list: the roster row and the strip chip of the room you are
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
tab is just the project, and with no workspace at all it says `aforge`.

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

Some models put their working on the wire. The compact conversation hides this behind
the work disclosure. Open the work with `ctrl+e` to inspect it, then click the thought
block to expand or collapse that block. Inside the opened work, or on a task page,
streaming thinking uses a dim three-line window under a `thinking · N tok` header; the moment the first word of the reply lands it
collapses to one row — `thought for 6s · 148 tok · ctrl+e`. Clicking the row
reopens it; `ctrl+e` prioritizes the whole work disclosure when one is available. The count of tokens on the row is how much working the model wrote, and the
seconds are how long it spent.

Some models keep thinking in between the words of their own answer, a few tokens at a
time. That does not split the reply and does not stack up extra rows: the one thought row
above the answer keeps its place and its numbers grow, while the reply below streams on
unbroken. A new thought row only appears after a real boundary — a tool call, or the next
turn — because that is a genuinely new stretch of thinking.

Thinking is never saved into the conversation's record. A reopened session shows the
answers, not the working behind them — so a `thought for` row you can see now will not be
there after a restart, and that is deliberate.

## A reply that shows up as thinking, or a turn that seems not to have answered

It should not happen any more, and if you saw it before, this is what it was.

Some endpoints put the whole reply on the **working** channel and send no answer at all.
aforge used to draw that as a model that thought for a while and said nothing: a dim
`thought for 9s` row, no answer under it, and — because a reply with no words in it looks
exactly like a call that failed — the same question asked again at your expense.

Now the decision about which words are the answer is made once, where the wire is: **a
turn that asked for nothing and said nothing put its reply in its working, and the working
is the reply.** It arrives as the answer, it is rendered as markdown like any other answer,
and it is what a resumed conversation shows you later. A turn that called a tool and said
nothing is untouched — that is a model behaving, not a lost reply.

## `<think>` showed up in my answer

It does not any more. Some endpoints — anything reached through `AFORGE_BASE_URL`, and any
gateway in front of a raw open model — do not separate the model's working from its reply
and instead fence it inside the answer as `<think>…</think>`. aforge used to type the tag
and everything in it straight into your answer, and keep it in the conversation's record
as words the model had said out loud.

That working now goes where working goes: the dim `thought for` row above the reply. The
tag never reaches the answer and never reaches the record. `<thinking>` is read the same way.

Two limits worth knowing. The fence is only read as one when it **opens the reply** — a
`<think>` written in the middle of a paragraph is a model talking about the tag, and your
answer keeps its own words. And a fence that is opened and never closed leaves a reply with
no words outside it, which is the case above: the working is the reply.


## Task header status, timing, model and cost after breadcrumbs

The task breadcrumb keeps the current task bright and its ancestors quieter. When
space allows, it reserves a padded Back target by folding middle ancestors first.
The next row groups the outcome, elapsed time and activity on the left, with model,
effort and cost on the right. Stop stays at the far right while it is available.
All known facts fit on this one row on roomy frames; narrow frames use the existing
priority order and shorter labels. Unknown figures are absent.

When the task rail folds into compact chips, that row shares the task page's
reading margin. The chips retain their own inner padding; phone windows keep
the full-row task door.

## Stopped design task has a saved answer but an empty conversation

A finished or stopped design task shows its saved report when there are no
conversation messages to display. An older progress notice cannot hide that
answer. While work is still running, the task continues to show its current
progress and original prompt as they become available.

## Why progress stays compact until the answer is confirmed

While a response streams, its prose stays in the compact work area as a short
step heading. The same text channel can contain a lead-in to a tool call or the
answer itself, so the screen does not guess from the wording. Click the work or
press `ctrl+e` to inspect the complete words while they arrive.

When the response finishes with an answer and no tool calls, the full reply opens
as formatted text. Questions open at that same boundary, before later completion
checks finish. This means full answers no longer appear at full size token by
token in the compact view. A response that calls a tool stays a step. If work
continues later, earlier prose returns to the work hierarchy.

The same behavior applies inside task rooms. Saved answers remain readable when
you return, and completion still collapses the intermediate work. Explicitly
expanded work and `ui.work = open` keep the detailed reading view available.

A message queued beneath a streaming reply, or a notice displayed there, stays
below the complete answer when its response is confirmed. Stopping the turn keeps
its partial response dim even if a confirmation was already in flight.

If private work falls below a queued message, its finished work stays behind a
separate closed `worked` chip. Expanding that chip still reveals its details.
