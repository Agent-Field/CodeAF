---
kind: fixed
title: Recover journaled usage after a crash without changing charges
pr: 0
surface: [chat, engine, docs]
invalidates:
  - "Usage queued only in process memory could disappear after the close deadline; complete journaled rows now replay once."
  - "RecordUsage previously returned without waiting for disk; it now syncs the pending journal before durable handoff."
---

Complete usage rows now survive in `<ledger>.pending-v1.jsonl` (normally
`usage.jsonl.pending-v1.jsonl`). Startup replays missing ChargeIDs using the
original amount, day, ownership and billing facts. Startup and clean close
atomically compact booked rows under the ledger lock. Failed writes retain
rows in memory and retry with capped exponential backoff and one identity log;
accounting errors do not fail chats.

Model-only calls also write V3 model WAL records so a refused seal can recover
receipts on reopen. A downgrade can lose model-call receipt detail when an older
writer rewrites that WAL, never money saved in the complete-row usage journal.
Older builds do not replay the sidecar; recovery resumes in a supporting build.
