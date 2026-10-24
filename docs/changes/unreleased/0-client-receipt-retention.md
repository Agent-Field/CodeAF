---
kind: fixed
title: Bound scoped-client replay receipts while keeping chat history
surface: [engine, app, chat]
invalidates:
  - "Every acknowledged call stayed in runner.json and unpair retained all receipts. The runner now keeps a replay watermark per paired client, streams old state during migration, and deletes receipts on unpair while preserving chat lines separately."
---

Chat receipts are durably stored in the existing conversation footer sidecar
before acknowledgement. Replay protection still rejects older signed numbers.
