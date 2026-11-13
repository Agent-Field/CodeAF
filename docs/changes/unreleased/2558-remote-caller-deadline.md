---
kind: fixed
title: A remote task-root review or apply that runs out of time always reads as a timeout
pr: 2558
surface: [remote]
invalidates:
  - "When a remote task-root review or apply reached the caller's deadline, the error matched context.DeadlineExceeded only if this end noticed first; if the engine's answer arrived first it was the same words as a plain error, so errors.Is said no. It now matches whichever end reported it, and keeps the engine's words."
---
TestTaskWorkspaceEarlierCallerDeadlineStopsResidentOperation failed this way about half the
time on a loaded machine and was recorded flaky on #1749.
