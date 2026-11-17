---
kind: fixed
title: Saved agent spend reaches open answers and changes after late receipts
pr: 2347
surface: [app, engine]
invalidates:
  - "Agent spend was populated only in list answers, and late recovered cancellation charges left metadata stale until another spend stamp. Open answers and agent changes now carry saved spend, and late receipts save the cumulative meter immediately."
  - "A copied outgoing view or stale higher inventory row could restore an older saved spend figure. Open answers and agent changes now read the saved figure from disk, including exact lower saves; other method answers omit it and retain their existing event-writer independence."
---

`agents.list`, `agents.open` and `agent.changed` carry the cumulative spend saved
in the conversation's `meta.json`, including for held conversations. A late
recovered receipt saves the updated total without requiring another turn. The
app reads the saved figure for open answers and when sending an agent change,
and sends one change if it differs from the figure last said; repeated news
sends none. Other method answers omit spend and keep their existing behavior
when the event writer stalls. An already-opening answer reads saved spend
without waiting for that writer.
Spend snapshots serialize meter capture and metadata writes so an older
snapshot cannot overwrite a newer one. `agents.budget.get` keeps its live meter.
