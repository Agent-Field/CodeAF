---
kind: added
title: Delete a task with short Undo and keep the files it made
pr: 2776
surface: [engine, factory, app]
invalidates:
  - "Was true: workstream tasks could not be deleted. Is true now: the task's owner or workstream steward can delete one after stopping it, through the authenticated Delete endpoint and the desk's More menu."
  - "Was true: removing a task was not a way to release waits. Is true now: Delete removes it from live lists and Inbox, cuts dependent waits with a dim explanation, and preserves Files. Newly unblocked agent tasks wait for Start."
  - "Was true: task deletion had no Undo. Is true now: the desk offers ten seconds of Undo, and the server allows thirty seconds for delivery; it restores the task, its thread and unchanged dependants' waits and start choices. Afi has neither a delete nor a restore tool."
---
