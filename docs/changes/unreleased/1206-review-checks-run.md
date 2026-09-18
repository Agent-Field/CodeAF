---
kind: fixed
title: "review conclusions require an executed Checks: command"
pr: 1206
surface: [chat]
invalidates:
  - "A check: task could conclude holds: or does not hold: after reading alone, and a worker could add a leaf without Checks:. A review conclusion now lands only after its trajectory records a declared Checks: command, review wiring carries a worker leaf's executed commands when none were declared, and the worker page requires --check on delegated tasks."
---

The store checks the review task's recorded command before accepting either
conclusion. The review round keeps declared Checks: as its first choice. When a
worker did not declare one, its recorded commands become the review task's
Checks:, so the checker receives an executable contract rather than an empty
one.

The check seat remains read-only. This does not change `verifyOnlyBash` or
`refuseOutsideDoor`.
