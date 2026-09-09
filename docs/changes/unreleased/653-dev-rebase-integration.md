---
kind: internal
title: Reconcile conversation execution with the latest dev fixes
pr: 653
surface: [chat]
invalidates:
  - "The conversation branch was based on dev before its moved-commit landing guard, complete job endings, pinned effort and lane fixes. Those fixes now remain alongside conversation ownership and explicit verification contracts."
  - "The quoted-command guard's structural test assumed a prose-harvesting function still existed. Checker admission now has no prose-harvesting entry point; typed checks are the only repeatable verification contract."
---

The rebase preserves merge-time integration fixes for revision-scoped checks,
message authority, task attempt records and compaction. The moved-commit landing
reason uses the same inspection-first wording as the existing branch protections.
Task checkpoint records retain both the cut commit and verification history.

Upstream completion and task admission regressions use the current APIs. The
manual distinguishes budgeted unattended checkpointing from interactive chat and
keeps that explanation discoverable through the manual's search.
