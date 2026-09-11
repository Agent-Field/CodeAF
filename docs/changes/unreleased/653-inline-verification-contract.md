---
kind: fixed
title: Keep explicit verification checks for unattended inline work
pr: 653
surface: [chat, engine]
invalidates:
  - "The whole-request acceptance writer could return structured checks, but inline completion had no session-owned declaration source. An unattended session now freezes validated explicit checks beside its acceptance and uses them when its completion reader cannot answer."
  - "Commands in acceptance prose or action receipts still grant no execution authority. Historical acceptance receipts do not restore commands as permission for a fresh goal owner after reopening."
---

The original ask, acceptance, and verifier remain one immutable contract. A stale
ask or later replacement cannot change its commands; malformed declarations
supply no execution authority. The acceptance receipt records explicit checks,
and task-owned checks retain their existing revision rules.
