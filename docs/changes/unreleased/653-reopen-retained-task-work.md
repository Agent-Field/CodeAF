---
kind: fixed
title: Later acceptance restores a retained task checkout before delivery
pr: 653
surface: [engine, chat]
invalidates:
  - "Accepting a task after its Git worktree registration was released could report fatal: not a git repository. A recorded release is now reopened before landing, while its saved files stay in place."
  - "A renamed task branch could be lost to later checks and settled notices. The actual branch is retained at release and used for acceptance and fresh checkouts."
  - "A failed registration recovery is not an intentional kept-branch success. It remains answerable, names the retained folder, and can be retried after repair."
  - "The phrase asked twice and got no answer either time could sound like a missed question to the user. The report now names the checking calls and distinguishes a window that ended before the second call."
---

The live four-module repair completed its requested work and commit, but its late
acceptance hit a directory that aforge itself had unregistered. Recovery now recreates
only the Git registration and index. Existing files and prior recovery directories are
never relocated or removed by the pickup. Real Git regressions cover acceptance onto a
working branch, protected main, renamed branches, old release records, retained loose
files, a retained folder inside another checkout without changing its staged index,
and a deleted branch that leaves the task needing attention.
