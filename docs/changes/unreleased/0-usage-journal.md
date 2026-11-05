---
kind: fixed
title: Recover journaled usage after a crash without changing charges
pr: 0
surface: [chat, engine, docs]
invalidates:
  - "Usage queued only in process memory could disappear after the close deadline; complete journaled rows now replay once."
  - "RecordUsage writes one complete journal row before returning; fsync runs in background batches rather than on every call."
---

Complete usage rows now survive in `<ledger>.pending-v1.jsonl` (normally
`usage.jsonl.pending-v1.jsonl`). Startup replays missing ChargeIDs using the
original amount, day, ownership and billing facts. Startup and clean close
atomically compact booked rows under the ledger lock. Failed writes retain
rows in memory and retry with capped exponential backoff and one identity log;
accounting errors do not fail chats. Appenders hold shared ledger locks and open
the current journal after validating the ledger inode; compaction holds the
exclusive lock. Each append uses one O_APPEND write. A successful write survives
a process crash exactly once. Background fsync runs every 100 ms or 64 rows,
whichever comes first; clean close drains and syncs before compaction. On power
loss up to the last 100 ms batch can be lost, as on the previous asynchronous
writer. Parent directories are synced on file creation and rename.

Model-only calls also write V3 model WAL records so a refused seal can recover
receipts on reopen. A downgrade can lose model-call receipt detail when an older
writer rewrites that WAL, never money saved in the complete-row usage journal.
Older builds do not replay the sidecar; recovery resumes in a supporting build.
