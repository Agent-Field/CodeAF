---
kind: fixed
title: the task tree's total goes with the conversation it is about
pr: 217
surface: [chat, docs]
invalidates:
  - "Switching conversations zeroed the money, the tokens, the cache and the
    context meter, so a reader could assume the row was wholly the arriving
    conversation's. The live task-tree total added in #153 was NOT in that
    reset: the figure of the conversation being left stood on the row of the
    one being taken up until something recomputed it, which typing /cost
    usually did. Every meter the row draws is a fact about one conversation
    now, the tree included."
  - "The tree half of the bill arrived on the row only once the task column had
    a roster, because readTreeSpend ran on the frame clock and only while there
    was work to read about. A switch takes one reading on the way in through
    that same function, so the whole bill — the conversation's books and the
    ledger's running nodes — is on the FIRST frame after a switch."
  - "internal/manual/chat/screen.md said a resumed conversation's work `is added
    on top as soon as the task column is up`. It is on the same first frame.
    models-and-cost.md now carries a section on what a switch does to the row."
---

The reset was the whole defect and the reading is what makes the first frame
right rather than merely blank. One thing deliberately NOT reset beside the
tree is the tail-read cache under it: [session.UsageCache] is a parse of the
machine's one ledger, not a fact about a conversation — the tree is re-derived
from it per conversation on every read — and dropping it would put a cold walk
of the whole file on the switch frame, measured at 609ms against a ledger at
the cache's own 200,000-line ceiling, where keeping it costs 17µs.
