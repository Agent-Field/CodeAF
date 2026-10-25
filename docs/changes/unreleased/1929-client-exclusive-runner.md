---
kind: fixed
title: Exclude concurrent scoped-client executors sharing state or identity
pr: 1929
surface: [engine, chat]
invalidates:
  - "Two live runners could open the same state and reuse the same device claim. An OS lock now excludes shared state and the relay rejects fresh repeated claims; a persisted process token recovers lost claim replies, and started receipts acknowledge without reclaiming."
---

The app releases the lock only after joining its runner at shutdown.

Claim tokens are persisted before requesting ownership and never exposed by queue listings.
