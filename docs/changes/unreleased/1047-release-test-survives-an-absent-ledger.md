---
kind: fixed
title: the release workflow's test job survives the deleted known-red ledger
pr: 1047
surface: [build]
invalidates:
  - "The `Release` workflow's `test` job read `.github/known-red.txt` with an unguarded awk under `set -euo pipefail`; once #1012 deleted the ledger, that line exited 2 inside an assignment and the step ended before `go test` ran, on every channel but `dev` — which skips the job by design, so no push to `dev` could show it. The first staging build after the deletion published nothing. The ledger is now read only when it is present, the way `scripts/laws.sh` reads it, and a test runs the step's real prologue under the real shell options with the ledger absent, empty, comments-only and naming a test."
---
