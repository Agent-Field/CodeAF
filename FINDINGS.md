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

## Inspection result

This is a test-side timing expectation, not a product cancellation hole.

The deciding events are:

1. Silence evaluation enters the stall action at `internal/provider/hedge.go:654-662`, and the report action dispatches a rescue at `internal/provider/hedge.go:669-687`.
2. The rescue gate snapshots that the race is still undecided at `internal/provider/hedge.go:897-904`. The rescue is then started at `internal/provider/hedge.go:939-977`.
3. Each completed arm sends its result at `internal/provider/hedge.go:537-540`. The run loop receives that result and calls `commit` at `internal/provider/hedge.go:324-360`.
4. The first call to `decide` wins under the race mutex at `internal/provider/hedge.go:1549-1564`. A later completion cannot replace that winner.
5. After the decision, `commit` marks every losing arm and cancels it at `internal/provider/hedge.go:1521-1542`.
6. Settlement derives report winner and loser from the committed result at `internal/provider/hedge.go:2007-2029`. An unnamed primary cancelled before its first chunk remains unnamed by `laneOf` at `internal/provider/hedge.go:2052-2065`.

The named test scripts A's first token at 300 ms and B's at 5 ms at `internal/provider/hedge_test.go:582-587`, but it does not synchronize A behind B. Its assertion requires B to win and A to remain unnamed at `internal/provider/hedge_test.go:614-617`, then requires A's observed cancellation at `internal/provider/hedge_test.go:618-620`. If scheduling delays rescue startup or B long enough for A to complete first, the product correctly commits A, cancels B, and the test's fixed B-wins expectation flips.

A real request can reach either winner ordering because both arms run concurrently. It cannot reach an uncancelled loser through this decision path: the same successful `commit` that records the first winner enumerates all other registered arms and cancels them. Cancellation is intentionally asynchronous relative to returning the answer, which is why the test waits for the router observation.

The smallest deterministic change is test-only: hold A before its first token with a channel barrier that is released during cleanup, as the neighboring stall test does at `internal/provider/hedge_test.go:633-653`. Then B's completion, rather than elapsed-time slack, decides the winner. No product change is supported by this seam.

## Ruled-out paths

- No evidence supports a winner overwrite. `decide` rejects every caller after `winner` is set.
- No evidence supports a loser omitted from cancellation after it is registered. `decide` returns every nonwinner arm and `commit` cancels each one.
- No evidence supports deriving the winner from report timing. The report is populated from the committed result during settlement.
- No inspection or change was made under `internal/provider/pool`.
- Forced-order reproduction and implementation belong to downstream tasks. This task owns only the findings note and recon commits.
