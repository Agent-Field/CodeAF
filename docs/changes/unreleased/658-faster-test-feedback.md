---
kind: changed
title: focused tests now report progress and the slowest work
pr: 658
surface: [build, docs]
invalidates:
  - "Repeated Go test runs had no repository workflow for a named regression or machine-readable per-test timings. `make test-focus` selects a named regression, while `make test-report` forces fresh tests, preserves the Go build cache, shows progress, and writes structured durations."
---

The quick target mirrors deterministic light-gate feedback but explicitly does
not replace affected-package or full-suite acceptance.
