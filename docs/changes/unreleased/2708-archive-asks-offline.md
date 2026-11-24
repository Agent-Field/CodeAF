---
kind: changed
title: Archive asks what stops and what its parent keeps, and an offline computer loses nothing
pr: 2708
surface: [app, docs]
invalidates:
  - "The desk archived a workstream at once even with tasks running or a parent to keep files for; it now asks first in those two cases, stops the running tasks and gives the parent a shortcut to each ticked file."
  - "The chat manual said a workstream inside another could be folded back into a heading of its parent; the app never offered that, and the manual now says it is not offered."
  - "A workstream with tasks and files but no Factory jobs was offered Delete in More; Delete is now offered only when it holds no tasks and nothing in Files, and Archive otherwise."
---

The desk side is Agent-Field/codeaf-desk#499 and B6's Archive slice. The manual quotes their words: the facts line "This Mac is offline · answers send when it is back; work is paused, not lost", and the Archive question.
