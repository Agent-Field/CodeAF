---
kind: added
title: The Model Pool's index is seated at start-up from a seed in the binary
pr: 1093
surface: [chat, engine]
invalidates:
  - "`config.AutoIndex` was a seam nothing in the binary set, so a tier row that says `auto` picked on the catalog's published figures alone. `cmd/codeaf` seats it at start-up from the seed the binary carries or a fresher cached document, so the pool's measured quality reaches the picker."
  - "The binary carried no index of its own, and `codeaf pool show` said only `no index cached yet`. `internal/pool/index` embeds a seed document, and show names it — `no index cached yet · built-in seed of <date>`."
---

`internal/pool/index` embeds a seed measurement document (`Seed`, `SeedIndex`)
so a machine that has never fetched an index still has numbers to pick a seat
against. `cmd/codeaf` reads the cached `doc.json` beside that seed through
`index.Fallback` once at start-up — the newer `generated` day wins — and hands
the result to `config.AutoIndex` where `config.AutoModels` is already seated, on
every door that resolves a seat (`sharedCatalog`, and the three chat doors).
The read is start-up work and never on a run's path.

A background goroutine, started where the index is seated, refreshes the cache
through `pull` only when the mode allows reading and the build carries a public
key; with no key compiled in today it starts nothing. A fresh document lands in
the puller's own cache and is read at the next start — the running process keeps
the index it was seated with. `codeaf pool show` names the seed when no cache is
present, and its `--json` shape carries the index it read with a `source` field
naming `cache` or `seed`.
