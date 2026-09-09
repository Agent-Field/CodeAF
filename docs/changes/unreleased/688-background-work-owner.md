---
kind: fixed
title: a background result reports to the request already being worked on
pr: 688
surface: [engine, chat]
invalidates:
  - "A job-result wake could inherit the whole original request and hand it out again while its worker still ran. A reporting-only turn now recognizes that request's live owner across wake turns and waits for its result instead of creating another owner."
  - "Task admission and background operations now retain their originating human-request identity, scoped to the admitting agent. A later notification follows its producer rather than borrowing the latest human message; mixed or unknown origins do not claim an owner."
---

The same ownership reading governs automatic handoff, stopped-turn reopening,
and explicit proposals during that reporting turn. It marks no goal complete.
A task result, a new human message, or a main-owned job with no matching live
worker still permits ordinary continuation. No task-name matching or additional
model call decides this boundary.

Batched receipts keep every origin and outcome while displaying their reporting
duty once. A forced reporting turn includes those outcomes before it waits.
