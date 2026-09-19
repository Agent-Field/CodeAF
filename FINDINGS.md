# Findings

Task c295: make a conversation rail task row open the store-backed task page at the gesture, for running or finished tasks, while preserving room fallback when no page exists and returning to the conversation on escape.

Known before implementation: rail clicks currently flow through `openRailRoom` to `openRoom`; `PlanTaskPage(number)` supplies the task page; stable work-tab frames must not close a finished page; frame drawing must not read the store or call the agent. Work is limited to `internal/tui3`, its focused tests, and `internal/manual/chat/reading-a-task-page.md`.

The implementation seam is the rail gesture: try the existing task-page reader there and only fall back to the room when the store has no page. The page state already owns escape/back behavior; frame rendering must consume cached page state only.

The committed c295 tests fail at the intended seam: neither click nor enter reads `PlanTaskPage`, and no page survives to paint. The rail page needs an explicit conversation-overlay latch rather than the settling work tab, while reusing `taskSheetPlan` and `taskPlanKey`.

The first implementation compile exposed one missing standard-library import only (`strconv` for the rail number). No design change is needed.

The focused c295 tests now pass. The implementation adds a rail-only page latch: the gesture reads once, stored pages paint from cached state and remain open after settlement, while absent pages retain the existing room path.

The manual already had the requested finished-task rail guidance. The remaining contrary wording was the old missing-transcript sentence and one reference to a finished room; both are removed or changed to the stored page.

The broad focused run found an order-dependent fallback defect: using `planOn` to infer whether the gesture's page read succeeded can observe unrelated page state. The gesture needs an explicit boolean answer from a page-opening helper.

The required focused suite passes with `CODEAF_TASK_BELT` unset. Final verification now covers formatting, build, vet, focused tui3 behavior, the four named law packages, and all laws.

Final verification passed formatting, build, tui3 vet, focused tui3 tests, and the four named law packages. `make test-laws` reached all 27 packages; only five pre-existing `cmd/codeaf` keptnothing tests failed before running a task (default-model setup returned exit 2), while `internal/tui3` and every other package passed. A clean retry will distinguish transient host setup from this internal/tui3-only change.

## Manual contradiction follow-up

`internal/manual/chat/tasks.md` still has current-facing directions that say a held task row opens its room, despite the stored-page behavior documented elsewhere. The remaining task is to make those directions say that a stored task opens its page running or finished, while a row without a stored page keeps its prior room or card path.
