---
kind: fixed
title: Slash command completion executes bare /task, /redo, and /workspace immediately
pr: 1563
surface: [chat]
invalidates:
  - "Selecting /task, /redo, or /workspace from the slash command completion menu previously inserted the command prefix with a trailing space into the prompt instead of executing or opening the destination view. Selecting bare /task now opens the task roster immediately, bare /redo reruns on a stronger crew immediately, and bare /workspace opens a picker that anchors a project-less conversation through the same action as a typed workspace path."
---
