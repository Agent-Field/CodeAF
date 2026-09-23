---
kind: internal
title: the dead-relay test fails fast on every machine, and the relay's law has a file of its own
pr: 1394
surface: [build]
invalidates:
  - "`make test-laws` spent 40 seconds on a WSL2 laptop in one relay test, because a closed loopback port hangs there until the 20-second dial timeout. The test now dials with a 50ms timeout and the laws run takes about 23 seconds on that box."
  - "`internal/relay/server_test.go` held the package's go/ast law, so `scripts/laws.sh` swept all nine behavioural relay tests into the gate. The law is in `server_law_test.go` now and only it is selected."
---

A file that mixes a law with slow behaviour makes every pull request pay for the
behaviour, which is what `scripts/laws.sh`'s own header asks a file not to do.
