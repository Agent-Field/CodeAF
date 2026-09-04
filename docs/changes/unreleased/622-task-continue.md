---
kind: added
title: continue re-arms the same failed task instead of proposing a new one
pr: 622
surface: [chat, engine, docs]
invalidates:
  - >-
    "A failed or finished task has no first-class continue — the only door is
    propose_task again, which mints a new id and a new working copy." `tasks`
    with `id` and `continue` re-arms that same node: same brief, same working
    copy, the last report as this round's finding.
  - >-
    `internal/manual/chat/how-tasks-run.md` already told a person to say
    `continue task 7`, and the `tasks` tool had no such verb. The page is now
    a capability the code keeps.
---

F23/F25: "continue task 4" re-derived a fresh brief and opened a new worktree.
