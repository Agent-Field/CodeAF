# Self-finished root review findings

## Opening record

Task: determine, with forced ordering evidence, why a self-finished root can end a `codeaf do` run without a check task, decide whether the path is test-only or product-reachable, and make the smallest fix without changing the fenced #1210 behavior or unrelated engine code.

Established from the supplied regression evidence:

- `TestDoOnTheRunEngineChecksASelfFinishedRootAndExitsZeroWhenItHolds` intermittently observes zero check tasks on the base tree.
- The check-model sibling has one compatible full-run failure but no isolated reproduction, so it remains neither confirmed nor ruled out as the same seam.
- The #1210 root-result race fix is shipped and fenced. This work will not modify it or behavior bearing on it.

Not yet established:

- The exact ordering and file:line path that skips check-task creation.
- Whether the store read is early only in the test or a real `codeaf do` run can return before review is seated.
- The minimal safe correction.

Next evidence step: inspect the two named tests and the run engine path they execute, enumerate orderings around observing root completion and creating the check task, then update this record before forcing one ordering with a deterministic hook or barrier.
