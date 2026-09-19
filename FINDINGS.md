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

## Forced-order regression step planned

The deterministic seam needs no product hook. The new run test will make the root worker call `store.Done`, then block on its worker context before returning its report. This guarantees the store exposes a terminal root while the worker return is unavailable. The supervisor timer then starts another `pass`, `pass` returns the terminal outcome, and `drain` cancels the blocked worker. The assertion will require the root check that the current product path skips, so the focused test must fail on this base tree with zero checks.

This delayed worker step directly forces source ordering 2 without load, busy loops, or parallel process spawning. It also avoids `run.Start` and therefore cannot modify or exercise the fenced pull request 1210 stored-result fallback. The command-level tests remain evidence of real reachability, while this smaller `internal/run` test isolates the product seam. Before editing the test, I will commit this checkpoint.

## Deterministic failing regression evidence

The delayed-step test `TestSupervisorChecksASelfFinishedRootBeforeAcceptingItsStoredEnding` now forces the suspected ordering. Its root worker writes `store.Done` and waits for context cancellation before returning. The next supervisor pass therefore sees the terminal root before any worker return can be selected; the terminal road enters `drain`, cancellation releases the worker, and its return is dropped.

Focused command on the unchanged product code:

```text
go test ./internal/run -run '^TestSupervisorChecksASelfFinishedRootBeforeAcceptingItsStoredEnding$' -count=1
--- FAIL: TestSupervisorChecksASelfFinishedRootBeforeAcceptingItsStoredEnding (0.34s)
    review_test.go:469: check tasks = 0, want exactly one for the root observed done before its worker returns
FAIL
```

This establishes a product path, not a test-side early read: `Supervisor.Run` has already returned `OutcomeDone` before the test inspects the store. It rules out scheduler load as necessary, rules out `AddRootCheck` refusing all finished roots, and rules out the command adapter reading before the supervisor returns. The sibling check-model failure remains consistent with this seam but is not independently proven to have the same cause. The smallest prospective product fix remains at the terminal-root decision in `pass`; `run.Start` and the fenced pull request 1210 path are untouched.

## Fix decision before product edit

The forced regression from commit `e87c9b8e8` confirms the terminal-root pass is product-reachable and returns before review creation. The correction will stay inside `internal/run.Supervisor`: when a pass sees a done root while its worker is still in flight, it will seat the root check from the persisted root result before accepting the ending. Review seating must be idempotent within the supervisor because the delayed worker return is later absorbed through the ordinary path. The pass must then continue so the newly ready check can launch; simply returning an empty outcome would strand the deterministic delayed worker. No `run.Start` code or #1210 fallback will change.

Before editing product code I will add only the minimum in-memory review association guard and terminal-root branch needed for that ordering, then run the forced regression plus the existing root review tests.

## First fix execution

The first focused run no longer reports zero checks, but the forced test times out with `OutcomeIncomplete` after 10 seconds. This disproves that seating alone is sufficient for the delayed-worker harness. Before changing the product again I will inspect the fake check worker and completion path to distinguish a test fixture that never lands the new check from a supervisor counter or cancellation defect. The product edit remains uncommitted until this is resolved; `run.Start` is still untouched.

## Timeout diagnosis before second edit

Inspection shows the check worker is not the stall: `fakeSeat` completes unscripted checks immediately. The root remains present in `s.cancels`, and `rootAwaitingWake` therefore prevents `completeTree` from closing the root even after the check lands. Once a pass observes that the root worker has already finished itself in the store, the supervisor must cancel that worker context as well as seat the check. Its return then follows the normal absorb path, removes the in-flight root entry, and the in-memory association prevents a duplicate check. This cancellation is confined to the already-done root ordering.

## Minimal fix focused evidence

After canceling only the already-self-finished root worker, the forced ordering and both existing root-review laws pass together:

```text
go test ./internal/run -run 'TestSupervisor(ChecksASelfFinishedRootBeforeAcceptingItsStoredEnding|ChecksAChildlessRootThatCompletedItselfInTheStore|RootWaitsOnAnOpenCheckTask)$' -count=1
ok github.com/Agent-Field/codeaf/internal/run 0.519s
```

The forced case now proves the check exists before `Supervisor.Run` returns. The implementation changes only terminal-root handling and review idempotence in `internal/run/run.go`; no `run.Start` line changed. The incidental attempt to pass `FINDINGS.md` to `gofmt` reported its expected Markdown illegal-character error and made no change; both touched Go files were successfully formatted before the passing test.

Next I will commit this passing product step, then exercise the command-level self-finished-root and check-model paths repeatedly as focused reachability checks.

## Repeated focused reachability evidence

The forced supervisor ordering passed 20 of 20 in one process, and the two command paths passed together 15 of 15:

```text
go test ./internal/run -run '^TestSupervisorChecksASelfFinishedRootBeforeAcceptingItsStoredEnding$' -count=20
ok github.com/Agent-Field/codeaf/internal/run 7.228s

go test ./cmd/codeaf -run '^(TestDoOnTheRunEngineChecksASelfFinishedRootAndExitsZeroWhenItHolds|TestDoOnTheRunEngineSeatsACheckOnTheCheckModel)$' -count=15
ok github.com/Agent-Field/codeaf/cmd/codeaf 10.332s
```

This confirms the command-level real `codeaf do` road now seats the self-finished root review before returning, and it gives no evidence that the sibling check-model failure has a separate cause. The sibling is consistent with the repaired seam, not proven uniquely attributable to it. Next I will run the five required pre-PR commands separately, with tree hashes before and after and no amendment afterward.

## Pre-PR failure and fenced-path correction

The required `go test ./cmd/codeaf/...` exposed one failure: `TestDoOnTheRunEngineRootStoreFinishBeforeWorkerReturnNamesRootResult` returned incomplete. The other four commands passed, `gofmt -l` printed nothing, and the tree hash stayed `8c1691558dad17e6593a61993dc4cc7f728f9194` before and after.

The cause is exact: the new terminal branch canceled an in-flight self-finished root even when `addReviewCheck` was a no-op because the run had no review round. That changes the fenced #1210 path. Before editing, the correction is to reload the root after attempting review creation and cancel the worker only if `AddRootCheck` actually reopened the root. With reviews off, the terminal result road remains byte-for-byte behavioral equivalent and returns immediately without cancellation.

## Fenced test still failed, revised minimal boundary

The conditional cancellation still fails the #1210 test because reviews are enabled there: `AddRootCheck` reopens the root, cancellation interrupts the real session worker, and `absorb` correctly records that worker error as an incomplete root. Cancellation is therefore not part of the product fix.

The deterministic regression's worker was waiting specifically for drain cancellation, which over-constrained the implementation. I will keep the forced terminal observation but release its delayed root return when the check worker starts. With two slots, this still proves the supervisor observed Done and created the check before receiving the root return, without requiring product code to cancel a successfully self-finished worker. The product change then consists only of seating the review and continuing the pass, plus idempotence when the ordinary return is absorbed.

## Final seam decision before correction

The #1210 command test still fails without cancellation because `AddRootCheck` reopens the root while the session worker is still inside the shell command after `plandb done`; the worker then reports an error against its no-longer-terminal task. Therefore creating the check before the worker return is unsafe and bears directly on the fenced behavior.

The smaller fix is to refuse the terminal outcome while the root worker is in flight. The pass leaves the stored Done untouched and waits for the worker return; ordinary `absorb` then creates the check at the established safe point before any run ending. The forced regression will delay the return for several pass intervals, which deterministically exposes Done to the supervisor, then return normally. On the base implementation the pass ends the run during that delay; with the fix it waits and seats the check. No cancellation, early root reopen, `run.Start`, or extra idempotence is needed.

## Final seam first execution

The three command tests, including the fenced #1210 result test, passed 10 of 10 with the wait-for-return fix. The internal forced regression did not compile because its new deterministic delay omitted the standard-library `time` import. This is a test-only import omission, not product evidence. I will add that import, rerun the focused internal and command sets, then commit the correction if both pass.

## Corrected minimal fix evidence

The final implementation passed the forced regression, both established root review laws, and all three command-level seams 10 times each. The fix now only makes `pass` defer a stored Done outcome while the root worker return remains in flight. The worker return is absorbed normally, creates the check, and preserves the #1210 stored-result path. The deterministic test forces terminal observation using a 500 millisecond delayed worker step, several times the 100 millisecond supervisor pass interval, then returns normally.

Commands:

```text
go test ./internal/run -run 'TestSupervisor(ChecksASelfFinishedRootBeforeAcceptingItsStoredEnding|ChecksAChildlessRootThatCompletedItselfInTheStore|RootWaitsOnAnOpenCheckTask)$' -count=10
ok github.com/Agent-Field/codeaf/internal/run 7.068s

go test ./cmd/codeaf -run '^(TestDoOnTheRunEngineRootStoreFinishBeforeWorkerReturnNamesRootResult|TestDoOnTheRunEngineChecksASelfFinishedRootAndExitsZeroWhenItHolds|TestDoOnTheRunEngineSeatsACheckOnTheCheckModel)$' -count=10
ok github.com/Agent-Field/codeaf/cmd/codeaf 19.986s
```

## Final pre-PR checkpoint

The corrected implementation and regression are committed at `ee6ec8b36`. I will now rerun all five required pre-PR commands separately, recording the tree hash before and after. No source change or amendment will follow a passing gate.
