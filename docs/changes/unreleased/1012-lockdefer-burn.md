---
kind: internal
title: The known-red ledger is empty and gone, and every guarded lock defers its unlock
pr: 1012
surface: [engine, chat, build]
invalidates:
  - "`.github/known-red.txt` held one entry, `TestEveryLockInTheGuardedTreeUnlocksFromADefer`. That test is green on a clean tree now, and the ledger it was the last line of is deleted — along with `internal/ci`'s `knownred_test.go` ratchet, whose own header said the burn-down commit deletes it."
  - "`internal/provider` and the last `cmd/aforge` stragglers held their mutexes with the Unlock on the success path, so an absorbed panic could wedge them. Each critical section is now a small method that holds the lock from a defer; no behaviour changed."
---

Forty-seven sites across thirteen `internal/provider` files and seven across
`cmd/aforge` were the snapshot / test-and-set / then-release idiom with the
`Unlock` on the success path — the provider half of the burn #431 started. Each
became a small method that holds the lock from a `defer` for its whole body and
answers what the caller needs, with the slow or re-entrant tail left outside so
nothing self-deadlocks. With the last loose site converted, the known-red ledger
burned to zero: the file and its ratchet test are deleted together, and every
reader (`make test`, `scripts/laws.sh`, both workflows) already treats an absent
ledger as "skip nothing". The shared `repositoryRoot` helper moved to
`internal/ci/root.go` so the remaining ci tests keep their door.
