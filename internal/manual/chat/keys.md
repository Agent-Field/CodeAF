# Keys, typing, and the mouse — what all the keys do

## Which key sends, and which key opens a new line

`enter` sends the message you have typed.

`alt+enter` opens a new line inside the message without sending. `ctrl+j` does the
same thing — it is a second spelling for terminals that swallow `alt+enter`.

`shift+enter` does **not** open a line. While a turn is running it **stops the answer
and sends what you have typed** — see "Interrupt and say something new in one key"
below. At rest it does nothing at all. Use `alt+enter` or `ctrl+j` to open a line.

`cmd+enter` while a turn is running **holds** what you have typed for the answer
after this one. It is the secondary choice for when you do not want to change the
work already under way. At rest it does nothing at all.

`ctrl+enter` sends it as **something to keep true** — a standing order — instead of
work to do once. The standing orders page has the whole of it.

What `enter` does depends on what is in the box:

- Text in the box: it is sent.
- An empty box with pictures in the attachment tray: it is still sent. A message of
  pictures and no words is a message.
- An empty box with a tool row selected: that row opens instead of anything being
  sent.
- An empty box with nothing selected: nothing happens.

While a turn is running, plain `enter` **steers**: it stops the model's current
reply where it is, keeps what has arrived, and sends your words into the same turn.
See "Typing while an answer is still coming" below. `ctrl+q` instead queues a fresh
turn to run after the current one; an empty box does nothing. Until its turn starts,
a dim row above the box reads `  after yield · N`. If queueing fails, aforge notes
`follow-up failed: <error>`.

## Typing while an answer is still coming — interrupting and steering

Typing is never blocked. The box works normally while an answer streams.

`enter` while a turn is running steers. Your words appear at once as a line of your
own in the transcript. A short dim clause beneath says where they landed, such as
`stopped the reply here`, `kept running as job 3`, or `waiting for the running step`.

If text or reasoning is streaming, aforge cancels that one model request, keeps the
partial answer it actually received, and continues the **same turn** with your words
as the next user message. An incomplete tool call is dropped because a provider
cannot accept a tool call with no result.

If a short tool is running, aforge lets it finish and lands your words at that
boundary. If a bash command has already been running for 3 seconds, aforge keeps it
alive as a job and lands your steer immediately. The clause names the job and
`jobs output N` shows its output. The exact words `stop`, `kill it`, `cancel`,
`abort`, `ctrl-c` and their tiny variants stop that long command instead.

## Enter, cmd+enter, shift+enter, and the waiting-message keys

| What you do | What happens |
|---|---|
| `enter` | stops the current generation and sends the words into this turn |
| `cmd+enter` | holds the message for an ordinary turn after this answer |
| `esc` | stops the answer and clears both waiting-message queues |
| `→` over an empty box | steers the oldest waiting words into the running answer |
| click `→ steers it in` | the same, with the pointer |
| `shift+enter` instead of `enter` | stops the answer and sends the sentence in one key — see below |
| `↑` over an empty box | takes the newest waiting message back into the box to edit |
| click the block | takes **that** message back into the box to edit |
| `cmd+enter` again | holds the edited sentence again |

Attachments in the tray go with the held message, and come back on the tray if you take
it back. `/`-commands are **not** held: a slash command is something you said to this
surface rather than to the model, and it runs at once.

**Limits.** `esc` with nothing waiting is exactly the plain interrupt it always was.
With messages waiting, it clears both the editable parked queue and the `ctrl+q`
follow-up queue. Each nonempty queue says what was dropped — `1 waiting message dropped`
or `N waiting messages dropped` for parked messages, and the corresponding `queued`
word for follow-ups. Replacing the conversation with `/new` or from the welcome box also
drops parked messages. Inside a **task room** `enter` steers the node instead and nothing
is held; that is the room's own key (see the room section below). Your line lands on the
task's page as a `└ ` elbow where you said it, with a short `· delivered` clause that
fades away.

**Why it goes straight in.** Plain `enter` is the gesture people expect to act now.
The current generation is itself made into a legal boundary: aforge keeps its partial
assistant message without incomplete tool calls, writes your user message after it,
and asks the model again. A steer that races with a turn already sealing still lifts
to the follow-up queue, so the words are never dropped.

## Correct it without stopping the turn — `enter` stops only the current reply and steers

`enter` while a turn is running sends what you have typed **into that turn**. The
current provider request is stopped, what it already streamed remains in the
transcript, and no second turn starts.

**It is the same question, not a new one.** Your sentence is added to the transcript of
the turn that is running, as your own words, and the model reads it at its next step —
after everything it has already done about your original question. That is what makes
it a correction rather than a restart: "no, the *other* file" arrives while the work
is still going, and the work carries on from there.

The correction appears on the page where you sent it, between the work already shown and
the next tool row. It is flush left in your own column, with a dim `└` marking that it
continues the same question. It stays in that position after the turn finishes and when
the conversation is reopened from disk.

**While it waits, the row says where it landed.** Next to your words, in dim, one of:
`stopped the reply here` when the reply in flight was cut for it, `stopped the running
command` when your words plainly told a long command to stop, `kept bash running as job 3`
(or `…as jobs 3, 4`) when a long command was moved to the background so your correction
could land, and `waiting for the running step` when a short tool is being allowed to
finish first. The clause goes when the model is actually given the words; the position of
the line is what says where they went from then on.

This is the key for the moment you are watching an answer go the wrong way and you do not
want to pay for stopping it. The three keys, side by side:

| Key | What happens to the answer | What happens to your sentence |
|---|---|---|
| `enter` | current generation stops; partial kept | goes into the same turn now |
| `cmd+enter` | keeps going | waits above the box until the answer finishes |
| `shift+enter` | stopped, and what it said is kept | opens the next turn |

**Over an empty box `enter` does nothing**, unless a waiting message offers the `→`
shortcut. At rest, `enter` sends an ordinary new turn.

**The `→` shortcut for a message that is already waiting.** If you pressed `cmd+enter` and
your message is sitting above the box, `→` over an **empty** box sends that message in
instead of leaving it to wait. It is the front of the queue that goes, and the dim line
under the block says so: `→ steers it in`. You can click those words instead. With
anything typed in the box, `→` is the caret key it always is — the shortcut only exists
where `→` had nothing else to mean.

**A message with pictures, or one marked with `ctrl+enter`, is left waiting.** Only
words steer; pictures take their own durable attachment path, and a marked sentence
is bound for the standing-order door. When either is at the front of the queue, the
line does not offer `→ steers it in`.

**The line that teaches it.** While a turn is running and you have words in the box, the
right end of the row under the message box reads exactly:

```
enter steers it in · shift+enter stops and sends · esc interrupt
```

That is the terminal-capable form when no command can be kept. A running foreground
command adds `ctrl+g backgrounds` immediately before `esc interrupt`; a terminal that
cannot deliver `shift+enter` leaves that clause out. `cmd+enter` still waits, but the
one-line slot no longer advertises it.

## My message went in too late — the answer finished first, so it became the next message

A sentence sent into a running turn is accepted only while the turn can still make a
boundary. A steer normally creates that boundary by stopping the current generation.
If the turn was already sealing, there is no request left to stop.

**Nothing is dropped and nothing pretends.** When that happens your sentence is lifted
out and simply becomes the next message: an ordinary one, waiting for a turn of its own.
The screen says that is what happened, and the reply arrives in the turn that follows.
You do not have to type it again, and you do not have to check.

The fall-through message becomes an ordinary next turn. The surface removes its
temporary steer clause and draws the normal user line when that next turn begins.

## Why cmd+enter does nothing — the secondary wait key needs terminal support

`cmd+enter` reaches a program only where your terminal can tell it apart from a plain
`enter` — the kitty keyboard protocol, xterm's modifyOtherKeys, or win32-input. Ghostty,
kitty, WezTerm and recent iTerm2 profiles all do; a plain Terminal.app does not.

Where it cannot be spelled, the key arrives as ordinary `enter` and therefore
**steers**. Use `ctrl+q` if you need a guaranteed fresh turn on such a terminal.

On those terminals aforge never advertises the chord. The line under the box still
begins `enter steers it in`, because plain enter works everywhere, and ends with the
stop clause that works in the current state.

**What works everywhere instead.** `ctrl+q` queues a new turn after the current one.

## Stop it and tell it something different at the same time — interrupt and say something new in one key

`shift+enter` while a turn is running **stops the answer and sends what is in the box**,
as one gesture. Unlike a steer, it ends the whole turn and starts your sentence as a
new one after the stop finishes.

What happens, in order:

1. Your sentence goes onto the waiting queue exactly as `cmd+enter` would put it there.
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
and your message **steers** instead. On those terminals aforge never advertises the
chord. Use `esc` to stop the whole turn, then send the next message normally.

**The line that teaches it.** While a turn is running and you have typed something, the
right end of the row under the message box reads exactly:

```
enter steers it in · esc interrupt
```

On a terminal that can spell the secondary chords, the line reads
`enter steers it in · shift+enter stops and sends · esc interrupt`. A foreground
command that can be kept inserts `ctrl+g backgrounds` before the final stop clause.

**A picture on the tray is a message even when the box has no words.** It cannot steer, so
that form reads `enter waits · esc interrupt`, or
`enter waits · shift+enter stops and sends · esc interrupt` on a terminal that can
spell the secondary chord. With neither words nor a picture, the line is simply
`esc interrupt`, unless a command can be kept, when it is
`ctrl+g backgrounds · esc interrupt`.

## I typed while it was working — did my message get lost?

No. Every road ends in an answer.

**If you pressed `enter`**, aforge stops the current model request, keeps its partial
reply, draws your words immediately, and continues the same turn from them.

**If you pressed `cmd+enter`**, the message is held above the box for the next turn.
It is still yours: edit it with `↑`, click it, or let it go when the answer finishes.
Pressing `→` over that waiting message steers it in instead.

**If there was no step left** — the turn's last request had already gone out, or it
finished a moment later — your words are not dropped and not pretended about. They become
an ordinary message waiting for a turn of its own, the screen says so, and aforge answers
them in the next turn. You see your line, a pause, and then a reply. You do not have to
type it again.

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

**The words aforge uses for one stop.** They are five slots and one key press, so they
are worth reading together: `stopping` is the status word while the turn is being let go,
`stopping · detaching in 7s` is that same word once the 10-second bound is counting down,
`interrupted` is the status word once it is over, `· stopped` is the note left in the
conversation, and `▸ stopped by you at 40s` is the chip a stopped turn collapses to. A
turn that never let go leaves a sixth: `detached — the turn was let go of and nothing is
waiting for it`. If
you are looking for the word *interrupted* anywhere else on the screen, that is where it
is — the status line, and only after the turn has truly ended.

**What the screen says.** While a turn runs, the right end of the row under the
message box ends with `esc interrupt` — for example
`enter steers it in · shift+enter stops and sends · esc interrupt` while you have
typed something and this terminal can deliver `shift+enter`. A foreground command that
can be kept inserts `ctrl+g backgrounds` immediately before the stop clause. When a
message of yours is already waiting for the answer to finish, the last clause becomes
`esc stops and drops`. On the very first frame of a session the conversation carries the note
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

## Esc is not stopping it — why the turn is still finishing, how long stopping takes, and what happens if it will not let go

**I pressed escape and it is still running.** That is this section: escape is not being
ignored, the turn is being let go of, and if it will not let go aforge ends it for you
after ten seconds.

`esc` cancels the turn on the keystroke, but the turn does not close on the keystroke. A
`bash` call whose command left something holding its output waits up to three seconds
before the pipes are forced shut, and a `jobs` kill spends two seconds on a polite signal
and two more on the one that is not polite. For those seconds the status line reads
`stopping` rather than `interrupted`, and that is the honest word: the work is being let
go rather than gone.

**That window is bounded at 10 seconds and the screen says so.** The status line reads
`stopping · detaching in 7s`, counting down from the moment you pressed the key. If the
engine lets go inside the window — which is what almost always happens, because the longest
ordinary wait is about four seconds — the countdown simply disappears and the status word
becomes `interrupted`.

**Nothing moves in that window and nothing new is drawn.** The spinners are already gone
from the status line and from every tool row. Any consent question, account offer or
harness offer that was open is taken down on the key, because each was about work that is
now over. Whatever the model says while the turn winds down is not shown — a sentence it
was still speaking stops where it was, and a call it was half-way through asking for never
becomes a row. Two things do still land, because neither can draw anything new: a call
that was **already** on screen reports its own result if it returns in that moment, and
what the turn spent is still counted.

**No key makes it stop harder, because the second stage is a clock and not a key.** A
second `esc` inside half a second is the rewind's door and `ctrl+c` is the quit arm, so
neither is free — and you do not need one. The `esc` you already pressed started the
10-second window, and when it runs out aforge stops waiting on its own.

**What happens at 10 seconds.** aforge detaches from the turn: the waits aforge holds are
ended and whatever request was still open to the model is aborted. A wait that ignores
being cancelled — a command whose output a grandchild is still holding, say — may run on
behind the detached turn; what detaching guarantees is that NOTHING IS WAITING FOR IT any
more, not that it is already gone. The conversation gets the note `detached — the turn was let go of and nothing is waiting for it`, and the box is
yours again — you can type the next thing straight away. If the turn had spent anything,
the note names it, for example `detached — the turn was let go of and nothing is waiting
for it — it spent $0.04`; under half a cent it says `it spent under a cent` rather than a
figure, and a turn that spent nothing says nothing about money at all.

**Nothing is silently orphaned.** A turn that had to be detached is written into the
conversation's own journal file as `abandoned`, with the tokens and the money it had spent
by then, so a turn nobody waited for is never a turn nobody can account for. What it cost
is already on your spending page either way: aforge counts money as each request is
answered rather than when a turn ends.

**You should almost never see this.** Ten seconds is well above the longest ordinary
letting-go, so the deadline only fires on a turn that was genuinely not going to end. If
something is wedged even further down, `ctrl+c` twice still quits and takes the whole
process with it — but you no longer have to reach for that just to get your prompt back.

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

## Keys — what all the keys do, the keyboard keys, keys on the keyboard, key bindings and keyboard shortcuts

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
| `ctrl+g` | A foreground command that can be kept: send that command to the background. Otherwise: close the task column, or bring it back. On a frame under 100 columns with no roster raised and no command to keep, it does nothing |
| `enter` while a turn runs | Stop the current generation, keep its partial reply, and steer the words into the same turn |
| `cmd+enter` while a turn runs | Hold the message above the box until the answer finishes. Empty box: nothing. Nothing running: nothing |
| `shift+enter` while a turn runs | Stop the answer and send what you have typed, as one gesture. Empty box: nothing. Nothing running: nothing |
| `→` over an empty box, a message waiting | Send that waiting message into the running answer. With text in the box it is the caret key |
| `↑` over an empty box | Take the newest waiting message back into the box to edit; with none waiting, walk your history |

The enter family, shortest first: `enter` sends or steers, `cmd+enter` holds it for the
next answer, `ctrl+enter` marks it as something to keep true, `shift+enter` stops the
whole turn and sends, and `alt+enter` or `ctrl+j` opens a line.

Neither `shift+enter` nor `cmd+enter` opens a line — use `alt+enter` or `ctrl+j` for that.
Both need a terminal that can tell them apart from plain `enter`; where it cannot, the
key arrives as ordinary `enter` and the message steers instead.

## Keys in the message box: opening things and moving the view

| Chord | What it does |
|---|---|
| `ctrl+o` | Selected landed card: open its output. Selected proposal: open its brief. Inside a task's page: open or fold the long instruction at the top. Otherwise: fold or unfold this turn's tool cluster |
| `ctrl+b` | Enter copy mode — freeze the view so you can read and copy |
| `ctrl+s` | Hand the pointer to your terminal so you can drag-select. Toggles; any other key takes it back |
| `ctrl+,` | Open the settings panel |
| `ctrl+v` | Walk this conversation's thinking rung one step: low → medium → high → xhigh → max, and round again. Works with a sentence half typed |
| `ctrl+.` | Open the tasks place (`/history`) — every task this machine has run, across every project and every session; type to filter it. It opens on a machine that has run nothing too, and the page says what tasks are |
| `space` `space` | On an **empty** box: open home (`/home`) — every project and conversation on the machine the session runs on, and an empty home on a fresh one. Does nothing when the box has words in it |
| `ctrl+l` | Jump back to the live edge of the conversation |
| `ctrl+t` | Give the keyboard to the task roster. Press again or `esc` to take it back |
| `ctrl+g` | A foreground command that can be kept takes the key first. Otherwise close the task roster's column, or bring it back — the column stands even with no tasks in it. Remembered for the next session. On a frame under 100 columns with no roster raised and no command to keep, it does nothing |
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
| `alt+left` / `alt+b` / `ctrl+left` | Jump a word left. `option+←` arrives as one of the first two on a Mac. Does nothing over an empty box — the plain arrows keep their navigation meaning |
| `alt+right` / `alt+f` / `ctrl+right` | Jump a word right, under the same three names |
| `super+left` / `meta+left` / `ctrl+a` | Start of the line. `cmd+←` arrives as one of these on a Mac. Both `super` and `meta` are bound because a modified arrow and a modified letter arrive under different ones |
| `super+right` / `meta+right` | End of the line — one of the two spellings `cmd+→` can arrive as |
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

It is the ordinary text-field gesture, and **the box on every place answers it
too** — home, tasks, standing, memory, spend, search, settings. While a picker's
filter box is standing in the box's place — the model picker, `/resume`,
`/files`, the memory panel — or while the composer layer is up, a click does not
move that box's caret; those are typed at and filtered, not edited by pointer.

## Why option+left or cmd+left does nothing — word jump and line jump on a Mac

**On a Mac, `option+←` / `option+→` are the word jumps and `cmd+←` / `cmd+→` are
the line's ends, in every box aforge has.** They work in the message box, in
home's box at the foot of the screen, in the errand pane, and in every filter and
search box on every place and panel.

They work because of what the terminal sends, and a Mac terminal sends them one
of two ways — aforge answers both:

- **iTerm2's Natural Text Editing key mappings** (the preset most people have)
  send `esc b` for `option+←`, `esc f` for `option+→`, the byte `0x01` for
  `cmd+←` and `0x05` for `cmd+→`. Those reach aforge as `alt+b`, `alt+f`,
  `ctrl+a` and `ctrl+e`, and all four are bound. **This works with the option key
  set to *Normal*** — the mappings do the work, so nothing has to be turned on.
- **Terminals that keep option a modifier** — Ghostty, Kitty, WezTerm, and iTerm2
  with **Settings → Profiles → Keys → Left Option Key** set to *Esc+* — send
  `alt+left` / `alt+right` instead, and `meta+left` / `meta+right` for the `cmd`
  arrows. Those are bound too.

If `option+←` still does nothing, the profile has neither: set **Left Option
Key** to *Esc+*, or load **Settings → Profiles → Keys → Presets → Natural Text
Editing**. Either one is enough, and you only need one.

**`ctrl+←` and `ctrl+→` are bound but will never arrive on a Mac.** macOS takes
them for Mission Control's "Move left/right a space" before any terminal sees the
keystroke. They are the Windows and Linux spelling of the word jump and they work
there. On a Mac, to get them you would have to turn those two shortcuts off in
**System Settings → Keyboard → Keyboard Shortcuts → Mission Control** — there is
nothing aforge can do about it from inside.

**`ctrl+a` and `ctrl+e` are the spellings that work on every terminal there is**,
and they are the same two jumps. If you would rather not depend on any of the
above, those are the keys.

## Word jump and line jump work in every box, not only the message box

The caret keys are one vocabulary and every box on the surface answers it: the
message box, **home's box at the foot of the screen**, the errand pane on home,
the settings filter and its value editor, the task page's filter, the rewind
search, and every filterable overlay — the model picker, `/resume`, `/files`, the
memory panel, the connect key box, the connections panel.

| Chord | Everywhere |
|---|---|
| `alt+left` / `alt+b` / `ctrl+left` | A word back |
| `alt+right` / `alt+f` / `ctrl+right` | A word forward |
| `super+left` / `meta+left` / `ctrl+a` | Start of the line |
| `super+right` / `meta+right` | End of the line |
| `alt+backspace` / `ctrl+backspace` / `ctrl+w` | Delete the word behind the caret |
| `ctrl+u` | Delete to the start of the line |

This did not used to be true: until this wave the jumps were bound in the message
box alone, so `option+←` moved a word in a conversation and did nothing at all in
home's box — which is the first box most people type into. `home` and `end` are
the exception and stay with the box that owns them: on the settings panel, the
task page and the rewind sheet they move the **list**, not the caret.

## cmd+right on home no longer puts a conversation away

`ctrl+e` sets the row under the cursor aside on home — a conversation goes to the
archive, a standing item is paused, and the card's legend says `ctrl+e put away`.
`cmd+→` arrives as `ctrl+e` on a Mac, so reaching for the end of a sentence used
to archive whatever the cursor was resting on.

**It reads the caret now.** With the caret somewhere before the end of what you
typed, `ctrl+e` moves it to the end of the line and leaves the row alone; from the
end of the line — and over an empty box — it is the put-away key the legend names.
So one press is never destructive, and the way back out of the archive still
works: type the name of a row you put away, the list finds it, and `ctrl+e` from
there brings it back.

## Click the box to put the caret there — on home and on every place

A click on the box puts the caret under the pointer: on the letter you aimed at,
at the row's end when you click past the end of a line, and at the start of the
text when you click on the prompt's side of it. It works on a wrapped, multi-line
draft.

It answers **on every place as well as in the conversation** — home, tasks,
standing, memory, spend, search, settings. Until this wave only the conversation's
message box answered it, so a click in home's box moved nothing.

Two things it does not do. With **nothing typed** there is no caret to place, so
the click falls through to the place underneath — the row is carrying a dim
sentence rather than a draft. And while a picker's filter box is standing in the
box's place — the model picker, `/resume`, `/files`, the memory panel — or while
the composer layer is up, a click does not move that box's caret; those are typed
at and filtered, not edited by pointer.

## Keys in the message box: deleting words and lines

| Chord | What it does |
|---|---|
| `backspace` | Delete the character behind the caret. Over an empty box with attachments, it removes the last attached picture instead — and its `[image #n]` token with it |
| `delete` | Delete the character in front of the caret |
| `ctrl+u` | Delete to the start of **this line** — not the whole message |
| `super+backspace` | Same as `ctrl+u` (Mac `cmd+delete`) |
| `ctrl+w` | Delete the word behind the caret. While the switcher is up it closes the conversation under the cursor instead |
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
| iTerm2 | Yes with the **Natural Text Editing** preset, which maps `⌘⌫` to the byte `0x15` — that reaches aforge as `ctrl+u`, the same deletion. Load it at **Settings → Profiles → Keys → Presets**. Without a mapping iTerm2 does not forward `cmd+delete` at all; you can also add one by hand at **Key Mappings**: `⌘⌫`, action *Send Escape Sequence*, `[127;9u` |
| Terminal.app | No, and it cannot be made to. It does not speak the keyboard protocol that carries modifiers like `cmd` |
| Anything over `ssh` or `tmux` | Only if the outer terminal is one of the first three, and tmux is passing the protocol through |

**`ctrl+u` is the spelling that works everywhere**, on every terminal on every
machine, and it is the same deletion. If `cmd+delete` does nothing where you are
sitting, that is the key to use instead — nothing is missing and there is nothing
to turn on inside aforge.

The same is true of `alt+backspace` and `ctrl+backspace` for the word kill, and
`ctrl+w` is *their* everywhere-spelling. On iTerm2's Natural Text Editing preset
`⌥⌫` is mapped to `esc del`, which arrives as `alt+backspace` and kills a word.
aforge does not detect what your terminal sends and cannot tell you which of
these it will deliver; the only test is pressing it.

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
- **A box holding only blank lines or spaces is not a draft**, and nothing is written
  for it — the file is removed instead. Blank lines are what `ctrl+j` and `alt+enter`
  leave behind, and what `ctrl+enter` and `shift+enter` leave behind on a terminal that
  cannot send those chords, and nothing on the frame draws them. One kept on disk used
  to be adopted by the next window in the directory, which then opened with a box that
  looked empty, was not, and refused `space space` for home.
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

## The thinking chip above the message box — `ctrl+v`, and making this chat think harder

The row above the message box carries a small chip naming how hard the model will think
about your next turn:

```
                                                                      ⠿ high
› what changed in the relay this week
```

The word is one of the five rungs of the effort ladder — `low`, `medium`, `high`,
`xhigh`, `max` — and it is **what will actually happen**, not what somebody chose: it is
the rung the next turn will ask for, whichever setting decided it. See *Making the model
think harder, deeper, or less* on the "Models and cost" page for the whole ladder and for
what each rung asks the provider for.

**`ctrl+v` walks it.** Each press moves one rung up and wraps off the top:
low → medium → high → xhigh → max → low. It works with a sentence half typed — it is a
chord, it carries no text of its own, and it leaves your draft and your caret exactly
where they were. Ordinary letters keep typing.

**Clicking the chip opens the ladder**: five rows, cheapest first, with the rung you are
on marked. `↑`/`↓` walk it, `enter` applies, `esc` closes, and `ctrl+v` moves the cursor
down a row while the list is up. While the list is up **every key belongs to it** — a
plain letter does not type into the message box underneath. Clicking the chip a second
time puts the list away, and a click on the chip never moves the caret in your draft.

What it changes and what it does not:

- It sets **this conversation's** rung. It is sticky — kept in this session's own
  `meta.json` — so it is still there after you close aforge and come back.
- The rung reaches the work this conversation hands out: task workers start at it too.
- It does **not** change other conversations. The default for those is the **thinking**
  row in `/settings`, which ships at `high`.
- **`off` is not on the chip or in the list.** The five rungs are the ladder; turning
  thinking off entirely is the `off` choice on the **thinking** settings row.
- With thinking set to `off` and nothing else asking for any, there is **no chip at all** —
  there is nothing to report. `ctrl+v` still works and puts the chip back at `low`.

**When the chip will not move.** A thinking level dialled onto the model itself — the
model picker's `ctrl+t`, or `--reasoning` at launch — beats this conversation's rung. Press
`ctrl+v` there and aforge says so in a note, naming the model and pointing at `ctrl+t`:
*thinking stays low · the level set on \<model\> decides this conversation — ctrl+t in
/model changes it*. Clear that level and the chip moves again.

The chip is dim, like the rest of that row. It brightens for about two seconds after it
changes, so you can see the new word without looking away from what you are typing, and
then it goes quiet again.

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
spaces backslashed, sometimes quoted, sometimes as a `file://` URL, and sometimes with
raw spaces or `%20` escapes. aforge reads every shape, including the narrow no-break
space in a macOS screenshot name, and reads several files dropped at once, separated by
spaces or by newlines.

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

**It is all or nothing, on purpose.** A paste is treated as attachments only when one
complete terminal reading of it names real files **on this machine**. A sentence that
mentions a `.png`, a diff, a stack trace, a log — all of it goes into the message box as
the text it plainly is, which is what pasting has always done.
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
still ask aforge to look at the file where it lies. Pressing `enter` on that retained
absolute path repeats the attachment refusal; it is not treated as an unknown slash
command and the path remains in the box.

A picture that is dragged in but **does not exist on this machine** stays in the message
box unchanged and says `<basename> is not on this machine`. The path is still there to
edit or retry; no chip means no picture will be sent.

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
tray. Once the message is sent, the transcript keeps the numbered marker and draws a
small thumbnail of each picture under your line.

A command with a full tray is still a command: `/image` adds a second picture rather
than sending the first.

## Do I see my own screenshot in the conversation?

Yes. After you send a message with pictures, each one is drawn under your line in tray
order. The dim `[#1 shot.png]` marker stays in the sentence above it, numbered to match
`[image #1]` and still clickable as the file door.

Each thumbnail is at most **12 rows**, or **4 rows** at phone width, with no heading,
border, path line or `… N more lines` foot. It uses half-block colour in TrueColor and
the 256-colour xterm cube. On a sixteen-colour or colourless terminal, an ASCII-only or
screen-reader display, a very narrow row, or when the file is missing or unreadable, no
thumbnail is added and the marker remains exactly as it was.

This applies to a live message, a resumed conversation while the referenced file is
available, and a task room's journal. A waiting message in the parked block remains its
words and markers; its picture appears after that message is actually sent.

## Completing a path with `@`

Type `@` and aforge offers **tasks first, then files and folders**, in one list under
the message box. It opens on the bare `@` — you do not have to type a letter first. It
closes on `esc`, on committing, or when the token stops being one.

**Folders are on the list too**, spelled with a trailing slash — `internal/tui3/` — and
marked `folder` on the right the way a picture row is marked `img`. Choosing one puts
its path into your sentence exactly as choosing a file does. It does not choose that
folder as a place; `/folder` is what does that.

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
- **A folder:** the same thing, with the trailing slash kept — `@internal/tui3/`.
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

**The honest limit: `/image `, `/attach ` (and its `/upload ` alias), and `/export `
get path completion.** That is the whole list. Any other command that takes a path gets
no completion at all, and says nothing about it. Over `--host`, completion still walks
the machine you are sitting at: `/attach` and `/image` send those local bytes across.

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

**Model picker** — opened by `/model` with no argument, by clicking the model name
in the status row, or by `enter` on the **your model** row of the settings panel's
Providers tab (the same list and the same keys, drawn in the panel's place):

`esc` close · `enter` switch to the highlighted model · `ctrl+t` cycle the reasoning
effort · `tab` and `→` open the lanes under the model the cursor is on, `tab` and
`←` close them again · `up`/`ctrl+p`, `down`/`ctrl+n`, `pgup`, `pgdown` walk the list ·
`backspace`, `delete`, `ctrl+u`, `ctrl+w`, `left`/`ctrl+b`, `right`/`ctrl+f`,
`home`/`ctrl+a`, `end`/`ctrl+e` edit the filter · anything else types into it.

`→` and `←` only open and close the lanes from the **end** and the **start** of what you
have typed — with anything to step over, they move the caret through the filter text
instead. `tab` always opens and closes. With the lanes open, `enter` on one of them pins
it instead of switching model.

Its placeholder reads exactly `filter · ↑↓ · → lanes · ctrl+t effort · enter · esc`.

**Sessions roster** — opened by `/resume`: the same key map, except `enter` opens the
selected session. Its placeholder reads `filter · ↑↓ · enter open · esc cancel`.

**The empty screen's greeting:** it takes only two keys, and only over an empty message
box — `up`/`down` walk the recent sessions listed under it and `enter` opens the selected
one. Every other key dismisses the greeting and then does whatever it normally does; the
first letter you type lands in the box, which is drawn inside the greeting until then.

Both pickers are modal: while one is up, every chord except `ctrl+c` belongs to it.
`ctrl+c` does not close the picker — it arms the door, and a second press within 1.5
seconds quits aforge with the picker still up.

## Keys when aforge asks you a question — what key answers switch to auto, and the other offers

**An approval question:** `y` allow once · `a` or `t` always — refused when it would
do nothing · `n`, `d` or `esc` deny. Every other key does nothing, but it **stops the
countdown**. `ctrl+c` is handed back to the message box, where it arms the door and a
second press within 1.5 seconds quits. On the second beat of
"always" for a bash command, `1`–`9` pick a shape and `esc` goes back.

**A task proposal** is not modal — the message box stays live as a redirect lane.
Always available: `enter` submits a typed answer or takes the focused option over
an empty box, `esc` says no, and `ctrl+e` opens the brief over an empty box. Over
an **empty box only**: `left`/`right` move the focus, and `1`–`4` pick the model.
Bare letters are ordinary answer text, not immediate shortcuts. Typing the first
character stops the countdown and changes the meter to `waiting on you`; deleting
the draft does not restart it. On
`enter`, a bare `no`, `nope`, `n`, `stop`, `cancel`, `don't` or `dont` declines,
while a bare `yes`, `y`, `ok`, `okay`, `go` or `sure` approves. Longer text is a
redirect. Clicking `no` or pressing `esc` always declines, whatever is in the
box; only `enter` interprets the typed answer.

**A slow lane's offer**, raised on the status line when a machine you pinned has gone
quiet: the row reads `coreweave is slow · switch to auto? (y)` and `y`, **over an empty
box only**, fetches this answer from somewhere else. There is no key to decline — the
question takes itself down when an answer starts arriving — and with anything typed in the
box a `y` is a `y`.

**A connect offer:** `enter` or `y` yes · `esc` or `n` no. While its key box is open,
every key goes into that box except `enter`, which submits, and `esc`, which
declines.

**A harness offer:** `enter` or `y` yes · `esc` or `n` no. Everything else does
nothing.

**The steer guard**, raised when you press `enter` in a room whose node is not
listening: `r` revive and send · `m` send to main · `esc` cancel and keep your words ·
`ctrl+c` handed back to the door, where two presses quit · everything else does nothing.
Its row reads
`[r] revive and send · [m] send to main · [esc] cancel`. When the task is still
running — refused mid-check, or while its work lands — the guard offers no `r`: its row
reads `[m] send to main · [esc] cancel`, because reviving live work would duplicate it.

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
every section at once and is shown at the foot as `filter · port`. `→` opens the row's
verbs, and this place has one: `s stop it`, over a task this conversation is holding that is
still queued or running. Its foot is assembled from what is true of the row under the
cursor — `enter open its room · → verbs: stop it · type to filter` over a task this window is
running, `enter go inside it` on a task another conversation ran, which has no room to open,
no `enter` clause at all where the row under the cursor has no door — which is a page
holding only work running in other aforge windows — and `esc clear the filter` in place of
`type to filter` while you are typing one. Clicking a row acts on the first press; the wheel
walks the cursor. The tasks pages describe what is on it.

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

**Thinking ladder** (click the `⠿ high` chip above the message box): `esc` closes ·
`up`/`ctrl+p`, `down`/`ctrl+n` walk the five rungs · `ctrl+v` moves down one · `enter`
applies the rung under the cursor. Clicking a rung applies it; clicking either of the
two sentences around the rungs does nothing. Its foot reads
`↑↓ · enter apply · esc · ctrl+v next rung`.

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

**`tab`, pressed in a conversation with an empty message box, goes to the conversation you
were in before this one.** Press it again and you are back. It is `cd -`.

It **does nothing at all** when there is nowhere to go: one conversation open, or none this
terminal has been in before. A key that cannot act says so by not being advertised — and
when it can, the legend line above the box says `space space home · tab last · / commands`.

With **three or more** open, that slot says `ctrl+k switch` instead, and `ctrl+k` opens the
card of all of them — see *Switch between open conversations*. `tab` still works and still
goes to the last one.

It works while a turn is running in either conversation. Nothing is interrupted: the turn
you leave keeps streaming into its own transcript, and it is redrawn from its first token
when you come back.

**On a place, `tab` is the next place instead.** Home, tasks, standing, memory, spend,
search and settings are one circle and `tab` walks it; `shift+tab` walks it back. That is
the same key doing the same kind of thing — going to the next thing of the kind you are
looking at — and it is the only meaning `tab` has while a place is up. See **Places**.

**Everything else that wants `tab` gets it first**, and that is the whole rule rather than a
claim that `tab` is free. In order: a paste bracket makes it a literal tab; the task roster
eats it while it holds the keyboard (`esc` gives the keyboard back first); a box that has
taken the whole keyboard on a place keeps it — the errand pane on home, the value being
edited in settings; the rewind timeline and the inline rewind lift with it; and path
completion takes it over `/image ` or `/export `. Then, on a place, it is the next place.
Only in a conversation, with none of those claiming it and the box empty, is it the way back.

Two claims on `tab` were withdrawn when the places arrived, and both moved to a key that
points the way they go: **the settings panel** changed its own section with `tab`, and now
uses `←` and `→` alone; **the memory panel** changed shelf with `tab`, and now uses `alt+s`.

The welcome box is the one exception worth naming: **`tab` does not dismiss it**. Every
other key does — that is the box's contract — but switching away is the opposite of
starting work here, so the box is still standing when you come back.

## Switch to another conversation without going home — ctrl+k, the conversation switcher, switch between my open chats, alt tab between conversations

**Press `ctrl+k` and you are in your previous conversation, at once.** Press it again and
you are one further back. This is quick switch, the default: the press is the switch, the
way a browser's `ctrl+tab` changes tabs. A card is drawn over the conversation you just
landed in — the screen behind it dims — showing **the conversations this terminal has
open**, and when you stop pressing, the card fades by itself after about a second. `esc`
takes the whole thing back to where you started. There is nothing to confirm: by the time
the card fades you are already there.

Touch any other key while the card is up — an arrow, `→`, `ctrl+w` — and the card stops
fading and holds still, so you can look around without switching; `enter` then goes. With
quick switch turned off in `/settings`, `ctrl+k` always opens this holding card and waits.

```
╭──────────────────────────────────────────────────────────────────────────────╮
│  open              3 of 12 · tab down · shift+tab up · enter go · esc back   │
│                                                                              │
│  1 ? harness dry run on one pub…  asking you something      aforge-v2    4m  │
│  2 ◐ openrouter price scrape      2 tasks running            research    1d  │
│  3 ○ Refactor the rail scope mo…  you are here              aforge-v2   40m  │
│  ▸ 9 more on this machine · → reach them                                     │
╰──────────────────────────────────────────────────────────────────────────────╯
```

Fixed columns — **number, glyph, subject, one clause, project, clock** — which is what
makes it scan: the subjects form a straight edge you read down. `3 of 12` is how many are
open, of how many this machine has.

**Everything else on the machine is behind the fold at the foot.** `→` reaches them and
`←` puts them away again. A conversation below the fold is not open in this terminal;
taking one opens it beside the one you are in, exactly as `enter` on home does, and the one
you are in keeps running.

It works from the **first** session: on a fresh launch you hold one conversation, the card
has that one row, and the fold has the rest of the machine in it.

| Key | What it does |
| --- | --- |
| `ctrl+k` | Switch to the previous conversation at once; each further press goes one older. With quick switch off, it opens the card and steps the cursor instead |
| `ctrl+tab` | The same key, on terminals that can send it. See below |
| `ctrl+shift+k` / `ctrl+shift+tab` | Up one, on those same terminals |
| `tab` / `↓` | Down one — cursor only, without switching, and the card stops fading |
| `shift+tab` / `↑` | Up one. Both wrap round at the ends |
| `1`…`9` | On the holding card, go to that row outright — the number is drawn on the rows that have one. While the card is fading, digits are typing and land in your message |
| `enter` | Go to the row you are on |
| `→` | Open the fold — every other conversation on this machine |
| `←` | Fold them away again |
| `ctrl+w` | Close the conversation under the cursor. See below |
| `esc` | Take it all back: the card goes and you are in the conversation you started from, however many presses ago that was |
| any other key | While the card is fading, it is typing — the card goes and the key lands in your message. On the holding card it puts the card away and is swallowed |

It works **in a conversation and on every place** — home, tasks, standing, memory, spend,
search, settings — because it is drawn over the screen rather than being a screen of its
own. Nothing under it moves by a cell.

The cursor opens on the **first row you can actually go to**, never on `you are here`, so
`ctrl+k` `enter` always lands somewhere.

**It does nothing on a machine with one conversation on it** — a first run, and nothing
else — and says so by not being there: no card, and the legend above the box does not name
it. Everywhere else the legend reads `space space home · tab last · ctrl+k switch ·
/ commands`, dropping clauses from the left as the frame narrows.

**There is no cap** on what one terminal holds at once: taking a row is never refused for
having too many open. The card draws the first twelve rows and hands a digit to the first
nine; past that the cursor is the way, and home is the page that shows every conversation
you have. Nothing closes one for you — `/quit` closes the one in front, `ctrl+w` here closes
the one under the cursor.

## What did my other chats do while I was away — what each row of the switcher tells you

Every row says **what changed since you last looked**, not what the conversation is about:

| The row says | What happened |
| --- | --- |
| `asking you something` | it is waiting on you — a question or an approval |
| `3 tasks running` | that many pieces of work are turning in it right now |
| `it finished while you were away` | a turn ended in there after you left |
| `nothing new` | it has been quiet since you left it |
| `you are here` | the conversation you are sitting in |
| `open in another window` | another terminal is holding it; `enter` will refuse |
| `that folder is gone` | the project directory it worked in is not there any more |
| *(nothing)* | quiet — nothing has happened in it since you left |

Then the project it is in and how long ago you left it. That is what makes the switcher
double as the catch-up: after twenty minutes in one chat, one key says what the other seven
did.

The card is **frozen the moment it opens**. A conversation that finishes a turn while you
are looking at the card does not re-rank the list under your finger.

## Switch without pressing enter — quick switch, it goes when I stop pressing, it switched right away

**Quick switch is on by default**: `ctrl+k` switches on the press, the card over the new
conversation is a receipt, and pausing is what makes it fade — there is no `enter` in the
gesture at all. `ctrl+shift+k` (where the terminal can send it) cycles the other way, and
with the card down it enters at the far end of the ring: the open conversation you have
not looked at for longest.

Three things to know about the fast gesture:

- **`esc` is the undo.** However many presses deep you are, `esc` puts you back in the
  conversation the first press left, with the card down.
- **`tab` afterwards returns to where you started.** Cycling through two conversations on
  the way to a third does not make a stepping stone "the last one" — after the card fades,
  `tab` goes back to the conversation you were actually in before the burst.
- **You can start typing immediately.** A letter typed while the card is still fading lands
  in your message; the receipt never eats a keystroke.

**To turn it off**: `/settings`, interface, the row named `quick switch`. Off, `ctrl+k`
opens the card, the cursor steps, and nothing moves until `enter` — the same card, held
open, that any non-chord key converts the fading one into.

It commits on the press and never on releasing `ctrl`, deliberately: a terminal only
reports key releases under an optional protocol that dies inside tmux and most terminals,
and a gesture that worked at the desk and died over ssh would be worse than one honest
gesture everywhere. Chrome's `ctrl+tab` commits on the press too — there is no difference
to feel.

## Why ctrl+k and not ctrl+tab or alt+tab

`ctrl+tab` **is** bound — but only on terminals that can send it, and many cannot.

`ctrl+tab` has no distinct encoding in an ordinary terminal: it arrives as a plain `tab` and
is indistinguishable from it. Only a terminal that speaks the kitty keyboard protocol sends
it as itself, and aforge asks yours on every frame — where the answer is yes, `ctrl+tab`
opens the switcher and `ctrl+shift+tab` walks it back. Where the answer is no, the chord is
not bound and is never named, because a key aforge tells you about is a key that works.
Two popular terminals — WezTerm and Windows Terminal — also spend `ctrl+tab` on their own
tabs by default, so it would never reach aforge there.

`alt+tab` is not available at any price: the window manager takes it on Windows and on most
Linux desktops.

`ctrl+k` has none of those problems. Every terminal sends it, no window manager wants it,
and `ctrl+k` is already "jump to a conversation" in Slack and the switcher in VS Code.

**`ctrl+shift+k` is the reverse, under the same rule.** An ordinary terminal sends
`ctrl+shift+k` and `ctrl+k` as the same byte, so it is bound only where the terminal
answered the keyboard query — and it costs nothing that half the terminals in the world
cannot send it, because **`shift+tab` walks the card back on every one of them**. With the
card down, `ctrl+shift+k` opens the ring at its far end — the open conversation longest
unlooked-at — and under quick switch lands you in it at once.

## Close a conversation from the switcher — ctrl+w, closing a chat, too many open

**`ctrl+w` on the row closes that conversation in this terminal.** The card stays up and
says `closed · <name>`, so tidying three of them costs three keystrokes rather than three
openings.

**Closing is not switching.** It ends that conversation's session, and **work running inside
it stops with it** — the same thing the quit door warns about. So:

- a conversation with nothing running closes on **one** press;
- a conversation with work in it takes **two**, and the card says what is running in
  between: `2 tasks running · ctrl+w again to close it anyway`;
- moving the cursor cancels that warning, so a second press never lands on a row you have
  walked away from.

`ctrl+w` on the row marked `you are here` closes the conversation you are in and brings the
next one forward — the same thing `/quit` on it would do. With only one conversation open it
says `that is the only conversation open — /quit closes aforge`.

**`ctrl+w` on a row below the fold does nothing** and says
`that one is not open here — enter opens it`. There is nothing to close: this terminal is
not holding it.

Nothing caps how many one terminal holds, so `ctrl+w` is never about making room. It is
about ending something you are done with: a conversation left open goes on running, holding
its transcript's lock and its share of this window's memory, until you close it.

## `tab` still goes straight to the last one

`tab` on an empty message box has not changed: it goes to the conversation you were in
before this one, in one key, with no card. Use `tab` to flick between two and `ctrl+k` when
there are more.

The legend above the box names **every door that would act**: `tab last` appears once a
second conversation is open, `ctrl+k switch` whenever there is anywhere at all to go. On a
place the switcher is named on the map (`alt+.`) instead, because a place's foot is four
fixed clauses the design sets word for word.

## Keys in the composer layer — `alt+enter`, `alt+w`, `alt+o`, and typing a number

On macOS every `alt+` below is drawn `⌥` — `alt+enter` is `⌥enter`, `alt+w` is `⌥w`, `alt+o`
is `⌥o`. Same key, same chord, the spelling the keycap uses.

`alt+enter` with something typed into the composer on any place opens the **composer
layer**: the page behind dims, the box stays where it is, and the three facts a task needs
appear under it. The places page has the layer in full; these are its keys.

| Key | What it does |
| --- | --- |
| `alt+enter` | first press opens the layer; second press sends the task off |
| `alt+w` | move the task to the next project aforge knows, and round again |
| `alt+o` | open the model list for the **execution** slot — what the work runs on |
| a digit, or `.` | type the spend cap; the figure changes as you type |
| `backspace` | take one character off the cap |
| `enter` | talk about it instead — an ordinary conversation carrying the same sentence |
| `esc` | back to the place you were on, sentence still in the box |

`alt+w` and `alt+o` are bound **only** inside this layer. No place binds either of them, so
neither can move a view while you are aiming at a destination, and pressing them with no
layer up does nothing at all.

While the layer is up it has the whole keyboard: `tab` does not walk to the next place, and
letters do not reach the composer — what you typed is already written and is on the screen
above you. Inside the model list `alt+o` opens, the keys are the model picker's own — type
to filter, `↑↓` to walk, `enter` to use it, `esc` to go back to the layer.

## Keys on home, and is there a shortcut for it

**Press the space bar twice with an empty message box.** That is the way back to home from
inside a conversation, and `/home` opens it too.

**There is also a number: `alt+1` (`⌥1` on a Mac).** Home is the first of seven places, and
every one of them answers to its position on the tab bar — `alt+1` through `alt+7`. Hold
`alt` and press the digit. On macOS aforge draws the modifier as `⌥` because that is what the
keycap says; it is the same key and the same chord, and on Linux and on Windows it is drawn
`alt+`. It arrives in every terminal aforge runs in, which is why the numbers are on `alt`
rather than on `ctrl`.

**`ctrl+1` … `ctrl+7` are a second spelling, on the terminals that can send them.** `ctrl`
and a digit has no encoding in the forty-year-old scheme most terminals speak, so it is not
the first spelling and never will be — but a terminal running the kitty keyboard protocol
sends exactly the keys that scheme cannot spell, and it tells aforge it does. Where that
report arrives, `ctrl+1` … `ctrl+7` jump to the same seven places and `ctrl+.` draws the same
map, and the map's own line says `alt+1…7 or ctrl+1…7 go to a place` so you can see it is
live. Where it does not, those chords do nothing and are never advertised. kitty, ghostty,
WezTerm, foot and Windows Terminal are the usual ones that report it. **On a Mac this is the
way in that needs no setting at all** — see "Why my option key types ¡ ™ £ instead of
jumping" on the screen page.

**The numbers work from a conversation as well as from a place.** They are the one class of
place key that does: `tab` belongs to the composer's path completion while you are typing,
and the rest of the place grammar — `→` for the row's verbs, `alt+<letter>` for how a place
is shown, `shift+←→↑↓` for its time window — is about the room you are standing in. Every
number opens its room whatever is in it: a place with nothing of its own to draw spends the
frame saying what it is for, and none of the seven is ever a key that does nothing.

There is no `ctrl+<letter>` chord for home: every one this surface could use is already
taken, and `ctrl+.` is the tasks place (`/history`) from a conversation — while a place is
standing that same `ctrl+.` draws the map, on the terminals that can send it, because a place
takes the whole frame and never reaches the conversation's keys. `esc` was not available either: on an idle conversation it
already arms rewind and already clears messages waiting from the turn, and a third
meaning on one key in that state is how a surface stops being predictable.

**The first space types itself.** The second one, finding a box that still shows nothing
with that space behind the cursor, takes the whole draft away and opens home — so a leading
space you actually wanted is never eaten (space then `x` leaves ` x`). It does nothing when
the box has words in it, and it is not a paste: text pasted with two leading spaces is two
spaces. A machine with one conversation, or none, opens an empty home; so does a session
over `--host`, where what opens is the **far machine's** home.

**A box that looks empty and is not still answers it.** Blank lines left by `ctrl+j`,
`alt+enter`, or by `ctrl+enter`/`shift+enter` on a terminal that cannot send those chords,
draw nothing on the frame — and the gesture reads the box the same way the frame does, so
two spaces open home and the blank lines go with the draft. The rule in one sentence:
wherever the foot advertises `space space home`, two spaces open it.

It works while a turn is running; the answer keeps streaming underneath and `esc` puts you
back in it.

When the box is empty, the legend line above the box says so:
`space space home · / commands`. Clicking those words opens home. It vanishes as soon as
you type.

**The door does not ask what the machine holds.** It is open on a machine with only this
conversation and on one with none, from the first minute, and starting a second
conversation with `/new` changes nothing about it. It used to be shut until the launch
found somewhere else to go, and that rule is gone (the home page, *space space does
nothing*).

Once it is open: `esc` clears the box if anything is in it, and closes home otherwise ·
`up`/`ctrl+p` and `down`/`ctrl+n` walk the rows, stepping over the headings and the section
line · home opens with the cursor on **the conversation this window is holding**, and `↑`
off the top of the list walks up onto **the tab bar**, from where the first `down` lands
back on the row you left (*The tab bar is a row the cursor can stand on*) ·
`pgup`/`pgdown` jump a screenful · `enter`
acts on the row under the cursor · **`tab` is the next place** — home is one column now and
there is nothing on it for `tab` to cycle · **`alt+g`** groups the list by project and
**`alt+q`** hides everything that is neither asking nor moving, both remembered for as long
as aforge is running and neither written to disk · `backspace`,
`ctrl+u`, `ctrl+w`, `ctrl+b`, `ctrl+f` edit the box · with the box empty and the cursor on
a conversation that is **waiting on you**, the digits on its chips answer that question
where it stands (`1 allow once · 2 always · 3 deny`, and the like for the other two kinds
— home's own page has the table) · **anything else you type goes into the box**, which
searches the whole machine and offers to start a new conversation at the same time.

**`→` opens the row's verbs** on a strip drawn **directly under that row**, pushing the rest
of the list down by its own height, and while that strip is drawn its letters are the verbs
and the box is asleep — `y`/`n` in a question's own words, `a put it away`, `t new chat here`,
`o open folder`, `c copy path`, `p pause it` or `r resume it` on a standing item. `esc` or
`←` closes it, `enter` still opens the row, and walking off the row closes it too. On a row
with no verbs the arrows are the fold's, the way they are in the task column: on home's one
fold — `▸ 15 more, quiet since aug 21` — `enter` or `→` shows every row and `enter` or `←`
folds them back, and on a card `→` opens every folded band while `←` folds them again.
While something is typed the two arrows move the caret in the box instead.

**A letter always types**, unless the verb strip that names it is on screen — that visible
strip is the one state where a printable key is a verb, and it is why it has to be drawn.
Everywhere else "make me a site" comes out whole wherever the cursor is resting. The row's
actions otherwise ride chords, which can never begin a word: **`ctrl+e` puts the
conversation away** — it leaves the list, and typing its name is how you find it again, with
`ctrl+e` on the found row bringing it back. **`ctrl+t`** starts a new conversation in that row's
project (the browser's new-tab key — ctrl+n is the walk down), **`ctrl+o`** opens its
folder, **`ctrl+y`** copies its path. On a `◦` row of the `keeping an eye on` list,
**`ctrl+e` pauses** it, **`ctrl+x` stops it for good**, and **`ctrl+v` raises how hard that
item thinks** one rung. **With the cursor on no row at all** — one `↑` up off the top row,
where the card becomes the machine's own — **`ctrl+v` moves the machine-wide default**
instead, which is the `thinking` row in `/settings`. Each chord acts on the card you are looking at — the row
under your pointer when there is one, the cursor's row otherwise — and the card's own
dim legend names the verbs, and the strip names the letters. The other printable exception
is the digits on a waiting row's answer chips, which are drawn on the line above the box.

With the mouse: a click puts the cursor on a row and a second click on that row opens it;
a click on a `…13 more` line toggles it in one press. The wheel walks the list three rows a
turn, and the **tab bar above the list is a control** — clicking a place's word goes there,
and clicking a gap between two words does nothing.

**Under 60 columns those two clicks are one.** At phone width home is an inbox and a
row's card is a full-frame sheet, so a tap selects and opens in one gesture; the sheet's
top row reads `‹ back` and `esc` or a tap on it returns to the list with the cursor where
it was. The hint line becomes a bar of at most three wide targets — `open · new ·
ask here`, or `‹ back · open · more` on a sheet — and mouse motion is ignored, because
there is no hover on glass. Home's own page has the whole shape.

The box row reads `› say what you want done`, with the scope chip — `here ~/aforge-v2` —
against its right edge. The line under it is the foot, and **at rest it is exactly**
`type to search or start something new · ↑↓ pick · enter open · tab next place`: four keys
and no more. `esc` still closes home from anywhere; the resting foot does not spend a cell
naming it, and `alt+.` draws the whole map when you want it.

On any other row the foot says what THAT row's keys do and gains the two that are true
everywhere — `enter opens the place this happened in · alt+. map · tab next place · esc close`
on a `since you left` line, `enter or → show them · alt+. map · tab next place · esc close`
on the fold, and
`enter starts a new conversation and sends this · ctrl+enter ask here · ↑ pick a match · alt+. map · tab next place · esc clear`
on the action row — and
`enter runs this command · ctrl+enter ask here · ↑ pick a match · alt+. map · tab next place · esc clear`
on that same row when what is typed is a slash command, because `enter` runs it rather than
sending it (home's page has that rule whole).

**With nothing typed the list hangs from the top** and the cursor is on the conversation
this window is in; a card stands beside it only at 160 columns and wider. **While anything
is typed the list is a drop-up**: the action row — `start a new conversation: "…"` — is the LAST row of the list,
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

## The tab bar is a row the cursor can stand on — ↑ off the top row, and ←/→ along the words

**On every place, `↑` from the first row of the page lands the cursor on the tab bar** —
the row of seven words under the top line. The word you are standing in wears the cursor's
band there instead of its usual mark, and five keys mean something on that row:

| Chord | What it does while the cursor is on the bar |
| --- | --- |
| `←` / `→` | walk one word along, wrapping round from either end. **Nothing opens** |
| `enter` | go into the place under the cursor |
| `↓` | the same — go into the place under the cursor |
| `esc` | back into the page, on the row you walked up from. It does **not** close the place |
| `↑` | nothing. Above the bar is the top line, which is a reading rather than a control |

Everything else means exactly what it means everywhere else: `tab` and `shift+tab` are the
next and previous place, `alt+1` … `alt+7` jump, `alt+.` draws the map, and **any printable
key goes into the composer** — taking the cursor back down into the page with it, because
somebody who has started typing has stopped looking at the bar.

**`←` and `→` are not the row's keys up here.** On a row they open that row's verb strip
and its folds; the bar is not a row of any page's list, so both arrows are the walk along
the words and nothing else.

**The first `↓` back off the bar lands where you left.** `↑` onto the bar does not move the
page's own cursor, so walking up and straight back down costs nothing.

**And it works only where a bar is drawn.** Home on a phone-shaped frame and a task's record
card draw something else in those cells, so `↑` there is the walk it has always been — no
key does anything that is not on the screen.

The **places** page has the same thing with the pointer's half beside it: *How do I move
between the tabs with the arrow keys*.

## `b` on the spend place — the letter that opens the limits, and the money figure you can press

On the spend place (`/spend`, or `alt+5`) two things lead to the money limits, and neither
one is an editor on that page — the page answers *what did it cost*, and the Spending tab
of `/settings` is the one place *what may it spend* is set.

- **`enter` on the first line.** The page's top line is a dim pointer —
  `today $3.42 of $500 · /budget sets the limits` — and `enter` on it opens the Spending
  tab.
- **`→` then `b`.** `→` on any row of the page opens that row's verb strip, and this place's
  strip is one letter: `b the limits`. Pressing `b` while the strip is drawn opens the same
  tab. `esc` or `←` closes the strip, and walking off the row closes it too.

**`b` is a verb only while the strip naming it is on screen.** That is the rule everywhere
on this surface: a printable key belongs to the message box unless a drawn strip has
claimed it, so there is no bare `b` anywhere else that means "the limits". The door that
works from wherever you are standing is the command — **`/budget`**, also `/limits` — and
that is the door a refused turn names, because your refused message is still in the box
and every letter you type there goes into it.

The third door on the same subject is the **money segment of the status line**: press
`$0.14` and the Spending tab opens. It also brightens under the pointer, and takes the warm
ink once this conversation has spent four fifths of its own `per conversation` limit.

## Keys in the task roster and inside a room

**While the task roster holds the keyboard** (`ctrl+t`): `esc` gives the keyboard
back · `up`/`down` move · `right`/`left` open and fold · `enter` opens that row's room ·
`alt+w` widens the column and narrows it again. Its hint reads exactly
`↑↓ move · →← tree · enter open · alt+w wide · esc`. On a row whose work is still running or
still queued the hint gains one more clause before `esc` — `ctrl+v think harder`, which
moves that task's thinking rung; a finished row does not offer it, because a finished
task's rung is a fact about what happened.

**Widen is a chord and not the bare letter `w`.** It used to be `w`, and `w` was read
before the message box: a sentence typed while the roster still held the keyboard came out
as `riting the port` and `orktree`. Every bare letter on this surface is either a key on a
modal page with no message box, or an answer to a question drawn on screen, pressed over an
empty box — and widening a column is neither, so it took a chord. The bare `w` still works
on the **full-frame roster** (`ctrl+t` under about 100 columns, where the roster is drawn
over the whole frame and there is no message box on screen). The column's own footer says
`alt+w widen · click seam` or `alt+w narrow · click seam`, and dragging or clicking the
seam does the same thing with the pointer.

**`→` and `←` fold two things, and it is one gesture.** On a family's root row they open
and close the family. On a row whose **work has finished** they open and close that row's
own detail line — the merge word and price — which a finished row keeps folded so that the
column's height goes to work that is still moving. A job's log path is not on that fold:
it is on the job's page. `→` on anything else does nothing.

**The walk stops at this conversation's last job, after its last task.** The roster holds
this conversation's work, then the jobs section under it, so `↓` walks both and clamps at
the bottom rather than carrying on into the project's record. Old tasks from earlier
sessions are on the tasks place, reached from the column's own `ctrl+. earlier` line, from
`ctrl+.` or from `/history`; `enter` on an `earlier` row there goes inside that task's
card. In a directory whose earlier sessions ran tasks but where **this** conversation has
run none and started no jobs, `ctrl+t` falls through — there is nothing on the column to
put a cursor on. A session that has only started a server still has the jobs section, so
`ctrl+t` takes it.

**The column's other lines take no cursor.** Its `standing` section, and the two `+` rows
that close each of those sections (`+ /task`, `+ /standing`), are the pointer's — the walk
skips them. The `jobs` section does take the cursor: the label, then every job row the
column actually drew. Their keyboard equivalents for standing are the commands themselves:
`/standing` opens the standing orders page, and typing `/task ` is exactly what pressing
`+ /task` puts in the box. **No new key is added to the column by either of them.** `enter`
on the `jobs` label toggles the section; `enter` on a job row opens that job's page.

**Under 60 columns the roster page is a thumb's, not a keyboard's.** Its rows are two-line
cards a tap opens, its foot is a `‹ back` bar in place of the key legend
`enter open its room · type to filter`, and the strip that opens it is one full-width door
(`▸ 3 tasks · 1 running`) rather than a row of chips. Mouse motion is ignored — a tap opens
in one gesture. The tasks page describes the phone flow in full.

**`ctrl+g` closes the roster's column, and opens it again when it has no foreground
command to keep.** It works from the message
box, from inside a room, and while the roster holds the keyboard — it is the one key
here you do not have to ask for the roster first to use. Closing it hands the keyboard
back to the box. The choice is written to your profile as `ui.task_column`, so the next
session opens the way you left it, and `ctrl+t` counts as asking for the column back.

**A closed column leaves a two-column edge down the right of the frame with a `❮` in it,
drawn in ink, and clicking anywhere on that edge opens the column again. Clicking the
column's `❯` door while it stands closes it** — one chevron control, two states, so the
pointer can go both ways. With no foreground command to keep the line says
`❯ ctrl+g hide`; while a command owns `ctrl+g`, it says only `❯ hide`. The chevron
works in both states; the keyboard chord belongs to the command in the second one.
When no command can be kept, the key falls through and does nothing only when there is
no roster on the frame to close: a frame under 100 columns where nothing has raised the overlay. It works with
no tasks at all — the column stands with only its `+ /task` and `+ /standing` doors, with the
`ctrl+. earlier` door under them if earlier sessions ran anything, and either way an
empty column is still a column to close. On the untouched empty screen there is no
column yet; there `ctrl+g` is a first keystroke like any other — the greeting goes and
the key then closes the column it would just have raised, so a second press brings it
back (*The empty screen* page).

**With a room open:** `esc` leaves the room, though a history recall walk is
cancelled first · `enter` steers the node (see *What steering a task looks like on its
page* below) · `ctrl+b` freezes the room's own rows for
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

**What steering a task looks like on its page.** Your line is drawn where you said it,
under whatever the task had already done, as an elbow:

```
└ the config lives under etc/ · delivered
```

The `└ ` says it is a correction to the work already running and not a new question, so
the page's turn count does not move — a task's page is one question, the instruction it
was given, with your corrections hanging off it. The clause after your words is what the
sending did, and it is there for a few seconds and then gone: `· delivered` ordinarily,
and `· it was waiting on its pieces — your line wakes it` when the task had handed its
work out and was parked on the reports, because then your line is what starts it moving
again. The elbow itself stays.

**A task's page reopened later reads your corrections as corrections.** Leave the room
and come back, or open the task tomorrow, and every line you steered into it comes back
as a `└ ` elbow in the place you said it — with no clause, because what happened to those
words is news and the elbow's position is the whole of the record. They used to come back
as fresh questions with a `›`, which made yesterday's correction read as a second
instruction and made the page count turns nobody opened.

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
question, and four letters answer it. **Any landing that needs a look, at any depth** — a
task you asked for, or a part of one it handed out itself:

| Key | What it does |
| --- | --- |
| `a` | accept — take the work; its branch merges and its dependents unblock |
| `l` | look again — a fresh check runs; the task keeps waiting until that answers |
| `n` | not right — the task becomes incomplete and keeps its branch; its dependents still fail because it did not finish |
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
at the foot of the page where `this task has finished — say it to main` would otherwise be,
and the hint slot reads `a accept · l look again · n not right` while the question stands.
The room and the card are one question: answer in either and both show the receipt.

**And the roster's row answers them too.** With the roster holding the keyboard (`ctrl+t`)
and the cursor on a row that **needs your look**, the hint slot reads
`a accept · l look again · n not right · esc` in place of the move keys, and those three
letters answer that row's landing without opening its room. Same card, same answers, same
receipt — the column, the card and the room cannot disagree, because there is one card
behind all three. `d` works there too and is left off the hint for the room's own reason:
it is a preference and not an answer to the question in front of you.

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
4. The row above the message box: an **attachment chip** or a picked harness's chip
   removes it, and the **thinking chip** at the right end of that row opens the five-rung
   ladder (pressing it again closes it). Neither moves the caret in your draft.
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
   a press anywhere on it opens the column again. With no foreground command to keep,
   `ctrl+g` does that too; while a command can be kept, the key backgrounds that command.
   Within the column, its own lines are asked before its task rows: a `+ /task` or
   `+ /standing` row types that command into your message box, and a row in the
   `standing` section opens `/standing` with the cursor already on that order.
9. Three segments of the status row: `◦ keeping an eye on N`, which opens the standing
   orders page; the model name, which opens the model picker; and the **money figure**
   (`$0.14`), which opens the **Spending** tab of `/settings`. Each brightens under the
   pointer over its own cells to say it is a door. A press elsewhere on
   the status row falls through — the rest of it is figures, not controls. On a narrow
   terminal the whole two-row deck answers.
10. A message of yours **waiting** for the answer to finish, in the block above the
   box — a click anywhere along its line takes that message back into the box to be
   edited, and the block loses it. The whole line answers, because nothing shares it.
   On the dim line under it, only the words `→ steers it in` answer; a press on the
   rest does nothing.
11. The body: an inline **task link** inside prose, which is the one mouse-only target
   on the surface; a cut markdown table's foot; a waiting sign-in, where a click
   copies its link; a thinking block, clickable over its whole height; a tool row,
   which opens its expansion, except that hovering a running foreground command reveals
   `click to background` in its right-hand slot and only those words keep that command;
   or the full-frame sheet on a narrow terminal; the
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

## Selecting text with your mouse — drag to copy, select a word with the mouse, why did copying take the whole line instead of the words I dragged over

**Just drag.** Put the pointer on a character, sweep to another with the left button
down, and the cells between them highlight — from where you pressed to the end of
that line, every line between in full, and the last line up to where you are, exactly
as your terminal would select it. The moment you release, that text is **on your
clipboard** — stripped of colours and the drawn left rails, exactly as copy mode
strips a yank. There is nothing further to press: no ctrl+c, no key at all —
releasing the button IS the copy. The highlight stays lit for the few seconds the
status line says what landed — `copied · 14 chars` for a span inside one line,
`copied · 3 lines` across several — so you can see exactly what you got. The write
goes over OSC 52, so it works over ssh and through tmux. What is highlighted is
exactly what is copied, to the character.

**Double-click takes the word, triple-click takes the line.** A word is what a
person means by one: a path, a hash, a flag, `go.mod` and a URL are each one word, a
bracket or a quote ends one, and the full stop closing a sentence is left behind.
The status line says `copied · 1 word` or `copied · 1 line`. Two quick clicks on a
tool call or a fold are still two clicks — open, then shut — because a button acts on
every click; only text is taken by a double-click.

**Copying took the whole line instead of the words you dragged over?** Two things do that,
and neither is a fault. A **third** click in quick succession takes the whole row — that is
what triple-click is, and a double-click with a stray third click becomes one.
And a sweep that **leaves the row it started on** is a stream, not a box: it takes your
anchor to the end of that row, every row between in full, and the last row up to the
pointer, exactly as your terminal would. To take a few words and nothing else, sweep
sideways and stay on the one row — the status line then says `copied · 14 chars` rather
than `copied · 2 lines`, which is how you tell the two apart.

**A wide glyph is never split.** A sweep that starts or ends inside a CJK character
or an emoji takes the whole glyph, and the highlight covers both of its cells.

The sweep is drawn in **the same background copy mode's selection wears** — the strongest
of the three this screen draws, a shade above the one under the pointer. It is the same
claim ("these rows are what a copy would take"), so it is the same paint; it used to be
drawn at the pointer's own quieter step, which said a sweep in progress was a shadow
rather than a selection.

**The selection covers every cell it takes — a code span or a chip included.** An inline
code span wears a background of its own (the raised plane that makes it read as code),
and so does a chip, but under a sweep they are part of what is being claimed: the
highlight paints over them, in the sweep's colour, their text keeping its own colour
and weight. Every terminal's own selection replaces the background it sweeps over, and
this one reads the same. The clipboard is unchanged — what is highlighted is still
exactly what is copied — so a sweep across a row with `code spans` in it copies the
row you saw lit, to the character.

**The selection sticks to the text, not to the screen.** A task's page streams and
follows its live edge, so rows can scroll while your button is down; the highlight
rides the rows you swept, the copy is those rows wherever they moved to, and a click
opens the row you actually pressed even when it has shifted. A row that scrolls
clean off the screen before you release is no click at all.

A click is unchanged in feel: press and release in place and it lands where it always
did. (Under the hood the body's click now fires on release, the way every button in
every GUI does, which is what lets a drag never trigger the thing it started on — a
sweep that begins on a thinking block does not collapse it.)

**A click is allowed to wobble.** A hand is not a vice, so a press that drifts up to
**two columns sideways or one row up or down** before you let go is still a click, and
it lands on the row you pressed rather than the row you drifted onto. Past that it is a
sweep. The cost of the tolerance is one gesture: you cannot select exactly two adjacent
rows by dragging down exactly one row — sweep past them and come back, or sweep sideways
within a single row to select that one row.

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

## Clicking does not seem to do anything — what to check when the mouse is dead

Five things stop a click, and only one of them is a setting.

**You pressed `ctrl+s`.** That hands the pointer to your terminal so you can drag-select,
and it is a **toggle** — it is the one key excepted from the automatic handback, so if you
press it and then only touch the mouse, nothing aforge draws will answer a click until you
press a key or press `ctrl+s` again. While the pointer is out, the row under the message
box reads exactly `drag to select · any key ends it`. That line is how you tell this apart
from everything else here.

**The `ui.mouse` setting is off.** It is **on** by default — `/settings`, the Display
section, the row labelled `mouse`. With it off, aforge never asks your terminal to report
the pointer at all: no hover, no click, no wheel, and your terminal keeps drag-select
permanently. `/select` then says `your terminal already has the pointer — drag to select.`

**Your click drifted.** A press that moves more than two columns or more than one row
before you release is a **sweep**, not a click: it copies the rows it crossed and the
status line says what was copied instead of opening anything. See "selecting text with
your mouse" above.

**There is nothing under the pointer.** A click on empty space does nothing anywhere on
this surface, including the gap between two words of the tab bar and the blank rows of a
task's page. A click in copy mode acts on nothing at all, because those rows are a frozen
snapshot.

**The terminal is too narrow for the word you are aiming at.** The tab bar gives up words
as the frame narrows, and at its narrowest it carries only the place you are standing in —
so on a narrow window there is no other place-word on screen to click. `tab`, `shift+tab`
and `alt+1`…`alt+7` still go everywhere.

**A file path is your terminal's click, not aforge's** — usually **cmd+click**
(ctrl+click on Linux). If a plain click on a path does nothing, that is why.

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

Three chords carry unrelated meanings. Which one you get depends on where you are.
A fourth, `ctrl+v`, carries **one** meaning on several surfaces — move the thinking rung of
the thing you are standing on — and its own section below has the table.

**`ctrl+.` — two meanings, and the two screens can never both be up:**

| Where you are | What it does |
| --- | --- |
| in a conversation | every task this project has run — the same list `/history` opens |
| on a place | draws the key map, exactly as `alt+.` (`⌥.`) does — **only** on terminals that report they can send `ctrl+<digit>` |

A place takes the whole frame, so while one is standing the conversation's keys are not
under it at all. Where your terminal has not reported that it can send `ctrl+.`, the place
reading simply does not exist and the chord does nothing there.

**`ctrl+t` — two meanings:**

| Where | What it does |
|---|---|
| Message box | Give the keyboard to the task roster. Press again or `esc` to take it back |
| Model picker only | Cycle the reasoning effort |

**`ctrl+o` — five meanings:**

| Where | What it does |
|---|---|
| A landed card is selected | Open its output |
| A proposal is selected | Open its brief |
| Phone tool detail sheet | Lift the line cap |
| Inside a task's page, with a long instruction at the top | Open the rest of it, and press again to fold it back |
| Nothing selected | Fold or unfold this turn's tool cluster |

Inside a task's page the first thing `ctrl+o` reaches is the **instruction** the task was
given, where that instruction is longer than three lines: the page shows the first three
and one dim line reading `▸ …14 more lines · ctrl+o`, and the chord opens the whole of it.
Pressing it again folds it back, and the line then reads `▾ …14 fewer · ctrl+o`. A short
instruction has no such line and the chord falls through to the fold below. See *tasks*,
"The long brief at the top of a task's page".

`ctrl+o` also folds and unfolds a task page's own tool cluster where there is one, and
scrolling up at the top of the page opens that fold as well; the fold line there reads
`N earlier tool calls · scroll up or ctrl+o`.

`ctrl+o` is a chord, so it never costs you a character: you can press it with half a
sentence in the box and go on typing into the same words.

Two more chords surprise people:

- **`ctrl+b` is copy mode, not emacs "left".** The alternate screen took your
  terminal's selection away, and copy mode is what buys it back.
- **`ctrl+e` means two things** depending on whether the box is empty: end of line
  when there is text, open the most recent thinking block when there is not.
- **`ctrl+w` means two things**, and never on the same screen: in the message box it
  deletes the word behind the caret; while the **switcher** is up it closes the
  conversation under the cursor. The card has taken the whole keyboard by then, and there
  is no caret on it to delete a word behind.
- **`ctrl+k` means two things**, and never on the same screen: it opens the switcher, and
  inside a harness design's room, while its approval row is up, it saves the design. That
  row is modal and takes the key first.

And over an **empty** box, `left` and `right` are navigation rather than caret
movement. `ctrl+f` never is — it always moves the caret right.

## ctrl+v — how hard the thing you are looking at thinks, and making this one task think harder

`ctrl+v` moves one step up the thinking ladder — `low`, `medium`, `high`, `xhigh`, `max` —
and it moves the rung of **the thing you are standing on**. One chord, three scopes:

| Where you are | What moves |
|---|---|
| The message box, typing or empty | **This conversation's** rung — the chip above the box, see *The thinking chip above the message box* |
| The task roster holds the keyboard (`ctrl+t`) and the cursor is on a task | That task's rung |
| You are inside a task's page | That task's rung |
| Home, with the cursor on a `◦` standing item row or its card | That item's rung |

Everywhere else it does nothing at all. A conversation row on home is deliberately not on
the list: a conversation's rung belongs to the window that conversation is open in, where
the chip above its message box moves it.

**The machine's own default is not one of the scopes.** It used to be — home had a state
where the cursor stood on no row at all and the right-hand side became a card about the
machine, and this chord moved the install's rung from there. `↑` off the top of home's list
reaches the **tab bar** now, so that card is gone. To change how hard this machine thinks by
default, open `/settings` and walk to the **`thinking`** row, which is the setting both
roads always wrote.

**It climbs and it wraps.** Each press goes one rung up, and `max` wraps back to `low`. It
never returns to "nobody said" — clearing a rung hands the work back to whatever stands
over it, which is a decision rather than something a wheel does on its way past. Set a
thing back to nothing in the place it is written down: the `thinking` row's own `off`.

**The rung reads as a quiet clause where the thing already states its facts.** A task's is
on its page's header, after the model — `◆ Fix nil-map · running · 42s · $0.31 · gpt-5 ·
thinking high` — and on the roster's own figures row when the column is wide enough to hold
it. An item's is on that item's card. The machine's own is the `thinking` row of
`/settings`. A thing nobody has dialled says nothing, which is not the same as `low`.

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
| `cmd+enter` | **Bound**, in one state: while a turn is running with something typed, it holds that message above the box for the next turn. It does **not** open a new line. Over an empty box, or with nothing running, it does nothing. Needs a terminal that can spell it |
| `ctrl+d` | Not bound |
| `ctrl+k` | **The switcher**: the card of every conversation this terminal has open. Inside a harness design's room, while its approval row is up, it saves the design instead — that row is modal and takes the key first |
| `ctrl+r` | Bound. In the message box it is **spell it out** — see "Make my prompt better" above — and in the `/files` list it opens the folder a file is in. Nowhere else |
| `ctrl+v` | **Bound**, on three surfaces: it moves how hard the thing you are standing on thinks — this conversation from the message box, a task, or a standing item on home. The machine's own default is the `thinking` row of `/settings` and is not on this chord. See "The thinking chip above the message box" and "ctrl+v — how hard the thing you are looking at thinks". Anywhere else it does nothing. It is **not** paste: most terminals spend `ctrl+v` (or `cmd+v`) on pasting before aforge ever sees it, and a paste arrives as bracketed text rather than as this chord. Where your terminal does hand the chord over, it dials thinking |
| `ctrl+x` | Bound in the same one place: it drops a harness design from inside its room. Not bound anywhere else |
| `ctrl+y`, `ctrl+z` | Not bound |
| `ctrl+<digit>` | **Bound as a second spelling of the place keys, on the terminals that report they can send it.** `ctrl` and a digit has no encoding in the scheme most terminals speak — which is why `alt+1` … `alt+7` (`⌥1` … `⌥7` on a Mac) are the first spelling and always will be — but a terminal running the kitty keyboard protocol sends it and says so, and where that report arrives `ctrl+1` … `ctrl+7` reach the same seven places. The map's line says `alt+1…7 or ctrl+1…7 go to a place` exactly when the alias is live. Where the terminal has said nothing, the chord does nothing and is never drawn |
| `ctrl+.` | Two meanings, on two screens that cannot both be up. In a conversation it is every task this project has run (`/history`); while a place is standing it draws the key map, on the terminals that can send `ctrl+<digit>` |
| `alt+<letter>` | Bound **only where a place says so, and only on that place**. `alt+s` changes the shelf on the memory place; `alt+b` and `alt+f` are the word jumps inside every box and are never taken by a place. Every other `alt+<letter>` does nothing |
| `shift+←` `shift+→` `shift+↑` `shift+↓` | The **time window** of a place that has one: `shift+←→` moves it by its own length, `shift+↑↓` changes how coarse it is. Three places have one — tasks (when it ran), standing (when it fired) and spend (which days) — and each draws the same control on its head row, `shift+← aug 12 – aug 25 →` with `shift+↑ coarser` beside it. Anywhere else, on a terminal too narrow to draw the control, and (for the zoom alone) on a line with no room for its clause, they do nothing |
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
file is named in the call's own result, and on the job's page — not on the row.

**The key is absent whenever it cannot work**, and absent means it does nothing
as a background gesture rather than telling you it cannot. The same absence rule
governs the row's pointer offer:

- nothing is running
- the running call is not `bash` — a `read` or a `write` has no process to hand over
- the running `bash` asked for the background already, so it was a job from the start
- the row has already been sent away
- the call is being replayed or is still forming
- this session is over `--host`, where there is no door for the local surface to hand
  that process over

When more than one command is running at once, `ctrl+g` takes **the one that has
been running longest**, which is the one you are waiting on.

While a command can be kept, this meaning takes precedence over hiding or restoring the
task column, so the column stays where it was. With no such command the key belongs to
the column as described above. `ctrl+b` is copy mode and `esc` interrupts; neither changes.

## When a settings change lands

The `ui.mouse` and `ui.timestamps` settings are read at boot and again at the end of
every turn — not the moment you change them. The same is true of the approval posture
and the consent countdown.

So if you turn the mouse off, or turn timestamps on, mid-turn, the change takes hold
**one turn later**. Nothing is wrong; the surface has not re-read the setting yet.

This holds however the row was changed — in the panel, or by asking aforge to do it with
`change_setting`. The row is written straight away and the transcript says so; the screen
picks it up when the turn ends.

The **background after** row is different. Its key is
`bash.background_after_seconds`, and the session engine and the visible countdown arm
from it together at launch. A change lands on the **next session**, not at the end of the
current turn. Because this row controls how hard the machine may be worked,
`change_setting` refuses it; open `/settings`, choose the Safety tab, and change the row
yourself.

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

## Why did aforge add a [silent] note while tools were running?

The chat loop watches for a model that keeps calling tools without putting any visible
words between the calls. After **6 consecutive tool-using replies with no visible assistant
text**, it adds a note beginning `[silent]`. If the silence continues, a stronger note
arrives at **12**, and that one is the last — one silent run is mentioned twice and no more.
Each note asks the model to write what it has learned, what it will check next, and why
before making another call.

**A `[silent]` note is not an accusation of being stuck.** It is about the record rather
than the work: it says the reasoning between steps is being lost. It spends none of the
loop guard's warning limit, and no number of `[silent]` notes will end a turn.

**The first note is advice; the second says the tools are about to be held**, and the
section below says what that means.

That request matters for reasoning models because their streamed thinking is shown on the
screen but is not put into the next request. Of the model's prose, only visible assistant
text becomes part of the conversation the following step can read.

Three things reset the count — and with it the hold, if one was on: a visible note, a
successful `edit` or `write`, and a successful shell command that left the working folder
different from how it found it. That last one is why a commit-and-push run is not scolded
for being quiet — landing work is work, whatever verb it is spelled with. Each rung is
issued once in one silent stretch; after a reset, a later silent stretch begins again at 6.

## Why did aforge stop running tool calls, and what is a [held] answer?

Because the note it asked for twice was never written. The `[silent]` note at 6 replies is
advice. The one at 12 is the last, and it says outright `from here your tool calls are
held`. From that point the loop stops running a reply that carries **only** tool calls: each
call is answered with `[held] Nothing was run this step…` in place of its result, nothing
reaches your files or your shell, and the answer says what to write and that the next call
runs as soon as it is there.

**A rule the loop can enforce is not a suggestion.** The reasoning between steps is not
saved anywhere — a task's room, its checker, its parent and you all read what was written
down — so past a certain amount of silence aforge stops asking and starts holding.

**Writing anything visible clears it immediately** and the loop is back to normal, at the
first rung again. A reply that carries both a note and tool calls was never held in the
first place, so a model that writes as it works never sees any of this.

**After 3 held replies the turn stops.** If it keeps sending only tool calls, the turn ends
with this line rather than arguing forever:

`stopped here · would not write its notes down, so what this turn worked out is not on the record`

Nothing is handed to a task — the same model under the same rule would be as quiet — and
whatever the turn had already saved is on disk where it left it.

**When the turn that stopped belongs to a task worker, the task says so.** Its row on the
rail reads `would not write its notes down — branch kept`, it wears the `!` that asks you to
pick it up rather than the cross that reports a fault, and the branch is kept. How tasks run
has it under "What the words and the ! exclamation mark under a stopped task mean".

The loop also notices command variants that keep returning information already seen. After
**5 consecutive tool rounds in which every result contains no fresh line**, a `[stuck]`
note says: `the last 5 rounds read nothing new; what you are looking for is already in the
transcript`. A fresh line, a successful write, or a command that changed the working folder
resets that count.

## When does a loop actually end the turn — the [stuck] warning limit

Four signals say the turn is not moving: **the same call three times in a row**, **the same
failure three times**, **the same argument refused twice**, and **five rounds that read
nothing new**. They share one warning limit. After two `[stuck]` notes, a third such signal
ends the turn instead of adding a third ineffective note.

In an interactive conversation, aforge uses the same checkpoint hand-off as any other
overlong turn and moves the remains to a watched task. If that hand-off cannot be made —
for example inside a task or without a consent surface — it ends the turn with
`this turn is going in circles · stopping here with anything remaining left undone`.

Two things soften that limit. **`[silent]` notes spend none of it**, so a quiet turn cannot
be ended for being quiet; being held for not writing its notes down is a separate road with
its own ending, described in the section above. And **getting something done gives one
spent note back**: a batch
that wrote a file, or ran a shell command that changed the working folder, steps the count
down by one — unless it was the very call the turn has already been warned about, because
writing the same file seven times is the loop and not the way out of it. It is a step down
and not a wipe: a turn that keeps looping still reaches the limit, it just takes longer to
get there.

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
