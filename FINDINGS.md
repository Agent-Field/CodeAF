# Findings

## Task

Inspect the named `internal/provider` late-first-token rescue test and the rescue and cancellation seam it drives. Identify the event ordering that decides the winning stream and loser cancellation. Do not inspect or change `internal/provider/pool`.

## Settled facts

- Base revision is `39ffbc548` on the current task branch.
- `TestALateFirstTokenIsRescuedByTheAlternativeAndTheLoserIsCancelled` failed once during a full gate run under machine load and passes when run alone.
- The scenario starts an alternative when the primary stream's first token is late, selects a winner, and cancels the loser.
- The observed failure is confined to `internal/provider`. The pool package passed in the same run.

## Ruled out before inspection

- Machine-load reproduction is out of scope. Any reproduction must force the ordering with a hook, barrier, or delayed token.
- Changes to `internal/provider/pool` and files outside this note are out of scope for this task.

## Pending inspection

Exact deciding events and file locations, test-versus-product classification, real-request reachability, and the smallest deterministic change remain to be established from the named seam.
