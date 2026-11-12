---
kind: fixed
title: Join cancelled headless accounting before returning and isolate shared team fixtures
pr: 0
surface: [chat, engine, build, docs]
invalidates:
  - "A local headless task could return before cancelled-call accounting created its usage writer; it now joins that producer before closing usage."
---

Headless tasks join admitted leaf receipt settlement within the existing background
stop wait, then close the process usage writers under their existing bound. A
leaf's late accounting retains the ledger chosen when the leaf was constructed.
Cancelled requests still keep their unbilled markers, and journal sync and retry
retain their existing durability and shutdown limits.

Shared team fixtures create a paired identity in a test-owned home instead of
inheriting a solo identity from an earlier custody save. Explicit solo delivery
and device cursor assertions retain their original postures.
