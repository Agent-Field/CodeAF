---
kind: added
title: A tier row that says `auto` computes its model from the catalog
pr: 1093
surface: [chat, engine]
invalidates: []
---

Any of the five tier rows (`models.tiers.*`) may hold the bare word `auto`,
case folded: the seat's model is computed from the catalog's own published
figures through `internal/crewpick` — the three indexes against the three
prices, under each seat's call shape — every time the row is read. The word
stays on disk; the id is derived on every read.

- `config.AutoValue`, `config.IsAuto` and `config.AutoPick` are the word and
  its pure pick; `config.AutoModels` is how the catalog reaches seat
  resolution, set once at start-up from the binary's non-blocking read,
  never a fetch. Nil is an ordinary state, not an error.
- Two new rungs on the seat ladder: `config.SeatComputed` and
  `config.SeatTable`. An auto row resolves on both ladders
  (`config.TierSeatAt` and `config.ResolveSeats`) through one seam — to the
  computed id, or, when nothing can be computed, to the family's table row
  for the preset the stored rows name. It never resolves to `auto` and never
  to empty.
- A seat on either new rung names the preset it ran at: `crew frugal,
  computed` on a run's receipt.
- Rows that don't say auto are unchanged, and so is everything above the
  crew: a flag, `--plan-model` and the environment still outrank an auto
  row, and a flag whose text is `auto` is handed on whole.
