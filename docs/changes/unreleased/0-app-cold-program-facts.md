---
kind: changed
title: A handed-over task's list row names its program, ending and branch
pr: 0
surface: [engine]
invalidates: []
---

The app's task list used to say only that a handed-over task had failed. A
program's name, why the task ended and the branch its work was kept on were
stored with the task but never sent, and a task a person stopped looked like a
crash. Each task now carries `program`, `ending` and `branch` when its row
recorded them, on the list, the page and live updates, with no log read. A
person's stop reads `ending: "stopped"` beside `state: "failed"`. A task
recorded before these existed carries none of them: nothing is guessed from its
title, and an older failure is not relabelled as a stop. The branch is context
only and does not allow a push, post, fix or land.
