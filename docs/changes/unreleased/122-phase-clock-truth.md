---
kind: fixed
title: A live stage keeps drawing, and the mark's reading is one of them
pr: 122
surface: [chat, engine]
invalidates:
  - "A stage longer than fifteen seconds went dark on the screen while it was still running — no longer true. A phase held open says itself again every five seconds, so a reading that takes a quarter of an hour draws a clock for the whole quarter of an hour."
  - "The handover's brief announced itself twice, once per model call, to survive that window — no longer true. It is opened once and held, and one clock counts the whole stage."
  - "The mark's reading between rounds drew nothing — no longer true. It says `taking stock` while it runs and takes the clock off the screen on every way out, the reading that failed and the one its window cut short included."
  - "How long a phase describes the present was internal/tui3's own number. It is `provider.PhaseWindow`, and every beat that keeps a phase alive is derived from it."
---

Two model runs and one long gate stood in front of a person with the screen
drawing nothing for them, and the reason was the same in all three: a surface
draws no phase it has not heard again for fifteen seconds, and nothing in the
turn was saying one again. The route judge at the end of a turn was measured
running for a quarter of an hour and drawn for fifteen seconds of it.

A phase held open now beats while it lasts, through one mechanism every holder
gets by holding a phase — no timer per site — and the beat dies with the stage
it belongs to. The handover's brief drops the double post it used as a
workaround. The mark's reading, which stops a turn mid-round to weigh the whole
ask against what has been done, has a word of its own: `taking stock`, and it is
a different word from `checking` because it is a different question.
