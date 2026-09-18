---
kind: internal
title: The lane a failed call names is read by internal/provider, not internal/exec
pr: 1105
surface: [engine]
invalidates:
  - "`internal/exec` had a private `failedLane` that compared a provider status itself. It is gone; the fact is `provider.FailedLane(err)`, beside `WithRetryAvoid`, with the same behaviour."
  - "`TestOnlyTheTaxonomyTurnsAStatusIntoAMove` failed on `santos/dev` at `internal/exec/linear.go`. It passes, and its allowlist stayed empty."
---
