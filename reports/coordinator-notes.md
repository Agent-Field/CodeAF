# Coordinator notes

- Integration owns `Makefile`, `scripts/test-report.sh`, `internal/ci/testreport`,
  developer docs, changelog, PR, and any measured CI changes.
- Harness owns `internal/tui3` harness/fake lifecycle. Do not edit the workflow
  files or coordinator-owned paths without recording the contract here first.
- Heavy runs must use `scripts/one-suite.sh` when whole-tree and must not overlap.
  The coordinator will defer its final uncached relevant suite until the harness
  lane reports that its measurement has finished.
