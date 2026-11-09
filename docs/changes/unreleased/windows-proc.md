---
kind: fixed
title: Windows process stops own and verify the whole launch tree
pr: 0
surface: [chat, engine, resident]
invalidates:
  - "Windows teardown depended on live parent PID chains; launches now retain a Job Object through shim and parent exits."
  - "A stop could report completion after its waits expired; an unverified ending now remains running and sends no stopped note."
---
Windows launches are assigned while suspended, before their first instruction.
The existing soft stop precedes a whole-job hard kill, and completion waits for
empty job accounting. Deliberate detaches retain their lifetime. POSIX process
groups and signals keep their existing behavior.

Windows shell facts explain the Python Store stub, package-manager `.cmd`
shims, and using jobs to start and stop long-running native servers.
