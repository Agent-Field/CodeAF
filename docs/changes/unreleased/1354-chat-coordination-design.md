---
kind: changed
title: the belt measurement and the design for the chat as manager over the plan store
pr: 1354
surface: [docs]
invalidates:
  - "The only large clean belt comparison was docs/design/bash-task-loop/REPORT.md, which scored the older engine 17/18 against bash's 13/18 and predated every harness wave. docs/design/chat-coordination/BELT-DOE.md replaces it: 66 cells over three batches on one binary and one model, in which quality is a tie and bash is about a third cheaper and about three times faster. The default moving back to bash rests on that table."
  - "Wall time was ruled not a dimension because a shared box makes it a function of the launch schedule. That holds for a staggered launch only. When both arms are interleaved into ONE shuffled batch every cell meets the same contention, and the DIFFERENCE between arms is then a measurement; the 32-cell batch is launched that way and its wall column is used."
  - "The chat was read as unable to see anything in the plan store. It reads rows and a task's page through its tasks tool; what it cannot see is notes, which planTasksText drops. Likewise a note was read as having no path into a running worker: the engine has one for its own sentences in internal/run/bashworker.go, and the design gives notes that road rather than building a second."
---

Two documents, no code. `BELT-DOE.md` is the measurement record, including
what it does not establish and the instrument fault that would have made
every arm a copy of its control. `DESIGN.md` is the coordination design in
four changes, each a missing reader on a channel that already exists, with
end-to-end acceptance on the real binary against a real model.
