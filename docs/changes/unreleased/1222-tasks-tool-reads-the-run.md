---
kind: fixed
title: the conversation's tasks tool reads a run's tasks, by the names the rail shows
pr: 1222
surface: [chat, engine]
invalidates:
  - "Under the bash belt the `tasks` tool could not see a hand-off at all: `tasks {\"id\":\"1\"}` answered `No task \"1\" in this project` and `tasks {}` answered `No tasks have run in this project yet` beside a rail showing `#1 done`, and the conversation then redid the work's checks by hand. It now lists the run's tasks first and reads one from the run's own record."
  - "A run's tasks had no name a conversation could use. A hand-off is `#2`, the number its card and the rail show, and a part the run made is `#2.1` by its place under it. A store id is never shown."
  - "The manual said a person could open an ask box on the run's pane and ask about one task. No screen opens that box. The way to ask what a task did is to ask the conversation, and the manual now says so."
---

Seen on the real binary on 2026-09-18: a hand-off ran, split in two, was
checked and landed, and one turn later the conversation was told no task
existed. It set out to check the work by running the suite itself and spent
five minutes on an approval that timed out.

Tasks from earlier sittings and other windows are still listed after the
run's, and a name the run does not hold is answered as it always was.
