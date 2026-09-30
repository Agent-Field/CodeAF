---
kind: changed
title: Conversation close, task stop and permanent deletion stay synchronized across views
pr: 1432
surface: [chat, engine, docs]
invalidates:
  - "Closing a conversation could stop its agent or hide its work. Close now only closes its tab; saved conversations and active work remain available."
  - "Task rows offered Close. They now offer Stop while active and confirmed Delete for a settled task and its descendants."
  - "Deleting a record could leave live task bars and cached lists unchanged. Permanent deletion now removes all views immediately and survives replay."
  - "Stopped work could appear finished or count as done. Conversation rows and task pages now preserve stopped status."
---

Home and Sessions share conversation actions, dark grey closed rows and permanent-delete
confirmation. Enter reopens; y confirms deletion; n or Escape returns to the actions.
Conversation deletion stops its agent and removes all owned tasks, while task deletion
keeps its conversation and unrelated siblings. Option+K remains usable with every tab closed.

The dev integration keeps program badges, interrupted-run recovery and live Home
activity readings. The retired rail footer totals and its fold-state map stay
removed; stopped-state checks cover the task and room surfaces that still exist.

The current Home menu keeps Copy name second, including on closed conversations;
its first action becomes Delete while Enter remains the reopen door. Sessions shares
these conversation actions, and task rows retain Copy project.
