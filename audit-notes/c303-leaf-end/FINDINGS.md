# FINDINGS

Task: determine by forced ordering whether leaf-end survivor termination or its noted count has an internal/exec test-side race or a reachable product ordering hole, then make the smallest deterministic correction.

Established from the work order:
- `internal/exec/jobs_test.go` has failed once under load because process 0 survived leaf end, while the named test passes alone.
- Leaf end is intended to terminate survivor processes and note how many it ended.
- The investigation and any correction are restricted to `internal/exec`.

Not yet established:
- The deciding event ordering and exact file:line locations.
- Whether the fault is test-side or product-side.
- Whether a product ordering hole is reachable from a real leaf end.
- The smallest deterministic correction.

Ruled out so far:
- Nothing beyond the settled scope in the work order.
