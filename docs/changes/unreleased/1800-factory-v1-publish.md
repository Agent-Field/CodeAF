---
kind: changed
title: Factory publishing binds approval to a commit and retries without new consent
pr: 1800
surface: [chat, engine, docs]
invalidates:
  - "Factory approval could publish edits made after the person opened the job. The worker now commits before opening proof, reports head_commit, and approval names that exact commit; a moved head returns changed_since_opened. Approval records and PR trailers carry the commit."
  - "A failed publish needed approval again and exposed only a log message. Publishing now reports queued, pushing, opened or failed, a stable failure code, attempt and receipt. Retry publishing keeps the same commit's approval, and publish_failed belongs in Needs you."
  - "Repeating a POST could repeat its outward action. Idempotency-Key now replays the first response for 24 hours, with durable responses on store-backed factories and shared locking for concurrent approval."
---

Contract 1 remains additive: existing fields, including desk.publishing, stay.
GET /v1/publish/codes supplies the fixed code table and each person's next step.
Machine-offline publishing waits on its holding machine. Unknown-head legacy jobs
retain their earlier approval body; a reported head requires a full commit SHA.
Real Git, worker HTTP, contract, failure-code, retry and concurrent replay tests
cover the approval-to-receipt path and response expiry without wall-clock waits.
