---
kind: added
title: The seed index is regenerated from the relay by one in-repo command
pr: 0000
surface: [chat, engine]
invalidates:
  - "Nothing in the repository regenerated `internal/pool/index/seed.json` — it was a hand-authored snapshot, and `relay/tools/prime.py` only compared against it. `internal/pool/index/cmd/seedgen` fetches the signed index through `pull` (signature and size checked, the mirror read when the relay does not answer), reads it through `index.Parse`, and writes the seed deterministically, so the fallback a fresh install picks from is a verified copy of the pool rather than a figure that lags it."
  - "The seed carried none of what the relay now publishes: a `version`, an `installs` count on every cell, the `acceptable` metric and its rubric, the relay's floor on installs, and the relay's judge list. Regenerating it fills all of them in, and the copy names no metric and no cell address, so a metric or a dim this build has never seen passes through verbatim."
  - "The build's trusted pool key was a base64 literal in `cmd/codeaf` — package `main`, so nothing else could import it. It lives in `internal/pool/poolkey` now, and both the verb and the generator check signatures under the one spelling."
---

`internal/pool/index/seed.json` is the index a machine with no cache picks a
seat against, and it is the same pool the relay publishes: the one difference
is that the embedded seed's aliases are merged under the live document's, so an
alias the relay has since dropped from its own map survives rather than
vanishing on a verbatim copy.

`go run ./internal/pool/index/cmd/seedgen` regenerates it. The command fetches
the signed index through `internal/pool/pull`, reads it through
`internal/pool/index.Parse`, refuses a document the reader would not trust
whole — a version that is not an integer, a day that does not parse, a cell
below the floor — and writes the seed with its keys in a fixed order and its
cells sorted by metric, then by the metric's own declared dims. A metric or a
dim it has never seen is copied rather than understood, because the copier
names neither. It also refreshes the paper's cell table,
`docs/design/model-pool/data/seed-cells.csv`, from the same cells.

The same command with `-check` writes nothing and leaves on exit 1 when either
file has drifted, which is the release step in the runbook: the regenerated
seed rides the release's pull request and change note.
