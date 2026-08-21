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
usually is. The left column keeps working the whole time: `esc` puts the cursor back on it,
and the exchange stays on screen beside it until you close home.

On the `ask here` row itself the hint under the box reads
`enter asks this here and keeps the record · ↓ start a conversation instead · esc clear`,
and while the exchange has the keyboard it reads `enter sends a follow-up · esc back to the
list`, with the card's three answers and `↓ continue as a conversation` added when they are
on screen.

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

- `1` — yes. It stands as proposed.
- `2` — change it. The pane says `type the change and press enter`; write the correction in
  your own words ("make it 8pm", "every weekday") and the model proposes again. Nothing is
  created by a change.
- `3` — once. The action runs now and nothing standing is created.

The three answers are the only three. There is no fourth key and no default: a card nobody
answers creates nothing.

## Where did that exchange go — the folder, at every stage

There is one folder and it only ever moves. Nothing here copies a transcript and nothing
here deletes one.

| What happened | Where the folder is |
| --- | --- |
| you asked | `~/.aforge/v3/standing/exchanges/<id>/transcript.jsonl` |
| something now stands | `~/.aforge/v3/standing/<item id>/exchange/` |
| you continued it as a conversation | the project's own folder, with a `meta.json` |
| it came to nothing | it stays in `exchanges/`, and the sweep clears it after 7 days |

When something stands, the exchange is filed **under the thing it made** — that is what
makes "why did I get this?" a door — and the pane says `kept · this exchange is filed under
it`. The conversation is closed at that moment: the thing exists now, and changing it is a
card of its own.

## Turn it into a conversation — continue as a conversation

Once the first reply has landed, a row appears under the exchange:
`+ continue as a conversation`. Press `↓` to reach it and `enter` to take it.

That moves the same folder into this project's own directory, writes the `meta.json` a
picker reads, closes home, and opens the conversation — with everything that was already
said in it. It is the same conversation, filed differently; nothing is replayed and nothing
is lost.

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
