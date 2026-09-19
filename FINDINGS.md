# Late first token rescue findings

## Opening note

Task: use the forced-order regression evidence to decide whether the nondeterminism is in the test expectation or the product rescue path, then make the smallest deterministic change within `internal/provider`.

Established from the handed-off facts: the named regression failed once during a loaded full gate and passes alone; `internal/provider/pool` passed and is out of scope. A predecessor has produced a forced-order failing regression whose evidence is the input to this step.

Not yet established: the deciding event order, whether a real request reaches any product ordering hole, and the smallest correct fix.

Ruled out: changes to `internal/provider/pool`, machine-load reproduction, busy loops, and probabilistic timing adjustments.
