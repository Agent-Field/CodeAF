---
kind: fixed
title: the manual said the status row's ledger stays the conversation's; it is the tree's
pr: 1143
surface: [docs, chat]
invalidates:
  - "The chat screen's manual said the ledger, the meter and the job counts on the status row all stay the conversation's. The ledger has not since c7142ca4 (2026-08-31): spendShown draws the larger of the subtree receipt and the conversation's own books, so a running task's spend is in the bill whether or not its room is open. The meter and the job counts are still the conversation's."
---

The same page already described the bill correctly where it introduces the groups — what
this conversation and its tasks have spent — so the page contradicted itself, and no gate
could catch it: the Manual CI step checks commands and aliases, not prose about a figure.
/cost is where the bill's two halves are taken apart, and it stays where it was.
