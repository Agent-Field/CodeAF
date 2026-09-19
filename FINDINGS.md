# Findings

## Task

Make a rail task row open the task page held in the run store, while preserving the existing room fallback and returning to the same conversation on `esc`.

## Known

- The rail currently routes a row through `openRailRoom` and `openRoom`; a task page is already rendered by `taskPlanBody` through the plan reader.
- The store task number shown by the rail is sufficient to request that task's page, including its parts.
- The store lookup must happen only on the click or `enter` gesture. If no page exists, the row must keep opening its room.
- A task page must remain open after the run settles; frame drawing must neither close it nor call the agent.
- Tests should use `planAppWith` and `planFake.pages` and cover held and missing pages, ten settled frames, `esc`, no room for held tasks, and no agent call while drawing.
- The manual must say that a rail row opens the task page for running or finished work, and `roomGoneWord` must not be shown when the store holds the task.
