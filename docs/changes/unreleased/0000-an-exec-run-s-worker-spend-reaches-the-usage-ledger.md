---
kind: fixed
title: an exec run's worker spend reaches the usage ledger
pr: 0000
surface: [engine]
invalidates:
  - "A headless `codeaf exec` run's worker calls never reached `usage.jsonl`: the door ran its own agent outside `internal/session`, so no row was ever minted and the status row's spend, the run cap and the pool's own accounting all under-counted every exec run — only the pool's judge rows and the chat seats were written. The door now leaves one row per run, under the run's own id, seated and roled as the worker."
---

`codeaf exec` talks to the provider through `internal/exec`'s own loop, which is
not the session engine and never was — so the one door every call's money goes
through (`session.RecordUsage`) was never reached, and a headless run spent real
money that no spending surface could see. The door now mints a row from what the
loop reported at its tail, so the run's cost and tokens land in the ledger the
limits are read from, whether or not anything else runs.

THE ROW IS MINTED AT THE DOOR AND NOT INSIDE THE RUNNER, and that is forced
rather than chosen: `internal/session` imports `internal/exec` (the attribution
law in `beltfacts.go`), so the runner cannot import the ledger's package without
a cycle. The grain is therefore one row per run where a chat seat writes one per
call — the row's shape is a seat's exactly, with the run's whole spend on it and
its request count in `calls`, and only its width differs. It carries two names:
`worker` as the role, in the ledger's own vocabulary, and the worker seat, since
that is the chair this run actually ran in. A run that made no call leaves
nothing, because `session.RecordUsage` refuses an all-zero row, and the door
waits on the background writer before it exits so the last row is on disk.
