---
kind: changed
title: the heavy test suites run in shards and the tui3 harness stops waiting out empty waiters
pr: 1419
surface: [build, docs]
invalidates:
  - "internal/tui3 took about 341s serially and about 80% of it was the harness waiting 150ms per drive step. A synchronous fake now registers its channels, an empty one costs nothing, and make test runs tui3 and session as SHARDS processes of one binary. tui3 takes about 11s on an eight-core box; SHARDS=1 is the old serial road."
  - "TEST_TIMEOUT was the ceiling for a whole heavy package. It is the ceiling for each shard now, still 15m."
  - "A new test fake that hands the surface a channel is not free by default. It keeps the full 150ms budget until it implements harnessWaiterQueue, and it must not implement it for a channel any goroutine writes."
  - "make test-laws spent 40s on a WSL2 laptop in one relay test, because a closed loopback port hangs there until a 20s dial timeout. The test dials with 50ms now and the relay's law has a file of its own, so the laws run takes about 23s there."
  - "scripts/one-suite_test.sh ran nowhere and blocked forever without bwrap. make pr-ready runs it and scripts/shard-test_test.sh when a change touches scripts/ or the Makefile, and it skips its namespace arms without bwrap."
  - "TestARecallThatLandsBeforeTheFirstWordIsAskedAgainWithIt hung about 1 run in 40 on two CPUs. It was the test's ordering, not the product; the fake recall now answers only once the first request is in flight."
---

Perf laws in this change count bytes and harness decisions, never time: an
allocation count and a stopwatch both passed against the regressions they were
written for.
