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
