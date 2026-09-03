---
kind: fixed
title: tasks page orders by work facts, not conversation recency
pr: 520
surface: [chat]
invalidates:
  - "The tasks page (`/history`) gathered work by walking `world.Projects → Sessions → Tasks.Rows`, so rows inherited project-by-conversation recency and this window's own unlanded work sat pinned at the bottom of its section. The pass now seeds from `world.Work()` instead, and the four authorities (file, this project, other windows, away) hold their order across the whole page."
---

Before, the tasks page gathered work by walking the project→conversation→task
nesting, so the order inside each section was whatever recency the conversation
and project happened to carry. That meant unlanded work — tasks this window had
started but the file had not seen yet — was always drawn last in its section,
under every landed task from the same project.

Now the page seeds its pass from `session.World.Work()`, which orders by task
facts — family, section, recency, tiebreak — rather than by conversation nesting.
The same order holds across all four authorities (the file, the project's
in-memory index, other windows' presence, and away work), so a task's position
does not shift when its section is drawn.