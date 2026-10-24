---
kind: added
title: Execute signed scoped-client calls through desktop methods
surface: [engine, app, chat]
invalidates:
  - "Scoped clients could enqueue calls but no binary drained them. The app backend now checks original request proofs, scope, routing, sequence and age locally, claims work, dispatches through appwire, records chat receipts and acknowledges it."
---

Irreversible asks require the signed client Face ID assertion. Durable started
receipts prevent replay after an interrupted dispatch or lost acknowledgement.
Chat calls never acquire a lease. Unsupported mutation routing and unexplained sequence gaps fail closed. A client
can explicitly sign skip_through to resume after missing numbers; the skipped
local range is kept in its chat receipt. The same client can enroll on several
executors without changing its relay permissions; multi-recipient sealed calls
let any eligible executor compete for the atomic claim without sharing keys.
