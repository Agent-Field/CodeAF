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
