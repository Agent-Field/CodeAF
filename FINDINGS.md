# Self-finished root check task investigation

## Assignment

Investigate why a self-finished root can end a `codeaf do` run without a check task in the store. Determine the ordering at the run engine seam, decide whether it is a test-side race or a product path, and prove any reproduction by forcing the ordering rather than by machine load.

## Settled facts

- `TestDoOnTheRunEngineChecksASelfFinishedRootAndExitsZeroWhenItHolds` intermittently reports zero check tasks on the base tree.
- `TestDoOnTheRunEngineSeatsACheckOnTheCheckModel` may exercise the same seam, but that relationship is not established.
- The behavior is present on trunk and was not introduced by pull request 1215.
- Pull request 1210 fixed a separate root-result race in `run.Start`; that fix is shipped, fenced, and must remain untouched.

## Remains to check

- Enumerate, with file and line evidence, each ordering between observing the root as finished and creating its check task.
- Force the suspect ordering with a hook, barrier, or delayed test step.
- Decide from runtime evidence whether the missing task is a test-side observation race or a reachable product path.
- If it is a product path, identify the smallest change that guarantees the check task exists before the run ends.

## Ordering-map checkpoint before source tracing

The opening note is committed at `0db2957f2`. The two named command tests have now been located at `cmd/codeaf/do_engine_test.go:512-558` and `cmd/codeaf/do_engine_test.go:645-735`. Both call `doErrand` synchronously, so the store assertion in the first test occurs only after the command runner returns. This rules out a test assertion that simply reads the store while `doErrand` is still executing. It does not yet rule out the run engine returning before an internal review-creation goroutine completes.

Next I will trace `internal/run/run.go` from `Start` through completion absorption and review creation, plus the command adapter that consumes the result. I will enumerate all branch orderings before drawing a test-versus-product conclusion. No product behavior has been changed, and the fenced pull request 1210 path remains untouched.

## Source ordering map

The decisive race is inside one `Supervisor.Run` loop, not between `doErrand` returning and the test reading the store.

1. Worker return wins. A real belt worker writes its own root completion in its worker goroutine, then `launch` sends its return on `finished` at `internal/run/run.go:317-326`. If `Run` receives that return at `internal/run/run.go:209-213` before the next `pass`, `absorb` recognizes the root and calls `addReviewCheck` at `internal/run/run.go:397-411`. For an already-done root, `addReviewCheck` uses `AddRootCheck` at `internal/run/run.go:498-512`. The open check then prevents final root completion until it lands, as exercised by `internal/run/review_test.go:414-445`.
2. Terminal-root observation wins. After the worker has written root done but before its return is selected, the loop may enter `pass` at `internal/run/run.go:204-208`. `pass` reads the terminal root and immediately returns its outcome at `internal/run/run.go:235-250`. `Run` then calls `drain` and returns at `internal/run/run.go:205-207`. `drain` waits for the worker goroutine but deliberately drops its queued return at `internal/run/run.go:329-352`. Therefore `absorb` and its root `addReviewCheck` call never execute. The root remains done with no check task. This is a product ordering reachable by the normal belt worker's own `plandb done`, not a post-return test race.
3. Supervisor-owned completion has no equivalent window. If the worker has not already completed the root in the store, only `absorb` handles its return. It calls `addReviewCheck` before `completeTree` at `internal/run/run.go:397-411` and `internal/run/run.go:466`, while `completeTree` can call `CompleteRoot` only afterward at `internal/run/run.go:611-625`.
4. Context completion also absorbs rather than drops every in-flight return at `internal/run/run.go:215-228`, so it does not create this particular missing-check ordering. Cancellation and failure paths do not represent a successful self-finished root and are ruled out by `internal/run/run.go:383-405`.

`TestDoOnTheRunEngineChecksASelfFinishedRootAndExitsZeroWhenItHolds` synchronously calls `doErrand` and reads the store only afterward at `cmd/codeaf/do_engine_test.go:512-558`, so its zero-task observation records the already-ended product run. `TestDoOnTheRunEngineSeatsACheckOnTheCheckModel` checks whether the check seat was ever built after the same synchronous command at `cmd/codeaf/do_engine_test.go:645-735`; its one observed failure is consistent with ordering 2, though that test's child-and-wake shape does not independently prove the root self-finished at the same instant.

The smallest prospective repair is to make the terminal-root road absorb a pending return for the root before it accepts terminal completion, or otherwise run the same root review-seating logic before returning. That change belongs at the `pass` terminal-root decision around `internal/run/run.go:248-250`, not in `run.Start`'s stored-result fallback at `internal/run/run.go:1231-1236`. The latter is the fenced pull request 1210 behavior and remains untouched.

## Evidence checkpoint before focused tests

Next I will run the existing deterministic run-engine tests for a childless self-finished root and for root waiting on its check. Passing them establishes the return-first ordering and review hold behavior. They do not force the terminal-observation-first ordering, so this task will not claim that existing tests deterministically reproduce the losing branch. No implementation or test behavior is being changed in this ordering-map task.

## Established and ruled out after focused execution

Command run:

```text
go test ./internal/run -run 'TestSupervisor(ChecksAChildlessRootThatCompletedItselfInTheStore|RootWaitsOnAnOpenCheckTask)$' -count=1
ok github.com/Agent-Field/codeaf/internal/run 0.127s
```

This establishes that when the worker return is absorbed, a self-finished childless root receives one check and the root is held until that check lands. Together with the synchronous command-test structure, it rules out the store assertion racing an active `doErrand`, and it rules out `AddRootCheck` universally refusing an already-done root.

The focused tests do not force the losing ordering where `pass` reads the terminal root before the return is absorbed. That missing forced-ordering test and any smallest product repair belong to the next investigative step, not this ordering-map output. The code inspection establishes why the observed command flake is product-side and exactly which branch skips review; it does not claim the sibling check-model failure has no other possible cause.

No Go source or test was changed. In particular, `run.Start` at `internal/run/run.go:1231-1236`, including the pull request 1210 stored-root-result fallback, is unchanged.
