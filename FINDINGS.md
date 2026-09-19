# c295 findings

## Task

Make a conversation rail task row open that task's stored task page when the store has it, while preserving the existing room fallback for rows the store does not hold. The page must remain open after a run settles, `esc` must return to the unchanged conversation, task parts must remain openable, and drawing frames must not call the agent. Update the task-page manual and ensure `roomGoneWord` is not drawn for a stored task.

## Bounded findings

- A rail click currently travels through `openRailRoom` to `openRoom`, which selects a room rather than consulting the task-page reader.
- The tasks place already opens a selected plan row with `taskSheetPlan`; `taskPlanBody` draws the stored page and supports opening its parts, while `closeTaskPlan` restores the prior view.
- `planReader` is the store-facing boundary used for `PlanTaskPage`; the store lookup must happen only in the click or `enter` gesture, never while drawing a frame.
- `openWorkTab` and `workTabFrame` manage and draw the conversation work rows, while `workTabStable` removes the in-conversation work tab shortly after all rows settle. A task page opened from a rail row therefore must not depend on the work tab remaining live.
- `roomGoneWord` belongs to the missing-transcript room path and should remain only as fallback behavior when no stored task page exists.
- Tests should use `planAppWith` and `planFake.pages` to prove page-over-room selection, persistence across ten all-done frames, `esc` restoration, missing-page fallback, and zero agent calls during frame drawing.
