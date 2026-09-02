---
kind: fixed
title: a test binary never writes into the model-call ledger of whoever ran it
pr: 352
surface: [engine, build]
invalidates:
  - "`calllog.PathFor` resolved a path for every caller: the profile directory when there was one and the state root otherwise. Under `go test` it now answers \"\" for any path that would land inside the state root the environment named — the unopened fallback, `Open(\"\")`, and `Open(dir)` on a profile directory derived from that root alike — so a test binary cannot write into the ledger of the person who started it. Outside a test binary it resolves exactly as it always has."
  - "A test that wants a model-call log to assert against can no longer let the log find its own way there: it says where the log goes, with `Open` on a directory of its own or with the `AFORGE_CALL_LOG` pin. Both still work under test; only an inherited path is refused."
  - "`internal/provider` and `cmd/aforge` each pin the log off in their own `TestMain`. Those pins are now belt and braces rather than the protection — the gate in `internal/calllog` covers every package, including ones that never mention the call log."
---

Running this repository's own suite — by hand, or through a leaf whose workspace is the
repository — put 356 rows of invented traffic into the launching profile's
`logs/calls.jsonl`: `vendor/vision-model` priced at $4.25, `work/model`, `sim/model`,
interleaved with one run's genuine rows. Anything summing that file afterwards reported
money nobody spent, and the fake vision row alone dwarfed the run beside it.

The refusal is one gate at the one place a path is resolved, rather than isolation in the
test helpers that build clients: that fix was already in the tree, in two packages'
`TestMain`, and the ten packages that reach a client without one are the whole defect. A
helper protects the packages somebody remembered; a gate at the resolution seam holds for
tests nobody has written yet. `TestNoTestInTheTreeWritesIntoTheLedgerOfWhoeverRanIt` is
the witness — it runs the five packages that leaked under a state root nobody else can
reach and asserts that ledger is never created, and it fails with 74 rows on the tree
before this change.
