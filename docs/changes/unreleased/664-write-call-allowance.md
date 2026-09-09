---
kind: changed
title: Small edits across several files can finish in the conversation
pr: 664
surface: [chat, engine]
invalidates:
  - "Two distinct workspace files previously spent the inline write allowance. Only the existing five-write-call allowance now triggers this handoff check."
---

Removing the second counter avoids handing a small unfinished operation to a new
task solely because its writes span several files. Successful workspace writes,
including those made by forked hands, still count; result ownership, approvals,
and the other turn limits retain their existing behavior.
