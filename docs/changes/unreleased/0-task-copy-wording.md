---
kind: fixed
title: Task prompts and manual describe folders without promising copies
pr: 0
surface: [chat, engine, docs]
invalidates:
  - "Task prompts and the manual promised every task a project copy; tasks that would copy home instead read it in place and start with an empty folder of their own."
---
Use folder wording for claims shared by both modes. Project tasks may still get
real copies, and child tasks still start from snapshots of their parent's work.
The rule never to name the workspace method remains unchanged.
