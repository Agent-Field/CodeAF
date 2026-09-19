# Survivor leaf-end findings

Run folder: `audit-notes/c303-leaf-end/`.

## Task

Map the ordering at the `internal/exec` leaf-end seam that terminates survivor processes and records how many were ended. This task changes only this evidence note.

## Settled facts

- `internal/exec/jobs_test.go:410` names `TestLeafEndTerminatesSurvivorsAndNotesCount`.
- A gate run under box load once reported `process 0 survived leaf end`.
- The named test passes when run alone.
- Leaf end is intended to terminate survivor processes left by a leaf and note the number ended.
- The reported failure is confined to `internal/exec`.

## Not yet established

The deciding event ordering, whether the defect is test-side or product-side, real-leaf reachability, and the smallest deterministic correction remain open until the named seam is inspected.
