# Asking from home — reminders and watches without opening a conversation

## Can I set a reminder from home

Yes. Type it on the home screen, press `↑` once — which lands on the row spelled
`ask here: "…"` — and press `enter`. On a terminal that can tell `ctrl+enter` from a plain
`enter`, `ctrl+enter` asks it without moving the cursor.

```
 ? ask here: "remind me at 6 to leave"
 + start a new conversation: "remind me at 6 to leave"
 ─ glm-5.3-flash:auto · ◇ asks ──────────────────────────────────────────────
 › remind me at 6 to leave
 enter starts a new conversation and sends this · ↑ ask here · ↑↑ pick a match · esc clear
```

What you get is **a row on home and a pane holding the exchange** — a short conversation of
its own, headed `ask here`, that is not added to your conversation list. The pane is what
you said, the reply as it streams, one line per tool call, and the automation's card when
one arrives — `wants to remind you`, answered right there with `1 Save`, `0 Don't save` or
`o Change…`. Once it is saved the pane says `saved · /automations lists it`, and from then
on it is an ordinary automation (*Automations*).

Scheduled work ("every Monday at 9, draft the weekly update") and watches ("tell me when CI
on main goes red") are asked the same way, and so is a plain question: an exchange that
sets nothing up simply answers.

The cursor lands on the new row and the keyboard goes into the pane, so you can answer
straight away. On home at rest the pane takes the whole screen while it has the keyboard;
`tab` or `esc` hands the keyboard back and the list returns with the row on it, and `enter`
or `→` on the row goes back in.

## What is ask here — and how is it different from starting a conversation

They send the same words to the same kind of model. The difference is **what is left behind
afterwards**.

- `start a new conversation` opens a session in this project. It gets a folder under
  `~/.codeaf/v3/projects/`, a row on home's conversation list, and it stays on that list.
- `ask here` opens a session too — a real one, with a real transcript — but its folder is
  made under `~/.codeaf/v3/errands/` instead. Home lists what is under `projects/`, so an
  errand that is finished can never fill up the screen it was typed at.

It is not an unstored chat. The record is the point: an automation saved from an exchange
names that exchange as where it was asked, so `→` then `o` — `open where it was asked` — on
its row in `/automations` opens this transcript, and "why did I get this reminder?" has an
answer.

The exchange talks on the model this window was launched with, under your approval rules as
they stand when you ask, and works in the project it belongs to (*Which project an ask here
belongs to*). The task composer's second `alt+enter` sends a sentence through this same
door, with the project, model and spending cap the layer settled (*Places*, the composer
layer).

## Which project an ask here belongs to — where its automation runs

The project **under the cursor**. While you type, walk `↑` past `ask here` onto one of a
project's rows among the matches and press `ctrl+enter`: the errand belongs to that project.
With the cursor on the typing rows — `ask here` itself, or `start a new conversation` — it
is the project this window is in, and your home folder `~` when the window is in no project
at all. The composer layer's `alt+p` chooses one too.

That project is where the exchange works, and where an automation saved from it runs —
exactly as one said in a conversation runs in that conversation's project. A reminder
belongs to no repository; a watch on CI belongs to one.

## The exchange row on home — working, waiting on you, answered, saved

Every `ask here` is one row, marked `?`, named with the first line of what you asked, and
saying at its right what it is doing:

```
 ? remind me at 6 to leave                          ? waiting on you
 ? what did we decide about pricing                 ⠹ working · 4s
 ? tell me when CI goes red                         ✓ saved
```

- **`working`** — a turn is in flight, with its clock.
- **`waiting on you`** — a card is up and nobody has answered it. It is the only tail drawn
  in amber, and it is the same `?` a conversation stopped on a question wears: this row
  costs one keystroke to unblock.
- **`saved`** — an automation was saved from it.
- **`answered`** — it is over and nothing was saved: a plain answer, or a card you declined.

On home at rest the rows are a short list of their own in the left column, under the
conversations, sorted **what wants you first, then what is moving, then what is done**.
While you type, a row is drawn among the matches, in the block of the project it belongs to.

`enter` or `→` on the row hands the keyboard to its pane. The foot there says
`enter or tab answer this ask here`, but `tab` on home is the way to the next place — use
`enter` or `→`.

## How do I know ask here is doing something — the spinner, the clock and the live strip

While a turn is running the pane says what is happening, in the words the conversation
itself uses, for the first turn and for every follow-up alike:

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
have happened this turn — a call starting, a call finishing with its own time, and the
growing tail of the reply, so a long answer visibly moves. A running call carries the
spinner; one that succeeded carries nothing at all; one that failed carries `✕` and what
went wrong. **The whole block goes when the turn ends**: it is a window onto the moment, and
everything in it is already a row above it.

A tool row says what the call is pointed at and never says `unknown`: `bash` shows the
first line of the command, `read` shows the path, and the `automation` tool says what it is
doing — `automation · proposing leave`, `automation · reading the automations`. A call that
came back with nothing to say says nothing.

## Why is the answer from home showing asterisks and hashes — the ask here pane formats markdown

A settled answer in the `ask here` pane is formatted: it goes through **the same markdown
renderer the conversation uses** — one parser, one set of colours, one answer to what your
terminal can draw. You never see `**bold**`, a leading `##`, or backticks around
`/automations`; you see bold, a heading and a code span.

```
Two automations

You have two of them. /automations lists them:

· leave, today at 18:00
· weekly update, every Monday at 09:00
```

**While the reply is still arriving it is plain wrapped text**, which is the transcript's
behaviour rather than a difference: formatting a half-written sentence re-flows it under
your eye. The whole block formats the instant the turn is over.

Beside the list the pane is 36 to 56 columns wide, the renderer's **phone tier**: a fenced
code block wraps rather than being cut, with a dim `↳ ` on each continued row, and a table
stacks as one `key: value` line per cell. Taking the whole screen, it renders at that width
as a conversation would.

What it does not give you is what hangs off an answer in a conversation — the offer to open
a wide table, task references as links, an image. Take the exchange there with
`continue as a conversation` for those.

## How do I answer the card in the ask here pane, or say no to it

When the exchange proposes an automation its card appears in the pane, headed
`wants to remind you`, `wants to schedule work` or `wants to watch for something`, with the
facts a conversation's card shows (*Automations*, *The card*). Nothing is saved until you
answer, and each answer is a row of its own, **taken by its key or by a click anywhere
along it**:

- `1 Save` — saved as shown.
- `2 Save and run it now` — saved, then done once, right away, in the exchange while you
  watch; `2 Save and check it now` on a watch. Not offered on a reminder, or on work kept
  in a separate worktree.
- `0 Don't save` — `Nothing is saved and nothing runs.`
- `o Change…` — the pane says `type the change and press enter`; write the correction in
  your own words ("make it 8pm", "every weekday") and the model proposes again, on a new
  card that replaces this one.

**`esc` is not an answer here.** In the pane it hands the keyboard back to the list, and the
card goes on waiting — there is no clock on it — with its row saying `waiting on you`. `0`
is the only way to say no in this pane.

The hint starts with exactly the answers the card drew, and never one more —
`1 Save · 2 Save and run it now · 0 Don't save · o Change…` on work,
`1 Save · 0 Don't save · o Change…` on a reminder — and goes on `enter sends a follow-up`
and `tab or esc back to the list`. While a card is asking, `1`, `2`, `3`, `0` and `o` are its
keys, and one with no answer under it — `2` on a reminder — does nothing.

## What the card says after you answer it — saved, not saved, you asked for something different

**The card stays after you answer it.** It settles in place, greys out, and its foot says
what was decided in the words a conversation's card uses:

- `leave · Save · saved`, or `weekly update · Save and run it now · saved`
- `leave · not saved`
- `leave · Change… · you asked for something different`

Answering `1`, `2` or `0` hands the keyboard back to the list, and once the automation is
saved the pane adds `saved · /automations lists it`. A change keeps the keyboard in the
pane, because the model is answering it.

The digits belong to a card only while it is still a question. With no card up, or with an
answered one on screen, `3` in the middle of "make it 3pm" is just a `3`.

## Why did my reminder card disappear when I opened another chat — it does not any more

It used to, and that was a defect. The exchange lived on the home screen, so closing home —
which is what opening another conversation does — closed the errand's session with it, and
the card you had not answered yet was answered for you: nothing was saved.

**An exchange now outlives the screen it was asked on.** Closing home does not touch it. Nor
does opening another conversation, walking the list, looking at a different project, or
letting the card sit there overnight. There is **no clock on the card**: it waits while you
read it, and while you do anything else.

Open home again and the row is where you left it, still `waiting on you`, still answerable
with its own keys. What ends an exchange is under *When does an ask here exchange go away*;
quitting the window that holds it is the one thing that does not wait for you.

## Can I ask two things from home at once — yes, one row each

Yes. A second `ask here` **adds** an exchange; it does not replace the first. Each one gets
its own folder, its own session and its own row, and they sort against each other by what
they are doing — the one holding a card sits above the one still working.

The pane is always **about the row under the cursor**. Walk onto another exchange and you
get that exchange; walk onto a conversation and, where the frame has a pane beside the
list, you get that conversation's ordinary card. Nothing takes the screen hostage.

## How do I get back to the list from ask here — tab, esc, → and clicking a row

While the cursor is on an exchange row, home has **two zones**: the list, and the pane the
exchange is drawn in. Exactly one of them has the keyboard:

- **`tab` inside the pane** hands the keyboard back to the list, from any state the pane is
  in — the box, `continue as a conversation`, a card waiting — and never loses what is in
  the pane.
- **`esc`** in the pane hands it back too, one layer at a time: if you have half a follow-up
  typed, the first `esc` clears that and the second one leaves.
- **`enter`** or **`→`** on the exchange's row in the list takes the keyboard back in.
- **clicking** puts the keyboard where the pointer is: a click on a list row opens that row,
  as `enter` would, and takes the keyboard to the list; a click in the pane brings it back.
- **answering the card** with `1`, `2` or `0` hands it back by itself.

With the keyboard on the list, `↑`/`↓`, `ctrl+p`/`ctrl+n` and `pgup`/`pgdown` walk it
exactly as they do with no exchange on screen. **The pane follows the cursor.**

While the pane has the keyboard the hint names `enter sends a follow-up` and `tab or esc
back to the list`, with the card's own answers in front while a card is asking and
`↓ continue as a conversation` between them once that row is on screen. `continue as a
conversation` is reached with `↓` and left again with `↑`, `tab` or `esc`.

## Ask here on a narrow window — the exchange takes the whole screen

Home's panels have no pane beside them **at any width**, and a search under **136 columns**
has none either. It does not refuse: the two zones are **stacked** instead of sat side by
side.

- the **list** is the screen until you enter an exchange;
- the **exchange** is the screen while it holds the keyboard — the same pane, the same card,
  the same live strip, drawn wide;
- **`esc`** or **`tab`** brings the list back, with the exchange's row still on it wearing
  its tail, and **`enter`** or **`→`** on that row opens it again.

`ask here` itself opens straight into the stacked pane, because it selects the new row and
gives it the keyboard. Resizing between the shapes costs nothing: it is the same exchange
and the same keyboard, drawn in whichever geometry fits.

**On a phone-shaped frame** — under 60 columns, where home is an inbox — the bar at the foot
offers `open`, `new` and `ask here` once something is typed, and a tap on `ask here` asks.
An exchange holding a card is listed with what waits on you; tapping it raises the exchange
over the screen, with `‹ back`, `send` and, while it is offered, `more` — continue as a
conversation — along the foot.

## Why can't I click a row while asking — you can, and it opens it

You can, and it does. A click on any row of the list puts the cursor on it, gives the
keyboard to the list and opens the row, exactly as `enter` on it would — one click, the
same as on every place. A click on an exchange's own row gives its pane the keyboard, and a
click on `ask here` or on the row of what you typed only puts the cursor there, because a
click never starts a paid turn.

A click on one of the card's answers in the pane answers the card. Each answer owns its
whole row, so there is no gap between two of them to miss.

**`ctrl+enter` is the chord for `ask here`, and the foot does not name it.** It only reaches
codeaf on a terminal that can tell it apart from a plain `enter` — the kitty keyboard
protocol, Windows terminals — so the foot names the arrow every terminal has. `alt+enter`
is **not** a second spelling of it: on home, as on every place, it opens the task composer
layer (*Places*), whose own second `alt+enter` sends the sentence through this same door
with the project, model and spending cap the layer settled.

If this window has no way to open a second session, the row answers in one line —
`this window cannot ask from home` — and nothing is created. That window is one attached to
another machine with `--host` or `--at`: the exchange would be made on this laptop while
the work and its automations belong to the machine over there. An ordinary `codeaf` asks
from home whether or not this project's engine is holding the conversation.

## When does an ask here exchange go away

Never while it is working, and never while it is waiting on you.

An exchange's row goes when **all three** of these are true:

1. **it is over** — the card, if there was one, was answered, and no turn is in flight;
2. **you have seen it that way** — its pane was on screen at least once after it ended; on
   home at rest the pane shows only while it has the keyboard, so that is `enter` or `→` on
   its row;
3. **you have moved off it** — the cursor is on some other row, after a key or a click on
   home.

Going means its session is closed. **Nothing is deleted and the folder does not move**
(*Where did that exchange go*). An automation saved from it is not the row: it is listed in
`/automations`, and in home's `automations` panel while it has a next time.

**Quitting the window that holds it ends every exchange**, including one still working — the
turn is interrupted and the session closed — and a card still waiting is then never
answered, so nothing is saved from it.

## Where did that exchange go — the folder, at every stage

There is one folder and it moves at most once. Nothing here copies a transcript.

| What happened | Where the folder is |
| --- | --- |
| you asked | `~/.codeaf/v3/errands/<id>/transcript.jsonl` |
| an automation was saved from it | the same folder: it does not move, and it is never cleared away, because the automation names it as where it was asked |
| you continued it as a conversation | the project's own folder, with a `meta.json` |
| it came to nothing | it stays where it was made for a week; once nothing in it has changed for seven days, the sweep a launch runs removes it |

The sweep never touches one that is open in a window, one an automation was saved from,
or anything you continued as a conversation.

So "why did I get this?" is a door: `→` then `o` — `open where it was asked` — on the
automation's row in `/automations` opens this exchange's transcript.

To change the automation itself, use `/automations`: `→` on its row, then `p` to pause,
`e` to edit or `d` to delete (*Automations*). Home's `automations` panel has no verbs;
`enter` on it opens the place.

## Turn it into a conversation — continue as a conversation

Once the first reply has landed, a row appears under the exchange:
`+ continue as a conversation`. Press `↓` to reach it and `enter` to take it; it also
**lights up under the pointer and takes one click**.

That moves the same folder into the project's own directory, writes the `meta.json` a
picker reads, closes home, and opens the conversation — with everything that was already
said in it. It is the same conversation, filed differently; nothing is replayed and nothing
is lost. A turn still running is interrupted first. The exchange's row on home goes with
it, because the conversation now has a row of its own.

It is offered only while the exchange is still an errand. **Once an automation has been
saved from it, the offer goes**: the automation names this folder as where it was asked,
and a folder moved after it was pointed at would be a door onto nothing. A promotion home
will not make is answered with `this exchange is already a conversation`.

## What it will not do

- **It will not put the errand on home's conversation list.** It gets an exchange row while
  it is live, and that row goes once the errand is over and seen. For a conversation on the
  list, use `continue as a conversation`.
- **It will not deliver an automation's runs into the pane.** A run's news is a desktop
  notification and a line on home's `since you left` panel, and `/automations` keeps the
  history. The one dim line a run leaves belongs to the conversation that made it — here,
  the exchange's transcript — and is drawn only while you have that open.
- **It will not run the full conversation surface in the pane.** It draws what you said, the
  reply **rendered as markdown**, one line per tool call, the live strip and the automation
  card. Slash commands, the task column, `/rewind`, images and permission questions are the
  conversation's own screen — take the exchange there with `continue as a conversation`.
- **It will not save anything without the card.** Typing a sentence at home sets nothing up;
  a card does, and only after you press `1` or `2`.
- **It will not end an errand because you looked away.** Closing home, opening another
  conversation and quitting a *different* window all leave it running.
- **It will not work over `--host` or `--at`.** Home lists the far machine's projects and
  automations there, but `ask here` answers `this window cannot ask from home`. Say it in the
  conversation instead: its card crosses the connection like any other, and the automation
  is saved on that machine (*Running on another machine*).
