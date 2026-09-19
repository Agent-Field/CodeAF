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

## Next step

Add a test-only first-token barrier to A in the named seam test and run the focused test. This probes whether forcing B-before-A exposes a product failure or instead confirms the ordering map that the existing failure is only an uncontrolled test expectation. Record either result and its exact command before committing the seam change.

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

## Forced-order evidence

The smallest seam is `lanestub.Profile.FirstTokenUntil` on lane A in `internal/provider/hedge_test.go`. A cleanup closes the channel only after assertions, so A cannot emit its first token before B completes. The focused command `go test ./internal/provider -run '^TestALateFirstTokenIsRescuedByTheAlternativeAndTheLoserIsCancelled$' -count=20` passed all 20 forced-order runs in 4.814 seconds.

This null reproduction confirms the ordering map rather than exposing a product defect. With B forced to finish first, B wins and A is observed cancelled. There is therefore no deterministic wrong product outcome to encode as a failing regression at this seam. The pre-fix failure is the uncontrolled test-side expectation, and the smallest change is the test-only barrier now applied.

## Ruled-out paths

- No evidence supports a winner overwrite. `decide` rejects every caller after `winner` is set.
- No evidence supports a loser omitted from cancellation after it is registered. `decide` returns every nonwinner arm and `commit` cancels each one.
- No evidence supports deriving the winner from report timing. The report is populated from the committed result during settlement.
- No inspection or change was made under `internal/provider/pool`.
- Forcing B before A does not reproduce a wrong winner or uncancelled loser in 20 focused runs.
- A deterministic failing product regression is ruled out by this seam evidence. Making A win and continuing to demand B would only encode an invalid expectation, not a product defect.

## Fix decision and focused verification

Decision: test-side expectation. The product chooses the first arm whose completed result reaches `commit`; the original test used TTFT durations as if they guaranteed that order. The forced `FirstTokenUntil` signal now makes B completion precede A's first token, so the assertion reads a deliberately ordered decision rather than scheduler timing.

Real-request reachability: a real request can legitimately produce either winner because the arms execute concurrently, but the inspected decision path does not expose an uncancelled loser. `decide` fixes one winner while holding the race mutex at `internal/provider/hedge.go:1549-1564`, and `commit` cancels every other registered arm at `internal/provider/hedge.go:1521-1542`.

Deciding events remain the stall and rescue dispatch at `internal/provider/hedge.go:654-687`, the rescue gate and start at `internal/provider/hedge.go:897-977`, completed-result send and receive at `internal/provider/hedge.go:537-540` and `internal/provider/hedge.go:324-360`, the mutex-protected winner decision at `internal/provider/hedge.go:1549-1564`, and loser cancellation at `internal/provider/hedge.go:1521-1542`.

Ruled out: winner overwrite, omission of a registered loser from cancellation, report timing as the winner source, a product-side fix, changes under `internal/provider/pool`, changes outside `internal/provider/hedge_test.go` and this findings file, machine-load reproduction, and probabilistic timing adjustments.

## Pre-PR verification plan

The implementation and focused verification are complete. The remaining step is the required five-command verification run on the committed tree. I will record `git rev-parse HEAD^{tree}` immediately before and after the five separate commands. The commands are `go build ./...`, `scripts/one-suite.sh go test ./internal/provider/...`, `gofmt -l ./cmd ./internal`, `go run ./cmd/codeaf-changes check`, and `go test ./internal/guard ./internal/namelaw`. Acceptance requires all commands to pass, gofmt to print nothing, and the two tree hashes to match. I will not amend or modify the verified tree afterward.
