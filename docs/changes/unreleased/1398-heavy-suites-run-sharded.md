---
kind: changed
title: make test runs internal/tui3 and internal/session as concurrent shards
pr: 1398
surface: [build, docs]
invalidates:
  - "A heavy package ran as one serial go test process: tui3 took 341s on an eight-core box. make test now runs it as SHARDS processes of one compiled binary (default min(8, CPUs)); tui3's run phase is 63s there, and SHARDS=1 is the old serial run."
  - "TEST_TIMEOUT was the ceiling for a whole heavy package. It is the ceiling for each shard now, still 15m."
  - "make test PKGS=./... held the suite lock for one go test over the tree. It holds it for the rest of the tree and then for each heavy package's sharded run in turn."
  - "scripts/one-suite_test.sh ran nowhere and blocked forever without bwrap. make pr-ready runs it and scripts/shard-test_test.sh when a change touches scripts/ or the Makefile, and it skips its namespace arms without bwrap."
---
