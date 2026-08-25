# Keys, typing, and the mouse

## Which key sends, and which key opens a new line

`enter` sends the message you have typed.

`alt+enter` opens a new line inside the message without sending. `ctrl+j` does the
same thing — it is a second spelling for terminals that swallow `alt+enter`.

`shift+enter` does **not** open a line. While a turn is running it **stops the answer
and sends what you have typed** — see "Interrupt and say something new in one key"
below. At rest it does nothing at all. Use `alt+enter` or `ctrl+j` to open a line.

`ctrl+enter` sends it as **something to keep true** — a standing order — instead of
work to do once. The standing orders page has the whole of it.

What `enter` does depends on what is in the box:

- Text in the box: it is sent.
- An empty box with pictures in the attachment tray: it is still sent. A message of
  pictures and no words is a message.
- An empty box with a tool row selected: that row opens instead of anything being
  sent.
- An empty box with nothing selected: nothing happens.

While a turn is running, plain `enter` **holds** the message instead of sending it —
see "Typing while an answer is still coming" below. `ctrl+q` instead hands the message
to the session there and then, to run *after* the current turn; an empty box does
nothing. Until its turn starts, a dim row above the box reads `  after yield · N`. If
the queueing fails, aforge notes `follow-up failed: <error>`.

## Typing while an answer is still coming — interrupting and steering

Typing is never blocked. The box works normally while an answer streams.

`enter` while a turn is running does **not** send. The message is held on the surface
and drawn in its own block directly above the box, in your own hue, with a dim line
under it:

```
› do much more of a deep research please
  waits for this answer · esc stops and sends · ↑ or click to edit
```

It is not written into the conversation and it is never drawn inside the streaming
reply. The box is cleared, so you can keep typing.

| What you do | What happens |
|---|---|
| the answer finishes | the waiting message sends itself as an ordinary new turn |
| several are waiting | one per finished turn, oldest first, in the order you typed them |
| `esc` | stops the answer and sends the waiting message immediately |
| `shift+enter` instead of `enter` | stops the answer and sends the sentence in one key — see below |
| `↑` over an empty box | takes the newest waiting message back into the box to edit |
| click the block | takes **that** message back into the box to edit |
| `enter` again | holds the edited sentence again |

Attachments in the tray go with the held message, and come back on the tray if you take
it back. `/`-commands are **not** held: a slash command is something you said to this
surface rather than to the model, and it runs at once.

**Limits.** `esc` with nothing waiting is exactly the plain interrupt it always was.
The held message is dropped, with a note — `1 waiting message dropped` or `N waiting
messages dropped` — if the conversation is replaced under it by `/new` or by opening a
session from the welcome box. Inside a **task room** `enter` steers the node instead
and nothing is held; that is the room's own key (see the room section below).

**Why it waits rather than going straight in.** The session can take a message into a
running turn, but only at a *step boundary* — before the next model request. A turn
whose last request has already gone out has no boundary left, so a message pushed into
it would land in the transcript with nothing coming to answer it. Waiting for the turn
to end means the message always gets a reply, and it is what makes the message editable
until it goes.

## Interrupt and say something new in one key — barge in, stop it and tell it something else

`shift+enter` while a turn is running **stops the answer and sends what is in the box**,
as one gesture. It is the two keys `enter` then `esc` collapsed into one, for the moment
you are watching an answer go the wrong way and want to say "no — the other file" *now*
rather than after it finishes.

What happens, in order:

1. Your sentence goes onto the waiting queue exactly as plain `enter` would put it there.
2. The turn is interrupted: everything it already said is **kept**, and the note
   `interrupted` is added, exactly as `esc` does it.
3. When that turn has actually finished stopping, your message opens the **next** turn.

Nothing is sent into the turn you stopped. The transcript reads in the order it
happened: the partial answer, the `interrupted` note, then your message.

**The box is cleared** the moment you press it, and the draft file it came from is done
with — from your side you have said the thing. Attachments in the tray go with it.

**What it does not do.** A `/`-command is run at once and the turn is **left running** —
a slash command is something you said to aforge rather than to the model, so there is
nothing to interrupt for. The same is true of a live `/task` or `/stand` tag, of a picked
harness, and of a refusal. The rule is simple: the turn is stopped only if the key
actually queued a message.

**With an empty box it does nothing at all** — not even a plain interrupt. Use `esc` for
that. With nothing running it also does nothing: `enter` already sends.

**It marks nothing.** `ctrl+enter` is the chord that means "keep this true"; this one
means "instead of that". One key does not do both.

**Where it does not exist.** Inside a **task room** there is nothing for it to mean —
`enter` in a room steers the node there and then, with no queue to jump, and a room's way
of ending work is `x` and a card that asks first. The chord is ignored there.

**Terminals that cannot send it.** `shift+enter` reaches a program only where the terminal
can tell it apart from a plain `enter` — the kitty keyboard protocol, xterm's
modifyOtherKeys, or win32-input. Where it cannot, the key arrives as an ordinary `enter`
and your message simply **waits**, which is the safe half of the same meaning; nothing is
lost and nothing breaks. On those terminals aforge also **never advertises the chord**:
the line under the box keeps saying `esc interrupt`. If you never see
`shift+enter stops and sends` there, that is why — use `enter` then `esc` instead, which
works everywhere.

**The line that teaches it.** While a turn is running and you have typed something, the
right end of the row under the message box reads exactly:

```
enter waits · shift+enter stops and sends
```

It is shown only in that state — a turn running, something in the box, and a terminal that
can deliver the chord. Over an empty box it goes back to `esc interrupt`.

## I typed while it was working — did my message get lost?

No. There are two moments, and both end in an answer.

**Early in the turn**, your words land at the turn's next step boundary — between
one tool batch and the next request — so the model reads them as part of the turn
it is already in, and answers them there.

**In the last seconds of a turn** — while the final reply is streaming, or while
aforge is naming the session and doing its tail work — there is no step boundary
left, because the turn's last request has already gone out. Your message still
lands in the conversation, in your own words, and aforge then starts one more turn
by itself to answer it. You see your line, a pause, and then a reply. You do not
have to type it again.

The same is true of a task or a background job that finishes in that window: its
note lands and aforge speaks about it rather than leaving it sitting there.

The one thing that is not answered is a message you queued with `ctrl+q` for a
turn you then **interrupted**. A drain never restarts a turn you stopped, so those
are dropped — press `enter` again to send it.

## Interrupting a running turn — how do I stop it mid answer

Press `esc` or `ctrl+c`. While a turn is running, both do the same thing: the turn
is stopped and everything it already said is kept.

What happens:

1. The session is told to stop, and a note `stopped` is added to the conversation.
2. The screen stops on the key: every spinner goes, and every call that was running keeps
   the time it ran until you stopped it.
3. Any queued follow-ups are dropped, and aforge says so — `1 queued message
   dropped`, or `N queued messages dropped`.
4. The status word becomes `stopping`, then `interrupted`, and `interrupted` stays as the
   status word until the next turn starts.
5. If a message of yours was **waiting** for that answer, it is *not* dropped: it sends
   immediately as the next turn. That is the whole difference `esc` makes while
   something is waiting.

**The words aforge uses for one stop.** They are four slots and one key press, so they
are worth reading together: `stopping` is the status word while the turn is being let go,
`interrupted` is the status word once it is over, `· stopped` is the note left in the
conversation, and `▸ stopped by you at 40s` is the chip a stopped turn collapses to. If
you are looking for the word *interrupted* anywhere else on the screen, that is where it
is — the status line, and only after the turn has truly ended.

**What the screen says.** While a turn runs, the right end of the row under the
message box reads exactly `esc interrupt` — or `enter waits · shift+enter stops and
sends` while you have typed something and this terminal can deliver that chord, or `esc
stops and sends` while a message of yours is already waiting for the answer to finish. On
the very first frame of a session the conversation carries the note
`esc interrupts · ctrl+c twice quits`.

**Stopping it and saying something new at once.** `shift+enter` does both in one key —
see "Interrupt and say something new in one key" above. `esc` on its own stops without
sending anything you have not already committed with `enter`.

**Limits.** Interrupting does nothing at all when no turn is running. `esc` reaches
the interrupt last: a history recall is cancelled first, rewind is armed on the way
past, and any open list or overlay takes the key before the message box sees it. So
`esc` while the command list or the `@` list is open closes that list and does
**not** interrupt.

**Mid-turn `ctrl+c` only ever interrupts — it never leaves.** Pressing it a second
time straight away does not quit either: the press that stopped the turn does not
arm the door, so the second press only arms it and a third one is needed to leave.
See "Quitting aforge — how do I exit, close it, or why did ctrl+c not quit" below.

## Why is the turn still finishing after esc — the stopping window

`esc` cancels the turn on the keystroke, but the turn does not close on the keystroke. A
`bash` call whose command left something holding its output waits up to three seconds
before the pipes are forced shut, and a `jobs` kill spends two seconds on a polite signal
and two more on the one that is not polite. For those seconds the status line reads
`stopping` rather than `interrupted`, and that is the honest word: the work is being let
go rather than gone.

**Nothing moves in that window and nothing new is drawn.** The spinners are already gone
from the status line and from every tool row. Any consent question, account offer or
harness offer that was open is taken down on the key, because each was about work that is
now over. Whatever the model says while the turn winds down is not shown — a sentence it
was still speaking stops where it was, and a call it was half-way through asking for never
becomes a row. Two things do still land, because neither can draw anything new: a call
that was **already** on screen reports its own result if it returns in that moment, and
what the turn spent is still counted.

**No key makes it stop harder, and there is no second stage.** A second `esc` inside half
a second is the rewind's door and `ctrl+c` is the quit arm, so neither is free — and there
would be nothing behind a third key: the waits that make this window long are inside a
tool that has already been cancelled. If something genuinely will not let go, `ctrl+c`
twice quits and takes it with it.

## Quitting aforge — how do I exit aforge, how do I close aforge, or why did ctrl+c not quit

**`ctrl+c` twice.** One press does not leave. The first press *arms* the door and the
right end of the row under the message box reads exactly:

```
ctrl+c again to quit
```

Press `ctrl+c` again within **1.5 seconds** and aforge exits. Anything else — any
other key, or letting the 1.5 seconds lapse — puts the door back and the hint leaves
the screen. Press it once more and you get the same arm again.

**Why it takes two.** One keystroke used to end the session outright, and that
keystroke is the one every terminal habit tells you to hit when something seems stuck.
It could end a session that was running tasks, holding live background jobs, and still
carrying a message you had typed and pressed `enter` on.

**What is running is named before it stops, across every conversation this terminal
holds.** If there is more than one open, the armed line says how many first — a person who
has forgotten they left something open in another project needs that number before the work
count means anything:

```
ctrl+c again to quit · a task will stop
ctrl+c again to quit · 2 tasks and a job will stop
ctrl+c again to quit · 3 conversations · 2 tasks and a job will stop
```

Each clause is absent when it is zero: one conversation drops the first, nothing running
drops the second, and a quiet single conversation reads exactly `ctrl+c again to quit`.

**The task count covers every open conversation; the background-job count covers only the
one on screen.** There is no way to ask an agent you are not drawing what shells it has
promoted, so the line is short of a fact there rather than guessing at one.

**Mid-turn it is still only the interrupt.** While an answer is streaming, `ctrl+c` is
the same key `esc` is: it stops the turn and does **not** arm the door. So the two-tap
people make mid-turn — press it again, harder — stops the model once and then arms;
you would have to press a third time to leave.

**It works over everything.** `ctrl+c` is read above every picker, panel, room, mode
and paste bracket — leaving is never modal. Pressing it with the model picker or the
settings panel up does not close them: it arms the door underneath, the hint slot
shows `ctrl+c again to quit`, and the second press leaves with the panel still up.

**What quitting does.** Your unsent draft is written to disk first, with any message
still waiting for an answer folded in underneath it, then the turn is interrupted and
the session is closed. Nothing is lost that was typed. Every conversation this terminal
holds is closed together, and the ones behind the screen already wrote their own boxes to
disk when you switched away from them.

**Limits.**

- **`/quit` closes one conversation, not the program.** It is typed out on purpose, so it
  is not asked twice — but what it closes is the conversation in front, and aforge stays up
  with the previous one forward when this terminal is holding another. It leaves only when
  that was the last one. `ctrl+c` twice is the key that closes everything. `/exit` and `/q`
  are the same command.
- A real signal — `kill -INT`, `kill -TERM`, or `^C` on a terminal that is not in raw
  mode — also leaves at once, through the same clean exit: draft written, session
  closed, status 0. Only the keystroke asks twice.
- The 1.5-second window cannot be changed.
- Closing the terminal window is not a quit aforge sees; the draft written 300ms after
  you stopped typing is what survives that.

## Keys — the keyboard keys, keys on the keyboard, key bindings and keyboard shortcuts

This page is about **the keys you press**. Every key, chord and keyboard shortcut aforge
listens for is on this page, in the tables below.

If you came here looking for a different kind of key, it is somewhere else:

- an **API key** for a model — see *Models and cost* and *Connected accounts*
- a **device key** or a **pairing code** for reaching another machine — see
  *Reaching a machine with a pairing code*
- an **ssh key** — that is your own ssh setup, and aforge runs your `ssh` unchanged; see
  *Running on another machine*

## Keys in the message box: sending, stopping, and queueing

These apply with no overlay up, no room open, and no mode on.

| Chord | What it does |
|---|---|
| `enter` | Send the message. Empty box with attachments still sends; empty box with a tool row selected opens that row |
| `ctrl+enter` | Send it as something to **keep true** — aforge shapes it into a standing order's card instead of doing it once. See the standing orders page |
| `alt+enter` | Open a new line in the message |
| `ctrl+j` | Same as `alt+enter` |
| `esc` | In order: cancel a history recall, then arm rewind, then interrupt the running turn — and send any message that was waiting for it |
| `esc` `esc` | Two presses inside a short window open the quick inline rewind mode. `/rewind` opens the full timeline instead |
| `ctrl+c` | Turn running: interrupt, and nothing else. Nothing running: arm the door; press it again within 1.5 seconds to quit |
| `ctrl+q` | Queue this message to run after the current turn. Empty box does nothing |
| `ctrl+g` | Send the running command to the background. Nothing running: does nothing |
| `enter` while a turn runs | Hold the message above the box until the answer finishes |
| `shift+enter` while a turn runs | Stop the answer and send what you have typed, as one gesture. Empty box: nothing. Nothing running: nothing |
| `↑` over an empty box | Take the newest waiting message back into the box to edit; with none waiting, walk your history |

`shift+enter` does **not** open a line — use `alt+enter` or `ctrl+j` for that. It needs a
terminal that can tell it apart from a plain `enter`; where it cannot, the key arrives as
an ordinary `enter` and the message waits instead.

## Keys in the message box: opening things and moving the view

| Chord | What it does |
|---|---|
| `ctrl+o` | Selected landed card: open its output. Selected proposal: open its brief. Otherwise: fold or unfold this turn's tool cluster |
| `ctrl+b` | Enter copy mode — freeze the view so you can read and copy |
| `ctrl+s` | Hand the pointer to your terminal so you can drag-select. Toggles; any other key takes it back |
| `ctrl+,` | Open the settings panel |
| `ctrl+.` | Open the task page (`/history`) — every task this project has run, across every session; type to filter it. Does nothing when the project has run none |
| `space` `space` | On an **empty** box: open home (`/home`) — every project and conversation on this machine, and an empty home on a fresh one. Does nothing when the box has words in it; not bound over `--host` |
| `ctrl+l` | Jump back to the live edge of the conversation |
| `ctrl+t` | Give the keyboard to the task roster. Press again or `esc` to take it back |
| `ctrl+g` | Close the task roster's column, or bring it back — the column stands even with no tasks in it. Remembered for the next session. On a frame under 100 columns with no roster raised, it does nothing |
| `ctrl+e` | Empty box: open or close the latest completed turn's `▸ worked` chip, or the most recent thinking block when there is no chip. Otherwise: go to end of line |
| `pgup` / `pgdown` | Scroll one page — the height of the view minus one, never less than one row |
| `tab` | Open or commit path completion, over a command's path argument only — and over an **empty** box with no completion showing, go back to the last conversation. Does nothing when this terminal holds only one |

## Keys in the message box: moving the caret

| Chord | What it does |
|---|---|
| `up` | Four meanings, tried in this order: move the caret up inside a multi-line message; walk back through history; over an empty box, select the previous tool row; scroll up one row |
| `down` | The mirror of `up` |
| `left` | Empty box: step back a level — close the room, else clear the selection. Two `left` presses inside about 600ms go home to the live edge. Non-empty box: move the caret left |
| `right` | Empty box: go into the next running task's room. Non-empty box: move the caret right |
| `ctrl+f` | Move the caret right, always. Never navigation |
| `alt+left` / `alt+b` / `ctrl+left` | Jump a word left. `alt+left` is what `option+←` arrives as on most Mac terminals. Does nothing over an empty box — the plain arrows keep their navigation meaning |
| `alt+right` / `alt+f` / `ctrl+right` | Jump a word right, under the same three names |
| `super+left` / `meta+left` | Start of the line — Mac `cmd+←`, on terminals that forward `cmd` at all (see the terminal table below). Both names are bound because a modified arrow and a modified letter arrive under different ones |
| `super+right` / `meta+right` | End of the line — Mac `cmd+→`, under the same two names |
| `home` / `ctrl+a` | Start of the current line |
| `end` | End of the current line, always |
| `ctrl+e` | End of the line — unless the box is empty, where it opens the latest completed turn's `▸ worked` chip, falling through to the most recent thinking block when there is no chip |
| any printing key | Types the character |

`home`, `end`, `up` and `down` work on the logical line — the run between newlines —
not on the row your terminal wrapped it onto. `up` only reaches history when the
caret is on the first logical line, and `down` only when it is on the last.

A word jump crosses the same boundary the word kill deletes: spaces first, then
the run of non-spaces, so `alt+left` then `ctrl+w` always deletes exactly the
word it just crossed.

## Click to move the cursor — clicking the message box places the caret

A click anywhere on the message box puts the caret under the pointer: on the
letter you aimed at, at the row's end when you click past the end of a line, and
at the start of the text when you click on the prompt's side of it. It works on
a wrapped, multi-line draft — the row you click is the row the caret lands on.

It is the ordinary text-field gesture, and only the message box answers it.
While a picker's filter box is standing in the box's place — the model picker,
`/resume`, `/files`, the memory panel — a click does not move that box's caret;
those are typed at and filtered, not edited by pointer.

## Why option+left or cmd+left does nothing — word jump depends on your terminal

If `option+←` does nothing, the key never reached aforge: some terminals keep
the option key to themselves or send it as something else. aforge answers every
spelling terminals actually send — `alt+left`, `alt+b` (the esc-b some profiles
send instead), and `ctrl+left` — so on Ghostty, Kitty, WezTerm and iTerm2's
default profile it simply works. On iTerm2 with the option key set to *Normal*,
set **Settings → Profiles → Keys → Left Option Key** to *Esc+* to make
`option+←` a word jump. `alt+b` and `alt+f` are the spellings that work nearly
everywhere.

`cmd+←` / `cmd+→` follow the `cmd+delete` rule below: they arrive only on
terminals that speak the keyboard protocol carrying the `cmd` modifier
(Ghostty, Kitty, WezTerm). Everywhere else use `ctrl+a` / `ctrl+e` or
`home` / `end`, which are the same jumps on every terminal.

On those terminals a modified **arrow** and a modified **letter** arrive under
different names: `cmd+delete` comes in as `super+backspace`, while `cmd+←` comes
in as `meta+left`. Both spellings are bound, so the jump works — it did not
until this wave, when only the `super` spelling was, and `cmd+←` did nothing on
every terminal there is.

## Keys in the message box: deleting words and lines

| Chord | What it does |
|---|---|
| `backspace` | Delete the character behind the caret. Over an empty box with attachments, it removes the last attached picture instead — and its `[image #n]` token with it |
| `delete` | Delete the character in front of the caret |
| `ctrl+u` | Delete to the start of **this line** — not the whole message |
| `super+backspace` | Same as `ctrl+u` (Mac `cmd+delete`) |
| `ctrl+w` | Delete the word behind the caret |
| `alt+backspace` | Same as `ctrl+w` |
| `ctrl+backspace` | Same as `ctrl+w` |
| `ctrl+h` | Deliberately not bound — some terminals send plain `backspace` as `ctrl+h` |

All five of the kills above work the same way in **every** box aforge has, not
only the message box: the model picker, the sessions roster, the deliverables
list, the connect key box and panel, the memory panel, the settings filter and
its value editor, and the task page's filter.

## Why cmd+backspace does nothing — which terminal you are in decides

`cmd+delete` is bound. aforge answers it under the name `super+backspace`, and it
deletes to the start of the line, exactly as `ctrl+u` does. When it does nothing
at all, the key never reached aforge: **your terminal decides whether cmd
combinations are sent to the program at all**, and several do not send them.

| Terminal | Does `cmd+delete` reach aforge? |
|---|---|
| Ghostty | Yes |
| Kitty | Yes |
| WezTerm | Yes |
| iTerm2 | Not by default. It has no default action for `cmd+delete` and does not forward it. You can make it work: **Settings → Profiles → Keys → Key Mappings**, add `⌘⌫`, action *Send Escape Sequence*, and give it `[127;9u` |
| Terminal.app | No, and it cannot be made to. It does not speak the keyboard protocol that carries modifiers like `cmd` |
| Anything over `ssh` or `tmux` | Only if the outer terminal is one of the first three, and tmux is passing the protocol through |

**`ctrl+u` is the spelling that works everywhere**, on every terminal on every
machine, and it is the same deletion. If `cmd+delete` does nothing where you are
sitting, that is the key to use instead — nothing is missing and there is nothing
to turn on inside aforge.

The same is true of `alt+backspace` and `ctrl+backspace` for the word kill, and
`ctrl+w` is *their* everywhere-spelling. aforge does not detect what your
terminal sends and cannot tell you which of these it will deliver; the only test
is pressing it.

## The message box itself

It is one line marked `› ` with no border, and it holds newlines, so it is a small
multi-line editor rather than a single-line field.

- It shows at most **6 rows** at once, further capped by your terminal height minus
  two, and never fewer than one row. It never takes more than 6 rows from the
  conversation no matter how large the paste.
- A longer message scrolls **inside** the box, following the caret. The rows scrolled
  past are marked with an ellipsis in the same two cells the `› ` occupies, so
  nothing shifts under your caret.
- Continuation rows are indented to sit under the text. Soft wrapping
  breaks at the last space before the edge, and mid-word only when the line offers no
  space.

**Inside a task's room the box wears a segment in front of its `› `**, naming the work
your words are going to: the task's state glyph and its name, on the same tinted
background a selected row wears, in the hue that task's state is drawn in everywhere
else — accent while it runs, the warn colour while it needs your look, muted once it is
done, the bad hue when it failed, dim when it is queued or you stopped it. It is there
whether the box is empty or full, which is the point: the placeholder that used to say
this disappeared the moment you started typing. The name is cut to at most 18 cells; on
a frame too narrow to spend the cells, the segment is dropped and the placeholder goes
back to naming the task itself. There is no segment at all in the main conversation.

A **slash command you type into the box is highlighted as you type it** — `/task`,
`/compact`, `/clear` get a tinted background behind the word, so a real command looks
different from ordinary text and a typo like `/tsak` does not. The highlight is drawn in
your sent message too. It adds no characters and no cells; see "Slash commands are drawn
as chips" in the commands page for the whole of it.

**A key chord is never given that background.** Where aforge names a key — the hint slot
on the legend, the `/help` sheet, the opening `esc interrupts · ctrl+c twice quits` — the
chord is drawn one tier brighter than the words around it and nothing else changes. A
tinted background always means a slash command and only ever that, so the two marks
never have to be told apart. See "Why is one word in a line brighter than the rest" on
the screen page.

**Pasted text lands as one edit** with its newlines intact — it never submits line by
line. Bracketed paste is on. CRLF and bare CR become LF at the door.

Inside an open paste bracket, every key is text: `enter` and `ctrl+j` become a
newline, `tab` becomes a tab, everything else contributes its text. Nothing between
the brackets can submit, interrupt, or answer a question. `ctrl+c` is the one
exception and still works — it arms the door without closing the bracket, and a
second press within 1.5 seconds quits. A bracket that goes quiet for 2 seconds is treated as
abandoned, flushed, and the keyboard handed back.

A paste while copy mode is up is **declined** — nothing happens, and your clipboard
still holds the text.

## Make my prompt better — spell it out with `ctrl+r`

Type what you want and press **`ctrl+r`**. aforge reads the sentence sitting in the box
and writes, in dim text under it, what it takes that sentence to mean:

```
› build me a login page

  taking it to mean —
  - email and password, and the form says which field is wrong
  - a session that survives a refresh
  - I'll pick a cookie session unless you say
  enter add it to what you're saying · esc leave it
```

Three kinds of thing go into that block, and each is handled differently. **What you
said** is your requirement and is never repeated, improved or reworded — it is already in
your box and it stays there untouched. **What anyone would obviously want** is supplied
outright, as a concrete line, without asking you about it. **What is genuinely yours to
decide** gets a sensible default with a flag on it — `I'll pick … unless you say` — and
only where a wrong guess would waste the work; the rest takes a default in silence.

It is one small model call on your cheap tier, made **only** when you press the chord. It
never runs on its own, it never sends anything, and nothing about it goes into the
conversation. It is added to what this session has spent, the way the session's own name
and other calls aforge makes for itself are, and it does not count as a turn.

If it fails or takes longer than 10 seconds you get **nothing** — no error line, no note.
The hint under the box simply comes back, and your draft has not been touched.

## It added details I didn't ask for — nothing goes in until you press enter

The `taking it to mean —` block is **not part of your message**. It is drawn below the
box precisely so it does not look like it is. Two keys:

- **`enter` adds it to what you're saying.** The lines are appended to your draft after a
  blank line, as plain text. From that moment they are your words like any others — edit
  them, delete the ones you disagree with, keep typing. Nothing has been sent: the *next*
  `enter` sends the message, and it goes as one ordinary message.
- **`esc` leaves it.** The block goes, your draft is byte for byte the characters it was,
  and nothing is remembered anywhere.

So a line you did not want is a line you press `esc` on, or delete after adding. There is
no setting to turn off, because there is nothing running until your finger is on the
chord.

Any key that **changes the draft dismisses the block** as well. It was about the sentence
you had, not the one you now have, and an expansion of an older sentence must never be
added to a newer one. Press `ctrl+r` again for the new one.

## The dim `ctrl+r spell it out` line — why it is not always there

The hint appears at the right end of the rule above the message box only while your draft
**looks like something to build and still has room to grow**: it contains a making word —
`build`, `create`, `make`, `write`, `design`, `add`, `implement`, `set up`, `generate`,
`draft`, `put together` — and is under 120 characters with no bullet list in it.

**A detailed draft gets no hint, and that is the point rather than a limit.** If you have
already spelled out what you want, there is nothing here for you, and a dim line under
every draft on the screen is a line people learn to stop seeing. The same goes for a draft
that is empty, one that starts with `/`, and one where you have typed the list yourself.

The word list is a courtesy for teaching you the chord, not a rule about what can be
spelled out — the chord works wherever the hint is drawn, and nowhere else. Pressed where
the hint is absent, `ctrl+r` does nothing at all.

It **shares the slot** with the standing-order hint, and `ctrl+enter keeps this true`
wins whenever both would show. A sentence read as one-off work when you meant a rule is a
rule that silently never existed; a request sent without its details is still a good
answer to a slightly vague question.

The hint **never moves the message box**. It rides a line that is on the frame either way,
so a draft that starts looking like something to build changes one word at the end of a
rule and nothing else. While the call is out that same slot turns a small spinner in front
of the words; when the answer lands the block appears under the box, and the box itself
has still not moved.

## Your unsent draft is kept

The half-written message survives closing the window, a crash, `/new`, and a session
that has moved on. There is nothing to press; it is automatic.

- It is written 300ms after you stop typing, and again synchronously on quit before
  anything else happens.
- **Anything still waiting for an answer is folded in on quit.** A message you parked
  with `enter` while a turn was running (see "Typing while the model is still
  answering") is written into the draft file underneath your unsent sentence, each on
  its own line, so it comes back the next time you open aforge here instead of
  vanishing with the session.
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

- **A message waiting for the answer is read first.** With an empty box and something
  waiting above it, `up` takes that message back into the box to be edited instead of
  walking your history. Only once nothing is waiting does `up` walk the history again.
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

There are three ways in.

1. **Drag a file in, or paste one.** Drop a screenshot on the terminal — or copy a
   file in Finder or your file manager and press `cmd+v` / `ctrl+shift+v` — and
   aforge attaches it. See "Dragging or pasting a screenshot in" below, which is
   the way most people do this.
2. **`/image <path>`.** `~` becomes your home directory, a relative path is resolved
   against the conversation's directory — or against **your own machine's** working
   directory over `--host` — and an absolute path is left alone.
3. **The `@` completion.** An image row in the list is tagged `img`. Choosing it
   **removes the half-typed `@token` from your sentence** and puts the file in the
   tray, instead of typing a path.

Typing out an `@` path to an image by hand does not attach — attaching happens when
you choose the completion row.

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
one dim chip each, drawn as `▣ #1 name.png` — `*` in place of the square on an ASCII
terminal. The number is the picture's place in the message and the number `[image #1]`
in your sentence refers to. The message box stays the sentence. `backspace` over an
empty box drops the last chip, and clicking a chip removes that one — and takes its
`[image #n]` out of your sentence, counting the ones behind it down so the numbers
stay true.

## Dragging or pasting a screenshot in

**Drag a picture onto the terminal, or paste one you copied as a file, and aforge
attaches it.** What the terminal actually hands over is the file's *path* as pasted
text — `/var/folders/.../Screenshot 2026-08-21 at 5.21.40 PM.png`, usually with its
spaces backslashed, sometimes quoted, sometimes as a `file://` URL. aforge reads all
three shapes, and reads several files dropped at once, separated by spaces or by
newlines.

**Your sentence gets `[image #1]`, not the path.** The picture goes on the tray and a
short token takes its place in the message box, numbered in the order the pictures
were attached. It is ordinary text: type around it, delete it, move it. And it is
what you say out loud — "what font is image #1", "compare image #1 with image #2" —
because **the token goes to aforge inside your message, in the position you left it,
and the picture itself travels with it.** aforge is told that `[image #1]` marks the
first picture in the message, so the number you read is the picture it is looking at.

**A picture attached by `/image` or the `@` completion gets its token too**, appended
to the end of your sentence when you press `enter`, so "image 2" means the same thing
whichever way the picture got there.

**It is all or nothing, on purpose.** A paste is treated as pictures only when *every*
word in it names one of the five picture types **and that file exists on this machine**.
A sentence that mentions a `.png`, a diff, a stack trace, a log — all of it goes into
the message box as the text it plainly is, which is what pasting has always done.
A paste over a line that starts with `/` is left as text too, so `/image ` and
`/export ` still take a path.

**Raw image data on the clipboard is not read.** Copying a picture out of a browser or
a screenshot tool — as *pixels* rather than as a file — pastes nothing here. Save it to
a file first, then drag that in, or use `/image <path>`.

## What aforge says when a picture is refused

| Situation | Exact text |
|---|---|
| `/image` with no path | `/image takes a path · try /image shot.png` |
| Not one of the five types | `<basename> is not a picture · png, jpeg, webp and gif are` |
| Missing file, or a directory | `no such picture: <path as typed>` |
| Already in the tray | `<basename> is already attached` |
| Dragged or pasted in over the ceiling | `<basename> is over the 10MB image limit` |
| Unreadable when you send | `could not read <basename>` |
| Over the ceiling when you send | `<basename> is over the 10MB image limit` |

A dragged or pasted picture is measured **at the moment you drop it**, and one over the
ceiling is refused there rather than attached and refused later. Nothing is lost when
that happens: the path stays in your message box as the text it arrived as, so you can
still ask aforge to look at the file where it lies.

A picture that is dragged in but **does not exist on this machine** is not refused at
all — the paste was never a picture, so the text goes into the message box unchanged
and nothing is said about it.

**A refusal keeps your pictures.** The tray is emptied while the message is in
flight; if sending fails, the chips are put back, in front of anything attached in
the meantime, without duplicating.

## Sending a message that has pictures

`enter` with a full tray sends. The files are read at the moment you press `enter`.
The line in the conversation becomes your sentence — tokens and all — plus the file
names in dim square brackets, numbered to match:
`› what is wrong with this [image #1]  [#1 chart.png]`. A message with pictures and no
words is still a message; it goes out as its tokens alone.

**aforge really sees the picture.** It does not receive the path and go and open it:
the bytes travel inside the message as the picture itself, base64-encoded, beside your
words, which is why the path is rooted on your **local** machine even on a remote
session. If the model you are talking to cannot see, the picture is shown to a model
that can and its answer comes back prefixed `[vision: <model>]`; if nothing available
can see, the message is refused before anything is sent and your pictures stay on the
tray. The tray is not rendered as a picture — a terminal cell is not a place to show
one.

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
| `esc` | Close the list. For the command list it also **seals that word** — the list does not reopen on the next letter of it. It does **not** interrupt a running turn |
| `enter` | Command list: take the highlighted command. At the start of an otherwise empty box that **runs** it; anywhere else it replaces just that word with the command's name and runs nothing. If nothing matched, the line is sent as typed. `@` list: insert the highlighted task or file; if nothing is picked, the line is sent |
| `tab` | Read **before** the list. It only opens or commits an *argument* completion, over `/image ` or `/export `. With nothing to complete and an empty box it goes back to the last conversation |
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

**The empty screen's greeting:** it takes only two keys, and only over an empty message
box — `up`/`down` walk the recent sessions listed under it and `enter` opens the selected
one. Every other key dismisses the greeting and then does whatever it normally does; the
first letter you type lands in the box, which is drawn inside the greeting until then.

Both pickers are modal: while one is up, every chord except `ctrl+c` belongs to it.
`ctrl+c` does not close the picker — it arms the door, and a second press within 1.5
seconds quits aforge with the picker still up.

## Keys when aforge asks you a question

**An approval question:** `y` allow once · `a` or `t` always — refused when it would
do nothing · `n`, `d` or `esc` deny. Every other key does nothing, but it **stops the
countdown**. `ctrl+c` is handed back to the message box, where it arms the door and a
second press within 1.5 seconds quits. On the second beat of
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
`ctrl+c` handed back to the door, where two presses quit · everything else does nothing.
Its row reads
`[r] revive and send · [m] send to main · [esc] cancel`.

## Keys in the settings panel and the other panels

**Settings panel** (`ctrl+,`): `esc` backs out one layer at a time — search, then an
open account, then the panel · `left`/`shift+tab` and `right`/`tab` change tab ·
`up`/`ctrl+p`, `down`/`ctrl+n`, `pgup`, `pgdown`, `home`, `end` walk · `enter` and
`space` activate · `backspace`, `ctrl+u`, `ctrl+w` edit the search · anything else
types into it.

**Task page** (`ctrl+.`, or `/history`, or the one dim door line at the bottom of the task
column — `ctrl+. earlier`, or `ctrl+. view more` where the column has only folded a
family away): `esc` closes it — or clears the filter first, if one is being typed —
and `ctrl+.` closes it either way · `up`/`ctrl+p`, `down`/`ctrl+n` move, stepping
over the `running` and `earlier` section words · `pgup`/`pgdown` move twelve · `home`/`end`
first and last · `enter` opens the row · `backspace`, `ctrl+w` and `ctrl+u` edit the filter
· **every other printable key, the space included, types into the filter**, which narrows
both sections at once and is shown at the foot as `filter · port`. Its foot reads
`esc close · ↑↓ move · enter opens its room`, or
`esc close · ↑↓ move · enter goes inside it` on a task another conversation ran, which has
no room to open, `esc close · ↑↓ move` where the row under the cursor has no door
at all — which is a page holding only work running in other aforge windows — or
`esc clears the filter · ↑↓ move · enter opens the row` while you are typing one. Clicking a row acts on the first press; the wheel walks the
cursor. The tasks pages describe what is on it.

**Inside an old task's card** (`enter` on an `earlier` row): `esc` or `←` backs out to the
list · `ctrl+.` closes the whole page · `↑`/`↓` (also `k`/`j`) scroll · `pgup`/`pgdown` and
`space` move a screenful · `home`/`end` the ends · **`m` puts that task's name in your
message box** and closes the page. Its foot reads
`esc back · ↑↓ scroll · m puts it in your message`. Clicking its head row or its foot goes
back to the list; its body is read.

**Rewind timeline** (`/rewind`, or `tab` from inside the quick `esc` `esc` mode): `esc`
clears the search first and closes the page second · `up`/`ctrl+p`, `down`/`ctrl+n` move ·
`pgup`/`pgdown` move twelve · `home`/`end` the oldest point and the newest · **`enter`
places the pick, and `enter` again on that same point does the rewind** · `backspace`,
`ctrl+u` and `ctrl+w` edit the search · **every other printable key types into the search**,
which reaches your messages, the model's replies and the tool calls' arguments. Its head
reads `⟲ rewind — pick where the conversation goes back to` and its foot
`⟲ drops 2 turns — everything below the pick is let go` above
`esc close · ↑↓ move · enter picks the point` — which becomes
`esc close · ↑↓ move · enter again rewinds here` once a pick is placed, and
`esc clears the search · ↑↓ move · enter picks the point` while you are typing one.
Clicking a row places the pick; clicking the placed point rewinds; the wheel walks the
cursor. The sessions and rewind page describes what the cut does.

**Inside the quick rewind mode** (`esc` `esc`): `↑`/`↓` walk turns · `←`/`→` step inside
one · `enter` cuts · **`tab` lifts you onto the rewind timeline** with the cut you had
chosen · `esc` leaves with nothing changed.

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
`ctrl+c` does not close the panel — it arms the door, and a second press within 1.5
seconds quits aforge.

## Go back to the last conversation — tab

**`tab`, pressed with an empty message box, goes to the conversation you were in before
this one.** Press it again and you are back. It is `cd -`.

It **does nothing at all** when there is nowhere to go: one conversation open, or none this
terminal has been in before. A key that cannot act says so by not being advertised — and
when it can, the legend line above the box says `space space home · tab last · / commands`.

It works while a turn is running in either conversation. Nothing is interrupted: the turn
you leave keeps streaming into its own transcript, and it is redrawn from its first token
when you come back.

**Everything else that wants `tab` gets it first**, and that is the whole rule rather than a
claim that `tab` is free. In order: a paste bracket makes it a literal tab; the task roster
eats it while it holds the keyboard (`esc` gives the keyboard back first); the settings
panel changes page with it; the memory panel changes scope; the rewind timeline and the
inline rewind lift with it; and path completion takes it over `/image ` or `/export `. Only
when none of those is claiming it, and the box is empty, is it the way back.

The welcome box is the one exception worth naming: **`tab` does not dismiss it**. Every
other key does — that is the box's contract — but switching away is the opposite of
starting work here, so the box is still standing when you come back.

## Keys on home, and is there a shortcut for it

**Press the space bar twice with an empty message box.** That is the way back to home from
inside a conversation, and `/home` opens it too.

There is no `ctrl+` chord for home: every `ctrl+<letter>` this surface could use is already
taken, `ctrl+.` is the task page (`/history`), and the chords that were left — the `alt+`
letters — arrive in some terminals and do nothing at all in others. `esc` was not available either: on an idle conversation it
already arms rewind and already sends a message you parked with `ctrl+q`, and a third
meaning on one key in that state is how a surface stops being predictable.

**The first space types itself.** The second one, finding a box holding exactly one space,
takes both away and opens home — so a leading space you actually wanted is never eaten
(space then `x` leaves ` x`). It does nothing when the box has words in it, it is not bound
over `--host` where home refuses, and it is not a paste: text pasted with two leading
spaces is two spaces. A machine with one conversation, or none, opens an empty home.

It works while a turn is running; the answer keeps streaming underneath and `esc` puts you
back in it.

When the box is empty, the legend line above the box says so:
`space space home · / commands`. Clicking those words opens home. It vanishes as soon as
you type, and it is absent only over `--host`.

**The door does not ask what the machine holds.** It is open on a machine with only this
conversation and on one with none, from the first minute, and starting a second
conversation with `/new` changes nothing about it. It used to be shut until the launch
found somewhere else to go, and that rule is gone (the home page, *space space does
nothing*).

Once it is open: `esc` clears the box if anything is in it, and closes home otherwise ·
`up`/`ctrl+p` and `down`/`ctrl+n` walk the rows, stepping over the project headings ·
home opens with the cursor on **no row**, and **the first `down` lands at the top of the
projects list** on a frame 110 columns or wider — the middle column, always, whatever the
zones beside it hold; on a narrower frame, where the zones are strips over the list, it
walks into `needs you` instead · `pgup`/`pgdown` jump four · `enter` acts on the row under
the cursor · **`tab` moves to the
next zone** on a frame 110 columns or wider, where home draws three columns — `needs you`,
`moving`, the list, then round again, and from rest it enters `needs you`; the foot names
it `tab next zone` · `backspace`,
`ctrl+u`, `ctrl+w`, `ctrl+b`, `ctrl+f` edit the box · with the box empty and the cursor on
a conversation that is **waiting on you**, the digits on its chips answer that question
where it stands (`1 allow once · 2 always · 3 deny`, and the like for the other two kinds
— home's own page has the table) · **anything else you type goes into the box**, which
searches the whole machine and offers to start a new conversation at the same time.

`→` and `←` are the fold's, the way they are in the task column: on a project's
`…13 more, quiet since 1d` line, `enter` or `→` opens it and `enter` or `←` folds it away;
`←` on a conversation inside an opened project folds that project too. On a conversation
or watch row with the box empty, `→` opens **every folded band on the card** you are
looking at and `←` folds them all back — one layer at a time, so `←` folds the card's
bands before it folds anything on the list. **On the three-column frame (110 columns and
up) the same arrows also cross the columns**: `→` off a row in `needs you` or `moving`
crosses into the list and `←` off a conversation in the list crosses back, each landing
on the same conversation when the far column holds it. While something is typed the two
arrows move the caret in the box instead.

**A letter always types. Every letter, always** — there is no row, cursor, hover or pick
that turns one into a shortcut, so "make me a site" comes out whole wherever the cursor
is resting. The card's actions ride chords, which can never begin a word: **`ctrl+e` puts
the conversation away into the archive** — the folded `archive · N put away` line at the
very foot — and `ctrl+e` on a row inside the open archive brings it back (the archive
page section has the whole shape). **`ctrl+t`** starts a new conversation in that row's
project (the browser's new-tab key — ctrl+n is the walk down), **`ctrl+o`** opens its
folder, **`ctrl+y`** copies its path. On a `◦` row of the `keeping an eye on` list,
**`ctrl+e` pauses** it, **`ctrl+x` stops it for good**, and **`ctrl+v` raises how hard that
item thinks** one rung. **With the cursor on no row at all** — one `↑` up off the top row,
where the card becomes the machine's own — **`ctrl+v` moves the machine-wide default**
instead, which is the `thinking` row in `/settings`. Each chord acts on the card you are looking at — the row
under your pointer when there is one, the cursor's row otherwise — and the card's own
dim legend names exactly the keys that work. The one printable exception is the digits
on a waiting row's answer chips, which are drawn on the row itself.

With the mouse: a click puts the cursor on a row and a second click on that row opens it;
a click on a `…13 more` line toggles it in one press.

**Under 60 columns those two clicks are one.** At phone width home is an inbox and a
row's card is a full-frame sheet, so a tap selects and opens in one gesture; the sheet's
top row reads `‹ back` and `esc` or a tap on it returns to the list with the cursor where
it was. The hint line becomes a bar of at most three wide targets — `open · new ·
ask here`, or `‹ back · open · more` on a sheet — and mouse motion is ignored, because
there is no hover on glass. Home's own page has the whole shape.

The top line carries `esc close` on the right. The foot reads exactly
`type to search or start something new · ↑↓ pick · enter open`, and the line under it
changes with what the cursor is on — `↑↓ move · enter open · esc close` at rest,
`enter starts a new conversation and sends this · ctrl+enter ask here · ↑ pick a match · esc clear` on the action
row, and `enter or → show them · esc close` on a folded project.

**With nothing typed the list hangs from the top** and the cursor is on the conversation
this window is in, with the preview card beside it. **While anything is typed the list is a
drop-up**: the action row — `start a new conversation: "…"` — is the LAST row of the list,
with `ask here: "…"` directly above it, both directly above the box, and the matches rise
above the pair **best one first**; the cursor starts on the action row, so one `↑` reaches
`ask here` and a second lands on the strongest match, further `↑` walks into weaker ones,
and `↓` walks back down toward the box. Clearing the box puts the list back at the
top.

The right-hand preview is there in both shapes and never moves. It follows the cursor
through a filter too, and is empty while the cursor is on the action row.

**On an `ask here` row** — one of the `?` rows an errand leaves on the column — the preview
is the exchange itself, and the line under the box reads
`↑↓ move · enter or tab answer this ask here · esc close`. `enter` or `tab` hands the
keyboard to the pane, where it reads `enter sends a follow-up · tab or esc back to the list`;
`esc` or `tab` hands it back. On a window too narrow for two columns that pane is drawn over
the list instead of beside it, and `esc` brings the list back (*Asking from home*).

Home is modal like the panels above: while it is up, every chord except `ctrl+c` belongs
to it. `ctrl+c` does not close home — it arms the door, and a second press within 1.5
seconds quits aforge.

## Keys in the task roster and inside a room

**While the task roster holds the keyboard** (`ctrl+t`): `esc` gives the keyboard
back · `up`/`down` move · `right`/`left` open and fold · `enter` opens that row's room ·
`w` widens the column and narrows it again. Its hint reads exactly
`↑↓ move · →← tree · enter open · w wide · esc`. On a row whose work is still running or
still queued the hint gains one more clause before `esc` — `ctrl+v think harder`, which
moves that task's thinking rung; a finished row does not offer it, because a finished
task's rung is a fact about what happened.

**`→` and `←` fold two things, and it is one gesture.** On a family's root row they open
and close the family. On a row whose **work has finished** they open and close that row's
own detail line — the merge word and price, a job's log path — which a finished row keeps
folded so that the column's height goes to work that is still moving. `→` on anything else
does nothing.

**The walk stops at this conversation's last task.** The roster holds this conversation's
work and nothing else, so `↓` clamps at the bottom of it rather than carrying on into the
project's record. Old tasks from earlier sessions are on the task page, reached from the
column's own `ctrl+. earlier` line, from `ctrl+.` or from `/history`; `enter` on an
`earlier` row there goes inside that task's card. In a directory whose earlier sessions ran
tasks but where **this** conversation has run none, `ctrl+t` falls through — there is
nothing on the column to put a cursor on.

**The column's other lines take no cursor.** Its `standing` section, and the two `+` rows
that close each section (`+ /task`, `+ /standing`), are the pointer's — the walk skips
them. Their keyboard equivalents are the commands themselves: `/standing` opens the
standing orders page, and typing `/task ` is exactly what pressing `+ /task` puts in the
box. **No new key is added to the column by either of them.**

**Under 60 columns the roster page is a thumb's, not a keyboard's.** Its rows are two-line
cards a tap opens, its foot is a `‹ back` bar in place of the key legend `esc close · ↑↓
move · enter opens its room`, and the strip that opens it is one full-width door
(`▸ 3 tasks · 1 running`) rather than a row of chips. Mouse motion is ignored — a tap opens
in one gesture. The tasks page describes the phone flow in full.

**`ctrl+g` closes the roster's column, and opens it again.** It works from the message
box, from inside a room, and while the roster holds the keyboard — it is the one key
here you do not have to ask for the roster first to use. Closing it hands the keyboard
back to the box. The choice is written to your profile as `ui.task_column`, so the next
session opens the way you left it, and `ctrl+t` counts as asking for the column back.

**A closed column leaves a two-column edge down the right of the frame with a `❮` in it,
drawn in ink, and clicking anywhere on that edge opens the column again. Clicking the
`❯ ctrl+g hide` line while the column stands closes it** — one chevron control, two
states, so the pointer can go both ways. The key is unchanged; the chevron is there so the
column is not a thing you have to already know a chord to get back.
The key falls through and does nothing only when there is no roster on the frame to
close: a frame under 100 columns where nothing has raised the overlay. It works with
no tasks at all — the column stands with only its `+ /task` and `+ /standing` doors, with the
`ctrl+. earlier` door under them if earlier sessions ran anything, and either way an
empty column is still a column to close. On the untouched empty screen there is no
column yet; there `ctrl+g` is a first keystroke like any other — the greeting goes and
the key then closes the column it would just have raised, so a second press brings it
back (*The empty screen* page).

**With a room open:** `esc` leaves the room, though a history recall walk is
cancelled first · `enter` steers the node · `ctrl+b` freezes the room's own rows for
copying, not the conversation's · `pgup`/`pgdown` page · `up`/`down` walk your history,
and scroll the page one row only when there is no history to walk. `left` is
deliberately **not** taken here — it falls through to the message box's
back-navigation.

**`up` and `down` in a room mean what they mean in the message box**, in the same order:
inside a multi-line message they move the caret; on the first line — or over an empty box
— they walk your own history, newest first; and only with nothing to walk do they scroll
the page. It is the same history the main conversation walks, and **a line you steered
into a task goes into it**, so `↑` brings back the last thing you said to the task and
you can edit it and send it again. A line the steer guard refused is not remembered. On
an **adaptive run's page** `↑`/`↓` over an empty box walk the graph's chips and the fuel
gate's answers instead; type something and they walk the history from there.

**`esc` in a room never interrupts and never stops.** Out in the conversation `esc`
interrupts the running turn; inside a room the first `esc` leaves the room and the next
one interrupts. Ending the task itself is `x` and its card. The legend's left end always
names what the next `esc` does: `room · esc/←← main`, and `room · esc your line back`
while a history walk is on. The hint at the legend's right end reads `x stop` while there
is work here to stop and `↑↓ history` during a walk — it never reads `esc interrupt`
inside a room, because in here that is not what the key does.

**A click inside the room's page does not leave it.** A press that lands on nothing —
a blank row, the gap beside a paragraph, the slack under a short transcript — does
nothing at all, exactly as it does in the conversation. Leaving is `esc` and `←`, and
the pinned header at the top of the page names both: `esc/← main`. That header row is
also a button — press it anywhere along its width and you are back in the conversation
— except the `✕` at its right end, which asks to stop the work instead.

**Inside a harness design's room, while its card is waiting on you**, two more chords
appear above the message box: `ctrl+k` saves the design and `ctrl+x` drops it, and both
are clickable. They are chords rather than letters because `esc` and `enter` are already
spoken for and a bare letter would stop being a letter you can type — and they are bound
only while that row is up. Every other key still goes to the message box, which is where
you say what you want changed instead. The saved-shapes pages describe the row in full.

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

## Deciding about a landed task from the keyboard — accept, look again, not right

A landing that **needs your look** is the one card in the transcript that is still a
question, and four letters answer it:

| Key | What it does |
| --- | --- |
| `a` | accept — take the work; its branch merges and its dependents unblock |
| `l` | look again — a fresh check runs; the task keeps waiting until that answers |
| `n` | not right — the task fails, its branch is kept, its dependents fail with it |
| `d` | decide these for me — hands this one to aforge and sets `task.settle` to `auto` |

**They are held to the same rule `x` is.** The card must be the **selected** one — walk to
it with `↑`/`↓`, which steps through tool calls, proposals and landed cards — and the
message box must be **empty**, with no panel, picker or copy mode up. A letter typed into a
sentence stays a letter, always.

Once answered the four go away and one dim line takes their place saying what you chose.
The same four are clickable on the card. See the tasks page for what each answer does to
the work.

**Inside the task's room the same four keys need no selection.** The room is the task, so
`a`, `l`, `n` and `d` over an empty message box answer it directly, the answers row stands
at the foot of the page where `task finished — esc to return` would otherwise be, and the
hint slot reads `a accept · l look again · n not right` while the question stands. The room
and the card are one question: answer in either and both show the receipt.

## The mouse: what you can click

aforge owns the pointer by default, using all-motion tracking so hover works.

Only the left button acts. A press is resolved in this order:

0. The quick rewind mode (`esc` `esc`), which takes **every** press on the conversation
   while it is up: a click on any transcript row moves the cut to the nearest point at or
   above it, and a click on the `⟲ rewind here` line does the rewind.
1. The settings panel, the task page, home, the rewind timeline, the status deck, or the
   phone tool sheet — each takes **every** press inside its frame, padding included. On the
   task page a press on a row opens it on the first press; a press on a section word or on
   empty padding does nothing. Inside an old task's record card, the head row and the foot
   go back to the list and its body is read. On home a click puts the cursor on a row and a
   second click opens it. On the rewind timeline a click places the pick and a click on the
   point already placed does the rewind.
2. An approval question block, then a connect offer, then a harness offer.
3. The harness panel, the permissions panel, the connections panel — a press on a row
   acts, and a press anywhere else **closes** the list.
4. An attachment chip — removes it.
5. The jump-to-latest chip.
6. A stop target: the confirmation card's two answers while it is up, and the `✕` at
   the right end of a room's pinned header. On a phone-width terminal the `✕`'s hit
   box is three rows tall, because a finger is about that wide.
7. A room's **pinned header**, which is the pointer's way back to the conversation.
   The whole row answers, both ends of it, because the row says `esc/← main` and a
   row that named the exits and did nothing when pressed would be dead. The dim
   family lines under it are facts, not doors, and do nothing.
8. Task strip chips, then the rail column, then a proposal's choices row. On a wide
   terminal the strip chip the roster's cursor is on carries a `✕` of its own, and
   pressing it asks to stop that work instead of opening its room. **When the column is
   closed, the two-column edge it leaves at the right of the frame answers here too** —
   a press anywhere on it opens the column again, which is exactly what `ctrl+g` does.
   Within the column, its own lines are asked before its task rows: a `+ /task` or
   `+ /standing` row types that command into your message box, and a row in the
   `standing` section opens `/standing` with the cursor already on that order.
9. Two segments of the status row: `◦ keeping an eye on N`, which opens the standing
   orders page, and the model name, which opens the model picker. A press elsewhere on
   the status row falls through — the rest of it is figures, not controls. On a narrow
   terminal the whole two-row deck answers.
10. A message of yours **waiting** for the answer to finish, in the block above the
   box — a click anywhere along its line takes that message back into the box to be
   edited, and the block loses it. The whole line answers, because nothing shares it.
   A press on the dim line under the block does nothing: that line is a statement,
   not a message.
11. The body: an inline **task link** inside prose, which is the one mouse-only target
   on the surface; a cut markdown table's foot; a waiting sign-in, where a click
   copies its link; a thinking block, clickable over its whole height; a tool row,
   which opens its expansion, or the full-frame sheet on a narrow terminal; the
   `N earlier tool calls` fold; the `… N more lines` foot, which lifts the cap; a
   spawn card, which opens the node's room, or its brief if there is no node yet; and
   a landed card, which opens its full context — except on the answers row of a card
   that needs your look, where each of `[a] accept`, `[l] look again`, `[n] not right`
   and `[d] decide these for me` is its own target and a press between them does
   nothing.

**A file path is a different kind of target.** Everything numbered above is a click
aforge itself answers. A real file path — in a reply, in a note, on a `read`/`edit`/
`write` row, under a picture — is a **terminal hyperlink**, so your terminal answers it,
usually on **cmd+click** (ctrl+click on Linux). The underline is how you can tell it
works. See "click a file path to open it" on the "what is on the screen" page for which
terminals open one and what is deliberately not linked.

**A click on empty space does nothing, anywhere** — there is no empty-space gesture on
this surface, and that includes inside a room: a press on a blank row of a task's page
is not the way out and never closes it. The way out of a room is `esc`, `←`, or a press
on the pinned header that names them. **A click in copy mode acts on nothing**, because
the rows there are a frozen snapshot.

**Hover** lights whatever the pointer is on, at the size of the thing rather than the size
of its row: a row that is one target — a tool call, a roster row, a parked message, a
room's header — takes a background band across the width, and something that shares its
line — a strip chip, a picture on the tray, one answer of a card, a task reference in a
reply — lights only its own cells, leaving its neighbours dark. Anything that answers to
nothing does not react. On home it does one thing more: the preview on the right becomes
the row you are pointing at, and returns to the cursor's row when you point somewhere else
(see the home page). There is no hover in copy mode, on the linear/screen-reader tier, or
in the phone tool sheet. The screen page says what lights, under "When a row brightens
under the pointer".

## Scrolling

The wheel moves three rows per notch, on whichever surface owns the frame. It is
routed to copy mode, then the settings panel, then the task page, then home, then the
rewind timeline, then the status deck, then the phone tool sheet, then the fullscreen
roster, then **the task column** when the pointer is over it, then an open room, and
otherwise the conversation.

On the settings panel, the task page, home, the rewind timeline and the fullscreen roster
the wheel walks the **cursor** rather than a scroll offset of its own, because on those the
window follows the cursor.

**The task column on the right scrolls under the pointer** and leaves the conversation
beside it where it is. It moves the column's own window — running work stays pinned at the
top, and the `tasks` label with it — unless the column is holding the keyboard (`ctrl+t`),
in which case the window is already following the cursor and the wheel walks that instead.

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

## Selecting text with your mouse — drag to copy

**Just drag.** Sweep the pointer over the conversation (or a task's room, or a run's
page) with the left button down: the rows under the sweep highlight, and the moment
you release, their text is **on your clipboard** — stripped of colours and the drawn
left rails, exactly as copy mode strips a yank. There is nothing further to press:
no ctrl+c, no key at all — releasing the button IS the copy. The highlight stays lit
for the few seconds the status line says `copied · 12 lines`, so you can see exactly
what landed. The write goes over OSC 52, so it works over ssh and through tmux. The
selection is by rows — whole lines, not characters — and what is highlighted is
exactly what is copied.

The sweep is drawn in **the same background copy mode's selection wears** — the strongest
of the three this screen draws, a shade above the one under the pointer. It is the same
claim ("these rows are what a copy would take"), so it is the same paint; it used to be
drawn at the pointer's own quieter step, which said a sweep in progress was a shadow
rather than a selection.

**The selection sticks to the text, not to the screen.** A task's page streams and
follows its live edge, so rows can scroll while your button is down; the highlight
rides the rows you swept, the copy is those rows wherever they moved to, and a click
opens the row you actually pressed even when it has shifted. A row that scrolls
clean off the screen before you release is no click at all.

A click is unchanged in feel: press and release in place and it lands where it always
did. (Under the hood the body's click now fires on release, the way every button in
every GUI does, which is what lets a drag never trigger the thing it started on — a
sweep that begins on a thinking block does not collapse it.)

aforge still owns the pointer by default — that is what makes wheel scrolling,
clickable paths and pressable rows work — and there is no scrollback to fall back on,
because aforge runs in the alternate screen. Two further doors remain for when you
want your terminal's own selection:

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
- The selection highlight is a background, and a terminal below ANSI256 gets no
  highlight at all. Read the span off the `COPY · N lines` count instead. It is the
  strongest of the three backgrounds this screen draws — a shade above the one under
  the pointer and the one under a chosen row — because a selection is held open and
  runs across many lines at once, and you are looking for both of its ends.

**An image drawn in an expansion copies as what it is on screen** — rows of `▀`, with
the colour stripped, which is no use to anybody. Take the dim line under it instead:
it is the picture's whole absolute path. It is also a link you can click, the same as
every other real file path on this screen — see "click a file path to open it" on the
"what is on the screen" page. What copy mode gives you is the plain path, with the link
stripped off it.

## Chords that mean more than one thing

Two chords carry unrelated meanings. Which one you get depends on where you are.
A third, `ctrl+v`, carries **one** meaning on several surfaces — move the thinking rung of
the thing you are standing on — and its own section below has the table.

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

Inside a task's page `ctrl+o` folds and unfolds the task's own turn, and scrolling up at
the top of the page opens the fold as well; the fold line there reads `N earlier tool
calls · scroll up or ctrl+o`.

Two more chords surprise people:

- **`ctrl+b` is copy mode, not emacs "left".** The alternate screen took your
  terminal's selection away, and copy mode is what buys it back.
- **`ctrl+e` means two things** depending on whether the box is empty: end of line
  when there is text, open the most recent thinking block when there is not.

And over an **empty** box, `left` and `right` are navigation rather than caret
movement. `ctrl+f` never is — it always moves the caret right.

## ctrl+v — how hard the thing you are looking at thinks

`ctrl+v` moves one step up the thinking ladder — `low`, `medium`, `high`, `xhigh`, `max` —
and it moves the rung of **the thing you are standing on**. One chord, three scopes:

| Where you are | What moves |
|---|---|
| The task roster holds the keyboard (`ctrl+t`) and the cursor is on a task | That task's rung |
| You are inside a task's page | That task's rung |
| Home, with the cursor on **no row at all** | The machine's own default — the `thinking` row in `/settings` |
| Home, with the cursor on a `◦` standing item row or its card | That item's rung |

Everywhere else it does nothing at all. A conversation row on home is deliberately not on
the list: a conversation's rung belongs to the window that conversation is open in.

**It climbs and it wraps.** Each press goes one rung up, and `max` wraps back to `low`. It
never returns to "nobody said" — clearing a rung hands the work back to whatever stands
over it, which is a decision rather than something a wheel does on its way past. Set a
thing back to nothing in the place it is written down: the `thinking` row's own `off`.

**The rung reads as a quiet clause where the thing already states its facts.** A task's is
on its page's header, after the model — `◆ Fix nil-map · running · 42s · $0.31 · gpt-5 ·
thinking high` — and on the roster's own figures row when the column is wide enough to hold
it. The machine's is on the card home draws at rest. An item's is on that item's card. A
thing nobody has dialled says nothing, which is not the same as `low`.

**On a task it lands on the next call, not this one.** A worker already running keeps the
rung it started with, so the line aforge writes says so: `task 7 · thinking · high · its
next call takes it`. A task that has finished refuses, in the engine's own words — `task 7
is done, not running` — because what it spent is a fact you may read and must not edit.

**Every card that takes it says so.** The card's dim legend reads `ctrl+v think harder`,
and it is drawn only where the key would work: a settled task's roster row does not offer
it, and neither does a window with nowhere to write the setting.

## Chords that are not bound

These do nothing in the v3 chat. If you expect one of them, here is the straight
answer:

| Chord | Status |
|---|---|
| `shift+enter` | **Bound**, in one state: while a turn is running with something typed, it stops the answer and sends that message. It does **not** open a new line — use `alt+enter` or `ctrl+j`. Over an empty box, or with nothing running, it does nothing |
| `ctrl+d` | Not bound |
| `ctrl+k` | Bound in **one** place: it saves a harness design from inside that design's room, while its approval row is up. Not bound anywhere else |
| `ctrl+r` | Bound. In the message box it is **spell it out** — see "Make my prompt better" above — and in the `/files` list it opens the folder a file is in. Nowhere else |
| `ctrl+v` | **Bound**, on three surfaces: it moves how hard the thing under your cursor thinks — a task, the machine's own default on home at rest, or a standing item. See "ctrl+v — how hard the thing you are looking at thinks" below. Anywhere else it does nothing. It is **not** paste: paste with your terminal's own paste, which aforge reads as bracketed paste |
| `ctrl+x` | Bound in the same one place: it drops a harness design from inside its room. Not bound anywhere else |
| `ctrl+y`, `ctrl+z` | Not bound |
| `ctrl+h` | Deliberately not bound, because some terminals send plain `backspace` as `ctrl+h` |

A key that is not bound falls through to "does this key carry text". If it carries
text it types; if it does not, nothing happens.

## ctrl+g — send the running command to the background without killing it

While a foreground `bash` command is running, `ctrl+g` hands it to the background
instead of waiting for it. **Nothing is killed and nothing is run again**: the
command keeps going as a job, the row gains a dim `job 3` beside its other
trailing marks, and the turn carries straight on. Read it as "go on" — let the
command run and get on with the work.

This is the key for the moment you realise `go test ./...` is going to take nine
minutes. The alternatives are `esc`, which stops the turn and throws the run
away, and waiting.

Afterwards it is an ordinary job: ask aforge to list them, tail one, or kill one,
and it is killed with everything else when the session closes. The command's log
file is named in the call's own result, which you can read by opening the row.

**The key is absent whenever it cannot work**, and absent means it does nothing
at all rather than telling you it cannot:

- nothing is running
- the running call is not `bash` — a `read` or a `write` has no process to hand over
- the running `bash` asked for the background already, so it was a job from the start
- the row has already been sent away

When more than one command is running at once, `ctrl+g` takes **the one that has
been running longest**, which is the one you are waiting on.

`ctrl+b` is copy mode and `esc` interrupts; neither changes. `ctrl+g` was
previously unbound.

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

Indented reply text is also **greyer** than the answer, and carries no markdown — no
bold, no headings, no code colouring. See "Why is part of the reply grey, and where is
the actual answer" on the screen page.

## How do I see what aforge did?

This is the answer to "what did aforge just do", "show me the work behind that answer",
"see the tool calls it ran", and "what happened during that turn" — the finished work is
folded, and one gesture opens it.

When a successful turn has work and a trailing answer, the finished work collapses to
one indented chip between your message and the answer, such as
`▸ worked 47s · thought 6s · 6 tool calls · ctrl+e`. Its figures are the whole turn's
elapsed time, the thinking block's time when there was one, and the real call count.
There is a blank row between the chip and the answer under it.

**A turn you stopped with `esc` says so instead**, and it collapses whole:
`▸ stopped by you at 40s · 4 tool calls · ctrl+e`, with nothing left standing under it.
A stopped turn never reached an answer, so there is no answer to leave out of the chip —
that is the point of the wording. aforge's own lines about the stop (`· stopped`, and
what it dropped from the queue) stay outside the chip where you can read them.

Click the chip or press `ctrl+e` over an empty message box to open or close it. There is
no transcript cursor, so the key chooses the latest completed turn's work in the
conversation. Opening restores the existing bounded views: thinking remains its
own chip and only the latest 3 tool calls show until those are opened separately.
Questions, approval prompts, failure lines, text-only turns, and work with no trailing
answer are never hidden — nor is a second message you sent into a running turn, which
ends the chip above it and starts a new one. Fold state belongs to this window; resumed
sessions derive fresh closed chips from their saved entries.

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
- **Rewind**, which `esc` `esc` opens quick and `/rewind` opens whole: the sessions and
  rewind page.
