---
kind: fixed
title: Keep missing-pack capture evidence and retry with a fresh plan
pr: 2427
surface: [engine, chat, app]
invalidates:
  - A missing durable pack used to fail capture immediately without keeping diagnostic metadata; codeaf now saves bounded local evidence before one fresh-plan retry.
  - A second capture failure used to report only the engine error; it now preserves the first error and names the evidence bundle when one was saved.
---

A missing-pack capture-plan failure records the furrow path and reported version,
store entry names, sizes and modification times, the full original error, and
copies of catalog and durability-frontier metadata only. Object and pack contents
and credential directories are never copied. Bundles are private, capped at
1 MiB, and limited to the newest three per chat; oversized metadata is explicitly
omitted. Other errors do not trigger this evidence or immediate retry.

The cause of the original first-share failure is still unknown. This change adds
evidence and one opportunity to recover, while preserving mandatory planning and
screening on every capture.
