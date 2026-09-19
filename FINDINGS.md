# Findings

Task: make a rail task row open the stored task page at the gesture, while preserving the existing room fallback and returning to the conversation on `esc`.

Known facts:

- A rail row currently opens through `openRailRoom` and `openRoom`; a missing transcript can therefore show `roomGoneWord` even when the run store has the task page.
- `planReader` reads task pages, and `taskPlanBody` draws them; plan rows already open task pages from the tasks place.
- The store lookup must happen on click or `enter`, not while drawing a frame.
- A stored page must remain open after the run settles; `workTabStable` currently removes the conversation work tab after settled frames.
- If the store has no page for the row, the current room behavior must remain unchanged.
- Tests should use `planAppWith` and `planFake.pages`, cover click/enter, settled frames, `esc`, fallback, and prove frame drawing makes no agent call.\n- The click seam is `task.go`’s rail selection into `openRailRoom`; the existing tasks-place page-opening behavior and task-page child navigation provide the enter and nested-page contracts to replay.\n- The page must be opened independently of `workTabStable`/`workTabFrame`, so settled-frame cleanup cannot close it; held store tasks must bypass the `roomGoneWord` path entirely.
