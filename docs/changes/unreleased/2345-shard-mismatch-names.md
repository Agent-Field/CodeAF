---
kind: internal
title: The shard test runner names a test mismatch instead of exiting with no result
pr: 2345
surface: [build]
invalidates:
  - "A package missing from a sharded test run meant it never finished; a test list mismatch could also end the runner with no result line."
---

The run now fails and names the missing and extra tests, in any locale.
