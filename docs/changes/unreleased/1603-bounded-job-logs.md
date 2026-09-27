---
kind: fixed
title: bound background logs and keep recursive searches out of runtime output
pr: 1603
surface: [chat, engine]
invalidates:
  - "Background jobs wrote unbounded logs and promised full output. Each job now retains two 4 MiB chunks, reports discarded output and disk failures, and continues draining the child process."
  - "Completed job logs could accumulate without an aggregate budget. Eligible completed managed logs now share a 128 MiB and 64-job budget per directory, while startup preserves seven-day expiry and durable job ID history. Active and unmarked legacy logs are protected from cleanup."
  - "Recursive grep could read its own runtime output and the fallback loaded whole files. Both engines now exclude known runtime output, while explicit file inspection and fallback reads use bounded snapshots. Shell search guidance now requires explicit exclusions and byte-limited log reads."
---

The spool retains the same active file and lease through rotation, so recursive
readers cannot chase an ever-growing old inode. Write, rotation and close errors
remain visible without failing the child process. Retention uses durable ID
allocation, ownership markers and independent file leases. Unsafe linked files
or storage ancestors are refused; legacy output remains outside the budget.
The bounds apply per job and per jobs directory, not across the machine.

Structured search preserves source worktrees, including those inside custom
state homes. Recursive searches skip files above 8 MiB. Explicit inspection reads
at most the first 8 MiB present at open; oversized 64 KiB lines are skipped with
an incomplete-result notice. Requests allow at most 1000 matches and 20 context
lines per side. Oversized ripgrep records terminate and reap the child with an
explicit error. Arbitrary shell commands do not inherit structured exclusions.
