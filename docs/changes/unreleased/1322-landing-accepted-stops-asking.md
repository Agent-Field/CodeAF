---
kind: fixed
title: a landing whose accept is already in flight stops asking the person to accept it
pr: 1322
surface: [engine, chat]
invalidates:
  - "After a person accepted a landed task, home and the switcher went on saying your call about it for as long as the merge took, because the arm read the pending list and the list filters on node state alone."
  - "With two landings open, one answered and one not, the row named the answered one: the arm took the first entry in admission order rather than the first entry that was still a question."
---
waitingOnPerson's landing arm took the first pending decision and named it. An
accept does not move the node's state until the merge finishes, so the entry
stays on the list through the whole settle and the row kept asking a question
the person had answered. The record card already read the decision record and
said accepted, so the two readers of one state disagreed.

The arm now takes the first pending entry whose Settling is empty and
contributes nothing when none are, which covers both the all-answered case and
the case where an answered landing was admitted before an unanswered one.
PendingDecisions is unchanged: its membership is state in, state out, and the
skip belongs at the arm that draws the line.
