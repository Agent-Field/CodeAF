---
kind: fixed
title: Join cancelled headless accounting before returning and isolate shared team fixtures
pr: 0
surface: [chat, engine, build, docs]
invalidates:
  - "A local headless task could return before cancelled-call accounting created its usage writer; it now joins that producer before closing usage."
---

The node engine joins admitted leaf receipt settlement within its existing
background stop wait. Every `codeaf do` invocation closes the process usage writers
after its workers stop, under the existing usage close bound. Each leaf's late
accounting retains the ledger chosen when the leaf was constructed.
Cancelled requests still keep their unbilled markers, and journal sync and retry
retain their existing durability and shutdown limits.

Shared team fixtures create a paired identity in a test-owned home instead of
inheriting a solo identity from an earlier custody save. Explicit solo delivery
and device cursor assertions retain their original postures.
