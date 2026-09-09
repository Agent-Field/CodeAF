---
kind: fixed
title: a background result reports to the request already being worked on
pr: 688
surface: [engine, chat]
invalidates:
  - "A job-result wake could inherit the whole original request and hand it out again while its worker still ran. A reporting-only turn now recognizes that request's live owner across wake turns and waits for its result instead of creating another owner."
  - "A task's admission tracked the turn and steering epoch only. It now also records the existing human-message sequence, scoped to the admitting agent, so a notification does not look like a new request and an unrelated older worker cannot claim newer work."
---

The same ownership reading governs automatic handoff, stopped-turn reopening,
and explicit proposals during that reporting turn. It marks no goal complete.
A task result, a new human message, or a main-owned job with no matching live
worker still permits ordinary continuation. No task-name matching or additional
model call decides this boundary.
