# Findings

Task c295: make a conversation rail task row open the store-backed task page at the gesture, for running or finished tasks, while preserving room fallback when no page exists and returning to the conversation on escape.

Known before implementation: rail clicks currently flow through `openRailRoom` to `openRoom`; `PlanTaskPage(number)` supplies the task page; stable work-tab frames must not close a finished page; frame drawing must not read the store or call the agent. Work is limited to `internal/tui3`, its focused tests, and `internal/manual/chat/reading-a-task-page.md`.

The implementation seam is the rail gesture: try the existing task-page reader there and only fall back to the room when the store has no page. The page state already owns escape/back behavior; frame rendering must consume cached page state only.

The committed c295 tests fail at the intended seam: neither click nor enter reads `PlanTaskPage`, and no page survives to paint. The rail page needs an explicit conversation-overlay latch rather than the settling work tab, while reusing `taskSheetPlan` and `taskPlanKey`.

The first implementation compile exposed one missing standard-library import only (`strconv` for the rail number). No design change is needed.

The focused c295 tests now pass. The implementation adds a rail-only page latch: the gesture reads once, stored pages paint from cached state and remain open after settlement, while absent pages retain the existing room path.
