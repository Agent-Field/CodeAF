# Keys, typing, and the mouse

## Which key sends, and which key opens a new line

`enter` sends the message you have typed.

`alt+enter` opens a new line inside the message without sending. `ctrl+j` does the
same thing — it is a second spelling for terminals that swallow `alt+enter`.

`shift+enter` is **not bound** anywhere in the v3 chat. If you press it, nothing
happens. Use `alt+enter` or `ctrl+j`.

What `enter` does depends on what is in the box:

- Text in the box: it is sent.
- An empty box with pictures in the attachment tray: it is still sent. A message of
  pictures and no words is a message.
- An empty box with a tool row selected: that row opens instead of anything being
  sent.
- An empty box with nothing selected: nothing happens.

While a turn is running, plain `enter` steers the running turn — your words land
inside it at its next step boundary. `ctrl+q` instead queues the message to run
*after* the current turn; an empty box does nothing. Until its turn starts, a dim
row above the box reads `  after yield · N`. If the queueing fails, aforge notes
`follow-up failed: <error>`.

## Interrupting a running turn — how to stop it

Press `esc` or `ctrl+c`. While a turn is running, both do the same thing: the turn
is stopped and everything it already said is kept.

What happens:

1. The session is told to stop, the state becomes `interrupted`, and a note
   `interrupted` is added to the conversation.
2. Any queued follow-ups are dropped, and aforge says so — `1 queued message
   dropped`, or `N queued messages dropped`.
3. `interrupted` stays as the status word until the next turn starts.

**What the screen says.** While a turn runs, the right end of the row under the
message box reads exactly `esc interrupt`. On the very first frame of a session the
conversation carries the note `esc or ctrl+c interrupts`.

**Limits.** Interrupting does nothing at all when no turn is running. `esc` reaches
the interrupt last: a history recall is cancelled first, rewind is armed on the way
past, and any open list or overlay takes the key before the message box sees it. So
`esc` while the command list or the `@` list is open closes that list and does
**not** interrupt.

**When no turn is running, `ctrl+c` quits aforge.** It writes your unsent draft to
disk first, then interrupts and closes the session. Only mid-turn is `ctrl+c` the
interrupt.

## Keys in the message box: sending, stopping, and queueing

These apply with no overlay up, no room open, and no mode on.

| Chord | What it does |
|---|---|
| `enter` | Send the message. Empty box with attachments still sends; empty box with a tool row selected opens that row |
| `alt+enter` | Open a new line in the message |
| `ctrl+j` | Same as `alt+enter` |
| `esc` | In order: cancel a history recall, then arm rewind, then interrupt the running turn |
| `esc` `esc` | Two presses inside a short window open rewind mode |
| `ctrl+c` | Turn running: interrupt. Nothing running: quit aforge |
| `ctrl+q` | Queue this message to run after the current turn. Empty box does nothing |

`shift+enter` is not bound. Use `alt+enter` or `ctrl+j` to open a line.

## Keys in the message box: opening things and moving the view

| Chord | What it does |
|---|---|
| `ctrl+o` | Selected landed card: open its output. Selected proposal: open its brief. Otherwise: fold or unfold this turn's tool cluster |
| `ctrl+b` | Enter copy mode — freeze the view so you can read and copy |
| `ctrl+s` | Hand the pointer to your terminal so you can drag-select. Toggles; any other key takes it back |
| `ctrl+,` | Open the settings panel |
| `ctrl+l` | Jump back to the live edge of the conversation |
| `ctrl+t` | Give the keyboard to the task roster. Press again or `esc` to take it back |
| `ctrl+e` | Empty box: open or close the latest completed turn's `▸ worked` chip, or the most recent thinking block when there is no chip. Otherwise: go to end of line |
| `pgup` / `pgdown` | Scroll one page — the height of the view minus one, never less than one row |
| `tab` | Open or commit path completion, over a command's path argument only |

## Keys in the message box: moving the caret

| Chord | What it does |
|---|---|
| `up` | Four meanings, tried in this order: move the caret up inside a multi-line message; walk back through history; over an empty box, select the previous tool row; scroll up one row |
| `down` | The mirror of `up` |
| `left` | Empty box: step back a level — close the room, else clear the selection. Two `left` presses inside about 600ms go home to the live edge. Non-empty box: move the caret left |
| `right` | Empty box: go into the next running task's room. Non-empty box: move the caret right |
| `ctrl+f` | Move the caret right, always. Never navigation |
| `home` / `ctrl+a` | Start of the current line |
| `end` | End of the current line, always |
| `ctrl+e` | End of the line — unless the box is empty, where it opens the latest completed turn's `▸ worked` chip, falling through to the most recent thinking block when there is no chip |
| any printing key | Types the character |

`home`, `end`, `up` and `down` work on the logical line — the run between newlines —
not on the row your terminal wrapped it onto. `up` only reaches history when the
caret is on the first logical line, and `down` only when it is on the last.

## Keys in the message box: deleting words and lines

| Chord | What it does |
|---|---|
| `backspace` | Delete the character behind the caret. Over an empty box with attachments, it removes the last attached picture instead |
| `delete` | Delete the character in front of the caret |
| `ctrl+u` | Delete to the start of **this line** — not the whole message |
| `super+backspace` | Same as `ctrl+u` (Mac `cmd+delete`) |
| `ctrl+w` | Delete the word behind the caret |
| `alt+backspace` | Same as `ctrl+w` |
| `ctrl+backspace` | Same as `ctrl+w` |
| `ctrl+h` | Deliberately not bound — some terminals send plain `backspace` as `ctrl+h` |

`super+backspace`, `alt+backspace` and `ctrl+backspace` only arrive at all on
terminals that report those modifiers, such as ones speaking the kitty keyboard
protocol or win32-input. aforge does not detect whether yours does.

## The message box itself

It is one line marked `› ` with no border, and it holds newlines, so it is a small
multi-line editor rather than a single-line field.

- It shows at most **6 rows** at once, further capped by your terminal height minus
  two, and never fewer than one row. It never takes more than 6 rows from the
  conversation no matter how large the paste.
- A longer message scrolls **inside** the box, following the caret. The rows scrolled
  past are marked with an ellipsis in the same two cells the `› ` occupies, so
  nothing shifts under your caret.
- Continuation rows are indented two cells to sit under the text. Soft wrapping
  breaks at the last space before the edge, and mid-word only when the line offers no
  space.

**Pasted text lands as one edit** with its newlines intact — it never submits line by
line. Bracketed paste is on. CRLF and bare CR become LF at the door.

Inside an open paste bracket, every key is text: `enter` and `ctrl+j` become a
newline, `tab` becomes a tab, everything else contributes its text. Nothing between
the brackets can submit, interrupt, or answer a question. `ctrl+c` is the one
exception and still works. A bracket that goes quiet for 2 seconds is treated as
abandoned, flushed, and the keyboard handed back.

A paste while copy mode is up is **declined** — nothing happens, and your clipboard
still holds the text.

## Your unsent draft is kept

The half-written message survives closing the window, a crash, `/new`, and a session
that has moved on. There is nothing to press; it is automatic.

- It is written 300ms after you stop typing, and again synchronously on quit before
  anything else happens.
- It is cleared **only** when you send it, or queue it as a follow-up. `/new` does
  **not** clear it.
- The file is keyed by the directory plus this process's id, and is written with mode
  0600.
- At startup, if this window's own draft file is missing, aforge takes the newest
  draft in the same directory whose process is no longer running, moves it into this
  window's name, and loads it. Files belonging to a process that is still alive are
  never touched, and older orphans are left where they are. An empty orphan is
  deleted. A process id that cannot be checked is treated as still alive — aforge
  errs toward leaving your sentence on disk.
- CR and CRLF in a restored draft are normalised to LF.
- A failed write is dropped in silence. No draft is kept at all if aforge was started
  without a draft file.

## Getting back something you typed before

`up`, with the caret on the first line of the message box, walks back through the
prompts you typed before, newest first. `down` walks forward again toward your live
draft. `esc` cancels the walk and restores your own sentence exactly as you left it.

- Your live draft is stashed on the way in, and comes back on `esc` or on walking
  forward past the newest entry.
- Walking past the oldest entry stays put rather than emptying the box.
- The list is built once per walk, in two passes: everything you typed **in this
  directory** first, then everything you typed anywhere. Duplicates are dropped.
  Each pass goes 200 entries deep.

**Slash commands are recalled too.** Every non-empty line you press `enter` on is
remembered, commands included, and a command you pick out of the command list is
remembered as well.

With history not wired up (`--no-history`), `up` takes nothing and keeps its other
meanings.

## Attaching a picture

There are exactly two ways in, and no third.

1. **`/image <path>`.** `~` becomes your home directory, a relative path is resolved
   against the conversation's directory — or against **your own machine's** working
   directory over `--host` — and an absolute path is left alone.
2. **The `@` completion.** An image row in the list is tagged `img`. Choosing it
   **removes the half-typed `@token` from your sentence** and puts the file in the
   tray, instead of typing a path.

**Drag-and-drop does not attach, and pasting an image does not attach.** A terminal
drag-drop arrives as pasted *text*, and aforge inserts it into the message as text.
Nothing in the paste path looks at the text for a picture. Typing out an `@` path to
an image by hand does not attach either — attaching happens when you choose the
completion row.

**What is accepted:** `.png`, `.jpg`, `.jpeg`, `.webp` and `.gif`, case-insensitive.
Those five are exactly what aforge will send. The ceiling is **10 MB per picture**,
checked against the file's size first and again against the bytes actually read. The
same path attached twice is one chip.

**You do not always have to attach.** A file already on the machine can be looked
at without the tray: ask aforge to look at it by path and it uses its `view_image`
tool, which opens the picture with the **looking model** — the same one model that
answers every picture question here, whether you attached the file or not.
Attaching is for a picture you are handing over as part of what you are saying.

**The tray.** Attached pictures sit in a one-row tray directly above the message box,
one dim chip each, drawn as `▣ name.png` — `*` on an ASCII terminal. The message box
stays the sentence. `backspace` over an empty box drops the last chip, and clicking a
chip removes that one.

## What aforge says when a picture is refused

| Situation | Exact text |
|---|---|
| `/image` with no path | `/image takes a path · try /image shot.png` |
| Not one of the five types | `<basename> is not a picture · png, jpeg, webp and gif are` |
| Missing file, or a directory | `no such picture: <path as typed>` |
| Already in the tray | `<basename> is already attached` |
| Unreadable when you send | `could not read <basename>` |
| Over the ceiling when you send | `<basename> is over the 10MB image limit` |

**A refusal keeps your pictures.** The tray is emptied while the message is in
flight; if sending fails, the chips are put back, in front of anything attached in
the meantime, without duplicating.

## Sending a message that has pictures

`enter` with a full tray sends. The files are read at the moment you press `enter`.
The line in the conversation becomes your sentence plus the file names in dim square
brackets — `› what is wrong with this  [chart.png]`. A message with pictures and no
words is still a message.

A picture never travels as a path. It travels as bytes alongside the message, which
is why the path is rooted on your **local** machine even on a remote session. The
tray is not rendered as a picture — a terminal cell is not a place to show one.

A command with a full tray is still a command: `/image` adds a second picture rather
than sending the first.

## Completing a path with `@`

Type `@` and aforge offers **tasks first, then files**, in one list under the message
box. It opens on the bare `@` — you do not have to type a letter first. It closes on
`esc`, on committing, or when the token stops being one.

The token is found by walking back from the caret to a space, a newline, or the start
of the message; that run must **begin** with `@`. So an `@` in the middle of a word —
an email address, a Go doc link — never opens the list.

**What it walks:** the conversation's workspace, or **your own machine's** working
directory over `--host`. Skipped: `.git`, `vendor`, `node_modules`, every
dot-directory, every dot-file, and every symlink. Unreadable directories are skipped
rather than fatal. The walk is capped at **10,000 files**, and paths are stored
relative to the root with forward slashes.

**The walk runs once per session.** A file created part-way through the conversation
will not appear in the list.

Ranking puts a prefix match above a substring above a subsequence; the whole path and
the base name are both tried at each tier, the base name a hair below the path.
Inside a tier the earlier match wins, then the shorter path. File hits are capped at
32. The list shows 8 rows, or **14** when there are tasks on it; task rows are capped
at 8 and searched to a pool of 40.

While the walk is still running the list reads exactly `  looking…`; with no match it
reads `  no file matches`. Only the file half waits — the task index lands first.

## What `@` puts into your message

- **A file:** the path replaces what you typed after the `@`, and **the `@` stays**.
  Nothing is read at this point. `@internal/session/agent.go` is sent exactly as it
  stands, and aforge's read tool resolves it if it wants to.
- **An image:** the whole half-typed `@token` is removed and the file is attached
  instead.
- **A task:** the task's name goes in after the `@`. The pointer block is minted when
  you send, not here.
- **Under a command's path argument:** the path replaces the argument whole, with no
  `@` in front, and an image is written into the line like any other file.

**When you send,** every `@<slug>` that names a task aforge already knows about grows
a pointer-block footnote after the message — one block per task, in token order,
deduplicated. Unknown tokens are left alone in silence. This resolves against the
snapshot already in memory and never touches the disk, so a slug pasted whole and
sent in the same beat resolves to nothing and stays plain text. The entry remembered
for `↑` is the sentence as you typed it, before expansion.

**The honest limit: only `/image ` and `/export ` get path completion.** That is the
whole list. Any other command that takes a path gets no completion at all, and says
nothing about it.

## Keys in the command list and the `@` list

These two lists are **not modal** — you keep typing into the same box and the list
follows what you type. Only these keys are taken from you:

| Chord | What it does |
|---|---|
| `up` / `ctrl+p` | Move the list cursor up |
| `down` / `ctrl+n` | Move the list cursor down |
| `esc` | Close the list. It does **not** interrupt a running turn |
| `enter` | Command list: run the highlighted command; if nothing matched, the line is sent as typed. `@` list: insert the highlighted task or file; if nothing is picked, the line is sent |
| `tab` | Read **before** the list. It only opens or commits an *argument* completion, over `/image ` or `/export ` |
| `enter`, with an argument completion open | Closes the list and runs the line **as typed**. Your path is never swapped for the top-ranked row |

## Keys in the model picker and the sessions roster

**Model picker** — opened by `/model` with no argument, or by clicking the model name
in the status row:

`esc` close · `enter` switch to the highlighted model · `ctrl+t` cycle the reasoning
effort · `up`/`ctrl+p`, `down`/`ctrl+n`, `pgup`, `pgdown` walk the list ·
`backspace`, `delete`, `ctrl+u`, `ctrl+w`, `left`/`ctrl+b`, `right`/`ctrl+f`,
`home`/`ctrl+a`, `end`/`ctrl+e` edit the filter · anything else types into it.

Its placeholder reads exactly `filter · ↑↓ · ctrl+t effort · enter switch · esc
cancel`.

**Sessions roster** — opened by `/resume`: the same key map, except `enter` opens the
selected session. Its placeholder reads `filter · ↑↓ · enter open · esc cancel`.

**Welcome box:** it takes only two keys, and only over an empty message box —
`up`/`down` walk the recent sessions and `enter` opens the selected one. Every other
key dismisses the box and then does whatever it normally does.

Both pickers are modal: while one is up, every chord except `ctrl+c` belongs to it.

## Keys when aforge asks you a question

**An approval question:** `y` allow once · `a` or `t` always — refused when it would
do nothing · `n`, `d` or `esc` deny. Every other key does nothing, but it **stops the
countdown**. `ctrl+c` is handed back to the message box. On the second beat of
"always" for a bash command, `1`–`9` pick a shape and `esc` goes back.

**A task proposal** is not modal — the message box stays live as a redirect lane.
Always available: `enter` answers the focused option, `esc` says no, and `ctrl+e`
opens the brief over an empty box. Over an **empty box only**: `left`/`right` move
the focus, `y` yes, `r` redirect, `n` no, and `1`–`4` pick the model.

**A connect offer:** `enter` or `y` yes · `esc` or `n` no. While its key box is open,
every key goes into that box except `enter`, which submits, and `esc`, which
declines.

**A harness offer:** `enter` or `y` yes · `esc` or `n` no. Everything else does
nothing.

**The steer guard**, raised when you press `enter` in a room whose node is not
listening: `r` revive and send · `m` send to main · `esc` cancel and keep your words ·
`ctrl+c` handed back · everything else does nothing. Its row reads
`[r] revive and send · [m] send to main · [esc] cancel`.

## Keys in the settings panel and the other panels

**Settings panel** (`ctrl+,`): `esc` backs out one layer at a time — search, then an
open account, then the panel · `left`/`shift+tab` and `right`/`tab` change tab ·
`up`/`ctrl+p`, `down`/`ctrl+n`, `pgup`, `pgdown`, `home`, `end` walk · `enter` and
`space` activate · `backspace`, `ctrl+u`, `ctrl+w` edit the search · anything else
types into it.

**Connections panel:** `esc` · `up`/`ctrl+p` · `down`/`ctrl+n` · `pgup` · `pgdown` ·
`enter`. Its filter placeholder reads `filter · ↑↓ · enter connect · esc close`.

**Harness panel:** `esc` · `up`/`ctrl+p` · `down`/`ctrl+n` · `pgup` · `pgdown` ·
`enter`.

**Permissions panel:** `esc` — which drops an armed confirmation first, then closes ·
`up`/`ctrl+p` · `down`/`ctrl+n` · `pgup` · `pgdown` · `enter` **or `d`** to drop the
line under the cursor. Press it twice; the first press arms it.

**Status deck sheet** (narrow terminals): `esc` or `q` close · `up`/`k`, `down`/`j`
move · `enter` activate. Its foot reads `esc close · ↑↓ move`.

**Phone tool detail sheet:** `esc`, `q`, `left` and `enter` all close · `up`/`k`,
`down`/`j` scroll · `pgup`/`ctrl+b`, `pgdown`/`ctrl+f`/`space` page · `home`/`g` top ·
`end`/`G` bottom · `ctrl+o` lifts the line cap. Its foot reads exactly
`esc close · ↑↓ scroll`, or `esc close · ↑↓ scroll · tap … for the rest`.

All of these are modal: while one is up, every chord except `ctrl+c` belongs to it.

## Keys in the task roster and inside a room

**While the task roster holds the keyboard** (`ctrl+t`): `esc` gives the keyboard
back · `up`/`down` move · `right`/`left` fold and unfold the group · `enter` opens
that row's room. Its hint reads `↑↓ move · →← fold · enter open · esc`.

**With a room open:** `esc` leaves the room, though a history recall walk is
cancelled first · `enter` steers the node · `ctrl+b` freezes the room's own rows for
copying, not the conversation's · `pgup`/`pgdown` page · `up`/`down` scroll, but only
over an empty message box. `left` is deliberately **not** taken here — it falls
through to the message box's back-navigation.

A click on empty space does nothing anywhere else, but inside a room it is the way
out.

**`x` asks to stop the work.** It is taken on the roster's focused row and inside a
room or an adaptive run's page, and only over an empty message box — the moment there
is a sentence in the box it is the letter `x`. It never stops anything by itself: it
raises a card, and the card is answered below.

The tasks pages describe what rooms and the roster are for.

## Stopping work with `x` — the confirmation card

`x` raises one card above the message box:

```
? Stop this task? Its work halts; the branch it wrote on is kept.
  [stop it]   [keep going]
```

On an adaptive run's page it reads `Stop this run? In-flight nodes halt; partial
results stay.` A harness being designed is a task, so `x` on its row reaches it like
any other — and the card says what is actually true of it: `Stop this task? The page
it is writing is dropped; nothing was saved.` It has no branch and wrote no files, so
the reassurance about a kept branch would be pointing at nothing.

**The cursor opens on `keep going`.** `left`/`right` move it, `enter` takes the
answer under it, `esc` is `keep going`, and every other key does nothing while the
card is up. A click on either answer is that answer, and a click anywhere else on
that row does nothing rather than falling through to the box.

**There is no bypass key and no "don't ask me again".** Stopping cannot be undone —
the worker's turn ends where it stands — so the card is always asked, and pressing
`x` again while it is up is a keystroke the card swallows.

**`esc` never stops anything.** It closes the card, then a room, then a page, in that
order.

Nothing is thrown away by stopping: see the tasks page for what a stopped task and a
stopped run keep.

## The mouse: what you can click

aforge owns the pointer by default, using all-motion tracking so hover works.

Only the left button acts. A press is resolved in this order:

1. The settings panel, the status deck, or the phone tool sheet — each takes **every**
   press inside its frame, padding included.
2. An approval question block, then a connect offer, then a harness offer.
3. The harness panel, the permissions panel, the connections panel — a press on a row
   acts, and a press anywhere else **closes** the list.
4. An attachment chip — removes it.
5. The jump-to-latest chip.
6. A stop target: the confirmation card's two answers while it is up, and the `✕` at
   the right end of a room's pinned header. On a phone-width terminal the `✕`'s hit
   box is three rows tall, because a finger is about that wide.
7. Task strip chips, then the rail column, then a proposal's choices row. On a wide
   terminal the strip chip the roster's cursor is on carries a `✕` of its own, and
   pressing it asks to stop that work instead of opening its room.
8. The model name in the status row, which opens the model picker. A press elsewhere
   on the status row falls through. On a narrow terminal the whole two-row deck
   answers.
9. The body: an inline **task link** inside prose, which is the one mouse-only target
   on the surface; a cut markdown table's foot; a waiting sign-in, where a click
   copies its link; a thinking block, clickable over its whole height; a tool row,
   which opens its expansion, or the full-frame sheet on a narrow terminal; the
   `N earlier tool calls` fold; the `… N more lines` foot, which lifts the cap; a
   spawn card, which opens the node's room, or its brief if there is no node yet; and
   a landed card, which opens its full context.

**A click on empty space does nothing** — there is no empty-space gesture — except
inside a room, where it is the way out. **A click in copy mode acts on nothing**,
because the rows there are a frozen snapshot.

**Hover** raises the row under the pointer one step in background and brightens its
marker. Rows that answer to nothing do not react. There is no hover in copy mode, on
the linear/screen-reader tier, or in the phone tool sheet.

## Scrolling

The wheel moves three rows per notch, on whichever surface owns the frame. It is
routed to copy mode, then the settings panel, then the status deck, then the phone
tool sheet, then the fullscreen roster, then an open room, and otherwise the
conversation.

Reaching the bottom **re-arms sticking**, so new replies follow along again. Scrolling
up drops out of it.

`pgup` and `pgdown` move the height of the view minus one row, never fewer than one.

**The jump-to-latest chip** is one dim right-aligned chip reading `↓ latest · ctrl+l`
— `v latest · ctrl+l` on the linear tier — and it appears only when you are parked
away from the live edge. It is drawn into the first row of the frame's existing
breathing gap, so it never takes a row of its own; on a window too short to have a
gap it is not drawn at all, though `ctrl+l` still works. It is dim normally and
accent-coloured under the pointer. Clicking it, or pressing `ctrl+l`, rejoins the live
edge — the room's edge if a room is open. It is not shown while copy mode, a room, or
the fullscreen roster is up.

## Selecting text with your mouse

aforge owns the pointer by default, and owning it kills your terminal's own
drag-to-select. There is no scrollback to fall back on, because aforge runs in the
alternate screen.

**`ctrl+s` hands the pointer back** so a drag selects text the way it does everywhere
else. The `/select` command does the same thing. Your **next keystroke takes the
pointer back** — there is no mode to leave. `ctrl+s` is a toggle, and it is the one
key excepted from that automatic handback.

The hover highlight is dropped along with the pointer, so no band is left lit under a
pointer that has moved on. While the pointer is out, the row under the message box
reads exactly `drag to select · any key ends it`.

If you have turned the `ui.mouse` setting off, your terminal already has every drag
permanently, and `/select` says exactly:

```
your terminal already has the pointer — drag to select.
```

## Copy mode: taking text out of the conversation

`ctrl+b` freezes the view and hands the keyboard to a reader, so you can pull text out
of a surface that runs in the alternate screen where your terminal's own selection is
gone. The `/copy` command does the same. Inside a room, `ctrl+b` freezes **the room's
rows** rather than the conversation's.

| Chord | What it does |
|---|---|
| `up`/`k`, `down`/`j` | Move |
| `pgup`, `pgdown` | Page |
| `home`, `end` | Jump to top or bottom |
| `v` | Drop or lift the mark |
| `a` | Take the block under the cursor |
| `y` | Yank the selection |
| `esc`, `ctrl+b`, `q` | Leave |

Copy mode takes **every other key too**, and does nothing with them.

`a` asks the narrower question first — a run of fenced **code** rows. Press `a` again
to widen to the whole answer around it. A blank row belongs to nothing, and `a` there
does nothing.

`y` **stays** in copy mode and lifts the mark, so a second `y` cannot copy the same
span by accident.

The status line reads exactly `COPY`, or `COPY · N lines` when more than one line is
selected. The row under the box reads `v select · a block · y yank · esc`.

## What copy mode copies, and what it refuses

Freezing snapshots the rows both painted and plain. The conversation underneath keeps
streaming, and none of it moves the rows you are reading. Leaving rejoins the live
edge — the room's edge if a room was frozen.

**What comes out** is the plain text with the left rail lifted — the stem under an
expanded tool call, the hairline beside a fenced block or a blockquote — along with
the indent in front of it and any trailing padding. The one-off marks `› ` and `· `
are deliberately **kept**, because they say who was speaking.

**How it reaches your clipboard:** OSC 52, written in band, so it works over ssh and
inside a container with no display. When `TERM` starts with `screen` or `tmux` it is
wrapped in tmux's DCS passthrough with every ESC doubled. It uses the clipboard
selection, not the primary one.

**Refusals while copy mode is up:**

- A click does nothing.
- A paste is declined, and your clipboard keeps the text.
- There is no hover.
- The selection highlight is the hover background, so a terminal below ANSI256 gets
  no highlight at all. Read the span off the `COPY · N lines` count instead.

**An image drawn in an expansion copies as what it is on screen** — rows of `▀`, with
the colour stripped, which is no use to anybody. Take the dim line under it instead:
it is the picture's whole absolute path, and it is a hyperlink in terminals that make
one.

## Chords that mean more than one thing

Two chords carry unrelated meanings. Which one you get depends on where you are.

**`ctrl+t` — two meanings:**

| Where | What it does |
|---|---|
| Message box | Give the keyboard to the task roster. Press again or `esc` to take it back |
| Model picker only | Cycle the reasoning effort |

**`ctrl+o` — four meanings:**

| Where | What it does |
|---|---|
| A landed card is selected | Open its output |
| A proposal is selected | Open its brief |
| Phone tool detail sheet | Lift the line cap |
| Nothing selected | Fold or unfold this turn's tool cluster |

Two more chords surprise people:

- **`ctrl+b` is copy mode, not emacs "left".** The alternate screen took your
  terminal's selection away, and copy mode is what buys it back.
- **`ctrl+e` means two things** depending on whether the box is empty: end of line
  when there is text, open the most recent thinking block when there is not.

And over an **empty** box, `left` and `right` are navigation rather than caret
movement. `ctrl+f` never is — it always moves the caret right.

## Chords that are not bound

These do nothing in the v3 chat. If you expect one of them, here is the straight
answer:

| Chord | Status |
|---|---|
| `shift+enter` | Not bound. Use `alt+enter` or `ctrl+j` to open a new line |
| `ctrl+d` | Not bound |
| `ctrl+k` | Not bound |
| `ctrl+r` | Not bound |
| `ctrl+v` | Not bound. Paste with your terminal's own paste; aforge reads bracketed paste |
| `ctrl+g`, `ctrl+x`, `ctrl+y`, `ctrl+z` | Not bound |
| `ctrl+h` | Deliberately not bound, because some terminals send plain `backspace` as `ctrl+h` |

A key that is not bound falls through to "does this key carry text". If it carries
text it types; if it does not, nothing happens.

## A settings change lands one turn later

The `ui.mouse` and `ui.timestamps` settings are read at boot and again at the end of
every turn — not the moment you change them. The same is true of the approval posture
and the consent countdown.

So if you turn the mouse off, or turn timestamps on, mid-turn, the change takes hold
**one turn later**. Nothing is wrong; the surface has not re-read the setting yet.

This holds however the row was changed — in the panel, or by asking aforge to do it with
`change_setting`. The row is written straight away and the transcript says so; the screen
picks it up when the turn ends.

With `ui.mouse` off, every drag belongs to your terminal permanently, and `ctrl+s` has
nothing to hand over.

## Thinking is shown but never saved

Models that stream their working get a dim italic block above the answer, headed
`⠿ thinking · N tok · ctrl+e`.

While it streams, only the **last 3 wrapped lines** show, each painted a step further
along a fade so the newest reads brightest. The moment the turn says anything that is
not reasoning, the block collapses to one row reading
`thought for Ns · N tok · ctrl+e`.

Open it with `ctrl+e` over an empty message box — which opens the most recent block —
or by clicking anywhere on the block. Opening **latches** your choice, so the
automatic collapse can never close a block you opened. An opened live block shows the
whole buffer, not the 3-line window.

An expanded block is capped at **200 rows**, and says how much is held back. The token
count is an estimate at 4 bytes per token.

**Reasoning is never written to the session file.** A resumed conversation shows the
answers, not the thinking.

## What does the indented part mean?

Flush-left text is said to you: your messages and aforge's trailing answer. Text with a
two-column gutter is work done on your behalf: thinking, tool calls and their details or
results, and assistant text that was followed by another call. Below 60 columns the
gutter disappears and the dim treatment carries the same distinction.

## How do I see what aforge did?

When a successful turn has work and a trailing answer, the finished work collapses to
one indented chip between your message and the answer, such as
`▸ worked 47s · thought 6s · 6 tool calls · ctrl+e`. Its figures are the whole turn's
elapsed time, the thinking block's time when there was one, and the real call count.

Click the chip or press `ctrl+e` over an empty message box to open or close it. There is
no transcript cursor, so the key chooses the latest completed turn's work in the
conversation. Opening restores the existing bounded views: thinking remains its
own chip and only the latest 3 tool calls show until those are opened separately.
Questions, approval prompts, failure lines, text-only turns, and work with no trailing
answer are never hidden. Fold state belongs to this window; resumed sessions derive
fresh closed chips from their saved entries.

**The chip is the conversation's alone.** A task's room, and a node's transcript inside an
adaptive run's page, never fold their work: those pages are the machinery, and a chip there
would hide the only thing on them. So in a room `ctrl+e` over an empty box opens the
thinking block, and `ui.work` changes nothing.

## How do I keep everything expanded?

Set `ui.work` to `open` to keep completed work visible. `fold` is the default. The row
live-applies on the next render; work stays indented in either mode.

## Things this page does not cover

- **Slash commands** — what `/image`, `/export`, `/select`, `/copy`, `/model`,
  `/resume`, `/permissions` and the rest do: the commands page.
- **The status line, the legend under the box, and the layout**: the screen page.
- **Tasks, rooms, proposals and the roster**: the tasks pages.
- **Rewind**, which `esc` `esc` opens: the sessions and rewind page.
