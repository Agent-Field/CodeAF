---
kind: fixed
title: a task that lands mid-answer is said after it, and Done draws every task its heading counts
pr: 1640
surface: [chat, docs]
invalidates:
  - "A task that finished while the answer was still running had its landed card written in the middle of that reply, and its crew line was folded into the turn's `▸ worked` chip. Both now appear after the answer ends, once each, in the order the tasks landed. They also appear when the answer fails or is stopped."
  - "A task's landed crew line (`task N crew · … · not right? /redo stronger`) could be folded into a `▸ worked` chip even when the task landed after the answer. It is addressed to the person and no chip hides it now."
  - "Two attempts at one request with the same title, such as a stopped senior-dev run handed over again, made `Done 2 ▾` draw one row. Folded, `Done 2 ▸` floated both runs' rows above the headings. Every task the heading counts now has its own row, and a folded group hides the runs its rows carry."
  - "The manual said the side column folds a finished family into one `✓ N done` line. That renderer was removed earlier. The page now describes the Running, Queued, Waiting and Done groups."
---

Part of #1556. Item 1, check rows missing from the side column, is still open: a review check is
seated in the run store without its parent's chat tag, so the conversation's reading never sees it.
