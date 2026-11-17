---
kind: added
title: A workstream task carries a checklist, and a task's run links the files it reads and makes
pr: 2745
surface: [engine, chat, docs]
invalidates:
  - "A task had no steps of its own: the only way to split work was more tasks, and the manual said there were no sub-tasks. A task now carries a checklist (ws_task_create checklist, ws_task_update checklist_done / checklist_undone), still with no sub-tasks."
  - "Afi and the chats never attached to a task the files they read, and the manual said so. A task's run now writes its task into used_by of every file of the workstream it reads, and stamps from_task on what it makes."
---
