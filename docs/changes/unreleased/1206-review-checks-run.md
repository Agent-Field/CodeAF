---
kind: fixed
title: "review conclusions require a command that bears on the claim"
pr: 1206
surface: [chat]
invalidates:
  - "A check: task could conclude holds: or does not hold: after reading alone, and a worker could add a leaf without Checks:. A review conclusion now requires a recorded command that bears on the claim; a wrapped Checks: command satisfies the contract, and a contract that is empty or cannot be satisfied still has an ending."
---

The store checks the review task's recorded command before accepting either
conclusion. The review round keeps declared Checks: as its first choice. When a
worker did not declare one, only its invocableChecks become the review task's
Checks:. A recorded command that wraps a Checks: command satisfies the contract.
When the contract is empty or cannot be satisfied, the check can use a check:
ending. A holds: or does not hold: conclusion still requires an executed Checks:
command.

The check seat remains read-only. This does not change `verifyOnlyBash` or
`refuseOutsideDoor`.
