# Keys, typing, and the mouse — what all the keys do

## Which key sends, and which key opens a new line

`enter` sends the message you have typed.

`shift+enter` opens a new line without sending in home's message box, its ask-here
pane, and conversations, including task rooms and while an answer is running.
In conversations, `alt+enter` and `ctrl+j` also open a line. On home, `alt+enter`
keeps its task-composer action.

On home, even a blank line hides the placeholder and moves the cursor onto the
new line. Left/right move through the draft; up/down move between its lines,
then return to list navigation at the top or bottom. Deleting the entire draft
brings the placeholder back.

Shift+enter requires a terminal that distinguishes it from plain enter. If your
terminal sends plain enter instead, it has the ordinary send behavior; use
`alt+enter` or `ctrl+j` in a conversation as a fallback.

`ctrl+shift+enter` while a turn is running **stops the answer and sends what you
have typed** — see "Interrupt and say something new in one key" below. At rest it
does nothing.

`cmd+enter` while a turn is running **holds** what you have typed for the answer
after this one. It is the secondary choice for when you do not want to change the
work already under way. At rest it does nothing at all. **On Linux and Windows that key
is called `super+enter`** — see "What cmd+enter is called on your keyboard" below.

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
a dim row above the box reads `  after yield · N`. If queueing fails, codeaf notes
`follow-up failed: <error>`.

## Typing while an answer is still coming — interrupting and steering

Typing is never blocked. The box works normally while an answer streams.

`enter` while a turn is running steers. Your words appear at once as a line of your
own in the transcript. A short dim clause beneath says where they landed, such as
`stopped the reply here`, `kept running as job 3`, `took this instead of the question`,
or `waiting for the running step`.

If text or reasoning is streaming, codeaf cancels that one model request, keeps the
partial answer it actually received, and continues the **same turn** with your words
as the next user message. An incomplete tool call is dropped because a provider
cannot accept a tool call with no result.

If a short tool is running, codeaf lets it finish and lands your words at that
boundary. If a bash command has already been running for 3 seconds, codeaf keeps it
alive as a job and lands your steer immediately. The clause names the job and
`jobs output N` shows its output. A command that is still YOUNGER than 3 seconds when
you steer is given those few seconds to finish on its own; if it is still running when
they are up, it is kept alive as a job then. So **a bash command holds your correction
for about three seconds at most** — never for its own ending and never for the
background-after clock. That is the bound on the handoff, not on the answer: the step
may hold other tools as well, and the model's reply then takes as long as the model
takes.

The exact words `stop`, `kill it`, `cancel`, `abort`, `ctrl-c` and their tiny variants
stop the command instead, **at any age** — a stop is not asked to wait out those three
seconds, because they exist to let a short command finish and that is the one thing you
have just said you do not want. If you then type something else, the model reads both
lines in the order you sent them; the stopped command is not restarted by the second
one.

## Enter, cmd+enter, ctrl+shift+enter, and the waiting-message keys

| What you do | What happens |
|---|---|
| `enter` | stops the current generation and sends the words into this turn |
| `cmd+enter` | holds the message for an ordinary turn after this answer |
| `esc` | stops the answer and clears both waiting-message queues |
| `→` over an empty box | steers the oldest waiting words into the running answer |
| click `→ steers it in` | the same, with the pointer |
| `ctrl+shift+enter` instead of `enter` | stops the answer and sends the sentence in one key — see below |
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
The current generation is itself made into a legal boundary: codeaf keeps its partial
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
could land, `took this instead of the question` when a question was standing and your
sentence answered it instead, and `waiting for the running step` when a short tool is
being allowed to finish first. The clause goes when the model is actually given the words; the position of
the line is what says where they went from then on.

This is the key for the moment you are watching an answer go the wrong way and you do not
want to pay for stopping it. The three keys, side by side:

| Key | What happens to the answer | What happens to your sentence |
|---|---|---|
| `enter` | current generation stops; partial kept | goes into the same turn now |
| `cmd+enter` | keeps going | waits above the box until the answer finishes |
| `ctrl+shift+enter` | stopped, and what it said is kept | opens the next turn |

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
enter steers it in · ctrl+shift+enter stops and sends · esc interrupt
```

That is the terminal-capable form when no command can be kept. A running foreground
command adds `ctrl+g backgrounds` immediately before `esc interrupt`; a terminal that
cannot deliver `ctrl+shift+enter` leaves that clause out. `cmd+enter` still waits, but the
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

On those terminals codeaf never advertises the chord. The line under the box still
begins `enter steers it in`, because plain enter works everywhere, and ends with the
stop clause that works in the current state.

**What works everywhere instead.** `ctrl+q` queues a new turn after the current one.

## What cmd+enter is called on your keyboard — super+enter off a Mac

It is one keystroke with two names, and codeaf spells it for the keyboard you are on.

| Where you are | What `/help` and this page's key sheet draw | What the terminal sends |
| --- | --- | --- |
| macOS | `⌘enter` | `super+enter` or `meta+enter` |
| Linux, Windows, WSL | `super+enter` | `super+enter` or `meta+enter` |

`super` is the key with the diamond, the Windows logo, or the command symbol on it,
depending on whose keyboard it is. codeaf binds **both** names the terminals send, because
which one arrives is a fact about the road the bytes took rather than about your hand.

This page writes it `cmd+enter` throughout, which is the name it was chosen under; the key
sheet substitutes the spelling for your platform at the moment it draws, exactly as it
does for `alt+`/`opt`.

## ctrl+r means two things, and each says where it acts

`ctrl+r` appears twice on the `/help` sheet, and the two are not the same key doing two
jobs at random — each is scoped, and each row says its scope:

- `ctrl+r` **over a draft** spells the draft out: codeaf says back what it takes the
  sentence to mean, and `enter` adds that to what you are saying. It is offered only while
  the hint slot says `ctrl+r spell it out`, and it does nothing at all otherwise.
- `ctrl+r` **in `/files`** reveals the folder a landed file is in, with `ctrl+y` beside it
  to copy the file somewhere.
- `ctrl+r` **in `/model`** fetches the newest model list; the picker's placeholder says
  `ctrl+r refresh`. It is not on the `/help` sheet — the placeholder is where it is named.

`/files` is a full-screen list with its own keyboard, and the model picker takes every key
while it is open, so none of them contend for one keystroke on one screen.

## ? — the key that opens the key sheet

`?` **over an empty box** opens `/help`: every command and every chord, written into the
transcript where you can scroll it. A row that is wider than the window wraps under the
column its sentence already starts in, so the next line does not sit at the left edge as
if it were another key.

**On a place** — home, tasks, standing, memory, spend, search, settings — `?` draws **the
map** instead, which is what `alt+.` draws: that place's own keys, in the cells the foot
was already using. One meaning, two screens: show me the keys for where I am standing.

**It never eats a question mark you are typing.** The binding wants the box EMPTY. With
anything at all in the draft the key is an ordinary `?` and goes into your sentence; and
every overlay, filter box, picker, panel and question on this surface takes the keyboard
before this binding is read, so a `?` typed into one of those reaches it and nothing else.

`/?` is an alias of `/help` as well, for fingers that arrived from elsewhere.

## Why a wrapped /help line stays under its key

`/help` is a column. The key sits on the left and the sentence starts in a fixed
column. When the window is too narrow for the sentence, the next line starts in
that same column. It does not start at the left edge, which would look like a
second key. The same is true of a line that was already indented under the
column above it.

## Stop it and tell it something different at the same time — interrupt and say something new in one key

`ctrl+shift+enter` while a turn is running **stops the answer and sends what is in the box**,
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
a slash command is something you said to codeaf rather than to the model, so there is
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

**Terminals that cannot send it.** `ctrl+shift+enter` reaches a program only where the terminal
can tell it apart from a plain `enter` — the kitty keyboard protocol, xterm's
modifyOtherKeys, or win32-input. Where it cannot, the key arrives as an ordinary `enter`
and your message **steers** instead. On those terminals codeaf never advertises the
chord. Use `esc` to stop the whole turn, then send the next message normally.

**The line that teaches it.** While a turn is running and you have typed something, the
right end of the row under the message box reads exactly:

```
enter steers it in · esc interrupt
```

On a terminal that can spell the secondary chords, the line reads
`enter steers it in · ctrl+shift+enter stops and sends · esc interrupt`. A foreground
command that can be kept inserts `ctrl+g backgrounds` before the final stop clause.

**A picture on the tray is a message even when the box has no words.** It cannot steer, so
that form reads `enter waits · esc interrupt`, or
`enter waits · ctrl+shift+enter stops and sends · esc interrupt` on a terminal that can
spell the secondary chord. With neither words nor a picture, the line is simply
`esc interrupt`, unless a command can be kept, when it is
`ctrl+g backgrounds · esc interrupt`.

## I typed while it was working — did my message get lost?

No. Every road ends in an answer.

**If you pressed `enter`**, codeaf stops the current model request, keeps its partial
reply, draws your words immediately, and continues the same turn from them.

**If you pressed `cmd+enter`**, the message is held above the box for the next turn.
It is still yours: edit it with `↑`, click it, or let it go when the answer finishes.
Pressing `→` over that waiting message steers it in instead.

**If there was no step left** — the turn's last request had already gone out, or it
finished a moment later — your words are not dropped and not pretended about. They become
an ordinary message waiting for a turn of its own, the screen says so, and codeaf answers
them in the next turn. You see your line, a pause, and then a reply. You do not have to
type it again.

The same is true of a task or a background job that finishes in that window: its
note lands and codeaf speaks about it rather than leaving it sitting there.

The one thing that is not answered is a message you queued with `ctrl+q` for a
turn you then **interrupted**. A drain never restarts a turn you stopped, so those
are dropped — press `enter` again to send it.

## Leaving for Home with a waiting message

A message waiting above the box stays with its conversation when you open Home with
`space` `space`, `/home`, or `alt+1`, and still sends when that answer ends. Its pictures,
pasted documents and standing mark stay with it. A connection that holds one conversation
at a time returns the waiting words and pictures to the box and tray when switching ends
the old conversation. `esc` stops the answer and drops waiting messages.
From Home, `alt+3` (`chats` on the bar) goes straight back to the conversation you
were in, and `alt+k` chooses one.

## Interrupting a running turn — how do I stop it mid answer

Press `esc` or `ctrl+c`. While a turn is running, both do the same thing: the turn
is stopped and everything it already said is kept.

What happens:

1. The session is told to stop, and a note `stopped` is added to the conversation.
2. The screen stops on the key: every spinner goes, and every call that was running keeps
   the time it ran until you stopped it.
3. Any queued follow-ups are dropped, and codeaf says so — `1 queued message
   dropped`, or `N queued messages dropped`.
4. The status word becomes `stopping`, then `interrupted`, and `interrupted` stays as the
   status word until the next turn starts.
5. If a message of yours was **waiting** for that answer, it is *not* dropped: it sends
   immediately as the next turn. That is the whole difference `esc` makes while
   something is waiting.

**The words codeaf uses for one stop.** They are five slots and one key press, so they
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
`enter steers it in · ctrl+shift+enter stops and sends · esc interrupt` while you have
typed something and this terminal can deliver `ctrl+shift+enter`. A foreground command that
can be kept inserts `ctrl+g backgrounds` immediately before the stop clause. When a
message of yours is already waiting for the answer to finish, the last clause becomes
`esc stops and drops`. On the very first frame of a session the conversation carries the note
`esc interrupts · ctrl+c quits · ? for help`.

**Stopping it and saying something new at once.** `ctrl+shift+enter` does both in one key —
see "Interrupt and say something new in one key" above. `esc` on its own stops without
sending anything you have not already committed with `enter`.

**Limits.** Interrupting does nothing at all when no turn is running. `esc` reaches
the interrupt last: a history recall is cancelled first, rewind is armed on the way
past, and any open list or overlay takes the key before the message box sees it. So
`esc` while the command list or the `@` list is open closes that list and does
**not** interrupt.

**Mid-turn `ctrl+c` only ever interrupts — that press never leaves.** It is spent on
the model. The NEXT press is read at rest, and at rest `ctrl+c` is the way out — so the
two-tap people make mid-turn, press it again harder because the first did not seem to
land, stops the answer and then quits. Nothing you typed is lost when it does: the box
and anything waiting for an answer are written to disk on the way out. See "Quitting
codeaf — how do I exit, close it, or why did ctrl+c not quit" below.

## Esc is not stopping it — how long does a stop take, why the turn is still finishing, how long stopping takes, and what happens if it will not stop or will not let go

**I pressed escape and it is still running.** That is this section: escape is not being
ignored, the turn is being let go of, and if it will not let go codeaf ends it for you
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
second `esc` inside half a second is the rewind's door and `ctrl+c` at rest is the way
out, so neither is free — and you do not need one. The `esc` you already pressed started the
10-second window, and when it runs out codeaf stops waiting on its own.

**What happens at 10 seconds.** codeaf detaches from the turn: the waits codeaf holds are
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
is already on your spending page either way: codeaf counts money as each request is
answered rather than when a turn ends.

**You should almost never see this.** Ten seconds is well above the longest ordinary
letting-go, so the deadline only fires on a turn that was genuinely not going to end. If
something is wedged even further down, `ctrl+c` at rest still quits and takes the whole
process with it — but you no longer have to reach for that just to get your prompt back.

## Quitting codeaf — how do I exit codeaf, how do I close codeaf, or why did ctrl+c not quit

To exit codeaf, press `ctrl+c` once. That is the whole gesture: there is no second
press to make, no window to beat, and nothing asking you to confirm it.

**`ctrl+c`, once.** With nothing running, the press that lands is the way out: codeaf
writes your draft to disk and exits.

**If `ctrl+c` did not quit, a turn was running.** Mid-turn that key is the interrupt —
the same thing `esc` does — and the press is spent on the model. Press it again once the
answer has stopped and codeaf leaves.

**Nothing you typed is lost when you exit.** The unsent sentence in the box goes to disk,
with any message that was still waiting for an answer folded in underneath it, and the
next launch puts them back in the box. See "What quitting saves and closes" below.

**What is running is NOT named first.** codeaf used to arm the door on a first press and
spend a second and a half telling you how many conversations were open and what leaving
would stop. That warning went with the second press. Two things still hold without it: a
conversation this machine's codeaf service is running **keeps working** after the window
closes — its tasks, its questions and its journal are all there when you open the same
workspace again — and a conversation running inside this terminal (`--no-host`, or a host
that could not be reached) stops with it. To see what is running before you go, the task
column (`alt+l`) and `/status` both say.

**Closing one tab still asks.** `ctrl+w` on a conversation with work running raises a card
that names that work — `a task and a job running` — and waits for an answer. Leaving the
program is the gesture that does not ask; closing one conversation out of several is the
one that does.

## Quitting from a running answer, picker, or panel — why ctrl+c stopped the turn instead

The quit gesture is one `ctrl+c`, but where the press lands changes what it does.

**Mid-turn it is only the interrupt.** While an answer is streaming, `ctrl+c` is the same
key `esc` is: it stops the turn, and that press does not leave. The next one, at rest,
does — so the two-tap people make mid-turn stops the model once and then quits.

**It works over everything.** `ctrl+c` is read above every picker, panel, room, mode
and paste bracket — leaving is never modal. Pressing it with the model picker or the
settings panel up does not close them: it leaves codeaf, with the panel still up. `esc`
is the key that closes the thing in front of you.

## What quitting saves and closes — kill, SIGTERM, SIGHUP, terminal closed, or hung up

**What quitting does.** Your unsent draft is written to disk first, with any message
still waiting for an answer folded in underneath it. Then this terminal comes off every
conversation it holds — the ones behind the screen as well, which already wrote their own
boxes to disk when you switched away from them. Nothing is lost that was typed.

**Leaving a window is not ending the work.** A hosted conversation is *detached*: the
window goes and the conversation keeps its turn, its tasks, its questions and its journal,
and opening the same workspace again rejoins it. A conversation running inside this
terminal has nowhere else to run, so it is interrupted and closed here. `/close` and a stop
you type are still the person ending something on purpose, wherever the conversation is
running.

**Limits.**

- **`/quit` closes one conversation, not the program.** It is typed out on purpose, so it
  is not asked twice — but what it closes is the conversation in front, and codeaf stays up
  with the previous one forward when this terminal is holding another. It leaves only when
  that was the last one. `ctrl+c` is the key that closes everything. `/exit` and `/q`
  are the same command.
- A real signal — `kill -INT`, `kill -TERM`, `kill -HUP`, a closed terminal window,
  or `^C` on a terminal that is not in raw mode — also leaves through the same clean
  exit: draft written, session closed, status 0. A second signal ends the process at once
  with status `128 + the signal's number`. The screen is handed back on a bounded best
  effort first; if that could not finish, the terminal may need `reset` afterwards.
- Nothing asks you to confirm leaving. A `ctrl+c` struck by accident at rest ends the
  session, and what you had typed comes back at the next launch.

## Quitting while a task is running — what happens to tasks and background work when the session closes

**A hosted conversation keeps working.** Closing the window, `ctrl+c`, `kill -HUP`
and a terminal that went away all detach: the task goes on running in this machine's codeaf
service, its questions stay waiting for you, and you rejoin it by opening the same
workspace again.

**A conversation running inside this terminal stops with it.** That is `--no-host`, and a
launch where no host could be reached. Every task the session is running is stopped when it
closes and codeaf waits for all of them before it leaves; a task that was only just admitted
is stopped before it opens anything — no worktree, no log, no model call for a session that
has left. Nothing can start in it afterwards: `this session has closed; nothing new starts
in it`. Background jobs go the same way, a moment later.

Either way, what a task wrote is on its branch and stays there; a stop is an interruption
and never a finding about the work. A headless `codeaf chat --once` leaves by the closing
road: the turn stops, the session closes, and the process exits cleanly.

## Keys — what all the keys do, the keyboard keys, keys on the keyboard, key bindings and keyboard shortcuts

This page is about **the keys you press**. Every key, chord and keyboard shortcut codeaf
listens for is on this page, in the tables below.

If you came here looking for a different kind of key, it is somewhere else:

- an **API key** for a model — see *Models and cost* and *Connected accounts*
- a **device key** or a **pairing code** for reaching another machine — see
  *Reaching a machine with a pairing code*
- an **ssh key** — that is your own ssh setup, and codeaf runs your `ssh` unchanged; see
  *Running on another machine*

## Keys in the message box: sending, stopping, and queueing

These apply with no overlay up, no room open, and no mode on.

| Chord | What it does |
|---|---|
| `enter` | Send the message. Empty box with attachments still sends; empty box with a tool row selected opens that row |
| `ctrl+enter` | Send it as something to **keep true** — codeaf shapes it into a standing order's card instead of doing it once. See the standing orders page |
| `shift+enter` | Open a new line without sending, on home and in conversations |
| `alt+enter` | Open a new line in a conversation |
| `ctrl+j` | Same as `alt+enter` |
| `esc` | In order: cancel a history recall, then arm rewind, then interrupt the running turn — and send any message that was waiting for it |
| `esc` `esc` | Two presses inside a short window open the quick inline rewind mode. `/rewind` opens the full timeline instead |
| `ctrl+c` | Turn running: interrupt, and nothing else. Nothing running: quit codeaf, on that press |
| `ctrl+q` | Queue this message to run after the current turn. Empty box does nothing |
| `ctrl+g` | A foreground command that can be kept: send that command to the background. Otherwise: close the task column, or bring it back. On a frame under 100 columns with no roster raised and no command to keep, it does nothing |
| `enter` while a turn runs | Stop the current generation, keep its partial reply, and steer the words into the same turn |
| `cmd+enter` while a turn runs | Hold the message above the box until the answer finishes. Empty box: nothing. Nothing running: nothing |
| `ctrl+shift+enter` while a turn runs | Stop the answer and send what you have typed, as one gesture. Empty box: nothing. Nothing running: nothing |
| `→` over an empty box, a message waiting | Send that waiting message into the running answer. With text in the box it is the caret key |
| `↑` over an empty box | Take the newest waiting message back into the box to edit; with none waiting, walk your history |

The enter family, shortest first: `enter` sends or steers, `cmd+enter` holds it for the
next answer, `ctrl+enter` marks it as something to keep true, `ctrl+shift+enter` stops the
whole turn and sends, and `shift+enter`, `alt+enter` or `ctrl+j` opens a line.

Neither `ctrl+shift+enter` nor `cmd+enter` opens a line — use `shift+enter` for that.
Both need a terminal that can tell them apart from plain `enter`; where it cannot, the
key arrives as ordinary `enter` and the message steers instead.

## Keys in the message box: opening things and moving the view

| Chord | What it does |
|---|---|
| `ctrl+o` | Selected landed card: open its output. Selected proposal: open its brief. Inside a task's page: open or fold the long instruction at the top. Otherwise: open or fold the live caption's tool rows; before a live caption exists, open or fold the `N earlier tool calls` fallback. It never opens a `▸ worked` chip — that is `ctrl+e` |
| `ctrl+b` | Enter copy mode — freeze the view so you can read and copy. Not on home, where it moves the caret |
| `ctrl+s` | Hand the pointer to your terminal so you can drag-select. Toggles; any other key takes it back |
| `ctrl+,` | Open the settings panel |
| `alt+e` | Walk this conversation's thinking rung one step: auto → low → medium → high → xhigh → max, and back to auto. Works with a sentence half typed. On home and every other place it walks the rung of the **next** conversation instead — the effort word after the model’s colon on home’s seam |
| `alt+a` | Walk what this conversation runs without asking one stop: asks → guardian → YOLO → asks. Never lands on `refuses`. Works with a sentence half typed; over `--host` it says the far machine's rules decide. On home and every other place it walks the gate of the **next** conversation — the `◇` cell on the rule above that box — and that pin is spent by the conversation that uses it |
| `ctrl+.` | Open the sessions place (`/history`) — every task this machine has run, across every project and every session; type to filter it. It opens on a machine that has run nothing too, and the page says what tasks are |
| `space` `space` | On an **empty** box: open home (`/home`) — every project and conversation on the machine the session runs on, and an empty home on a fresh one. Does nothing when the box has words in it |
| `ctrl+l` | Jump back to the live edge of the conversation |
| `alt+v` (`opt+v`) | Open the **conversations view** (`/wall`): every open conversation as a live tile, and the teams you group them into. Press again or `esc` to close it. Its own keys are on the *Conversations and teams* page |
| `ctrl+t` | Start a **new chat** — the same start page the `+` at the end of the tab strip opens. Nothing is created until you send the first message, `esc` comes back, and the conversation you were in keeps its draft, its attachments and its work. On a home row it starts the fresh chat in that row's own folder, while clicking a `projects` row selects the folder for the next message |
| `ctrl+w` | **Close this tab** — the same thing the `✕` on it does. Selects the last-used remaining tab, or Home if none remain. Drafts are kept, and the conversation keeps running; a tab with work in it asks `keep running` / `stop work` / `cancel` first |
| `alt+t` (`opt+t`) | Give the keyboard to the task roster. Press again or `esc` to take it back |
| `alt+l` (`opt+l`) | Close the column on the right, or bring it back, in every chat: the same column holds the Tasks and, in a chat in a team, the Traffic. The key is named at the right of the column's header. Under 100 columns it lays the column over the body, and a second press takes it off. Remembered for the next session |
| `alt+m` | In a team that has a manager: go to the manager. Over `--host` against an older codeaf on the far machine it says managers are not available there |
| `ctrl+g` | A foreground command that can be kept takes the key first. Otherwise it does what `alt+l` does: close the column, or bring it back; the column stands even with no tasks in it. Remembered for the next session |
| `ctrl+e` | Empty box: open or close the running conversation’s compact steps first; otherwise the newest `▸ worked` chip onto its outline of captions — the latest completed turn's out here, the newest settled phase's inside a task's page — or the most recent thinking block when there is no chip. A caption is a short status line per step; its tool rows are one expand further. Otherwise: go to end of line |
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
| `ctrl+e` | End of the line — unless the box is empty, where it opens or closes the running conversation’s compact steps, or the newest `▸ worked` chip onto its caption outline (the latest completed turn's out here, the newest settled phase's inside a task's page), falling through to the most recent thinking block when there is no chip |
| `shift+←` / `shift+→` | Select a character at a time, the way shift does in any text field. Does nothing over an empty box |
| `shift+↑` / `shift+↓` | Select a line at a time |
| `shift+home` / `shift+end` | Select to the start or end of the line |
| `alt+shift+←` / `alt+shift+→` | Select a word at a time. `ctrl+shift+←` / `ctrl+shift+→` are the same |
| `cmd+a` | Select the whole message. `ctrl+a` cannot be this — it is the start of the line, here and in every other box |
| `ctrl+z` | Undo — take back what you just typed, a word at a time |
| `ctrl+shift+z` | Redo, on a terminal that can tell it from `ctrl+z` |
| any printing key | Types the character — over a selection it replaces it |

`home`, `end`, `up` and `down` work on the logical line — the run between newlines —
not on the row your terminal wrapped it onto. `up` only reaches history when the
caret is on the first logical line, and `down` only when it is on the last.

A word jump crosses the same boundary the word kill deletes: spaces first, then
the run of non-spaces, so `alt+left` then `alt+backspace` always deletes exactly
the word it just crossed.

## How do I select text in the message box — highlight what I typed, drag to select in the input bar, the input box acts like normal text

**Just drag inside the box.** Put the pointer on a character of what you have typed,
sweep to another with the left button down, and the run between them highlights — the
same highlight a sweep over the conversation wears. On release that text is **on your
clipboard**, and the status line says what landed: `copied · 10 chars`, `copied · 1
word`, `copied · 2 lines`.

**The highlight stays after you let go**, because it is a live selection and not a
flash: type and the typed character **replaces** it, `backspace` or `delete` removes it
whole, a paste drops in over it. Any arrow key, any word jump, any line jump puts the
selection down again.

**Double-click takes the word, triple-click takes the whole line.** A word is what a
person means by one — a path, a hash, a flag, `go.mod`, a URL are each one word — the
same boundary a double-click uses in the conversation above.

**It works in every box you type into**: the message box in a conversation, the box at
the foot of home, and the composer on tasks, standing, memory, spend, search and
settings. Sweeping out of the box carries the selection to the end of what is in it,
rather than stopping at the edge.

**With the keyboard**, `shift` with any motion key selects: `shift+←`/`shift+→` by a
character, `shift+↑`/`shift+↓` by a line, `alt+shift+←`/`alt+shift+→` by a word,
`shift+home`/`shift+end` to a line's ends, and `cmd+a` takes the whole message. Those
`shift` chords are the message box's; on the eight places `shift+←→↑↓` are already the
time window that place is showing, so there they move the window and the pointer is how
you select.

## Undo what I typed — ctrl+z, redo, ctrl+shift+z, take back what I just wrote, I deleted too much

**`ctrl+z` undoes, `ctrl+shift+z` redoes.** They work in the message box and in every
other box on the surface — home's box, the errand pane, and every filter and search box
on every place and panel.

**A step is a word, not a keystroke.** Typing runs together into one step and the step
breaks where a word does, so one `ctrl+z` takes back the last word rather than the last
letter. A run of backspaces is one step too, a paste is a step of its own, and switching
from typing to deleting starts a new one. That is what makes `ctrl+u`, `ctrl+w` and
`alt+backspace` recoverable: each of those kills is one step, and one `ctrl+z` brings it
back.

**Sixty-four steps back**, and no further. A very large draft keeps fewer, because the
history is bounded by how much text it holds as well as by how many steps.

**Sending clears it.** Once a message has gone, `ctrl+z` will not pull it back into the
box — `↑` walks the history of what you sent, and that is where a sent message lives.

**`ctrl+shift+z` needs a terminal that can report it.** Shift is invisible on a control
byte: a terminal that has not taken the keyboard-disambiguation protocol sends the same
thing for `ctrl+z` and `ctrl+shift+z`, so on that terminal the redo arrives as a second
undo. Ghostty, kitty and WezTerm report it; older terminals do not. There is no second
redo chord — `ctrl+y` already copies a path on home and in `/files`, and taking a working
key away would be the worse trade.

**`ctrl+z` does not suspend codeaf.** In an ordinary shell that chord stops the program
and hands you back the prompt; codeaf runs the terminal in raw mode, so the key arrives
as an ordinary keystroke and is the undo instead. To leave, press `ctrl+c`.

## Click to move the cursor — clicking the message box places the caret

A click anywhere on the message box puts the caret under the pointer: on the
letter you aimed at, at the row's end when you click past the end of a line, and
at the start of the text when you click on the prompt's side of it. It works on
a wrapped, multi-line draft — the row you click is the row the caret lands on.

It is the ordinary text-field gesture, and **home's box at the foot of the screen answers
it too** — the one place with a box (the Places page, *Typing on a place*). Holding the button
down and sweeping selects instead, and releasing copies what is lit (see "how do I
select text in the message box" above). While a picker's
filter box is standing in the box's place — the model picker, `/resume`,
`/files`, the memory panel — or while the composer layer is up, a click does not
move that box's caret; those are typed at and filtered, not edited by pointer.

## Why option+left or cmd+left does nothing — word jump and line jump on a Mac

**On a Mac, `option+←` / `option+→` are the word jumps and `cmd+←` / `cmd+→` are
the line's ends, in every box codeaf has.** They work in the message box, in
home's box at the foot of the screen, in the errand pane, and in every filter and
search box on every place and panel.

They work because of what the terminal sends, and a Mac terminal sends them one
of two ways — codeaf answers both:

- **iTerm2's Natural Text Editing key mappings** (the preset most people have)
  send `esc b` for `option+←`, `esc f` for `option+→`, the byte `0x01` for
  `cmd+←` and `0x05` for `cmd+→`. Those reach codeaf as `alt+b`, `alt+f`,
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
nothing codeaf can do about it from inside.

**`ctrl+a` and `ctrl+e` are the spellings that work on every terminal there is**,
and they are the same two jumps. If you would rather not depend on any of the
above, those are the keys.

## Word jump and line jump work in every box, not only the message box

The caret keys are one vocabulary and every box on the surface answers it: the
message box, **home's box at the foot of the screen**, the errand pane on home,
the `something else…` box on a question's own answer row, the settings filter and
its value editor, the task page's filter, the rewind search, and every filterable
overlay — the model picker, `/resume`, `/files`, the memory panel, the connect key
box, the connections panel.

| Chord | Everywhere |
|---|---|
| `alt+left` / `alt+b` / `ctrl+left` | A word back |
| `alt+right` / `alt+f` / `ctrl+right` | A word forward |
| `super+left` / `meta+left` / `ctrl+a` | Start of the line |
| `super+right` / `meta+right` | End of the line |
| `alt+backspace` / `ctrl+backspace` | Delete the word behind the caret. `ctrl+w` does it too in every filter and search box — but **not** in the message box, where it closes the tab |
| `ctrl+u` | Delete to the start of the line |

This did not used to be true: until this wave the jumps were bound in the message
box alone, so `option+←` moved a word in a conversation and did nothing at all in
home's box — which is the first box most people type into. `home` and `end` are
the exception and stay with the box that owns them: on the settings panel, the
task page and the rewind sheet they move the **list**, not the caret.

## cmd+right on home no longer puts a conversation away

`ctrl+e` sets the row under the cursor aside on home — a conversation goes to the
archive, a standing item is paused, and the card's legend says `ctrl+e close`.
`cmd+→` arrives as `ctrl+e` on a Mac, so reaching for the end of a sentence used
to archive whatever the cursor was resting on.

**It reads the caret now.** With the caret somewhere before the end of what you
typed, `ctrl+e` moves it to the end of the line and leaves the row alone; from the
end of the line — and over an empty box — it is the put-away key the legend names.
So one press is never destructive, and the way back out of the archive still
works: type the name of a row you put away, the list finds it, and `ctrl+e` from
there brings it back.

## Click the box to put the caret there — in the conversation and on home

A click on the box puts the caret under the pointer: on the letter you aimed at,
at the row's end when you click past the end of a line, and at the start of the
text when you click on the prompt's side of it. It works on a wrapped, multi-line
draft.

It answers **on home as well as in the conversation** — home is the one place with
a box at its foot (the Places page, *Typing on a place*); the filters on tasks,
memory and search are rows of their own bodies.

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
| `ctrl+u` | Delete to the start of **this line** — not the whole message, and not the whole box. From the middle of `abcdef` it leaves `def` |
| `ctrl+k` | Delete to the end of **this line** — the other half of the pair. It stops at the newline rather than joining the next line onto this one, so a second press on an emptied line does nothing. This chord used to open the conversation switcher, which is now `alt+k` |
| `super+backspace` | Same as `ctrl+u` (Mac `cmd+delete`) |
| `alt+backspace` | Delete the word behind the caret. This is the word kill in the message box |
| `ctrl+backspace` | Same as `alt+backspace` |
| `ctrl+w` | **Not a deletion here.** It closes the tab in front and keeps its draft — see *Close the tab you are in* below. It still deletes a word in every filter and search box, and on the switcher it closes the tab under the cursor |
| `ctrl+h` | Deliberately not bound — some terminals send plain `backspace` as `ctrl+h` |

The kills above work the same way in **every** box codeaf has, not only the
message box: **home's own box**, the errand pane beside it, the model picker, the
sessions roster, the deliverables list, the connect key box and panel, the memory
panel, the settings filter and its value editor, the rewind search, and the task
page's filter. In those boxes `ctrl+w` is a word kill too —
they are the whole screen while they are up, and no tab could be closed from
inside one.

## Close the tab you are in — ctrl+w, close a chat, shut this conversation

**`ctrl+w` closes the tab in front, and it is exactly the `✕` on that tab.**
`ctrl+t` opens a tab and `ctrl+w` shuts one, which is what those two keys do in
a browser.

Your unsent sentence, caret and attachments stay with the conversation. The
conversation remains in `alt+k` and Home, and reopening restores its tab and draft.

Closing the active tab selects the most recently used remaining open tab. With
none left, the window goes **Home** with the current session behind it.
Closing an inactive tab does not switch the current conversation.

Conversations keep working when another tab is selected, over every door: the ordinary
engine-backed chat, `--host` and `--at` each hold **one connection per conversation**, and
`codeaf chat --no-host` holds them in this process. Selecting another tab ends nothing and
sends no Stop or Close to either side of the switch. Closing the final tab only opens Home
and leaves that conversation behind it.

**On the new chat page it closes that page**, exactly as `esc` does: the page
comes down, the conversation you were in comes back with its draft and its work,
and the first message you had half typed is parked for the next time you open it.

Ending a conversation for good is a different act, and this key is not it:
`Stop` on a task's page ends that work, and `/quit` closes the conversation in
front. See *Close a tab from the switcher* below, which is the same
gesture aimed at a row on the `alt+k` card instead of at the tab in front.

The trade is that `ctrl+w` no longer deletes a word in the message box.
**`alt+backspace` and `ctrl+backspace` still do**, and one of the two reaches
codeaf on every terminal.

## Why cmd+backspace does nothing — which terminal you are in decides

`cmd+delete` is bound. codeaf answers it under the name `super+backspace`, and it
deletes to the start of the line, exactly as `ctrl+u` does. When it does nothing
at all, the key never reached codeaf: **your terminal decides whether cmd
combinations are sent to the program at all**, and several do not send them.

| Terminal | Does `cmd+delete` reach codeaf? |
|---|---|
| Ghostty | Yes |
| Kitty | Yes |
| WezTerm | Yes |
| iTerm2 | Yes with the **Natural Text Editing** preset, which maps `⌘⌫` to the byte `0x15` — that reaches codeaf as `ctrl+u`, the same deletion. Load it at **Settings → Profiles → Keys → Presets**. Without a mapping iTerm2 does not forward `cmd+delete` at all; you can also add one by hand at **Key Mappings**: `⌘⌫`, action *Send Escape Sequence*, `[127;9u` |
| Terminal.app | No, and it cannot be made to. It does not speak the keyboard protocol that carries modifiers like `cmd` |
| Anything over `ssh` or `tmux` | Only if the outer terminal is one of the first three, and tmux is passing the protocol through |

**`ctrl+u` is the spelling that works everywhere**, on every terminal on every
machine, and it is the same deletion. If `cmd+delete` does nothing where you are
sitting, that is the key to use instead — nothing is missing and there is nothing
to turn on inside codeaf.

The same is true of `alt+backspace` and `ctrl+backspace` for the word kill, and
between the two of them every terminal sends one — which is what makes it safe
for `ctrl+w`, their old third spelling, to close the tab instead. On iTerm2's
Natural Text Editing preset
`opt+⌫` is mapped to `esc del`, which arrives as `alt+backspace` and kills a word.
codeaf does not detect what your terminal sends and cannot tell you which of
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
else — accent while it runs and again while it is your call, muted once it is done, the
bad hue when something broke, dim when it is queued or you stopped it. It is there
whether the box is empty or full, which is the point: the placeholder that used to say
this disappeared the moment you started typing. The name is cut to at most 18 cells; on
a frame too narrow to spend the cells, the segment is dropped and the placeholder goes
back to naming the task itself. There is no segment at all in the main conversation.

A **slash command you type into the box is highlighted as you type it** — `/task`,
`/compact`, `/clear` get a tinted background behind the word, so a real command looks
different from ordinary text and a typo like `/tsak` does not. The highlight is drawn in
your sent message too. It adds no characters and no cells; see "Slash commands are drawn
as chips" in the commands page for the whole of it.

**A key chord is never given that background.** Where codeaf names a key — the hint slot
on the legend, the `/help` sheet, the opening `esc interrupts · ctrl+c quits · ? for
help` — the
chord is drawn one tier brighter than the words around it and nothing else changes. A
tinted background always means a slash command and only ever that, so the two marks
never have to be told apart. See "Why is one word in a line brighter than the rest" on
the screen page.

**Pasted text lands as one edit** with its newlines intact — it never submits line by
line. Bracketed paste is on. CRLF and bare CR become LF at the door.

Inside an open paste bracket, every key is text: `enter` and `ctrl+j` become a
newline, `tab` becomes a tab, everything else contributes its text. Nothing between
the brackets can submit, interrupt, or answer a question. `ctrl+c` is the one
exception and still works — it leaves codeaf without closing the bracket. A bracket that goes quiet for 2 seconds is treated as
abandoned, flushed, and the keyboard handed back.

A paste while copy mode is up is **declined** — nothing happens, and your clipboard
still holds the text.

## Make my prompt better — spell it out with `ctrl+r`

Type what you want and press **`ctrl+r`**. codeaf reads the sentence sitting in the box
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
and other calls codeaf makes for itself are, and it does not count as a turn.

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

## Every recipient keeps its own message box — I typed something for you and it went to a task

**The box belongs to whoever it is talking to.** The conversation has one unsent
sentence, and every task page you open has its own. Opening a task's page, pressing
`esc` to come back, or clicking straight from one task to another never moves a word
from one of them to another.

What each one keeps: the text, where the caret is in it, its compact `[paste 1 · 42
lines]` chips with the documents behind them, and anything on its tray. So a
half-written message for the model is exactly as you left it when you come back from a
task, caret included, and a correction you started typing at a task is still there when
you open that task again.

**`enter` sends the box you are looking at, and clears only that one.** Steering a task
empties that task's box and leaves the conversation's sentence and every other task's
alone. If steering is refused and you answer the question with `[m]` — send it to the
main conversation instead — the words that go are the ones you typed at the task, and
your unsent sentence for the model is still in the box you come back to.

**All of it is kept on disk, per recipient**, and comes back after a crash or a restart:
the conversation's sentence with its caret, and each task page's own line with its
documents and tray. A task page's line is never restored into the conversation, and never
into a different conversation's task that happens to have the same number. The section
below is the whole of how that is written down.

Before this, there was one box and one set of words in it: typing a message for the
model, clicking a task and pressing `enter` sent that message to the task, with nothing
on the screen looking any different at any point.

## Your unsent draft is kept

The half-written message survives closing the window, a crash, `/new`, and a session
that has moved on. There is nothing to press; it is automatic.

- **Every recipient's box is kept, each to itself.** Quitting while a task's page is
  open writes the sentence you had for the model *and* the correction you were typing
  at the worker; the next launch puts each one back where it was typed. Nothing typed
  at a task ever comes back in a box pointed at the model.
- It is written 300ms after you stop typing, and again synchronously on quit before
  anything else happens.
- **Anything still waiting for an answer is folded in on quit.** A message you parked
  with `enter` while a turn was running (see "Typing while the model is still
  answering") is written into the draft file underneath your unsent sentence, each on
  its own line, so it comes back the next time you open codeaf here instead of
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
- At startup, if this window's own draft file is missing, codeaf takes the newest
  draft in the same directory whose process is no longer running, moves it into this
  window's name, and loads it. Files belonging to a process that is still alive are
  never touched, and older orphans are left where they are. An empty orphan is
  deleted. A process id that cannot be checked is treated as still alive — codeaf
  errs toward leaving your sentence on disk.
- CR and CRLF in a restored draft are normalised to LF.
- No draft is kept at all if codeaf was started without a draft file.

## What a crash does to your draft — what is saved, what comes back, what it will not send

Everything the box is holding is written to one record per conversation, and the plain
file you can read sits beside it.

- **One record, and a plain file beside it.** `draft-<id>-<n>-<pid>.json` is the record
  and is what a restore reads: every box's words and caret, the documents behind its
  `[paste 1 · 42 lines]` chips, its tray, each stamped with the machine, the project and
  the conversation it belongs to. `draft-<id>-<n>-<pid>.txt` is your sentence in plain
  text, exactly as it always was — a build that has never heard of the record still reads
  and writes it, and it is what a restore falls back on where there is no record.
- **A sent or cleared box stays empty.** The record is the answer even when it has
  nothing to say. It is written before the plain file, so a crash between the two can
  leave that file still holding the sentence you just sent; the record is believed
  instead, and the stale file is cleared away when it is found. An empty record is left
  behind saying "this box is empty" rather than deleted, and goes when the conversation
  is closed.
- **Nothing is dropped to make it fit.** There is no size limit and nothing is
  truncated: a pasted log of any size is written down with the sentence it belongs to.
- **A correction sent to a task is in the record too, before it is sent.** `enter` in a
  task's page writes the words, the caret, the pasted blocks and the name that correction
  was sent under into that page's slot, and the correction crosses only once that write
  has landed — so a message waiting for an answer does survive closing the window. The
  next launch puts it back on that page under the same name, so asking again is a repeat
  rather than a second correction. If the record cannot be written the correction is not
  sent at all: `task 7 was not corrected — the draft could not be saved first, and your
  words are back on its page`. See *I sent a correction and the window closed* on the
  tasks page.
- **What comes back is what you left, and what is wrong with it is said.** An attachment
  whose file was deleted meanwhile is still on the tray, and the conversation says
  `a restored draft still names a file that is gone · shot.png`; `enter` names it again
  if you send it anyway. A `[paste 1 · 42 lines]` tag restored from a plain file with no
  record behind it keeps its place in your sentence, and the conversation says
  `a pasted block could not be restored · its tag is still in the draft`. Nothing is
  edited out of your words for you.
- **And `enter` will not send that message.** With a compact tag whose text is gone, the
  conversation and a task page both refuse with `a pasted block could not be restored ·
  the words were not sent`, and your whole line stays in the box: sending would hand the
  model the tag instead of the document, and deleting the tag would send a different
  message from the one on your screen. Paste the block again, or delete the tag.
- **If it cannot be written, you are told**: `this draft could not be saved`, with the
  reason. A record codeaf cannot read — a later build's, or a half-finished write — is
  never overwritten either; it is moved aside as `<name>.json.unreadable-<number>` and
  the new one written in its place.
- **A task page's line follows its conversation, not the window.** Open that
  conversation tomorrow in a different window and its lines come with it; open a
  different conversation and they are neither shown to you nor deleted.
- **Only the newest record for a conversation is read.** If two windows each left
  one — two crashes — the later one wins outright, so a box you emptied is not
  refilled by the earlier one. The earlier record is left on disk untouched rather
  than merged or deleted; nothing in codeaf offers it back to you.

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

## The thinking chip above the message box — `alt+e`, `/effort`, and making this chat think harder

The line above the message box — the legend — names how hard the model will think about
your next turn, immediately after the model that will be doing the thinking:
The effort follows a colon with no space: `model:effort`. The model stays bold and
bright; effort keeps its own styling. The old six-dot badge is no longer shown.

```
─ glm-5.3-flash (deepinfra):high · ◇ asks ──── $0.27   66.8k/1.3M · 5%   idle ─
› what changed in the relay this week
```

The word is one of the five rungs of the effort ladder — `low`, `medium`, `high`,
`xhigh`, `max` — or **`auto`**, and it is **what will actually happen**, not what somebody
chose: it is the rung the next turn will ask for, whichever setting decided it. See
*Making the model think harder, deeper, or less* on the "Models and cost" page for the
whole ladder and for what each rung asks the provider for.

**A conversation nobody has dialled reads `auto`**, which is what a shipped install
says on every fresh conversation. See *What `auto` means beside the model* below.

**The same cell is on the rule above the box on home**, where it
says how hard the conversation you are about to start will think, and `alt+e` or a press
walks it there too. A rung set there is carried onto the conversation `enter` opens and
lasts as long as this window does. *The rule above home's box* on the Places
page has all four cells of that rule.

**`alt+e` walks it.** Each press moves one rung up, and off the top it comes back to
`auto`: auto → low → medium → high → xhigh → max → auto. It works with a sentence half
typed — it is a chord, it carries no text of its own, and it leaves your draft and your
caret exactly where they were. Ordinary letters keep typing.

**Pressing the rung walks it too**, one step per press — the same six stops, `auto`
included — which is the same gesture as pressing a task's thinking row inside that task. It brightens under the pointer over
exactly its own cells first, and the press never moves the caret in your draft. It does
not open a list: the list is `/effort`.

**`/effort` opens the ladder**: six rows — `auto` first, then the five rungs cheapest
first — with the row you are on marked. `↑`/`↓` walk it, `enter` applies, `esc` closes,
and `alt+e` moves the cursor down a row while the list is up. While the list is up
**every key belongs to it** — a plain letter does not type into the message box
underneath. Typing `/effort` again puts the list away. `/thinking` and `/think` are the
same command. The six rows read:

```
auto    the model decides — the shipped setting
low     answers quickly and barely deliberates
medium  a short think before it answers
high    thinks before it answers
xhigh   a deeper pass, and it takes the time that costs
max     the deepest pass there is
```

**`/effort <rung>` sets one outright** — `/effort max`, `/effort low` — without opening
anything. `/effort auto` clears this conversation's rung, and `/effort off` is the same
thing under its older name. A word that is none of the six changes nothing and prints
them all.

Until 2026-09-09 the rung was a chip at the right end of the tray row above the box, and
clicking *that* opened the ladder. The rung is on the legend now, beside the model it is
about, and the click walks it.

What it changes and what it does not:

- It sets **this conversation's** rung. It is sticky — kept in this session's own
  `meta.json` — so it is still there after you close codeaf and come back.
- The rung reaches the work this conversation hands out: task workers start at it too.
- It does **not** change other conversations. The default for those is the **thinking**
  row in `/settings`, which ships at `auto` (the provider default).
- **`auto` is on the legend, it is the ladder's top row, and it is a stop on the wheel.**
  With thinking at `auto` and no more specific level chosen — which is what a shipped
  install is — the cell reads `auto`, it is pressable, and `alt+e` walks it onto `low`.
  One more press past `max` brings it back to `auto`.
- **It works on a `--host` conversation.** The rung is set on the engine machine, where
  the conversation lives, and the word on your legend is the one that machine resolved.
  An engine too old to know the ladder says so at the door and there is then no rung on
  the line, no press, and no chord — rather than a knob that does nothing.

**When the rung will not move.** A thinking level dialled onto the model itself — the
model picker's `ctrl+t`, or `--reasoning` at launch — beats this conversation's rung. Press
`alt+e` there and codeaf says so in a note, naming the model and pointing at `ctrl+t`:
*thinking stays low · the level set on \<model\> decides this conversation — ctrl+t in
/model changes it*. Clear that level and the rung moves again.

The rung is dim, like the rest of that line. It brightens for about two seconds after it
changes — the cell takes a lit ground and its effort word goes cyan — so you can see the new word
without looking away from what you are typing, and then it goes quiet again. Walking it
back onto `auto` flashes the same way and writes one line: *thinking · auto · the model
decides*.

## The approvals chip above the message box — `alt+a`, turning YOLO on inside a chat, stop asking me for this conversation

After the thinking rung, the legend names what this conversation runs **without asking**:

```
─ glm-5.3-flash (deepinfra):high · ◇ asks ─── $0.27   66.8k/1.3M · 5%   idle ─
› what changed in the relay this week
```

The cell is the permissions panel's own mark for "a whole tool" and one word, and it is
there **at every posture** — a control you cannot see until you have used it is not a
control. The word is what is in force, whichever setting decided it:

| word | what it means |
|---|---|
| `asks` | every call the rules say to ask about is asked about |
| `guardian` | a small model answers the plainly safe ones first; you get the rest |
| `YOLO` | every tool runs without asking — the posture `--yolo` opens. Painted in the warning hue for as long as it is true |
| `refuses` | every call the rules do not name is refused. Also painted as a warning |

**`alt+a` walks it**: asks → guardian → YOLO → asks. Three stops, each more autonomy than
the last, and **the wheel never lands on `refuses`** — a press past YOLO that refused every
call would break the session you are in. It works with a sentence half typed and leaves
your draft and caret where they were. **Pressing the cell walks it too**, one stop per
press; it brightens under the pointer over exactly its own cells first.

Why not `ctrl+y` or `shift+tab`: `ctrl+y` copies a path on home and in `/files`, and one
chord means one thing on this surface; `shift+tab` walks backwards through the fields of
every question card, and on some terminals arrives as a plain `tab`.

**Inside a task's page the cell reads `◇ on its own`** — a task runs every tool without
asking and has no wheel — and `alt+a` there says so instead of moving anything (the task
page's "The line above the box on a task's page"). **On home and every other place, the same
`◇` cell sits on the rule above that box** and
says what the conversation you are about to start will run without asking — the settings
rows' answer, or `YOLO` if this process was started with `--yolo`. `alt+a` or a press walks
it there on the same wheel, the pin is carried onto the conversation `enter` opens, and it
is **spent** by that conversation: back on home the cell says the rows' word again, so an
open gate is never quietly the default for the one after. *The rule above the box on every
place* on the Places page has the whole rule.

What it changes and what it does not:

- It sets **this conversation's** posture, live — the very next tool call is decided under
  it. It is sticky, kept in this session's own `meta.json`, so it is still there after you
  close codeaf and `/resume`. `codeaf resume --yolo` outranks the saved word for that
  launch.
- It does **not** change other conversations. Their answer is the **"ask before running"**
  row and the **guardian** row on `/settings`' Safety tab, unless they have a saved
  posture of their own.
- **Neither floor moves.** Dangerous shell commands and anything sent in your name are
  asked about at every stop, `YOLO` included, exactly as under `--yolo`.
- **It works on a `--host` conversation.** The posture is set on the engine machine, where
  the gate is, and the word on your legend is the one that machine resolved. An engine
  too old to have the door says so when the connection opens: the cell is then a reading
  of the far machine's own row, and `alt+a` and the press both answer:
  *what runs without asking is decided on the machine the conversation runs on — its engine has no dial for this window · change it in that machine's /settings*.

The cell flashes for about two seconds after it changes and writes one line —
*approvals · YOLO · every tool runs without asking · dangerous commands still ask* — and
then settles. While the welcome box is on screen there is no legend and no badge either —
the cell arrives with the legend the moment the greeting goes (the first keystroke, or on the
very first conversation the first message). The status line carries the old
`YOLO` badge only on a frame whose legend has no cell, over an engine with no approvals
door, and only while the gate is open.

## What `auto` means beside the model — putting thinking back to auto, and why the cell is there at all

`auto` on the line above the message box means **nobody has asked this conversation to
think any particular amount**. codeaf sends no reasoning field on the request at all, and
the model thinks however it thinks — its own published default. It is not "think as little
as possible": that is a different request, and `low` is the rung for it.

**It is what a fresh install says.** The **thinking** row in `/settings` ships at `auto`,
so until you dial something — this conversation with `alt+e`, `/effort` or a press on the
cell; one model with the picker's `ctrl+t`; one task with `alt+e` on it; or the machine
itself in `/settings` — every conversation reads `auto`.

Home also keeps `model:auto` on the seam and `alt+e effort` in the bottom row.
This applies to local engine connections and `--host` alike: an unset default is
`auto`, not a missing control. Press the effort word or use `alt+e` to change the
next conversation's effort.

**To put it back to `auto`:** keep pressing `alt+e` or the cell — the wheel's stop after
`max` is `auto` — or type `/effort auto` (or `/effort off`, the older name for the same
thing), or open `/effort` and pick the top row. The typed word and the top row do it in
one move from any rung; the wheel gets there by walking. Until 2026-09-15 the wheel had
five stops and could not reach `auto` at all, which left the state a fresh install starts
at as the one thing the control in front of you could not say.

Clearing it does not always change the word on the line, and codeaf says why in a note
either way:

- Nothing else is set: the cell reads `auto` and the note is *thinking · auto · the
  model decides*.
- The **thinking** row in `/settings` is set on this machine: a cleared conversation
  falls back to that row, so the cell keeps its word and the note is *thinking · auto for
  this chat · \<rung\> · the thinking row in /settings decides now*.
- A level is dialled onto the model itself (`ctrl+t` in `/model`, or `--reasoning` at
  launch): that level beats every rung here, the cell keeps saying it, and the note names
  the model and points at `ctrl+t`.

Until 2026-09-09 there was **no cell at all** on a conversation nobody had dialled, which
on a shipped install meant every conversation — so the dial was invisible to anyone who
had not already found it. Both roads say `auto` now, the local one and `--host`. The cell
is missing in exactly one case: a session with no dial behind it, which is a `--host`
connection to an engine too old to know the ladder. There is then no rung, no press and no
chord, and `/effort` says *how hard this conversation thinks is unavailable — this session
has no dial onto it*.

## Attaching a picture

There are three ways in.

1. **Drag a file in, or paste one.** Drop a screenshot on the terminal — or copy a
   file in Finder or your file manager and press `cmd+v` / `ctrl+shift+v` — and
   codeaf attaches it. See "Dragging or pasting a screenshot in" below, which is
   the way most people do this.
2. **`/attach <path>`.** A picture handed to it is a picture. `~` becomes your home
   directory, a relative path is resolved against the conversation's directory — or
   against **your own machine's** working directory over `--host` — an absolute path is
   left alone, and a quoted or backslash-escaped path is read as the one path it is.
   (`/image <path>` was a second word for this until 2026-09-22 and is gone.)
3. **The `@` completion.** An image row in the list is tagged `img`. Choosing it
   **removes the half-typed `@token` from your sentence** and puts the file in the
   tray, instead of typing a path.

Typing out an `@` path to an image by hand does not attach — attaching happens when
you choose the completion row.

**What is accepted:** `.png`, `.jpg`, `.jpeg`, `.webp` and `.gif`, case-insensitive.
Those five are exactly what codeaf will send. The ceiling is **10 MB per picture**,
checked against the file's size first and again against the bytes actually read. The
same path attached twice is one chip.

**You do not always have to attach.** A file already on the machine can be looked
at without the tray: ask codeaf to look at it by path and it uses its `view_image`
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

**Drag a picture onto the terminal, or paste one you copied as a file, and codeaf
attaches it.** What the terminal actually hands over is the file's *path* as pasted
text — `/var/folders/.../Screenshot 2026-08-21 at 5.21.40 PM.png`, usually with its
spaces backslashed, sometimes quoted, sometimes as a `file://` URL, and sometimes with
raw spaces or `%20` escapes. codeaf reads every shape, including the narrow no-break
space in a macOS screenshot name, and reads several files dropped at once, separated by
spaces or by newlines.

**Your sentence gets `[image #1]`, not the path.** The picture goes on the tray and a
short token takes its place in the message box, numbered in the order the pictures
were attached. It is ordinary text: type around it, delete it, move it. And it is
what you say out loud — "what font is image #1", "compare image #1 with image #2" —
because **the token goes to codeaf inside your message, in the position you left it,
and the picture itself travels with it.** codeaf is told that `[image #1]` marks the
first picture in the message, so the number you read is the picture it is looking at.

**A picture attached by `/attach` or the `@` completion gets its token too**, appended
to the end of your sentence when you press `enter`, so "image 2" means the same thing
whichever way the picture got there.

**It is all or nothing, on purpose.** A paste is treated as attachments only when one
complete terminal reading of it names real files **on this machine**. A sentence that
mentions a `.png`, a diff, a stack trace, a log — all of it goes into the message box as
the text it plainly is, which is what pasting has always done.
A paste over a line that starts with `/` is left as text too, so `/attach ` and
`/export ` still take a path.

**Raw image data on the clipboard is not read.** Copying a picture out of a browser or
a screenshot tool — as *pixels* rather than as a file — pastes nothing here. Save it to
a file first, then drag that in, or use `/attach <path>`.

## What codeaf says when a picture is refused

| Situation | Exact text |
|---|---|
| `/attach` with no path | opens the file browser rather than refusing |
| Not one of the five types | it is attached as a file, not refused |
| Missing file | `no such file: <path as typed>` |
| Already in the tray | `<basename> is already attached` |
| Dragged or pasted in over the ceiling | `<basename> is over the 10MB image limit` |
| Unreadable when you send | `could not read <basename>` |
| Over the ceiling when you send | `<basename> is over the 10MB image limit` |

A dragged or pasted picture is measured **at the moment you drop it**, and one over the
ceiling is refused there rather than attached and refused later. Nothing is lost when
that happens: the path stays in your message box as the text it arrived as, so you can
still ask codeaf to look at the file where it lies. Pressing `enter` on that retained
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

**codeaf really sees the picture.** It does not receive the path and go and open it:
the bytes travel inside the message as the picture itself, base64-encoded, beside your
words, which is why the path is rooted on your **local** machine even on a remote
session. If the model you are talking to cannot see, the picture is shown to a model
that can and its answer comes back prefixed `[vision: <model>]`; if nothing available
can see, the message is refused before anything is sent and your pictures stay on the
tray. Once the message is sent, the transcript keeps the numbered marker and draws a
compact control for each picture under your line.

A command with a full tray is still a command: `/attach` adds a second picture rather
than sending the first.

## Do I see my own screenshot in the conversation?

Yes. After sending, each image has a compact filename row in tray order, with **preview**
and **open original** actions. The numbered `[#1 shot.png]` marker remains above it.
Images start collapsed; your words are never folded with them.

Click **preview** to show a low-resolution terminal view, and **collapse** to close it.
Only one attached picture per message expands at a time. **alt+i** toggles the last
visible image in chat or a task page. The terminal preview is at most **20 rows**;
it is not suitable for reading screenshot text.

Click the expanded picture itself or **[open original]**, or use **alt+o**, for the
full-quality image in your system's
viewer. The phone-width detail sheet also accepts **o**. In `--host` sessions,
engine-owned files are fetched to this machine first. In a plain SSH login, the
action explains how to use the local client and gives the original path instead
of opening a viewer on the server.
The original action works even when the terminal cannot draw pictures. If a requested
preview is missing, unsupported or unreadable, it says **Preview unavailable · open
the original**. Replaying a conversation starts its attachments collapsed again.

## Completing a path with `@`

Type `@` and codeaf offers one list under the message box. The first row is the
words **team**, **chat** and **file**. Under them: **teams**, then **conversations**,
then **tasks**, then **files and folders**. It opens on the bare `@`. You do not have
to type a letter first. It closes on `esc`, on committing, or when the token stops
being one.

**team**, **chat** and **file** are presses. The word under the pointer takes a
background, and the hint says `only teams · click`, `only conversations · click` or
`only files · click`. A press types that prefix, `@team:`, `@chat:` or `@file:`, and
the list keeps only that section. Press the same word again and the prefix comes off.
Typing still filters every section that is showing. A command's path argument
(`/image `, `/export `, `/attach `) stays a file list and has no prefix row.

**Folders are on the list too**, spelled with a trailing slash — `internal/tui3/` — and
marked `folder` on the right the way a picture row is marked `img`. Choosing one puts
its path into your sentence exactly as choosing a file does. It does not choose that
folder as a place; `/folder` is what does that.

The token is found by walking back from the caret to a space, a newline, or the start
of the message; that run must **begin** with `@`. So an `@` in the middle of a word —
an email address, a Go doc link — never opens the list.

**What it walks:** the conversation's workspace, or **your own machine's** working
directory over `--host`. **On home** the same list opens over home's box (since
2026-09-22) and walks the folder the next conversation opens in — the `project:` at the right of the keys row —
so moving the target with `alt+p` or `/project` walks again; it offers files and folders
there and never tasks, because a task pointer is minted when a conversation sends and
home has none yet. Skipped: `.git`, `vendor`, `node_modules`, every
dot-directory, every dot-file, and every symlink. Unreadable directories are skipped
rather than fatal. The walk is capped at **10,000 files**, and paths are stored
relative to the root with forward slashes.

**The walk runs once per session.** A file created part-way through the conversation
will not appear in the list.

Ranking puts a prefix match above a substring above a subsequence; the whole path and
the base name are both tried at each tier, the base name a hair below the path.
Inside a tier the earlier match wins, then the shorter path. File hits are capped at
32. The list shows 8 rows, or **14** when a team, a conversation or a task is on it.
Team rows, conversation rows and task rows are each capped at 8. Tasks are searched
to a pool of 40.

The prefix words are there at once. Teams and the conversations already open in this
window are there at once, from memory. Recent conversations and the file walk arrive
as they are read. With no match the line is `no matches`. A prefix that finds nothing
says `no team matches`, `no conversation matches` or `no file matches`.

## What `@` puts into your message

- **A file:** the path replaces what you typed after the `@`, and **the `@` stays**.
  Nothing is read at this point. `@internal/session/agent.go` is sent exactly as it
  stands, and codeaf's read tool resolves it if it wants to.
- **A folder:** the same thing, with the trailing slash kept — `@internal/tui3/`.
- **An image:** the whole half-typed `@token` is removed and the file is attached
  instead.
- **A task:** the task's name goes in after the `@`. The pointer block is minted when
  you send, not here.
- **A team:** the `@` comes out and a coloured dot plus the team's slug goes in,
  `●harbor`, in that team's colour. On a screen that draws no colour the dot is `*`.
- **A conversation:** `@` and its handle, or a short slug of its title when it has
  no handle. The row's note is the full title. A conversation in no team is still
  on the list.
- **Under a command's path argument:** the path replaces the argument whole, with no
  `@` in front, and an image is written into the line like any other file.

**When you send,** every `@<slug>` that names a task codeaf already knows about grows
a pointer-block footnote after the message, one block per task, in token order,
deduplicated. A team mark and a chat mark do something else, on the engine: the
model is handed a short digest of that team or that conversation, and your
transcript keeps the words you typed. The digest is members, handles, states and
recent traffic for a team, and the title, the state and an excerpt of the last
reply for a chat. It is never the whole transcript. Mentioning a conversation does
not message it and does not start its turn. Unknown tokens are left alone in
silence. A task slug pasted whole and sent in the same beat resolves against the
snapshot already in memory and never touches the disk, so it may stay plain text.
The entry remembered for `↑` is the sentence as you typed it, before the task
footnote.

**The honest limit: `/attach ` (and its `/upload ` alias) and `/export ` get path
completion.** That is the whole list. Any other command that takes a path gets no
completion at all, and says nothing about it. Over `--host`, completion still walks the
machine you are sitting at: `/attach` sends those local bytes across.

## Keys in the command list and the `@` list

These two lists are **not modal** — you keep typing into the same box and the list
follows what you type. Only these keys are taken from you:

| Chord | What it does |
|---|---|
| `up` / `ctrl+p` | Move the list cursor up |
| `down` / `ctrl+n` | Move the list cursor down |
| `esc` | Close the list. For the command list it also **seals that word** — the list does not reopen on the next letter of it. It does **not** interrupt a running turn |
| `enter` | Command list: take the highlighted command. At the start of an otherwise empty box that **runs** it; anywhere else it replaces just that word with the command's name and runs nothing. If nothing matched, the line is sent as typed. `@` list: insert the highlighted team, conversation, task or file; if nothing is picked, the line is sent |
| `tab` | Read **before** the list. It only opens or commits an *argument* completion, over `/attach ` or `/export `. With nothing to complete and an empty box it goes back to the last conversation |
| `enter`, with an argument completion open | Closes the list and runs the line **as typed**. Your path is never swapped for the top-ranked row |

## Keys in the model picker and the sessions roster

**Model picker** — opened by `/model` with no argument, by clicking the model name
in the status row, or by `enter` on the **your model** row of the settings panel's
Providers tab (the same list and the same keys, drawn in the panel's place):

`esc` close · `enter` switch to the highlighted model · `ctrl+t` cycle the reasoning
effort · `ctrl+r` fetch the newest model list (`/model` only — not the settings panel's
rows) · `tab` and `→` open the providers under the model the cursor is on and move the
cursor into them, `tab` and `←` close them and put it back on the model ·
`up`/`ctrl+p`, `down`/`ctrl+n`, `pgup`, `pgdown` walk the list ·
`backspace`, `delete`, `ctrl+u`, `ctrl+w`, `left`/`ctrl+b`, `right`/`ctrl+f`,
`home`/`ctrl+a`, `end`/`ctrl+e` edit the filter · **`alt+s`** orders the list by the next
column and **`alt+shift+s`** turns that column round · anything else types into it.

Every column is two rungs — its own direction, then reversed — so `alt+s` walks `model ↓`,
`model ↑`, `via ↓`, `via ↑`, and so on back round to the name, skipping any column this list
published nothing in; `alt+shift+s` retraces it. The list is always sorted and the sorted
column always wears `↓` or `↑` in the heading. Inside an open provider fold the same key
sorts the PROVIDERS, and the two tables keep their own orders. It is a chord and not a bare `s` for the reason the tasks place gives:
`s` is one of the commonest letters a filter starts with, and the list a person was
narrowing would re-sort instead.

`→` and `←` are the providers' keys **unless you are in the middle of typing**, in which case
they move the caret through the filter text. "In the middle of typing" means within **0.6
seconds** of the last change to the box; every keystroke pushes that out again, so they stay
the caret's for as long as you keep typing and become the tree's the moment you stop. They
are also always the tree's at the very end and the very start of the text, where there is no
character to step over — so an empty box, which is most of this list's life, never waits.

**To get the caret back without waiting, press any key that edits** — a letter, `backspace`,
`ctrl+w`, or a paste — or `ctrl+b`/`ctrl+f`, which are `←`/`→`'s understudies, are never the tree's, and
so move the caret without changing a letter of what you typed. Walking the list with
`↑`/`↓` does **not** count as editing, so reading a filtered list never takes the arrows back
from the tree. `tab` always opens and closes whatever the box is doing.

With the providers open, `enter` on one of them pins it instead of switching model.

Its placeholder reads exactly `filter by name · ctrl+r refresh` — the keys are on the FOOT, because
a placeholder vanishes under the first typed character and the foot does not. The hint slot
follows the cursor: `→ providers · alt+s sort · enter switch · ctrl+t effort · esc` on a model, `← back · alt+s sort · enter choose · esc` inside its
providers, and `enter unpin · ← back · esc` on the provider already pinned, where `enter` takes
the pin off — with `tab providers` and `tab back` in place of the arrows while you are
mid-typing and the arrow would step over a character instead. The foot always names whichever
of the two actually works at that moment. Typing while a fold is open filters that model's providers;
only a query none of them match falls through to filtering the model list.

**Sessions roster** — opened by `/resume`: the same key map, except `enter` opens the
selected session. Its placeholder reads `filter · ↑↓ · enter open · esc cancel`.

**The empty screen's greeting:** it takes only two keys, and only over an empty message
box — `up`/`down` walk the recent sessions listed under it and `enter` opens the selected
one. Every other key dismisses the greeting and then does whatever it normally does; the
first letter you type lands in the box, which is drawn inside the greeting until then.

Both pickers are modal: while one is up, every chord except `ctrl+c` belongs to it.
`ctrl+c` does not close the picker — it quits codeaf, with the picker still up.

## Keys when codeaf asks you a question — what key answers switch to auto, and the other offers

**An approval question:** `1` allow once · `2` always — not drawn and inert when it
would do nothing · `3` deny · `c` answer in words · `esc` **later**, which folds the
question to the chip and answers nothing. It is **not modal**: every other key belongs
to your message box, and a key it does read also **stops the countdown**. A key pressed
in the first quarter-second is dropped, so a question landing under a moving hand is not
answered by a keystroke aimed at your sentence. `ctrl+c` is handed back to the message
box, where it quits codeaf. On the second
beat of "always" for a bash command, `1`–`9` pick a shape and `esc` goes back — and
while that beat is up the digits are the shapes', not the answers'.

`alt+y` raises the newest question you put off, from any page.

**A task proposal** is answered on that same block, in that same grammar: `1` start
it · `2` no · `c` answer in words · `esc` **later**, which folds it to the chip and
answers nothing. `enter` over an empty box takes the answer marked `▸` — the one
the clock is about to take — and `enter` with words in the box sends them as a
correction, which starts the corrected work. `ctrl+e` over an empty box opens the
brief in the conversation. Bare letters are ordinary answer text: typing `no` is a
correction and does NOT decline, because the answers are on the row with their
keys. Any key the question reads also stops the countdown, and deleting the draft
does not restart it.

**A slow provider's offer**, raised on the status line when a provider you pinned has gone
quiet: the row reads `coreweave is slow · switch to auto? (y)` and `y`, **over an empty
box only**, fetches this answer from somewhere else. There is no key to decline — the
question takes itself down when an answer starts arriving — and with anything typed in the
box a `y` is a `y`.

**A connect offer:** `1` connect · `2` not now · `esc` **later**. It is on the question
block, so everything else falls through to the message box. A service connected by a
KEY has no `1` at all — a bare yes to one of those is read as a decline — and asks for
the key in the message box itself, masked: `enter` sends it, `2` is the way out, and
`esc` puts the question off with what you typed still in the box.

**A harness offer:** `1` run it · `2` not now · `esc` **later**. It is on the question
block, so everything else falls through to the message box rather than doing nothing, and a
digit is the offer's only over an empty box. `enter` and `y` no longer answer it, and `esc`
is no longer the no.

**A finished harness design:** `1` save it · `2` change it, which walks into the design's
own room · `3` drop it · `esc` **later**. The page in the conversation carries no answers of
its own, and the same three digits answer it from inside the design's room. It was `enter`,
`e` and `esc` on the card and `ctrl+k`/`ctrl+x` in the room; all five are gone.

**The steer guard**, raised when you press `enter` in a room whose node is not
listening: `r` revive and send · `m` send to main · `esc` cancel and keep your words ·
`ctrl+c` handed back to the door, where it quits · everything else does nothing.
Its row reads
`[r] revive and send · [m] send to main · [esc] cancel`. When the task is still
running — refused mid-check, or while its work lands — the guard offers no `r`: its row
reads `[m] send to main · [esc] cancel`, because reviving live work would duplicate it.

## Keys in the settings panel and the other panels

**Conversations view** (`alt+v`, `/wall`, or `▦ All` on the strip or under the box): the
arrows move the focus · `enter` opens · `space` picks · `x` closes a view (the work keeps
running) · `m` its teams · `s` a new team · `e` the shown team's settings · `tab` or `1` to
`9` show a team · `/` filters · `?` lists every key, and each of its rows is a button. The
**team switcher** under the strip's team chip takes `↑` `↓` `enter` `esc`. The whole map is on
the *Conversations and teams* page.

**Teams page** (`alt+2`, `/teams`): while the manager's conversation has the box, keys type
into it; `alt+↑` `alt+↓` put the keyboard on the page's buttons and `esc` gives it back. On
the buttons: `↑` `↓` walk, `←` `→` cross between the rail and the pane, `enter` presses,
`s` the team's card · `c` close · `w` open on the conversations view · `n` new team (inside
the chosen team) · `o` Organize · `m` Move into… another team · `space` pick a team for a
move of several · `p` the members card · `M` a manager · `r` reopen · `d` delete a closed
team · `u` Undo a close or a move · `esc` cancels a drag or a move's question. In the Move
into… picker, typing filters, `↑` `↓` walk, `enter` moves, `esc` cancels. The whole map is on
the *teams page* page.

**Settings panel** (`ctrl+,`): `esc` backs out one layer at a time — search, then an
open account, then the panel · `left`/`shift+tab` and `right`/`tab` change tab ·
`up`/`ctrl+p`, `down`/`ctrl+n`, `pgup`, `pgdown`, `home`, `end` walk · `enter` activates,
and `space` activates too — except while a search is on, when it goes into the box, so
a phrase like `shell command` can be written · `backspace`, `ctrl+u`, `ctrl+w` edit the
search · anything else types into it.

**Task page** (`ctrl+.`, or `/history`, or the one dim door line at the bottom of the task
column, `ctrl+. earlier`): `esc` closes it (or clears the filter first, if one is being typed),
and `ctrl+.` closes it either way · `up`/`ctrl+p`, `down`/`ctrl+n` move, stepping
over the `running` and `earlier` section words · `pgup`/`pgdown` move twelve · `home`/`end`
first and last · `enter` opens the row · `backspace`, `ctrl+w` and `ctrl+u` edit the filter
· **`1` and `2` answer the row under the cursor, but only while the pane beside the list is
drawing those two answers for it** — a frame at least 110 columns wide, over a task of this
conversation's that is waiting on you; everywhere else a digit is typed · **every other
printable key, the space included, types into the filter**, which narrows
every section at once and is drawn on the **control row** at the top of the list, as
`⌕ port`. `alt+s` sorts by the next column — age, name, state, files, cost — and
`alt+shift+s` turns the column you are on round; a press on one of the two column labels at
the right of that control row sorts by it. `→` opens the row's
verbs, and this place has one: `s stop it`, over a task this conversation is holding that is
still queued or running. Its foot is assembled from what is true of the row under the
cursor — `enter open its room · → verbs: stop it · alt+s sort · type to filter` over a task this
window is running, `enter go inside it` on a task another conversation ran, which has no room to open,
and one of three clauses on a task another codeaf **window** is running:
`enter go to that conversation` when this terminal is holding that conversation, which
switches to it standing in that task's room; `enter read it as it runs` when the engine is
running it and this window can join, which opens that task's own live transcript, read-only;
and `enter where it is running` when nothing here can reach it, which opens the card that
says which window has it — and
`esc clear the filter` in place of `type to filter` while you are typing one — the one fact
the control row itself cannot show is that esc now means the filter and not the page. On a machine that has run **nothing at all**,
where the body is teaching what tasks are, the foot drops to `tab next place · esc` alone:
there is no row to filter, none to open and no verb to press. **Clicking a row moves the cursor
to it and the pane follows; clicking the same row again opens it.** On a frame too narrow for
the pane there is nothing for a first click to show, so one click opens as it always did. A
click on one of the pane's own answers presses that answer and opens nothing. The wheel
walks the cursor. The tasks pages describe what is on it.

**Inside an old task's card** (`enter` on an `earlier` row): `esc` or `←` backs out to the
list · `ctrl+.` closes the whole page · `↑`/`↓` (also `k`/`j`) scroll · `pgup`/`pgdown` and
`space` move a screenful · `home`/`end` the ends · **`m` puts that task's name in your
message box** and closes the page. Its foot reads
`m puts it in your message · ↑↓ scroll`; `esc back` is in the head's right corner instead,
said once. Clicking its head row or its foot goes
back to the list; its body is read.

**Inside the card over another window's task** (`enter` on a row noted `another window`):
the same keys, minus the mention. Its foot reads `↑↓ scroll` — with `esc back` beside it
where the head has no room for its corner — and `m` does nothing: nothing has landed for a
`@` name to point at. `esc` or `←` backs out to the list.

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
`esc clear the search · ↑↓ move · enter picks the point` while you are typing one.
Clicking a row places the pick; clicking the placed point rewinds; the wheel walks the
cursor. The sessions and rewind page describes what the cut does.

**Inside the quick rewind mode** (`esc` `esc`): `↑`/`↓` walk turns · `←`/`→` step inside
one · `enter` cuts · **`tab` lifts you onto the rewind timeline** with the cut you had
chosen · `esc` leaves with nothing changed.

**Connections panel:** `esc` · `up`/`ctrl+p` · `down`/`ctrl+n` · `pgup` · `pgdown` ·
`enter`. Its filter placeholder reads `filter · ↑↓ · enter connect · esc close`.

**Harness panel:** `esc` · `up`/`ctrl+p` · `down`/`ctrl+n` · `pgup` · `pgdown` ·
`enter`.

**Thinking ladder** (`/effort`, `/thinking`, `/think`): `esc` closes ·
`up`/`ctrl+p`, `down`/`ctrl+n` walk the six rows — `auto` and the five rungs · `alt+e`
moves down one · `enter` applies the row under the cursor. Clicking a row applies it;
clicking either of the two sentences around them does nothing. Its foot reads
`↑↓ · enter apply · esc · alt+e next rung`. Pressing the effort word after the colon above the message box
does **not** open this list — it walks the rung one step.

**Permissions panel:** `esc` — which drops an armed confirmation first, then closes ·
`up`/`ctrl+p` · `down`/`ctrl+n` · `pgup` · `pgdown` · `enter` **or `d`** to drop the
line under the cursor. Press it twice; the first press arms it.

**Status deck sheet** (narrow terminals): `esc` or `q` close · `up`/`k`, `down`/`j`
move · `enter` activate. Its foot reads `esc close · ↑↓ move`.

**Phone tool detail sheet:** `esc`, `q`, `left` and `enter` all close · `up`/`k`,
`down`/`j` scroll · `pgup`/`ctrl+b`, `pgdown`/`ctrl+f`/`space` page · `home`/`g` top ·
`end`/`G` bottom · `ctrl+o` lifts the line cap. Its foot reads exactly
`esc close · ↑↓ scroll`, or `esc close · ↑↓ scroll · tap … for the rest`.

**Crew panel** (`/crew`): `up`/`k`/`ctrl+p`, `down`/`j`/`ctrl+n`, `home`, `end` walk the six
rows · `enter` changes the row · `esc` goes back one level and closes · `?` lists every key ·
`z` undoes the last change while the bottom edge offers `z undo`. On the **models** row
`←`/`→` step `all`, `open`, `price`, `custom`, and `enter` steps forward like `→` until
`price` or `custom`, where it opens the ceilings or the checklist. On the **providers** row `←`/`→` walk the
chips and `space` turns the one under the cursor off or on — the foot reads
`enter change · space toggle · esc close · ? keys` there and nowhere else — and `space` on
the `+` opens `/connect`, whose `esc` comes back to this row; `enter` opens the providers list, whose foot reads
`space or enter toggle · esc back`. On the **cap** row a digit starts the figure. A click on
a row is `enter`, a click on a provider chip toggles it, and the wheel walks. The models
page has every key (*Crew panel keys*).

All of these are modal: while one is up, every chord except `ctrl+c` belongs to it.
`ctrl+c` does not close the panel — it quits codeaf, with the panel still up.

## Go back to the last conversation — tab

**`tab`, pressed in a conversation with an empty message box, goes to the conversation you
were in before this one.** Press it again and you are back. It is `cd -`.

It **does nothing at all** when there is nowhere to go: one conversation open, or none this
terminal has been in before. The bottom row no longer advertises `tab`, but the key still
works. `alt+k` opens the card of conversations whenever there is another to switch to —
see *Switch between open conversations*.

It works while either conversation is running, over every door: the one you leave keeps
streaming into its own transcript and is all there when you come back.

**On a place, `tab` is the next place instead.** Home, tasks, standing, memory, spend,
search and settings are one circle and `tab` walks it; `shift+tab` walks it back. That is
the same key doing the same kind of thing — going to the next thing of the kind you are
looking at — and it is the only meaning `tab` has while a place is up. See **Places**.

**Everything else that wants `tab` gets it first**, and that is the whole rule rather than a
claim that `tab` is free. In order: a paste bracket makes it a literal tab; the task roster
eats it while it holds the keyboard (`esc` gives the keyboard back first); a box that has
taken the whole keyboard on a place keeps it — the errand pane on home, the value being
edited in settings; the rewind timeline and the inline rewind lift with it; and path
completion takes it over `/attach ` or `/export `. Then, on a place, it is the next place.
Only in a conversation, with none of those claiming it and the box empty, is it the way back.

Two claims on `tab` were withdrawn when the places arrived, and both moved to a key that
points the way they go: **the settings panel** changed its own section with `tab`, and now
uses `←` and `→` alone; **the memory panel** changed shelf with `tab`, and now uses `alt+s`.

The welcome box is the one exception worth naming: **`tab` does not dismiss it**. Every
other key does — that is the box's contract — but switching away is the opposite of
starting work here, so the box is still standing when you come back.

## Switch to another conversation without going home — alt+k, the conversation switcher, switch between my open chats, alt tab between conversations, ctrl+k does not switch any more

**Press `alt+k` to choose a conversation without leaving the one you are reading.**
On a Mac that is `opt+k`. **It was `ctrl+k` until this build**, and that chord is now an edit
in the message box — delete to the end of the line — so the switcher moved one modifier
across. If `opt+k` types a `˚` into your message instead of opening the card, your terminal
is composing accents with Option: see *Why option+left or cmd+left does nothing* above,
which is the same setting.
Press it again, use the arrows, or scroll to move the highlight. The list stays open
while you read its names; a pause never switches chats or dismisses the list.
Moving the pointer over a row highlights that row without changing the keyboard
selection or switching chats. A small dot marks the pointer even without color.
The “more conversations” control also highlights; headings and borders do not.
An outside click dismisses the card without activating anything behind it.
**Enter opens the highlighted row; clicking a row opens that conversation. Escape or
a click outside the card cancels.** A click outside the list acts on nothing.

This holds even when the `quick switch` setting is on. That setting applies only to
`ctrl+tab` on terminals that can send it. Ordinary terminals do not report modifier-key
releases, so opening a chat waits for an explicit choice rather than guessing when you
released Ctrl.

```
╭──────────────────────────────────────────────────────────────────────────────────────────╮
│                                                                                          │
│   open                3 of 12 · enter open · esc cancel · ↑↓ choose · ctrl+w close tab   │
│                                                                                          │
│>  1 ? harness dry run on one publi…  asking you something                  codeaf   4m   │
│   2 ◐ openrouter price scrape        2 tasks running                     research   1d   │
│   3 ○ Refactor the rail scope model  you are here                          codeaf  40m   │
│   → show closed                                                                          │
│                                                                                          │
╰──────────────────────────────────────────────────────────────────────────────────────────╯
```

It works from the **first** session: on a fresh launch you hold one conversation, the card
has that one row, and the fold has the rest of the machine in it.

## What the fold at the foot holds, and the tabs above the conversation

**Everything else on the machine is behind the fold at the foot** — `→ show closed` opens
it and `← hide closed` puts it away again. Most rows down there are conversations this terminal is not
holding at all; the exception is one whose tab you closed, which is still held and still
running and is behind the fold because it is not on the row any more. Taking either kind
puts it in front of you and gives it a tab, exactly as `enter` on home does, and the one
you are in keeps running — over the ordinary engine socket, over `--host`, over `--at` and
under `codeaf chat --no-host` alike. Each conversation holds its own connection, so
opening a second, third or fourth closes nothing and cancels nothing.

**The tabs above a conversation are the same journey with a mouse.** The conversations
this window has been in are drawn there, the one you are in bright and underlined;
clicking one switches to it. **The `Chats ▾` control that used to sit at the row's right
end is deleted** — the legend under the box names this key instead — and what is left there
is a dim `+3` counting the tabs the row could not spell, which does nothing when pressed. The
`×` on a tab dismisses its view, and `ctrl+w` is that `×` on
the tab you are in. See *Conversation tabs* and
*Closing a tab* on the screen page.

## Every key the switcher owns while it is up

| Key | What it does |
| --- | --- |
| `alt+k` (`opt+k`) | Open the list; each further press moves the highlight without switching |
| `ctrl+tab` | Switch immediately when quick switch is on, otherwise browse; requires a terminal that sends it |
| `alt+shift+k` (`opt+K`) | Walk the list backwards. **Every terminal sends it**, unlike the `ctrl+shift+tab` beside it, which needs a terminal that can spell the alias |
| `tab` / `↓` | Down one — cursor only, without switching, and the card stops fading |
| `shift+tab` / `↑` | Up one. Both wrap round at the ends |
| `1`…`9` | On the holding card, go to that row outright — the number is drawn on the rows that have one. While the card is fading, digits are typing and land in your message |
| `enter` / click a row | Open that conversation |
| `→` | Open the fold — every other conversation on this machine |
| `←` | Fold them away again |
| `ctrl+w` | Close the tab of the conversation under the cursor. A row that is not the conversation you are in closes at once with no question, says `tab closed · <the conversation's name>`, and leaves the list for the fold — so a second press closes the next tab. Only the conversation you are in raises `keep running` / `stop work` / `cancel` when it is working. A row below the fold has no tab to close, so nothing happens and nothing is said. See *Closing a tab* on the screen page |
| `esc` | Take it all back: the card goes and you are in the conversation you started from, however many presses ago that was |
| any other key | While the card is fading, it is typing — the card goes and the key lands in your message. On the holding card it puts the card away and is swallowed |

## Where the switcher works — on every place, and what it never refuses

It works **in a conversation and on every place** — home, tasks, standing, memory, spend,
search, settings — because it is drawn over the screen rather than being a screen of its
own. Nothing under it moves by a cell while the card is up.

**Taking a row from a place leaves that place.** The place comes down and you are looking
at the conversation you chose — the same arrival home's own `enter` on a conversation row
makes, and the same one `ctrl+tab` makes where quick switch is on. That holds for a row
below the fold too, which is opened beside the others and then walked into. The one case
that does not move you is a door that refuses: the place stays up with the refusal on its
own line, so you can read it.

**It does nothing on a machine with one conversation on it** — a first run, and nothing
else — and says so by not being there: no card, and the keys row under the box does not
name it. Everywhere else that row reads `alt+e effort · alt+a approvals · alt+k chats · / commands · space space home` — `opt` in place of `alt` on a Mac. Effort and approvals appear only when the session
has those controls. As the frame narrows, controls give way from the left, keeping
`/ commands · space space home`, then `/ commands` on its own.

**Taking a row is never refused for having too many open.** The card draws the first twelve
rows and hands a digit to the first nine; past that the cursor is the way, and home is the
page that shows every conversation you have. Past twelve open, a quiet conversation left
alone for fifteen minutes may be let go of — see *How many conversations can one
terminal hold* on the home page. Work, a question, a draft or news you have not seen
keeps it.

## What the switcher's card looks like — its columns, long chat names, and the show closed foot

Six columns — **number, glyph, name, one clause, project, clock** — which is what makes it
scan: the names all start in the same cell, so you read straight down them. `3 of 12` is
how many tabs are on the row above, of how many conversations this machine has.

**The name column takes whatever room the frame has spare**, up to about sixty cells, so a
wide terminal shows long names whole instead of spending the width on `nothing new`. It
keeps two cells of air before the clause, so a name that fills its column never runs into
the words beside it. A narrow frame gives up the project first, then shortens the clause,
then drops the clock, then the clause — the name is the last thing cut, and it is cut with
a `…` rather than repeated anywhere else.

**The foot is one instruction: `→ show closed`**, and `← hide closed` once the fold is
open. It is there only while there is something behind the fold.

**With the fold open, one dim `closed` marks where the tabs stop.** Everything above that
word has a tab on the row; everything below it does not. It is spaced the way the head's
`open` is — a blank line above it and a blank below, so it reads as a heading over the rows
under it. On a card too short for that it gives up the blanks, then the word itself: the
rows you asked for are what a short card spends its lines on.

**On a terminal of about twenty rows or fewer, `→` can change only the foot.** The card
gets a handful of lines there, and if every one of them is already a tab then the closed
rows are below the scroll line rather than missing — the head's `3 of 7` still counts them,
and `↓` walks down to them. A taller frame draws them straight away. It is not a row: the cursor
skips it and a click on it does nothing. It is not drawn at all while the list is only
tabs.

**The `✕` on a row does not mean the tab is closed.** It means that row will not open, and
it is always beside the reason: `open in another window`, or `that folder is gone`. A
conversation whose tab you closed keeps its ordinary mark — `○`, or `◐` if work is turning
in it — because nothing about it is wrong: it is still held and still running, and it is
below the `closed` word only because it is not on the tab row any more.

**A long name is never read out twice.** The card used to re-wrap the highlighted row's
name in a block under the list; it does not any more. The name gets the room on its own
row instead.

## Why the switcher lists conversations in that order — tab order, where the cursor starts, a closed tab

**The rows are in the same order as the tabs above them** — the leftmost tab is the first
row, so `2` on the card is the second tab on the row. The strip's order is the order you
first entered each conversation and it never re-sorts itself, which means the card does not
either: a conversation stays at the number you last saw it at. The one you are in wears
`you are here` wherever its tab is, rather than being drawn at the end.

**The cursor still opens on the conversation `tab` would go to** — the one you were in
before this one — which is usually not the first row. That is what keeps the common
journey two keys: `alt+k`, `enter`, and you are back where you just were. `↑↓` from there
walk the list in the order you see it.

**The list above the fold IS the tab row.** Close a tab — with its `×`, with `ctrl+w` in
the conversation, or with `ctrl+w` on this card — and the row leaves the list at the same
moment the tab leaves the strip. The conversation is not closed: it is still held, still
running, and behind the fold, where `enter` brings it and its tab back.

The cursor never opens on `you are here`, so `alt+k` `enter` always lands somewhere.

## What did my other chats do while I was away — what each row of the switcher tells you

Every row says **what changed since you last looked**, not what the conversation is about:

| The row says | What happened |
| --- | --- |
| `asking you something` | it is waiting on you — a question or an approval |
| `working` | something is turning in it — a reply, a task node, or a background command — and it wears `◐` |
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

The mark on each row is the mark on that conversation's tab, taken from the same reading.
That includes `you are here`: work in the conversation in front wears `◐`, and a question
there wears `?`, without replacing the note that says where you are.

**The marks, and the one that is not about tabs:** `○` quiet, `◐` working, `?` waiting on
you, and `✕` **this row will not open** — which is the last two clauses in the table above
and nothing to do with a tab being closed. The `✕` a person presses on a tab closes it; the
`✕` on a card row is the card refusing, and the clause beside it always says why. A
conversation whose tab you closed is `○` like any other, because nothing is wrong with it.

The card is **frozen the moment it opens**. A conversation that finishes a turn while you
are looking at the card does not re-rank the list under your finger.

## Switch without pressing enter — quick switch, it goes when I stop pressing, it switched right away

The `quick switch` setting controls **`ctrl+tab`**, where the terminal can send that
chord. It is on by default. Each press switches immediately and shows a brief receipt;
Escape returns to where the burst started. Typing dismisses the receipt and goes into
the new chat. An arrow turns the receipt into a list that waits for Enter.

Turn it off under `/settings`, interface, `quick switch` to make `ctrl+tab` browse too.
**`alt+k` always browses**, regardless of this setting. It never moves the underlying
chat until Enter, a numbered shortcut, or a row click chooses one. Key releases are not
available consistently across terminals, so there is no release-to-commit behavior.

## Why alt+k and not ctrl+k or ctrl+tab or alt+tab — the switcher moved off ctrl+k

**The switcher was `ctrl+k` and is now `alt+k`, because `ctrl+k` is kill-to-the-end-of-the-line
and the message box wanted it back.** `ctrl+u` deletes to the start of the line here and
always has; `ctrl+k` is the other half of that pair in every shell on your machine, and it
could not be that while it opened a card. Every other `ctrl+<letter>` on this surface is
already spent, so the switcher moved rather than the edit.

`alt+k` arrives as escape-then-`k`, which every terminal sends with no setting to turn on
and no protocol to negotiate — **except on a Mac**, where Option composes accents unless
you tell the terminal otherwise and `opt+k` types `˚`. That is the one thing this chord costs
that the old one did not, and it is the same setting every other `alt+` chord in codeaf
needs: *Why option+left or cmd+left does nothing* has the menu path for your terminal.
Nothing else wants `alt+k` — no window manager, no emulator, and no other key in codeaf.

`ctrl+tab` **is** bound — but only on terminals that can send it, and many cannot.

`ctrl+tab` has no distinct encoding in an ordinary terminal: it arrives as a plain `tab` and
is indistinguishable from it. Only a terminal that speaks the kitty keyboard protocol sends
it as itself, and codeaf asks yours on every frame — where the answer is yes, `ctrl+tab`
opens the switcher and `ctrl+shift+tab` walks it back. Where the answer is no, the chord is
not bound and is never named, because a key codeaf tells you about is a key that works.
Two popular terminals — WezTerm and Windows Terminal — also spend `ctrl+tab` on their own
tabs by default, so it would never reach codeaf there.

`alt+tab` is not available at any price: the window manager takes it on Windows and on most
Linux desktops.

**`alt+shift+k` is the reverse, and the move made it better.** It arrives as
escape-then-`K`, a different byte from escape-then-`k`, so **every terminal can send it**.
The old `ctrl+shift+k` could not be told apart from `ctrl+k` and was bound only where the
terminal answered the keyboard query, which was about half of them. With the card down,
`alt+shift+k` opens the ring at its far end — the last row, which is the rightmost tab —
and waits for your choice. **`shift+tab` still walks the card back** once it is up, on
every terminal, and that is the key most hands reach for.

## Close a tab from the switcher — ctrl+w, closing a chat, too many open

**`ctrl+w` closes the tab under the cursor**, which is what the card's own legend
says: `enter open · esc cancel · ↑↓ choose · ctrl+w close tab`. An inactive row leaves
the card open with `tab closed · <title>` and **drops off the list**, behind the fold with
everything else this window is not showing — so pressing it again closes the next tab
rather than the same one, and several go in a row. Its conversation, its work and its
draft are untouched; `→` reaches it and `enter` brings it back.

**Tab closing does not archive the saved conversation.** Home shows up to three
recently closed tabs as dimmed rows. `ctrl+e` or `→`, then `x close`, on Home also
archives the conversation and closes its tab, removing it from the default chats
list. Both routes keep work and drafts. Enter on a dimmed Home row reopens it.

On the row marked `you are here`, the card closes and the window selects the most
recently used remaining tab, or Home when none remain. This is the same action as
`ctrl+w` in the conversation and the tab's `×`, and it asks the same question where that
conversation is working. The work keeps running over every door; selecting another tab
ends nothing. Closing the final tab opens Home, leaving that conversation behind it.

**`ctrl+w` on a row below the fold does nothing, and says nothing.** There is no tab down
there to close — whether this terminal never opened that conversation, or you closed its
tab a moment ago and this is where it went — so the key has already got what it was pressed
for. Nothing is dismissed, nothing joins the `ctrl+shift+t` stack, and the card
stays exactly as it was.

**What actually ends things**: `Stop` on a task's page ends that work, and `/quit` closes
the conversation in front — leaving codeaf when it was the last one this terminal held.

`ctrl+w` is never about making room — it is about what you want on the row. A quiet
conversation left alone can be let go of on its own; this key only takes its tab down.

## `tab` still goes straight to the last one

`tab` on an empty message box has not changed: it goes to the conversation you were in
before this one, in one key, with no card. Use `tab` to flick between two and `alt+k` when
there are more.

The legend above the box names **every door that would act**: `tab last` appears once a
second conversation is open, `alt+k chats` whenever there is anywhere at all to go. On a
place the switcher is named on the map (`alt+.`) instead, because a place's foot is four
fixed clauses the design sets word for word.

## Keys in the composer layer — `alt+enter`, `alt+p`, `alt+o`, and typing a number

On macOS every `alt+` below is drawn `opt+` — `alt+enter` is `opt+enter`, `alt+p` is
`opt+p`, `alt+o` is `opt+o`. Same key, same chord, named the way the keycap names it.

`alt+enter` with something typed into home's box opens the **composer layer**: the page
behind dims, the box stays where it is, and the three facts a task needs appear under it.
It opens from home alone — only home starts things. The places page has the layer in full;
these are its keys.

| Key | What it does |
| --- | --- |
| `alt+enter` | first press opens the layer; second press sends the task off |
| `alt+p` | move the task to the next project codeaf knows, and round again |
| `alt+o` | open the model list for the **execution** slot — what the work runs on |
| a digit, or `.` | type the spend cap; the figure changes as you type |
| `backspace` | take one character off the cap |
| `enter` | talk about it instead — an ordinary conversation carrying the same sentence |
| `esc` | back to the place you were on, sentence still in the box |

`alt+o` changes a model only inside this task layer. On home, `/model` or a press on
the model name opens the draft's model list instead. `alt+p` also works on home to move
the draft to the next project; other places bind neither chord.

While the layer is up it has the whole keyboard: `tab` does not walk to the next place, and
letters do not reach the composer — what you typed is already written and is on the screen
above you. Inside the model list `alt+o` opens, the keys are the model picker's own — type
to filter, `↑↓` to walk, `enter` to use it, `esc` to go back to the layer.

## Keys on home, and is there a shortcut for it

**Press the space bar twice with an empty message box.** That is the way back to home from
inside a conversation, and `/home` opens it too.

**There is also a number: `alt+1` (`opt+1` on a Mac).** Home is the first of the six places on
the top line, `home  teams  chats  sessions  spend  settings`, and each answers to its position there,
`alt+1` through `alt+6`. **`alt+7`, `alt+8` and `alt+9` are kept**, on the three places that
are off the bar — standing, memory and search — so those keys still open a room rather than
doing nothing; `alt+.` draws them all with their numbers. `alt+2` is the teams page, and
`alt+3` is `chats`, the way back to the conversation in front (a new chat when none is open);
it is not a room, so `tab` steps over it. Hold
`alt` and press the digit. On macOS codeaf draws the modifier as `opt+`, after the name on
that keycap; it is the same key and the same chord, and on Linux and on Windows it is drawn
`alt+`. It arrives in every terminal codeaf runs in, which is why the numbers are on `alt`
rather than on `ctrl`.

**`ctrl+1` … `ctrl+9` are a second spelling, on the terminals that can send them.** `ctrl`
and a digit has no encoding in the forty-year-old scheme most terminals speak, so it is not
the first spelling and never will be — but a terminal running the kitty keyboard protocol
sends exactly the keys that scheme cannot spell, and it tells codeaf it does. Where that
report arrives, `ctrl+1` … `ctrl+9` jump to the same eight places and `ctrl+.` draws the same
map, and the map's own line says `alt+1…9 or ctrl+1…9 go to a place` so you can see it is
live. Where it does not, those chords do nothing and are never advertised. kitty, ghostty,
WezTerm, foot and Windows Terminal are the usual ones that report it. **On a Mac this is the
way in that needs no setting at all** — see "Why my option key types ¡ ™ £ instead of
jumping" on the screen page.

**The numbers work from a conversation as well as from a place.** They are the one class of
place key that does: `tab` belongs to the composer's path completion while you are typing,
and the rest of the place grammar — `→` for the row's verbs, `alt+<letter>` for how a place
is shown, `shift+←→↑↓` for its time window — is about the room you are standing in. Every
number opens its room whatever is in it: a place with nothing of its own to draw spends the
frame saying what it is for, and none of the eight is ever a key that does nothing. **On
such a page the line under the box names only the way out** — `tab next place · esc` — on
tasks, on standing orders and on memory alike: a foot that offered `enter` or `type to
filter` over a body with no rows would be naming a key with nothing to act on.

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

**It answers from every place as well as from a conversation.** Wherever a place is
standing, the two spaces are read against that place's own filter — tasks, memory, search —
and open home just as they do from a draft; on spend and standing, which have nothing to
type into, two bare spaces open it and any key between them disarms it. On home itself the
door is a no-op: the page is already open, and two spaces type into home's own filter. It also does not answer from under a layer that owns the
keyboard: on the settings panel space is the drawn verb on a row (`activate`),
memory's card editor keeps every key while it is open, and inside a task's
record — the room the roster opens on `enter` — `space` pages the card the way
`pgdown` and `ctrl+f` do, so the door yields there and the key scrolls. Standing
cannot arm the door — its own keys never type into its box — but the box is the
shared composer, so a space left in it on another place still opens home from
standing.

**A box that looks empty and is not still answers it.** Blank lines left by `ctrl+j`,
`alt+enter`, or by `ctrl+enter`/`shift+enter` on a terminal that cannot send those chords,
draw nothing on the frame — and the gesture reads the box the same way the frame does, so
two spaces open home and the blank lines go with the draft. The rule in one sentence:
wherever the foot advertises `space space home`, two spaces open it.

It works while a turn is running; the answer keeps streaming underneath and `esc` puts you
back in it.

When the box is empty, the keys row under the box says so:
`/ commands · space space home`, after any effort, approvals and chats hints. Clicking
`space space home` opens home; that clause vanishes as soon as you type. A tip never takes
its place: a conversation's tip covers the project at the row's right end instead.

**The door does not ask what the machine holds.** It is open on a machine with only this
conversation and on one with none, from the first minute, and starting a second
conversation with `/new` changes nothing about it. It used to be shut until the launch
found somewhere else to go, and that rule is gone (the home page, *space space does
nothing*).

Once it is open, **home is seven panels in one, two or three columns** (the home page has
what each holds), and its keys are a small grammar:

| Key | On home |
| --- | --- |
| `↑` / `↓` (`ctrl+p` / `ctrl+n`) | walk the field, from one panel into the next, and stop at both ends: `↑` off the top row stays there and does not climb onto the tab bar (reach the bar with a click, `tab`, or a place's chord) |
| `←` / `→` | cross to the next column, onto the row nearest the one you left — only into a column with a row to stand on |
| a digit, or a question's own key | answers **the one row of `needs you` that is drawing its answers**, from anywhere on home, with no cursor move — the row under the cursor when it can take one, the top answerable row otherwise. A question's chips are `1 allow once  2 always  3 deny`; a landing in `unread` offers `1 accept   2 not right`, its own `[a]`/`[n]` being letters and letters always type on home |
| `enter` | acts on the row under the cursor: a conversation opens, a `since you left` line opens its record, file or place, a `standing` row opens standing, a fold line opens or shuts its panel. `projects` and `spend` rows are not stops, so the cursor never reaches them; a click on a project picks the folder for the next message |
| `pgup` / `pgdown` | jump a screenful |
| `tab` | **the next place** on the bar |
| `esc` | clears the box if anything is in it, and closes home otherwise |
| `alt+.` | the map |
| `backspace`, `ctrl+u`, `ctrl+w`, `ctrl+b`, `ctrl+f` | edit the box |
| anything else | goes into the box, which searches the whole machine and offers to start a new conversation at the same time |

**Opening home puts the cursor on the chat you were in before this one**, so `space` `space`
then `enter` is a switch back; a window with only one conversation opens on its own row,
which says `here`. The panel holding the cursor marks its heading with the cursor's ground,
which is how you tell which column your arrows are in. **`alt+g` and `alt+q` are unbound on
home** — there is no list left to group or thin — and so is the answer strip above the box:
the answers are on the `needs you` row itself.

**`←` `→` cross home's columns first**; where no column with rows lies to the right, **`→`
opens the row's verbs** on a strip drawn **directly under that row**, pushing the rest
of the list down by its own height, and while that strip is drawn its letters are the verbs
and the box is asleep — a question's own answer keys and words, `x close`, `n new in project`,
`o open folder`, `p copy project`, `p pause it` or `r resume it` on a standing item. `esc` or
`←` closes it, `enter` still opens the row, and walking off the row closes it too. The arrows
never leave home's field, and on a panel's fold line (`N more`) `enter`
opens the panel and shows the rest; on `N fewer` it folds them again.
While something is typed the two arrows move the caret in the box instead.

**A letter always types**, unless the verb strip that names it is on screen — that visible
strip is the one state where a printable key is a verb, and it is why it has to be drawn.
Everywhere else "make me a site" comes out whole wherever the cursor is resting. The row's
actions otherwise ride chords, which can never begin a word, and they work from any column:
**`ctrl+e` closes the conversation tab** — it leaves the open Home list and the
default chats menu. Up to three closed rows stay dimmed on Home; typing finds older
ones. Enter or `ctrl+e` on a closed row reopens it. **`ctrl+o`** opens
its folder and **`ctrl+y`** copies its path. **`ctrl+t`** on a conversation's row
starts a new one in that row's folder; a click on a row of the `projects` panel instead
picks the folder for the next message sent from home. On a standing item's row — in `needs you` while it asks, in
`standing` otherwise, firing or not — **`ctrl+e` pauses** it, **`ctrl+x` stops it for good**, and
**`alt+e` raises how hard that item thinks** one rung. Each chord acts on the row under
your pointer when there is one, the cursor's row otherwise. The machine's own default is
not on this chord — it is the `thinking` row of `/settings`, and *alt+e — how hard the
thing you are looking at thinks* says why.

With the mouse: a click puts the cursor on a row and a second click on that row opens it.
The wheel walks the list three rows a turn, and the **places on the top line are
controls**: clicking a place's word, or the cell either side of it, goes there, and clicking
the air around the words does nothing.

**Under 60 columns those two clicks are one.** At phone width home is an inbox and a
row's card is a full-frame sheet, so a tap selects and opens in one gesture; the sheet's
top row reads `‹ back` and `esc` or a tap on it returns to the list with the cursor where
it was. The hint line becomes a bar of at most three wide targets — `open · new ·
ask here`, or `‹ back · open · more` on a sheet — and mouse motion is ignored, because
there is no hover on glass. Home's own page has the whole shape.

The box row reads `› type to search or start something new` — the promise itself, both
readings of what you type: a search of everything home shows, or the first message of a
new conversation. (Until 2026-09-17 the box said `› say what you want done` and the
promise opened the foot.) **The rule above it is a legend on home and nowhere else**, and
it says what the box is a draft *for*:
`─ glm-5.3-flash:auto · ◇ asks ───`

— the model, a colon and effort, then approvals at the left. The project the next
conversation opens in is `project: <path>` at the right end of the keys row under the box
(it stood at the rule's right until 2026-09-22). The arrow and effort badge are gone. A long
project path keeps its root and truncates on the right. The chords that change them are on **the line
under the box**, with home's own keys, because the lowest line is for keys on home as in a
conversation: `alt+p project` walks all projects in the projects panel's order, `alt+e effort`
cycles auto → low → medium → high → xhigh → max → auto, and `alt+a approvals`
walks asks → guardian → YOLO → asks. Pressing the project or approvals cell does the
same. The project choice survives starting a conversation and returning home for the
lifetime of this window; the seam alone shows it, with no footer announcement.
`/model`, or pressing the model name, opens home's model list; `alt+o` no longer
does. The model is always bold and bright cyan on both home's and a conversation's seam.
`alt+k chats` opens the conversation switcher and is absent when there is nowhere to go.
**On a Mac these clauses read `opt+p project · opt+e effort · opt+a approvals · opt+k chats`.**
`opt+y` raises the newest pending question, and `opt+w` toggles the folder browser
preview. These exchanged roles with approvals and projects respectively.

With all controls available the resting foot is
`alt+p project · alt+e effort · alt+a approvals · alt+k chats · / commands`.
The project and approvals hints are absent where those controls cannot act. `ctrl+o`
still opens the selected row's folder and `tab` still moves to the next place, but neither
has a hint in home's bottom row. `esc` still closes home; `alt+.` draws the whole map.

Every ordinary grid row keeps the same list keys as the cursor walks. A fold or action
row names its own keys, with the available draft controls before `esc`. For example:
`enter starts a new conversation and sends this · ↑ ask here · ↑↑ pick a match · alt+p project · alt+e effort · alt+a approvals · esc clear`.
Typing a slash command changes the first clause to `enter runs this command`.

**With nothing typed home is the panels**, hanging from the top. **While anything is typed
it is one list, a drop-up**: the action row — `start a new conversation: "…"` — is the LAST
row of the list, with `ask here: "…"` directly above it, both directly above the box, and
the matches rise above the pair **best one first**; the cursor starts on the action row, so
one `↑` reaches `ask here` and a second lands on the strongest match. Clearing the box puts
the panels back. On a frame 136 columns or wider a card about the match under the cursor
stands to the right of the list while you type; at rest there is no card.

**On an `ask here` row** — the `?` rows an errand leaves at the end of the conversation list —
the line under the box reads
`↑↓ move · enter or tab answer this ask here · esc close`. `enter` or `tab` hands the
keyboard to the exchange's pane, where it reads `enter sends a follow-up · tab or esc back to the list`;
`esc` or `tab` hands it back (*Asking from home*).

Home is modal like the panels above: while it is up, every chord except `ctrl+c` belongs
to it. `ctrl+c` does not close home — it quits codeaf, with home still up.

## The tab bar is a row the cursor can stand on — ↑ off the top row, and ←/→ along the words

**On every place, `↑` from the first row of the page lands the cursor on the tab bar**,
the six place words on the top line after the `codeaf` wordmark (seven while you stand in
standing, memory or search, whose word is drawn after the six). The chat tab strip is
not on a place. It is the row under that top line only while a conversation is in front.
The word you are standing in wears the cursor's
band there instead of its usual mark, and five keys mean something on that row:

| Chord | What it does while the cursor is on the bar |
| --- | --- |
| `←` / `→` | walk one word along, wrapping round from either end. **Nothing opens** |
| `enter` | go into the place under the cursor |
| `↓` | the same — go into the place under the cursor |
| `esc` | back into the page, on the row you walked up from. It does **not** close the place |
| `↑` | nothing. The bar is on the top line, and there is nothing above it |

Everything else means exactly what it means everywhere else: `tab` and `shift+tab` are the
next and previous place, `alt+1` … `alt+9` jump, `alt+.` draws the map, and **any printable
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

**While the task roster holds the keyboard** (`alt+t`, `opt+t`): `esc` gives the keyboard
back · `up`/`down` move over the needs-you band, the group headings and the tasks ·
`enter` opens a task's room, opens or folds a group on its heading, and opens a band row's
thing · `left`/`right` switch the column between Tasks and Traffic in a chat in a team ·
`x` stops the task under the cursor · `alt+w` widens the column and narrows it again. Its
hint reads exactly `↑↓ move · enter open · x stop · alt+w wide · esc`, led by
`←→ tasks/traffic · ` in a chat in a team. On a row whose work is still running or still
queued the hint gains one more clause before `esc`, `alt+e think harder`, which moves that
task's thinking rung. On an ordinary finished task, it saves the rung for when you
continue; it does not restart work or rewrite the last attempt.

**Widen is a chord and not the bare letter `w`.** It used to be `w`, and `w` was read
before the message box: a sentence typed while the roster still held the keyboard came out
as `riting the port` and `orktree`. Every bare letter on this surface is either a key on a
modal page with no message box, or an answer to a question drawn on screen, pressed over an
empty box, and widening a column is neither, so it took a chord. The bare `w` still works
on the **full-frame roster** (`alt+t` under 100 columns, where the column is drawn over the
whole frame and there is no message box on screen). The column's own footer says
`alt+w widen · click seam` or `alt+w narrow · click seam`, and clicking the seam (the
column's two leftmost cells) does the same thing with the pointer. Both the offer and the
handle exist only from 120 columns up, which is the only frame that lends the wider
column; narrower than that those two cells belong to the row under them and open its task.

**Folding is the headings' and nothing else's.** `Queued`, `Waiting` and `Done` start folded
to one heading line; `enter` on the heading, or a press on it, opens the group, and it stays
open for the session. `Running` never folds. A task row has no fold of its own: what it
used to fold away (the merge word, the price, the branch) is on the hint line when the
pointer is on it. `←` and `→` no longer fold anything; in a chat in a team they switch the
column's two words, and elsewhere they do nothing.

**The walk stops at this conversation's last job, after its last task.** The roster holds
this conversation's work, then the jobs section under it, so `↓` walks both and clamps at
the bottom rather than carrying on into the project's record. Old tasks from earlier
sessions are on the sessions place, reached from the column's own `ctrl+. earlier` line, from
`ctrl+.` or from `/history`; `enter` on an `earlier` row there goes inside that task's
card. In a directory whose earlier sessions ran tasks but where **this** conversation has
run none and started no jobs, `alt+t` falls through: there is nothing on the column to
put a cursor on. A session that has only started a server still has the jobs section, so
`alt+t` takes it.

**The column's other lines take no cursor.** Its `standing` section, and the two `+` rows
(`+ /task`, `+ /standing`), are the pointer's; the walk skips them. The `jobs` section does
take the cursor: the label, then every job row the column actually drew. Their keyboard
equivalents for standing are the commands themselves: `/standing` opens the standing orders
page, and typing `/task ` is exactly what pressing `+ /task` puts in the box. `enter` on the
`jobs` label toggles the section; `enter` on a job row opens that job's page.

**Under 60 columns the TASKS PAGE this column reaches is a thumb's, not a keyboard's**: the
column itself is unchanged, and its own keys are the ones above. The page's rows become
two-line cards a tap opens, its foot is a `‹ back` bar in place of the key legend
`enter open its room · alt+s sort · type to filter`, and the strip that opens it is one full-width door
(`▸ 3 tasks · 1 running`) rather than a row of chips. Mouse motion is ignored: a tap opens
in one gesture. The tasks page describes the phone flow in full.

**`alt+l` closes the column, and opens it again.** It works from the message box, from
inside a room, and while the roster holds the keyboard: it is the one key here you do not
have to ask for the roster first to use. It is the same key in every chat, whichever word
is in front, and the column's header names it at its right (`opt+l` on a Mac). Closing it
hands the keyboard back to the box. The choice is written to your profile as
`ui.task_column`, so the next session opens the way you left it, and `alt+t` counts as
asking for the column back. `ctrl+g` does the same while no foreground command can be kept;
while one can, `ctrl+g` backgrounds the command instead.

**A closed column leaves a two-column edge down the right of the frame with a `❮` in it,
drawn in ink, and clicking anywhere on that edge opens the column again. Clicking the
`alt+l` at the right of the column's header while it stands closes it**, so the pointer can
go both ways. `alt+l` works with no tasks at all: the column stands with only its header,
its `+ /task` and `+ /standing` doors, and the `ctrl+. earlier` door under them if earlier
sessions ran anything, and either way an empty column is still a column to close. Under 100
columns there is no column beside the conversation, and `alt+l` lays it over the body
instead. On the untouched empty screen there is no column yet; there the key is a first
keystroke like any other: the greeting goes and the key then closes the column it would
just have raised, so a second press brings it back (*The empty screen* page).

**With a room open:** `esc` leaves the room, though a history recall walk is
cancelled first · `enter` steers the node (see *What steering a task looks like on its
page* below) · `ctrl+b` freezes the room's own rows for
copying, not the conversation's · `pgup`/`pgdown` page · `up`/`down` walk your history,
and scroll the page one row only when there is no history to walk. `left` is
deliberately **not** taken here — it falls through to the message box's
back-navigation.

**Inside a program's room** — a task handed to senior-dev — `ctrl+y` turns the page
between the actions it took and its raw calls to its model, `ctrl+o` folds its brief, and
the box sends nothing. While it works the keys row under the box reads
`/stop · x with empty input · esc main · ctrl+y calls`, ending `ctrl+y actions` while the
calls are showing; once it has ended the row is the `ctrl+y` clause alone.

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
while a history walk is on. The keys row under the box reads `x stop` while there
is work here to stop and `↑↓ history` during a walk — it never reads `esc interrupt`
inside a room, because in here that is not what the key does.

**A click inside the room's page does not leave it.** A press that lands on nothing —
a blank row, the gap beside a paragraph, the slack under a short transcript — does
nothing at all, exactly as it does in the conversation. Leaving is `esc` and `←`, and
the pinned header at the top of the page names both: `esc/← main`. That header row is
also a button — press it anywhere along its width and you are back in the conversation
The `Stop` that ends the work is not on that row at all: it is at the right end of the
quiet facts row underneath, so a press aimed at leaving can never end a task.

**Inside a harness design's room, a finished design is answered by the same three digits
the page in the conversation uses**: `1` save it · `2` change it · `3` drop it, with `esc`
for later. There is no second row of chords in the room. It used to have one — `ctrl+k`
saved the design and `ctrl+x` dropped it — and that row is deleted along with the
`enter`/`e`/`esc` grammar that answered the card in the feed. Every other key goes to the
message box, which is where you say what you want changed instead. The saved-shapes pages
describe the answers in full.

**`x` asks to stop the work.** It is taken on the roster's focused row and inside a
room or an adaptive run's page, and only over an empty message box — the moment there
is a sentence in the box it is the letter `x`. When the roster does not hold the keyboard
and exactly one stoppable task row is visible, `x` takes that row straight away, because
there is nothing to choose between; with two or more rows it does nothing until `alt+t`
gives the roster a cursor to aim with. It never stops anything by itself: it raises a
card, and the card is answered below.

The tasks pages describe what rooms and the roster are for.

## See a program's raw calls — ctrl+y on senior-dev's page, the model calls behind its actions

A program's task page — senior-dev's — opens on the actions it took, each under the step
of its process. **`ctrl+y` turns it to the raw calls** it made to its model: what it sent,
what the model answered, which model it was, and the call in flight. `ctrl+y` again turns
it back. It works in the program's room, whichever door opened it — its row, its card, or
the tasks place — and the key row names it: `ctrl+y calls` over the actions, `ctrl+y actions`
over the calls. Every page opens on the actions.

It is a chord, so it never costs a character: the room's box keeps what you typed. It is
not bound on any other task's page.

## Stopping work with `x` — the confirmation card, why the stop card needs enter as well as the number

`x` raises one card above the message box:

```
?  Stop this task?
     Its work halts; the branch it wrote on is kept.
     1  stop it
     2  keep going
   enter take it · esc keep going · ←→ choose
```

The head is the question and nothing else; the sentence under it is the promise —
what stopping does **not** take away.

On an adaptive run's page the head reads `Stop this run?` and the promise is
`In-flight nodes halt; partial results stay.` A harness being designed is a task, so
`x` on its row reaches it like any other — and the promise says what is actually true
of it: `The page it is writing is dropped; nothing was saved.` It has no branch and
wrote no files, so the reassurance about a kept branch would be pointing at nothing.

**The cursor opens on `keep going`.** `left`/`right` move it, `enter` takes the
answer under it, `esc` is `keep going`, and every other key does nothing while the
card is up. A click on either answer's row is that answer, and a click anywhere else
does nothing rather than falling through to the box.

**`1` and `2` move the cursor; they do not answer.** The digit beside an answer walks
the cursor onto that answer and lights its row. `enter` is still what decides, which is
this card's whole rule said in the digits' own grammar.

**Your half-typed message is safe under this card and cannot be sent by it.** `enter`
belongs to the card while it is up; the sentence in the box is exactly where you left
it once you have answered.

**There is no bypass key and no "don't ask me again".** Stopping cannot be undone —
the worker's turn ends where it stands — so the card is always asked, and pressing
`x` again while it is up is a keystroke the card swallows.

**`esc` never stops anything.** It closes the card, then a room, then a page, in that
order.

Nothing is thrown away by stopping: see the tasks page for what a stopped task and a
stopped run keep.

## Deciding about a landed task from the keyboard — accept, not right, tell it

A landing that reads **your call** is a question, and it is put to you on the **question
block** above the message box — the same block every other decision in codeaf arrives on —
so the letters below work wherever you are standing: the conversation, the task's own room,
the `/tasks` page. **Any landing that is your call, at any depth** — a task you asked for,
or a part of one it handed out itself:

| Key | What it does |
| --- | --- |
| `a` | the ask's own yes — take the work; on a card whose branch clashed with yours it reads `resolve it`, which spends one more merge round and takes nothing as done |
| `n` | the ask's own no — `not right`, or `drop it` on a conflict; the task becomes incomplete and keeps its branch, and its dependents still fail because it did not finish |
| `s` | tell it — opens the task's own page with the message box pointed at it. What you type is sent as a correction; **it never answers the question by itself**, so "looks good" typed there does not become an accept |
| `esc` | later — the question folds to the chip in the status line and nothing is answered |

**`d` you decide is not on a landing's row.** The three answers and the way out are the
whole of it, and no key on this surface ever does something that is not drawn on the screen
in front of you. Where a landing draws each answer on a line of its own — the one road that
does, because its `[a]` moves files of yours — `d you decide` is on it. The standing
choice is `task.settle` in `/settings` under Session, and it is the right place for it: a
letter that hands one landing over changes nothing about the next one.

While `task.settle` is `auto` the reason row also reads `codeaf is deciding`; the answers
stay drawn, and pressing one yourself is how you take the question back.

**The words on `a` and `n` change with the question and the keys never do.** A card whose
branch clashed with yours reads `a resolve it · n drop it`; one the check did not pass
reads `a accept anyway`. There is always a third column, `s tell it`.

**`check again` is not offered.** codeaf retries a check that never answered by itself,
on another model, before the card ever appears — so there is nothing left for you to
spend a round on. `l` does nothing here now.

**A key that is not drawn does nothing.** If codeaf has no way to spend an answer — no
merge round behind `resolve it`, for instance — that chip is absent rather than present
and failing, and its letter is absent with it.

**They are held to the same rule `x` is.** The card must be the **selected** one — walk to
it with `↑`/`↓`, which steps through tool calls, proposals and landed cards — and the
message box must be **empty**, with no panel, picker or copy mode up. A letter typed into a
sentence stays a letter, always.

Once answered the letters go away and the receipt every question leaves takes their place,
`✓ <the card's head> → accept · you · 14:02 · c change`: the answer in the card's own
words (`accept`, `not right`, `resolve it`, `drop it`), who decided and when (the questions
page has the shape). The task's report then leads `you took this as done` or
`incomplete — you said it is not finished`. `d` decides nothing yet: the answers stay drawn
and the reason row reads `codeaf is deciding`. The same columns are clickable on the card.
See the tasks page for what each answer does to the work.

**Inside the task's room the same keys need no selection.** The room is the task, so
`a`, `n`, `s` and `d` over an empty message box answer it directly, the answers row stands
at the foot of the page where `this task has finished — say it to main` would otherwise be,
and the hint slot names the same letters while the question stands.
The room and the card are one question: answer in either and both show the receipt.

**And the roster's row answers them too.** With the roster holding the keyboard (`alt+t`)
and the cursor on a row that is **your call**, the hint slot names the same letters in place
of the move keys, and they answer that row's landing without opening its room. Same card,
same answers, same receipt — the column, the card and the room cannot disagree, because
there is one card behind all three.

## The mouse: what you can click

codeaf owns the pointer by default, using all-motion tracking so hover works.

Only the left button acts. A press is resolved in this order:

0. The quick rewind mode (`esc` `esc`), which takes **every** press on the conversation
   while it is up: a click on any transcript row moves the cut to the nearest point at or
   above it, and a click on the `⟲ rewind here` line does the rewind.
1. The settings panel, the task page, home, the rewind timeline, the status deck, or the
   phone tool sheet — each takes **every** press inside its frame, padding included. On the
   task page a press on a row puts the cursor there and the record pane beside the list
   follows it, and a second press on the same row opens it — on a frame too narrow for that
   pane one press opens, as it always did; a press on a section word or on
   empty padding does nothing. Inside an old task's record card, the head row and the foot
   go back to the list and its body is read. On standing, spend and search a press on a
   row opens it on the first press too; on memory it opens a line's card, or folds a
   shelf. On home a click puts the cursor on a row and a second click opens it. On the rewind timeline a click places the pick and a click on the
   point already placed does the rewind.
2. An approval question block, then a connect offer, then a harness offer.
3. The harness panel, the permissions panel, the connections panel — a press on a row
   acts, and a press anywhere else **closes** the list.
4. The row above the message box: an **attachment chip** or a picked harness's chip
   removes it. Neither moves the caret in your draft. The **thinking rung** is not on that
   row any more — it is on the legend above it, where a press walks it one step.
5. The jump-to-latest chip.
6. A stop target: the confirmation card's two answers while it is up, and the `Stop`
   at the right end of a room's **facts row** — the second row of its header, under the
   breadcrumbs. On a phone-width terminal `Stop`'s hit box is three rows tall,
   because a finger is about that wide.
7. A room's **pinned header**, which is the pointer's way back to the conversation.
   The whole row answers, both ends of it, because the row says `esc/← main` and a
   row that named the exits and did nothing when pressed would be dead. The dim
   family lines under it are facts, not doors, and do nothing.
8. Task strip chips, then the rail column, then a proposal's choices row. On a wide
   terminal the strip chip the roster's cursor is on carries a `✕` of its own, and
   pressing it asks to stop that work instead of opening its room. **When the column is
   closed, the two-column edge it leaves at the right of the frame answers here too** —
   a press anywhere on it opens the column again; `alt+l` does that too.
   Within the column, its own lines are asked before its task rows: the header's words
   (the other word brings its view to the front, and the `alt+l` at the right closes the
   column), a needs-you band row (it opens its thing), a group heading (it opens or folds
   the group), a Traffic row, a `+ /task` or `+ /standing` row (it types that command into
   your message box), and a row in the `standing` section (it opens `/standing` with the
   cursor already on that order). Whatever lights under the pointer is exactly what a press
   there does.
9. The figures on the status row: the **money figure** (`$0.14`) and the cache beside it,
   which open the **Spending** tab of `/settings`; and the **context meter**
   (`66.8k/1.3M · 5%`) and the compaction forecast, which print `/status`. Each brightens
   under the pointer over its own cells to say it is a door. The model's name is on the
   line **above** the message box and opens the model picker, and the **thinking rung**
   beside it walks one rung up the ladder; the `◦ N standing orders`
   count is at the foot of the task column and opens the standing orders page. A press
   elsewhere on the status row falls through — the rest of it is readings, not controls.
   On a narrow terminal the whole two-row deck answers.
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
   a landed card, which opens its full context. The answers to a landing that is your
   call are not on that card: they are on the question block above the message box,
   where each of `a accept`, `n not right` and `s tell it` is its own target and a
   press between them does nothing.

**A file path is a different kind of target.** Everything numbered above is a click
codeaf itself answers. A real file path — in a reply, in a note, on a `read`/`edit`/
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
beside it where it is. It moves the column's own window (the header and the needs-you band
stay pinned at the top) unless the column is holding the keyboard (`alt+t`),
in which case the window is already following the cursor and the wheel walks that instead.

Reaching the bottom **re-arms sticking**, so new replies follow along again. Scrolling
up drops out of it.

`pgup` and `pgdown` move the height of the view minus one row, never fewer than one.

**The jump-to-latest chip** is one dim chip at the left edge reading `↓ latest · ctrl+l`
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

**The message box answers the same sweep**, and so does the box at the foot of every
place. The one difference is that a selection there is live — you can type over it —
so it stays lit until you move the caret rather than fading with the status line. See
"how do I select text in the message box" above.

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

codeaf still owns the pointer by default — that is what makes wheel scrolling,
clickable paths and pressable rows work — and there is no scrollback to fall back on,
because codeaf runs in the alternate screen. Two further doors remain for when you
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
press it and then only touch the mouse, nothing codeaf draws will answer a click until you
press a key or press `ctrl+s` again. While the pointer is out, the row under the message
box reads exactly `drag to select · any key ends it`. That line is how you tell this apart
from everything else here.

**The `ui.mouse` setting is off.** It is **on** by default — `/settings`, the Display
section, the row labelled `mouse`. With it off, codeaf never asks your terminal to report
the pointer at all: no hover, no click, no wheel, and your terminal keeps drag-select
permanently. `/select` then says `your terminal already has the pointer — drag to select.`

**Your click drifted.** A press that moves more than two columns or more than one row
before you release is a **sweep**, not a click: it copies the rows it crossed and the
status line says what was copied instead of opening anything. See "selecting text with
your mouse" above.

**There is nothing under the pointer.** A click on empty space does nothing anywhere on
this surface, including the air around the place words on the top line and the blank rows of
a task's page. A click in copy mode acts on nothing at all, because those rows are a frozen
snapshot.

**The terminal is too narrow for the word you are aiming at.** The top line folds its
trailing places into `more ▾` as the frame narrows; click `more ▾` and pick the place from
its menu. `tab`, `shift+tab` and `alt+1`…`alt+9` still go everywhere.

**A file path is your terminal's click, not codeaf's** — usually **cmd+click**
(ctrl+click on Linux). If a plain click on a path does nothing, that is why.

## Copy mode: taking text out of the conversation — ctrl+b, freeze the screen, esc to leave, and ctrl+b on home does nothing of the kind

`ctrl+b` freezes the view and hands the keyboard to a reader, so you can pull text out
of a surface that runs in the alternate screen where your terminal's own selection is
gone. The `/copy` command does the same. Inside a room, `ctrl+b` freezes **the room's
rows** rather than the conversation's.

**On home `ctrl+b` is not copy mode.** It moves the caret in home's box one cell to the
left, as `←` does, and home's rows are never frozen. Nothing can be copied off the home
screen this way: a title or a path on home is on a row `enter` opens, and inside that
conversation the rows can be frozen. (For one build on 2026-09-22 the key froze home's
own screen; it was taken out the same day.)

Freezing looks like nothing when nothing is moving — the rows stay where they are on
purpose. What tells you the freeze is on is the keys row, which reads exactly
`v select · a block · y yank · esc`, the highlighted cursor row, and the status word
`COPY`. `esc` always leaves.

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
A fourth, `alt+e`, carries **one** meaning on several surfaces — move the thinking rung of
the thing you are standing on — and its own section below has the table.

**`ctrl+.` — two meanings, and the two screens can never both be up:**

| Where you are | What it does |
| --- | --- |
| in a conversation | every task this project has run — the same list `/history` opens |
| on a place | draws the key map, exactly as `alt+.` (`opt+.`) does — **only** on terminals that report they can send `ctrl+<digit>` |

A place takes the whole frame, so while one is standing the conversation's keys are not
under it at all. Where your terminal has not reported that it can send `ctrl+.`, the place
reading simply does not exist and the chord does nothing there.

**`ctrl+t` — three meanings:**

| Where | What it does |
|---|---|
| Message box, or the task roster holding the keyboard | Start a new chat — the start page the tab strip's `+` opens. The roster gives the keyboard back on the way |
| Home | Start a fresh conversation **in the folder of the conversation row under the cursor** — a click on a `projects` row picks the folder for the next message instead |
| Model picker only | Cycle the reasoning effort |

It used to hand the keyboard to the task roster everywhere. **That is `alt+t` (`opt+t`) now** —
the same letter under the other modifier, beside the roster's own `alt+w` widen chord. On
macOS, `opt+t` types `†` instead unless your terminal is set to send Option as Meta — see
*getting started*, "Option as meta, on macOS", for the setting and where it lives.

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
- **`ctrl+w` closes a tab wherever you press it**: in a conversation the tab in front,
  and while the **switcher** is up the tab of the conversation under the cursor — the card
  has taken the whole keyboard by then. Neither one puts the conversation away; that is
  `ctrl+e` on home. In the message box the word kill it used to be is `alt+backspace`.
- **`ctrl+k` means one thing now**, and it is an edit: delete to the end of the line, in
  the message box and in every filter box. It opened the conversation switcher until that
  card moved to `alt+k`, and it saved a harness design from inside that design's room until
  the room's approval row was deleted. Neither is bound any more.

And over an **empty** box, `left` and `right` are navigation rather than caret
movement. `ctrl+f` never is — it always moves the caret right.

## alt+e — how hard the thing you are looking at thinks, and making this one task think harder

`alt+e` moves one step up the thinking ladder — `low`, `medium`, `high`, `xhigh`, `max` —
and it moves the rung of **the thing you are standing on**. One chord, three scopes:

| Where you are | What moves |
|---|---|
| The message box, typing or empty | **This conversation's** rung — the one on the legend above the box, beside the model, see *The thinking chip above the message box* |
| The task roster holds the keyboard (`alt+t`) and the cursor is on a task | That task's rung |
| You are inside a task's page | That task's rung |
| Home, with the cursor on a standing item's row — in `needs you` or `standing` | That item's rung |

Everywhere else it does nothing at all. A conversation row on home is deliberately not on
the list: a conversation's rung belongs to the window that conversation is open in, where
the rung on the legend above its message box moves it — by this chord, by a press on it, or
by `/effort`.

**The machine's own default is not one of the scopes.** It used to be — home had a state
where the cursor stood on no row at all and the right-hand side became a card about the
machine, and this chord moved the install's rung from there. That state is gone: `↑` off the
top of the column on home stays on the top row, and there is no machine card. To change how hard this machine thinks by
default, open `/settings` and walk to the **`thinking`** row, which is the setting both
roads always wrote.

**It climbs, and what happens off the top is the scope's own answer.** Each press goes one
rung up. This conversation's rung, a task's rung, and the level `ctrl+t` dials onto one
model in `/model` come back to `auto` off the top — the surface is the only door that sets
any of them, so it has to be the door that clears them, and clearing hands the work back
to whatever stands over it. `/effort auto` and the top row of `/effort` clear the
conversation in one move instead of walking to it. (This conversation's rung wrapped from
`max` back to `low` until 2026-09-15, which left `auto` — the state a fresh install is in
— reachable only by name.) **A standing item's rung is the one that never returns to
"nobody said"**: it wraps from `max` back to `low`, and it is cleared where its rung is
written down.

**The rung reads as a quiet clause where the thing already states its facts.** A task's is
under `Task setup` (or `Next run setup` after it settles) in the expanded
task page, and in the compact header and roster figures when there is room. An item's is on that item's card. The machine's own is the `thinking` row of
`/settings`. A thing nobody has dialled says nothing, which is not the same as `low`.

**On a task it lands on the next call, not this one.** A worker already running keeps the
rung it started with, so the line codeaf writes says so: `task 7 · thinking · high · its
next call takes it`. On an ordinary task that has finished, it instead saves
the choice for the next continuation. The completed attempt stays unchanged.

**Every card that takes it says so.** The card's dim legend reads `alt+e think harder`,
and it is drawn only where the key would work. A window with nowhere to write
the setting does not offer it.

## Chords that are not bound

These do nothing in the v3 chat. If you expect one of them, here is the straight
answer:

| Chord | Status |
|---|---|
| `shift+enter` | **Bound**: opens a new line in the home and conversation message boxes, even while an answer runs |
| `ctrl+shift+enter` | **Bound**, in one state: while a turn is running with something typed, it stops the answer and sends that message. It does **not** open a new line — use `shift+enter`. Over an empty box, or with nothing running, it does nothing |
| `cmd+enter` | **Bound**, in one state: while a turn is running with something typed, it holds that message above the box for the next turn. It does **not** open a new line. Over an empty box, or with nothing running, it does nothing. Needs a terminal that can spell it |
| `ctrl+d` | Not bound |
| `ctrl+v` | Not bound. It used to change effort; use `alt+e` (`opt+e` on macOS) now. Pasting remains the terminal’s shortcut and arrives as pasted text |
| `ctrl+k` | **Bound, in every box**: delete from the caret to the end of the line, the pair to `ctrl+u`. It does not eat the newline. It was the conversation switcher until that moved to `alt+k` (`opt+k` on a Mac) to give this letter back to the message box |
| `alt+k` | **The switcher**: the card of every conversation this terminal has open. On a Mac it is `opt+k`, and it needs "use option as meta" turned on in your terminal — see "Why alt+k and not ctrl+k or ctrl+tab" |
| `ctrl+r` | Bound. In the message box it is **spell it out** — see "Make my prompt better" above — in the `/files` list it opens the folder a file is in, and in the `/model` picker it fetches the newest model list. Nowhere else |
| `alt+e` | **Bound**, on three surfaces: it moves how hard the thing you are standing on thinks — this conversation from the message box, a task, or a standing item on home. The machine's own default is the `thinking` row of `/settings` and is not on this chord. See "The thinking chip above the message box" and "alt+e — how hard the thing you are looking at thinks". Anywhere else it does nothing. On macOS it is shown as `opt+e`; the terminal must send Option as Alt/Meta, as for the other Option shortcuts |
| `ctrl+x` | Bound in three places: it drops a harness design from inside its room; on home it stops a standing item for good; and on a `tasks` row of home that this window holds it asks to stop that task (`ctrl+x stop it` on the `alt+.` map; the foot under a field row is the resting sentence and does not name it). Not bound anywhere else |
| `ctrl+y` | **Bound** on a program's task page: it turns the page between the actions the program took and its raw model calls; on a Home project row and in the `/files` list it copies the path under the cursor. Nowhere else |
| `ctrl+z` | **Bound**: undo in every box, with `ctrl+shift+z` to redo — see "Undo what I typed" |
| `ctrl+<digit>` | **Bound as a second spelling of the place keys, on the terminals that report they can send it.** `ctrl` and a digit has no encoding in the scheme most terminals speak, which is why `alt+1` … `alt+9` (`opt+1` … `opt+9` on a Mac) are the first spelling and always will be, but a terminal running the kitty keyboard protocol sends it and says so, and where that report arrives `ctrl+1` … `ctrl+9` reach the same eight places. The map's line says `alt+1…9 or ctrl+1…9 go to a place` exactly when the alias is live. Where the terminal has said nothing, the chord does nothing and is never drawn |
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

Afterwards it is an ordinary job: ask codeaf to list them, tail one, or kill one,
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
task column, so the column stays where it was. With no such command the key does what
`alt+l` does: it closes the column or brings it back. `ctrl+b` is copy mode and `esc` interrupts; neither changes.

## When a settings change lands

The `ui.mouse` and `ui.timestamps` settings are read at boot and again at the end of
every turn — not the moment you change them. The same is true of the approval posture
and the consent countdown.

So if you turn the mouse off, or turn timestamps on, mid-turn, the change takes hold
**one turn later**. Nothing is wrong; the surface has not re-read the setting yet.

This holds however the row was changed — in the panel, or by asking codeaf to do it with
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

The compact conversation keeps thinking behind its work disclosure. Open that with
`ctrl+e`, then click the thought block to inspect it. Inside the opened work, or on
a task page, models that stream their working get a dim italic block headed
`⠿ thinking · N tok · ctrl+e`.

While it streams, only the **last 3 wrapped lines** show, each painted a step further
along a fade so the newest reads brightest. The moment the turn says anything that is
not reasoning, the block collapses to one row reading
`thought for Ns · N tok · ctrl+e`.

Click anywhere on the thought block to open it. `ctrl+e` over an empty message box
opens the most recent thought only when no whole-work disclosure takes priority.
Opening **latches** your choice during streaming. An opened live block shows the
whole buffer, not the 3-line window. Completion still folds the whole turn’s work.

An expanded block is capped at **200 rows**, and says how much is held back. The token
count is an estimate at 4 bytes per token.

**Reasoning is never written to the session file.** A resumed conversation shows the
answers, not the thinking.

## Why did codeaf add a [silent] note while tools were running?

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

## Why did codeaf stop running tool calls, and what is a [held] answer?

Because the note it asked for twice was never written. The `[silent]` note at 6 replies is
advice. The one at 12 is the last, and it says outright `from here your tool calls are
held`. From that point the loop stops running a reply that carries **only** tool calls: each
call is answered with `[held] Nothing was run this step…` in place of its result, nothing
reaches your files or your shell, and the answer says what to write and that the next call
runs as soon as it is there.

**A rule the loop can enforce is not a suggestion.** The reasoning between steps is not
saved anywhere — a task's room, its checker, its parent and you all read what was written
down — so past a certain amount of silence codeaf stops asking and starts holding.

**Writing anything visible clears it immediately** and the loop is back to normal, at the
first rung again. A reply that carries both a note and tool calls was never held in the
first place, so a model that writes as it works never sees any of this.

**After 3 held replies the turn stops.** If it keeps sending only tool calls, the turn ends
with this line rather than arguing forever:

`stopped here · would not write its notes down, so what this turn worked out is not on the record`

Nothing is handed to a task — the same model under the same rule would be as quiet — and
whatever the turn had already saved is on disk where it left it.

**When the turn that stopped belongs to a task worker, the task says so.** Its row on the
rail reads `incomplete · would not write its notes down`, drawn **dim** rather than in the
bad colour — nothing was found wrong with the work — with `branch kept` beside it as a fact
of its own. How tasks run has it under "What the words under a stopped task mean".

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

In an interactive conversation, codeaf uses the same checkpoint hand-off as any other
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

Flush-left text is said to you: your messages and codeaf's trailing answer. Text with a
two-column gutter is work done on your behalf: thinking, tool calls and their details or
results, and assistant text that was followed by another call. Below 60 columns the
gutter disappears and the dim treatment carries the same distinction.

Indented reply text is also **greyer** than the answer, and carries no markdown — no
bold, no headings, no code colouring. See "Why is part of the reply grey, and where is
the actual answer" on the screen page.

## How do I see what codeaf did — see codeaf's work and tool calls behind the answer

This is the answer to "how do I see what codeaf did?", "what did codeaf just do",
"show me the work behind that answer", "see the tool calls it ran", and "what happened
during that turn" — the finished work is folded, and one gesture opens it.

When a successful turn has work and a trailing answer, the finished work collapses to
one indented chip between your message and the answer, such as
`▸ worked 47s · thought 6s · 6 tool calls · ctrl+e`. Its figures are the whole turn's
elapsed time, the thinking block's time when there was one, and the real call count.
There is a blank row between the chip and the answer under it.

**A turn you stopped with `esc` says so instead**, and it collapses whole:
`▸ stopped by you at 40s · 4 tool calls · ctrl+e`, with nothing left standing under it.
A stopped turn never reached an answer, so there is no answer to leave out of the chip —
that is the point of the wording. codeaf's own lines about the stop (`· stopped`, and
what it dropped from the queue) stay outside the chip where you can read them.

Click the chip or press `ctrl+e` over an empty message box to open or close it. There is
no transcript cursor, so the key first chooses the running conversation’s compact
steps, then the latest completed turn’s work. The running view shows up to three
wrapped rows of recent captions, with the newest live step shimmering. Opening
shows the existing outline: click a caption to inspect its calls, or click the
whole-work door to return to the compact steps. Completion collapses work opened
during the turn. The newest caption may exceed three rows on a narrow frame so
its words remain intact.
Questions, approval prompts, failure lines, text-only turns, and work with no trailing
answer are never hidden — nor is a second message you sent into a running turn, which
ends the chip above it and starts a new one. Fold state belongs to this window; resumed
sessions derive fresh closed chips from their saved entries.

**A task's page has chips too, cut differently.** The conversation folds by turn — one
chip per question you asked. A task's page is one long turn, so it folds by **phase**
instead: the work that came before each paragraph the task wrote goes behind its own
chip, and every paragraph stays standing. The work it is doing right now never folds.
`ctrl+e` opens the newest chip there, a click opens any of them, and scrolling up when
the page is already at its top opens the one nearest the top. `ui.work = open` opens them
all, exactly as it does out here. Where a task's page has no chip yet, `ctrl+e` falls
through to the thinking block as before.

**A node's transcript inside an adaptive run's page never folds.** You got there by
asking to see what that one node did, and the page draws the tail of its journal under
the graph — so there is nothing there to fold away.

## How do I keep everything expanded?

Set `ui.work` to `open` to keep completed work visible. `fold` is the default. The row
live-applies on the next render; work stays indented in either mode.

## Things this page does not cover

- **Slash commands** — what `/attach`, `/export`, `/select`, `/copy`, `/model`,
  `/resume`, `/permissions` and the rest do: the commands page.
- **The status line, the legend under the box, and the layout**: the screen page.
- **Tasks, rooms, proposals and the roster**: the tasks pages.
- **Rewind**, which `esc` `esc` opens quick and `/rewind` opens whole: the sessions and
  rewind page.

## Starting a new chat with plus

The `+` at the right of the conversation tabs opens a **New chat** start page. Type a first message or choose a recent conversation. Opening the page creates nothing. `esc` or its tab's close control returns to your previous chat or task with its draft. The first message creates the conversation. A draft parked on the start page stays for this window's lifetime. See *Starting a new chat with the `+` plus button beside the tabs* on the screen page.

## Reopen a tab you closed — ctrl+shift+t, undo close tab, get that chat back

**`ctrl+shift+t` puts the last tab you shut back**, and pressing it again walks
further back through the ones before it, newest first. It is the third key of the
same grammar: `ctrl+t` opens a tab, `ctrl+w` shuts one, `ctrl+shift+t` undoes the
shutting.

A held conversation returns with its unsent sentence, caret, attachments and reading
state — including one you left with `keep running`, which comes back with everything it
did while its tab was off the row. A remembered one is resumed on a connection of its own,
which ends nothing that is already open. It never creates a new conversation.
If reopening fails, the reason is shown and the entry stays first in line for retry.

**It works from home**, which is where shutting your last tab leaves you — that is
the press this key most often undoes.

What it will not do:

- **A tab you already brought back yourself is skipped.** Reopening a conversation
  from `alt+k` or from home takes it off the list, so the key moves on to the one
  under it rather than spending a press on the tab in front of you.
- **The same conversation is never on the list twice.** Shut it, reopen it, shut it
  again, and it is still one entry.
- **The New chat page is not on the list.** Nothing was created there, so there is
  nothing to come back to — and the first message you had half typed was parked
  when the page closed. `ctrl+t` opens the page again and hands it straight back.
- With nothing shut, the key does nothing at all, and it never types into your
  message box.

The window remembers the last **32** tabs you shut, which is as many as the tab row
itself remembers, and it remembers them only for as long as the window is open.

**Your terminal has to be able to send the key.** codeaf answers the event spelled
`ctrl+shift+t`. Whether it arrives distinctly depends on the terminal and its
keyboard configuration. A terminal that collapses it to `ctrl+t` sends that instead, and **you get a
new chat** — the plain chord is never read as a reopen, because a key that opened a
tab on one terminal and reopened another on the next is a key nobody could predict.
There is nothing to turn on inside codeaf, and the only test is pressing it.

## Switching chats while an account asks for a key

Ctrl+W offers Keep running, Stop work and Cancel while an account asks for typed
input, just as it does during tool permission. `alt+k` opens the chats card and `ctrl+t`
opens another conversation without answering the question. Cancel leaves the
pending input untouched. Stop work cancels this conversation's pending question;
reopening it does not restart the work.

## What does alt+y do — reopen pending questions

`alt+y` (`opt+y` on a Mac) brings the newest open question back from any page and
returns to its conversation. It leaves approvals alone. `alt+a` (`opt+a`) now cycles
approvals; the two shortcuts exchanged roles. A question with no answer remains open
when you put it off with `esc`.

## Get to my other conversation without going home — alt+k chats

Press `alt+k` (`opt+k` on macOS) to open the chats menu from a conversation.
It lists the same open tabs as Home, in the tab strip's order. Select one and press
Enter. Closed conversations are behind the menu's fold; Home also keeps up to
three recently closed conversations dimmed below the open list.
