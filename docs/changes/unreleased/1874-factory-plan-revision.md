---
kind: added
title: Send a waiting factory plan back with feedback
pr: 1874
surface: [engine, remote]
invalidates:
  - "The app protocol had no distinct action for revising a waiting plan; factory.job_reject_plan now sends feedback only when a completed earlier work step can receive it, using the reviewed version and caller checked inside the item write lock."
---

The capability and destination come from the stored job. A first gate or stale review refuses; a successful revision records an answer, never an approval or an Undo window. Generic answers and post-proof request changes retain their existing behavior.
