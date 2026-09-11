---
kind: fixed
title: a correction that fell through still gets its turn drawn — the steer lane is stamped with the conversation, not the turn
pr: 855
surface: [chat]
invalidates:
  - "A turn the engine answered was believed to always reach the screen. It was not: a correction typed into a running answer that fell through — the turn ended before a boundary came — carried its own stream across the seam, and the surface's lane was stamped with app.gen, the TURN generation. app.gen moves on every submit, every adopted stream and every drained follow-up, so the stamp was stale by the time a fall-through was read (which is, by definition, after a turn ended). app.steerFell drained that channel and threw it away: the engine ran the turn, paid for it and journaled the answer, and the window drew neither the question nor a word of the reply. The lane now carries app.steerGen, which moves only when the conversation on screen is replaced (detach.go)."
  - "A conversation that had answered a question and shown nothing was read as the model having gone silent. It was the surface: reopening the same conversation shows the answers the live window never drew, because the journal had them all along. A blank turn is now a bug report about tui3, not about the model or the router."
---

Measured on 2026-09-10 22:39: three `whats up` messages in a row, three answers
in `transcript.jsonl`, and a screen that showed none of them. Plain enter over a
running answer steers (input.go), the answer ended before a boundary came, and
each correction became a turn of its own on a stream the surface then discarded
for having the wrong turn number on it.

Beside the fix, two pins on the other road into the same law — a turn that hopped
to a fallback model draws the answer that model wrote, and the calls it made
after the hop keep their rows.
