---
kind: fixed
title: the pool spells a seat by its bare model id
pr: 1163
surface: [engine]
invalidates:
  - "A seat's model id reached the pool's records spelled the way the client routes it — the `~` alias marker on the front, a thinking level on the back — and the relay's wire schema admits only `<vendor>/<id>`, so a fresh install's first measurement rows were refused and dropped. The pool's copy of a seat's id is now the bare `<vendor>/<id>` at every entry: the landing's seat map and its judge-last record, and the pending rows the headless doors leave. The client's own routing path keeps the tilde."
---

A seat reaches the pool's records carrying the id it ran under, and a fresh
install's default routes by a floating alias: the id arrives with a leading
`~` and may carry a thinking level. Neither is part of the model's name. The
pool's rows — the own sheet, the outbox, the judge-last record, the pending
file — name a model by its bare `<vendor>/<id>`, which is also the only
spelling the relay's schema admits, so a tilde-spelled seat's rows were
refused on arrival and dropped. The same spelling defeated the judge picker's
same-vendor exclusion: the exclusion read the marker as its own vendor and
let the crew's own maker into the judge's running.

The normalising now happens once, where a seat's spelling enters the pool.
`poolSeatID` takes the level and the marker off — mirroring
`internal/catalog`'s own normaliser, which is unexported — and it is applied
to the landing's seat map and held record in `poolJudgeLanding` and to the
seats a pending row carries in `writePendingLanding`. The judge's
`vendor()` strips them too, because `Candidates` reads the seats its callers
wrote and must not depend on every caller having normalised first. The
client's own routing path keeps the tilde: only the pool's copy of the id
loses it.
