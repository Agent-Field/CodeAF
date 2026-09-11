---
kind: changed
title: Small edits across several files can finish in the conversation
pr: 664
surface: [chat, engine]
invalidates:
  - "Two distinct workspace files previously spent the inline write allowance. Only the existing five-write-call allowance now triggers this handoff check."
  - "Checkpoint custody was believed to cover every field a new task receives. It checked a handoff rung before the mark reader's shape and legend were put above it, and did not check the original request or fallback done-condition added at admission. The completed brief, request and done-condition are now read together before anything is admitted; if any names a piece still held by the conversation, the handoff stays here."
---

Removing the second counter avoids handing a small unfinished operation to a new
task solely because its writes span several files. Successful workspace writes,
including those made by forked hands, still count; result ownership, approvals,
and the other turn limits retain their existing behavior.

The same live check exposed a custody gap in the handoff it now reaches. A
symbolic drawing could put `A | B | C` above a safe draft while its legend
assigned B and C to tasks already out, and the original request could add those
tasks again after the brief was checked. Custody now reads the exact three text
fields the prospective worker would receive after composition and keeps the turn
in the conversation when any of them still names held work.
