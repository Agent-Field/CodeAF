---
kind: changed
title: The thinking wheel comes back to auto, so the chip can reach the state a fresh install is in
pr: 1057
surface: [chat]
invalidates:
  - "The conversation's thinking wheel had five stops and wrapped from `max` back to `low`, so `ctrl+v` and a press on the chip could never reach `auto` — the way back was `/effort auto` or the ladder's top row. The wheel has six stops now: one press past `max` clears the rung, the way the model picker's `ctrl+t` and a task's own rung have always walked. `/effort auto` and the top row still do it in one move from any rung."
  - "A standing item's rung is now the ONLY scope on the surface whose wheel never lands on absence, and `internal/tui3`'s `effortNext` has one caller left. The conversation moved to `effortNextClearing` beside the task rung and the model level."
  - "Clearing a conversation's rung on a machine whose `thinking` row in `/settings` is set used to write the note that blames a level dialled onto the model and points at `ctrl+t`. It names the row that actually catches it now: `thinking · auto for this chat · <rung> · the thinking row in /settings decides now`. The `ctrl+t` note is still what a level on the model gets."
---

The chip above the message box was the one control that could not say the state
its own install ships at. A person who dialled a conversation up once had to
leave the chip they were standing at and find `/effort` to put it back, which is
the flow the owner asked to be rid of.
