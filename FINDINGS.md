# internal/connect fixed-port investigation

## Task

Establish every internal/connect location that fixes port 8765, every path that binds it, whether non-test product code pins that port, and the smallest collision-proof way to preserve the busy-first-door fallback proof.

## Settled facts

- Tests in `internal/connect` bind `127.0.0.1:8765`.
- The anchor is `TestSlackBeginAuthUsesTheSecondDoorWhenTheFirstIsBusy` and its neighboring tests.
- Unrelated processes can hold port 8765, causing the internal/connect gate to fail or wait.

## Ruled out so far

- The heavy-suite lock is not a solution because it serializes project suites, not unrelated machine processes.
- Manufacturing an external collision or interfering with another process is not an acceptable test mechanism.

## Open evidence

File and line locations, binding reachability, the product-versus-test conclusion, and the smallest safe change remain to be established from the base tree.
