---
kind: fixed
title: Empty crew plans explain their ending and allow a fresh retry
pr: 2403
surface: [engine, chat, app]
invalidates:
  - "An empty opening plan was reported as done while its chat line claimed it stopped. It now ends as failed with the planner's reason, or The crew didn't plan any work for this goal., saved once as an assistant notice."
  - "Adaptive rows never offered retry. A live empty-plan root that created no workers can now start a fresh admitted run; ownership and current budget checks still apply. Other adaptive endings and restored historical rows remain unsupported."
---

The run page shows incomplete with its reason. The app carries the same failed
state, reason and retry capability. A revised goal starts a new run; no snapshot
field or saved format changes, and no earlier plan or output is reused.
