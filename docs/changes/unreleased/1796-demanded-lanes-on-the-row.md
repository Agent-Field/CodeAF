---
kind: fixed
title: the call row carries the demanded set, so served ≠ asked means what it says again
pr: 1796
surface: []
invalidates:
  - "The call log's `Record.Lane` named ONE machine - the ranked head of the request's preference. Since the chooser was taught to demand the whole admitted set (`provider.only` with `allow_fallbacks: false`), the router may serve any member of that set, so a row where `served != lane` could no longer tell `the router chose a different member of the set we admitted` from `the router went somewhere we never named`, and the census family built on it (asked ≠ served) silently changed what it measured. The row now also carries `Record.Lanes`, the whole demanded set, filled from `laneChoice.Only` while the demand is still being sent and present only when the set has two or more members (a one-machine demand is left to `Lane` alone, which is also every pre-set-demand row). The census counts asked ≠ served only when served fell OUTSIDE the admitted set."
---

Raised out of #937's review as the owed follow-up: the measurement was ruled
honest and sufficient, and this is the field it named. Scoped to the log and the
census; widening `cmd/*-replay`'s `Policy.Demand` to return the set is the second
half, worth doing only now the log can say whether the served machine was inside
one.
