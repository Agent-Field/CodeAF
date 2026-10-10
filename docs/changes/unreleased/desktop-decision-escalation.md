---
kind: added
title: Decision routing follows the place graph with a bounded escalation path
surface: [engine, desktop, docs]
invalidates:
  - "The decision ledger had no question-routing helper. Callers can now select an eligible place through its ordered parents, or return the question to the person, within three parent hops."
---

Multi-place chats start at their nearest common ancestor. Learning and
below-threshold places pass the question upward, shared nodes are visited once,
and explicit ask modes preserve the person's control. The helper consumes a
snapshot and caller-provided evidence; desktop question delivery and receipt
writing remain with their integration lanes.
