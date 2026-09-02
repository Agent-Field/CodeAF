---
kind: fixed
title: one model has one ledger identity, at every door and in the file already on disk
pr: 353
surface: [engine]
invalidates:
  - "PR #300 read as the fix for the split ledger identity. It closed two of the four doors — the beat and the sighting side — and its own entry named the rest as still open. All four are closed now: hedge.go's settle folds a raced arm's sighting, and the sheet's Wants, Refresh and cache file key on the id the router publishes an endpoints page under."
  - "The lane ledger was believed to key only forward, so a machine that had already run this build kept its split rows for ever. Beliefs, prior spreads, the four-level chains, their drift sums, the thinking chain's model|rung leaves and the quality evidence above a pair are all re-keyed as they are loaded, and the merge that writes is the merge that folds — the file is written back with one row."
  - "catalog.Servable answered a cold catalog's question with the id as written, which lane.LedgerModel could not tell from a real answer and therefore remembered. A catalog with no rows yet answers the EMPTY STRING; LedgerModel memoises only answers the catalog could give, and an unanswered ask costs one unfolded call rather than a whole run keyed on the alias."
  - "internal/lane/lane.go's ID.bare was the ledger's key normalisation. It is not: ID.key is, and it is bare plus the injected fold. ID.bare still exists and is still what the sheet's tag map uses."
---

A belief is only worth keeping if the next session asks for it under the same
name, and the model this build ships as its default is a floating alias — one
model that the wire, the endpoints page and the ledger each had a different
name for. #300 made the beat and the sighting side agree. This makes every door
agree, and makes the rows already on disk agree with them.

The four doors are shut at their own seams and the ledger keys through one
function behind them ([lane.ID.key]), so a spelling that reaches it from a
config slot, from a wire answer or from a row loaded off disk lands on the same
key. The fold is idempotent, which is what lets those three paths meet.

What was already written down is folded where it is read. A file holding the
facts under the served id and the timing under the alias comes back as one
belief with both halves — the existing `fresher` reconciliation, applied to
rows that turned out to be one lane's — and the four-level hierarchy is
re-keyed beside it, so a pair is not restored under a model whose parents say
nothing about it. It is one pass that allocates nothing when there is nothing
to move, which is every process after the first one has written the file back.

And the hazard #300 left named: a process that asked its catalog one moment
before it landed used to remember the unfolded name for the rest of its life.
A catalog that cannot answer now says so, and only real answers are remembered.

The wire is still unchanged. It sends the alias, the panel still shows it, and
the profile still keys its history its own way. Only what names a belief
resolves.
