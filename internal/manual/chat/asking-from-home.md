# Asking from home — reminders and watches without opening a conversation

## Can I set a reminder from home

Yes. Type it on the home screen, press `↑` once — which lands on the row spelled
`ask here: "…"` — and press `enter`.

```
 ? ask here: "remind me at 6 to leave"
 + start a new conversation: "remind me at 6 to leave"
 ─ → new conversation in ~/aforge-v2 · glm-5.3-flash ────────── alt+w folder · alt+o model ─
 › remind me at 6 to leave
 enter starts a new conversation and sends this · ↑ ask here · ↑↑ pick a match · esc clear
```

What you get is **a row at the top of home's list and a pane holding the exchange**. The row
stays there — with what the errand is doing written in its tail — until the errand is
finished and you have read what it came to. The pane is the exchange itself: what you said,
the reply as it streams, one line per tool call, and the card when one arrives. On an
ordinary width the pane takes the frame while it has the keyboard, and the list comes back
with `esc`.

The cursor lands on the new row and the keyboard goes into the pane, so you can answer
straight away. `tab` or `esc` puts the keyboard back on the column; the row stays, and `→`
on it takes the keyboard back in.

## The exchange row on home — working, waiting on you, stood

Every `ask here` is one row, marked `?`, named with the first line of what you asked:

```
 ? remind me at 6 to leave                          ? waiting on you
 ? what did we decide about pricing                 ⠹ working · 4s
 ? tell me when CI goes red                         ∙ stood
```

- **`⠹ working · 4s`** — a turn is in flight. The spinner turns and the clock counts up.
- **`? waiting on you`** — a card is up and nobody has answered it. It is the only tail
  drawn in the bright ink, and it is the same `?` a conversation stopped on a question
  wears, because it is the same fact: this row costs one keystroke to unblock.
- **`∙ stood`** — something now stands because of it.
- **`∙ answered`** — it finished and nothing standing came of it.

The rows sort the way everything else on this screen sorts: **what wants you first, then
what is moving, then what is done**. They sit at the very **top of home's one list**, above
everything the machine has to say for itself — an errand is a thing you asked for a minute
ago.

`enter` or `→` on the row hands the keyboard to the pane. The hint under the box says so:
`↑↓ move · enter or tab answer this ask here · esc close`. (`tab` on the row was the way in
until the places arrived and took that key for the next place; `→` points at the column the
pane is drawn in, which is where the gesture went.)

## How do I know ask here is doing something — the spinner, the clock and the live strip

While a turn is running the pane says what is happening, in the words the conversation
itself uses, and it says it for the first turn and for every follow-up alike:

```
 ⠹ thinking · 4s              nothing has come back yet
 ⠹ writing · 12s              the reply is streaming
 ⠹ running · 12s              a tool call is executing
   ⠹ bash · go test ./...
   read · internal/session/loop.go · 0.3s
```

The first line is the **state and the clock**: `thinking` before the first token, `writing`
once the reply is coming, `running` while a call is out. The clock is the age of this turn
and it appears after one second.

Under it is the **live strip**: at most **two lines**, always the newest two things that
have happened this turn, scrolling as they arrive — a call starting, a call finishing with
its own time, and the growing tail of the reply so a long answer visibly moves. A call that
is running carries the braille spinner; one that succeeded carries nothing at all, which is
how the conversation draws a finished call; one that failed carries `✕` and what went wrong.

**The whole block disappears when the turn ends.** It is a window onto the moment, not a
second copy of the transcript — everything in it is already a row above it.

A tool row says what the call is pointed at and never says `unknown`: `bash` shows the first
line of the command, `read` shows the path, `stand` shows what it is doing
(`stand · proposing remind me at 6 to leave`). A call that came back with nothing to say
says nothing.

## Why is the answer from home showing asterisks and hashes — the ask here pane formats markdown

Yes. A settled answer in the `ask here` pane goes through **the same markdown renderer the
conversation uses** — one parser, one set of colours, one answer to what your terminal can
draw. You never see `**bold**`, a leading `##`, or backticks around `aforge status`; you see
bold, a heading and a code span.

```
Two reminders

You have two of them. Run aforge status to see them:

· one at 6
· one at 9
```

**While the reply is still arriving it is plain wrapped text**, and that is the transcript's
behaviour rather than a difference: formatting a half-written sentence re-flows it under
your eye. The whole block formats the instant the turn is over. In a conversation the same
thing happens in two steps, because an answer there is long enough to be worth catching up
every 1500ms; the pane's answers are short, so the block simply settles.

The pane is under sixty columns, which is the **phone tier** of the renderer: a fenced code
block wraps rather than being cut, with a dim `↳ ` on each continued row, and a table stacks
as one `key: value` line per cell instead of being squeezed. That is the only difference
between an answer read here and the same answer read in the conversation.

What it does not do is give you the affordances that hang off a rendered answer in the
conversation — the offer to open a wide table, task references becoming links, an image.
Those are the conversation's screen; take the exchange there with `continue as a
conversation` if you need them.

## Why did my reminder card disappear when I opened another chat — it does not any more

It used to, and that was a defect. The exchange lived on the home screen, so closing home —
which is what opening another conversation does — closed the errand's session with it. The
card you had not answered yet was answered for you: the engine recorded
`the card was left unanswered — nothing was set up`.

**An exchange now outlives the screen it was asked on.** Closing home does not touch it. Nor
does opening another conversation, walking the list, looking at a different project, or
letting the card sit there overnight. There is **no clock on the card** and never was: it
waits while you read it, and while you do anything else.

Open home again and the row is where you left it, still `? waiting on you`, still
answerable with `1`, `2` or `3`. The only two things that end an exchange are described
under *When does an ask here exchange go away* below.

## Can I ask two things from home at once — yes, one row each

Yes. A second `ask here` **adds** an exchange; it does not replace the first. Each one gets
its own folder, its own session and its own row, and they sort against each other by what
they are doing — the one holding a card sits above the one still working.

The pane on the right is always **about the row under the cursor**. Walk onto another
exchange and you get that exchange; walk onto a conversation and you get that
conversation's ordinary preview card. Nothing takes the column hostage.

## When does an ask here exchange go away

Never while it is working, and never while it is waiting on you.

An exchange is filed when **all three** of these are true:

1. **it is over** — the card was answered (`stood`, `once`, or declined), or there was no
   card and the turn finished;
2. **you have seen it that way** — its pane has been drawn at least once since it settled,
   so what it came to was on your screen before its row went;
3. **you have moved off it** — the cursor is on some other row.

Filing means the session is closed and the folder is moved where it belongs. Nothing is
deleted. An exchange that made something standing leaves an ordinary item row on the same
screen, under the same project, so the row going is not the fact going.

**Quitting aforge files every open exchange**, including one that is still working — the
session is interrupted and closed, and a stood one's folder still reaches the item it made.

**The reminder itself does not fire into the pane.** The exchange is a pane on a screen you
close, not a room you sit in, so it is never a delivery target: when the reminder goes off it
lands in whichever conversation of that project you have open — the chat you are actually
working in. If none is open, the news waits under that project on home as
`◆ N things since you left`, and the next conversation you open in that project folds it into
its own `while you were away`. The full order is on the keeping-an-eye page under "Where a
reminder arrives".


## How do I get back to the list from ask here — tab, esc, → and clicking a row

While the cursor is on an exchange row, home has **two zones**: the list, and the pane the
exchange is drawn in. Exactly one of them has the keyboard, and there are five ways to move
it:

- **`tab` inside the pane** hands the keyboard back to the list, from any state the pane is
  in; **`→` on the exchange's row** takes it back in. (Both directions used to be `tab`; a
  place's `tab` is the way to the next place, and a box that has taken the keyboard keeps
  its own keys, which is why one half of the toggle stayed and the other moved to the arrow
  that points at the pane.) It works from
  `continue as a conversation` row, an open card. It never loses what is in the pane.
- **`esc`** in the pane hands the keyboard to the list. One layer at a time: if you have
  half a follow-up typed, the first `esc` clears that and the second one leaves.
- **`enter`** on the exchange's row in the list hands the keyboard to its pane.
- **clicking** puts the keyboard where the pointer is. A click on a list row selects that
  row *and* takes the keyboard to the column; a click anywhere in the pane brings it back.
- **answering `1`** on a card hands it back by itself. The thing you asked for is being
  made, and the list is where you go next.

With the keyboard on the list, `↑`/`↓`, `ctrl+p`/`ctrl+n` and `pgup`/`pgdown` all walk the
column exactly as they do with no exchange on screen, and `enter` opens the row under the
cursor. **The pane follows the cursor** — walk off the exchange and the row you land on
draws its own card again, where the frame is wide enough to have one.

While the pane has the keyboard the hint reads `enter sends a follow-up · tab or esc back to
the list`, with the card's own answers in front of it when a card is up and
`↓ continue as a conversation` after it when that row is on screen.

**The answers in that line are the chips the card actually drew, and never one more.** A
card that offers all three reads `1 yes · 2 change when or where · 3 just once · 0 no`; a
one-off reminder's card, which has no `just once` to give, reads
`1 yes · 2 change when or where · 0 no`. The line is built from the row of chips rather
than written out, so it cannot name a digit that would do nothing.

`continue as a conversation` is reached with `↓` inside the pane and left again with `↑`,
`tab` or `esc`. It also lights up under the pointer and takes one click.

## Ask here on a narrow window — the exchange takes the whole screen

On a terminal too narrow for two columns — **under 136 columns** — home has no right-hand
pane to put an exchange in. It does not refuse. The two
zones are **stacked** instead of sat side by side:

- the **list** is the screen until you enter an exchange;
- the **exchange** is the screen while it holds the keyboard — the same pane, the same card,
  the same live strip, drawn at the full width;
- **`esc`** or **`tab`** brings the list back, with the exchange's row still on it wearing
  its tail, and **`enter`** on that row opens it again.

`ask here` itself opens straight into the stacked pane, because it selects the new row and
gives it the keyboard. Everything answers the same keys it answers on a wide frame.

Resizing between the two shapes costs nothing: it is the same exchange and the same
keyboard, drawn in whichever geometry fits. Drag a window narrow with the pane open and the
pane fills the frame; drag it wide again and it goes back beside the list.

## Why can't I click a row while asking — you can, and it selects it

You can, and it does. A click on any row of the left column puts the cursor on it and gives
the keyboard to the column, so the next `↑` or `↓` walks from there. A second click on the
same row opens it, which is home's ordinary two-step — one click that switched conversations
would make a mis-aimed pointer close the session you are in.

A click on a card's chip in the pane answers the card, the same way clicking one answers it
in a conversation. A press anywhere on the row of chips counts as that row's, so missing the
gap between two answers costs nothing.

**There is a chord for it and the foot does not name it.** `ctrl+enter` is still bound to
`ask here`, and it only reaches aforge on a terminal that can tell it apart from a plain
`enter` — the kitty keyboard protocol, Windows terminals. `alt+enter` is **not** a second
spelling of it: on home as on every place, that chord opens the composer layer and sends
what you typed off as a **task** (the places page). So the arrow is the gesture the foot
names, because the arrow is the one every terminal has. This page used to say `alt+enter` was
bound to the same thing, and it was not.

If this window was launched with no way to open a second session, the row refuses in one
line: `this window cannot ask from home`, and nothing is created. The window that gets that
line is one attached to another machine — `--host` or `--at`: the errand's folder and the
standing store live on the machine that runs the errand, and a laptop cannot make either of
them on a server's disk. An ordinary `aforge` in a folder asks from home whether or not this
project's engine is holding the conversation, because that engine is a process on the same
machine as the folder.

## What is ask here — and how is it different from starting a conversation

They send the same words to the same kind of model. The difference is **what is left behind
afterwards**.

- `start a new conversation` opens a session in this project. It gets a folder under
  `~/.aforge/v3/projects/`, a row on home, and it stays on that list.
- `ask here` opens a session too — a real one, with a real transcript — but its folder is
  made under `~/.aforge/v3/standing/exchanges/` instead. Home lists what is under
  `projects/`, so an errand that is finished can never fill up the screen it was typed at.

It is not an unstored chat. The record is the point: "why did I get this reminder?" has to
be able to open the conversation that made it.

Which project the errand belongs to is the project **under the cursor** — walk `↑` onto one
of its rows and press `enter` on the `ask here` row to say "this one". With the cursor still
on the typing rows it is the project this window is in, and the home directory `~` when this
window is in no project at all. A reminder belongs to no repository; a watch on CI belongs to one. Its
row is drawn at the top of the list whichever project it ended up in, and the project it
belongs to is what the errand's own record says.

## How do I answer the card, or say no to it — 1 yes, 2 change when or where, 3 just once, 0 no

When the exchange gets far enough to propose something that keeps working, a card appears in
the pane with your own words, when it would wake, and what it would cost per run. Nothing is
created until you answer it:

- `1` — yes. It stands as proposed, and the keyboard goes back to the list.
- `2` — change it. The pane says `type the change and press enter`; write the correction in
  your own words ("make it 8pm", "every weekday") and the model proposes again. Nothing is
  created by a change.
- `3` — once. The action runs now and nothing standing is created. **Not every card offers
  it**: a one-off reminder draws no `3 just once` chip, because doing "remind me at six"
  now says the wrong thing hours early. The hint under the box names the digit only where
  the chip is on the card.
- `0` — no. Nothing is created and nothing is run, the card settles as `not set up`, and the
  keyboard goes back to the list. **This is the only way to say no in this pane**: `esc` here
  hands the keyboard back to the list without answering anything, and a card left standing on
  the column is not an answer. It is the same key on home's answer row and in a conversation,
  where `esc` also declines.

Those four answers are the only four, and a card draws three of them where `3` is not one
it can offer. There is no default: a card nobody answers creates
nothing — and nothing answers it for you. **There is no clock on it.** It waits, and its row
on home says `? waiting on you` for as long as it does.

**The card stays after you answer it.** It does not disappear — it settles in place, greys
out, and its bottom edge carries what was decided in the same words a card in a conversation
uses: `yes, set it up · set up`, `just once · done now, nothing kept`,
`change when or where · you asked for something different`,
`not set up`, `ended · nothing was set up`. The chips go, so `1`, `2`, `3` and `0` are
ordinary characters again and can be typed into a follow-up. The only card that ever replaces it is the new one the
model sends after `2 change when or where`.

The digits belong to a card only while it is still a question. With no card up, or with an
answered one on screen, `2` in the middle of "make it 2pm" is just a `2`.

## Where did that exchange go — the folder, at every stage

There is one folder and it only ever moves. Nothing here copies a transcript and nothing
here deletes one.

| What happened | Where the folder is |
| --- | --- |
| you asked | `~/.aforge/v3/standing/exchanges/<id>/transcript.jsonl` |
| something now stands | `~/.aforge/v3/standing/<item id>/exchange/`, **when the exchange is filed** |
| you continued it as a conversation | the project's own folder, with a `meta.json` |
| it came to nothing | it stays in `exchanges/`, and the sweep clears it after 7 days |

When something stands, the exchange is filed **under the thing it made** — that is what
makes "why did I get this?" a door — and the pane says `kept · this exchange is filed under
it`.

**The move waits until the exchange is finished with.** The news that something now stands
arrives while the turn that made it is still running, so the folder is not touched then:
aforge remembers where it belongs, and moves it when the exchange is filed — after you have
seen it settled and walked off its row, or when you quit — and after the session has been
closed. Until then **the exchange is still alive**: `→` back into the pane and a follow-up
goes to the same conversation. Nothing is copied and nothing is deleted; the folder only
ever moves, once.

Changing what now stands is still a card of its own — pause it with `p` or stop it with `s`
on its row on home.

## Turn it into a conversation — continue as a conversation

Once the first reply has landed, a row appears under the exchange:
`+ continue as a conversation`. Press `↓` to reach it and `enter` to take it.

That moves the same folder into this project's own directory, writes the `meta.json` a
picker reads, closes home, and opens the conversation — with everything that was already
said in it. It is the same conversation, filed differently; nothing is replayed and nothing
is lost. The exchange's row on home goes with it, because the conversation now has a row of
its own.

It is reached with `↓` inside the pane, and it also **lights up under the pointer and takes
one click**.

It is offered only while the exchange is still an errand. Once something stands, the folder
belongs to that thing and the pane says `this exchange is already a conversation` rather
than moving it a second time.

## What it will not do

- **It will not put the errand on the list as a conversation.** It gets an exchange row
  while it is live, and that row goes when the errand is finished and read. If you want a
  conversation on the list, use `continue as a conversation`.
- **It will not deliver the firing back into the pane.** News from something you set up here
  goes to a chat you have open in that project, or waits under the project on home. Nothing
  is ever written only into the exchange's own folder, which no screen reads.
- **It will not run the full conversation surface in the pane.** The right pane is forty
  cells wide, so it draws a reduced reading: what you said, what came back **rendered as
  markdown**, one line per tool call, the live strip, and the card. Slash commands, the task
  column, `/rewind`, images and the approval card are all the conversation's own screen —
  take the exchange there with `continue as a conversation` if you need them.
- **It will not create anything without the card.** Typing a sentence at home does not arm
  a reminder; a card does, and only after you press `1`.
- **It will not end an errand because you looked away.** Closing home, opening another
  conversation and quitting a *different* window all leave it running.
- **It will not work over `--host`.** Home lists the far machine's projects there, but an
  errand is a short conversation this surface opens **on this computer**, in a folder of its
  own beside the projects — and over a connection that is the wrong machine to run the work
  on. So the row is not offered. Everything else on a remote home works.
