# Asking from home — reminders and watches without opening a conversation

## Can I set a reminder from home

Yes. Type it on the home screen and press `ctrl+enter` — or press `↑` once and then `enter`,
which lands on the row spelled `ask here: "…"`.

```
 ? ask here: "remind me at 6 to leave"
 + start a new conversation: "remind me at 6 to leave"
 ──────────────────────────────────────────────────────────────────────────────────────────────────
 › remind me at 6 to leave
 enter starts a new conversation and sends this · ctrl+enter ask here · ↑ pick a match · esc clear
```

The sentence is answered **in the pane on the right of home**, where the preview card
usually is. The left column keeps working the whole time: `tab` or `esc` puts the keyboard
back on it, and the exchange stays on screen beside it until you close home.

On the `ask here` row itself the hint under the box reads
`enter asks this here and keeps the record · ↓ start a conversation instead · esc clear`,
and while the exchange has the keyboard it reads `enter sends a follow-up · tab or esc back
to the list`, with the card's three answers and `↓ continue as a conversation` added when
they are on screen. With the keyboard back on the column it reads
`↑↓ move · enter open · tab back to ask here · esc close`.

## How do I get back to the list from ask here — tab, esc, and clicking a row

While an exchange is up, home has **two zones**: the list on the left and the pane on the
right. Exactly one of them has the keyboard, and there are four ways to move it:

- **`tab`** toggles them, from any state either one is in — a half-typed follow-up, the
  `continue as a conversation` row, an open card. It never loses what is in the pane.
- **`esc`** in the pane hands the keyboard to the list. One layer at a time: if you have
  half a follow-up typed, the first `esc` clears that and the second one leaves.
- **clicking** puts the keyboard where the pointer is. A click on a list row selects that
  row *and* takes the keyboard to the column; a click anywhere in the pane brings it back.
- **answering `1`** on a card hands it back by itself. The thing you asked for is being
  made, and the list is where you go next.

With the keyboard on the list, `↑`/`↓`, `ctrl+p`/`ctrl+n` and `pgup`/`pgdown` all walk the
column exactly as they do with no exchange on screen, and `enter` opens the row under the
cursor. **The pane keeps drawing the exchange while you walk** — it does not flick back to
the preview card of whatever row you are passing — because an exchange is a conversation
happening now, and hiding the reply while you check which project you are in would be the
wrong trade.

`continue as a conversation` is reached with `↓` inside the pane and left again with `↑`,
`tab` or `esc`. It also lights up under the pointer and takes one click.

## Why can't I click a row while asking — you can, and it selects it

You can, and it does. A click on any row of the left column puts the cursor on it and gives
the keyboard to the column, so the next `↑` or `↓` walks from there. A second click on the
same row opens it, which is home's ordinary two-step — one click that switched conversations
would make a mis-aimed pointer close the session you are in.

A click on a card's chip in the pane answers the card, the same way clicking one answers it
in a conversation. A press anywhere on the row of chips counts as that row's, so missing the
gap between two answers costs nothing.

`ctrl+enter` only reaches aforge on a terminal that can tell it apart from a plain `enter`
(the kitty keyboard protocol, Windows terminals). `alt+enter` is bound to the same thing and
every terminal here sends it, so if `ctrl+enter` does nothing, use `alt+enter`.

If this window was launched with no way to open a second session, the row refuses in one
line: `this window cannot ask from home`, and nothing is created.

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
of its rows and press `ctrl+enter` to say "this one". With the cursor still on the typing
rows it is the project this window is in, and the home directory `~` when this window is in
no project at all. A reminder belongs to no repository; a watch on CI belongs to one.

## How do I answer the card — 1 yes, 2 change, 3 once

When the exchange gets far enough to propose something that keeps working, a card appears in
the pane with your own words, when it would wake, and what it would cost per run. Nothing is
created until you answer it:

- `1` — yes. It stands as proposed, and the keyboard goes back to the list.
- `2` — change it. The pane says `type the change and press enter`; write the correction in
  your own words ("make it 8pm", "every weekday") and the model proposes again. Nothing is
  created by a change.
- `3` — once. The action runs now and nothing standing is created.

The three answers are the only three. There is no fourth key and no default: a card nobody
answers creates nothing.

**The card stays after you answer it.** It does not disappear — it settles in place, greys
out, and its bottom edge carries what was decided in the same words a card in a conversation
uses: `yes, set it up · set up`, `once, not standing`, `you asked for a different when`,
`ended · nothing was set up`. The chips go, so `1`, `2` and `3` are ordinary characters again
and can be typed into a follow-up. The only card that ever replaces it is the new one the
model sends after `2 change when`.

The digits belong to a card only while it is still a question. With no card up, or with an
answered one on screen, `2` in the middle of "make it 2pm" is just a `2`.

## Where did that exchange go — the folder, at every stage

There is one folder and it only ever moves. Nothing here copies a transcript and nothing
here deletes one.

| What happened | Where the folder is |
| --- | --- |
| you asked | `~/.aforge/v3/standing/exchanges/<id>/transcript.jsonl` |
| something now stands | `~/.aforge/v3/standing/<item id>/exchange/`, **when the exchange ends** |
| you continued it as a conversation | the project's own folder, with a `meta.json` |
| it came to nothing | it stays in `exchanges/`, and the sweep clears it after 7 days |

When something stands, the exchange is filed **under the thing it made** — that is what
makes "why did I get this?" a door — and the pane says `kept · this exchange is filed under
it`.

**The move waits until you are finished with the exchange.** The news that something now
stands arrives while the turn that made it is still running, so the folder is not touched
then: aforge remembers where it belongs, and moves it when you close home (or ask something
else here), after the session has been closed. Until then **the exchange is still alive** —
`tab` back into the pane and a follow-up goes to the same conversation. Nothing is copied
and nothing is deleted; the folder only ever moves, once.

Changing what now stands is still a card of its own — pause it with `p` or stop it with `s`
on its row on home.

## Turn it into a conversation — continue as a conversation

Once the first reply has landed, a row appears under the exchange:
`+ continue as a conversation`. Press `↓` to reach it and `enter` to take it.

That moves the same folder into this project's own directory, writes the `meta.json` a
picker reads, closes home, and opens the conversation — with everything that was already
said in it. It is the same conversation, filed differently; nothing is replayed and nothing
is lost.

It is reached with `↓` inside the pane, and it also **lights up under the pointer and takes
one click**.

It is offered only while the exchange is still an errand. Once something stands, the folder
belongs to that thing and the pane says `this exchange is already a conversation` rather
than moving it a second time.

## What it will not do

- **It will not keep the exchange after home closes.** The conversation ends with the
  screen. The folder does not — the transcript stays on disk, and you can `cat` it.
- **It will not put the errand on the home list.** That is the whole mechanism. If you want
  it on the list, use `continue as a conversation`.
- **It will not run the full conversation surface in the pane.** The right pane is forty
  cells wide, so it draws a reduced reading: what you said, what came back, one dim line per
  tool call, and the card. Slash commands, the task column, `/rewind`, images and the
  approval card are all the conversation's own screen — take the exchange there with
  `continue as a conversation` if you need them.
- **It will not create anything without the card.** Typing a sentence at home does not arm
  a reminder; a card does, and only after you press `1`.
- **It will not work in a narrow window.** The exchange *is* the right pane, so a terminal
  too narrow for two columns refuses with `ask here needs a wider window` rather than opening
  a session you could not see. Widen the window and ask again.
- **It will not work over `--host`.** Home itself refuses there, and this is a row on home.
