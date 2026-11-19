---
kind: changed
title: a reply about the work is checked against its own last look, and a question after failed steps says what failed
pr: 2836
surface: [engine, chat]
invalidates:
  - "Was believed: canvas replies that misdescribe the work came from describing it before looking again after an edit. Is true: in qloop run12 all five such replies looked AFTER their last change and contradicted their own look's words. Now a reply after a look is compared with that look by one replycheck call (small-work tier, once a turn), and one that changed the work after its last look is sent back to look again; either way at most one extra pass."
  - "Was believed: a reply or ask card that ends by asking the person is never sent back. Is true: it is still never carried on to do more work, but a question after failed steps (with no write/edit succeeding) is sent back once to redo the step or say what failed and what did and did not change, and an ask card in that shape is held once before it is drawn."
  - "Was believed: the engine knows when a canvas turn changed nothing. Is true: it cannot see a shell line's effect; the failure check never claims 'nothing changed' itself, the model says it."
---

The comparison reads only contradictions, never what the look did not cover.
With no model for `replycheck` that arm is absent and the stale-look arm, which
needs no model, still applies. Both send-backs offer `[no change]` so a right
reply is kept with one short call.
