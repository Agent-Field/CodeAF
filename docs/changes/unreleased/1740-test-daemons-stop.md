---
kind: fixed
title: test runs stop the engine daemons they start, and a started daemon inherits no open files
pr: 1740
surface: [build, engine]
invalidates:
  - "A test run left its engine daemons (`furrow serve`) running after it finished, and a daemon could hold open files its starter was handed, such as a lock taken by a wrapper script. Now the test binaries of cmd/codeaf, internal/session and internal/tui3 stop every daemon they started before they exit, and a daemon started under a test exits after one idle minute if its test run crashed. Any daemon codeaf starts now inherits only stdin, stdout and stderr. The daemon a person's own codeaf starts still outlives it, as before."
---
