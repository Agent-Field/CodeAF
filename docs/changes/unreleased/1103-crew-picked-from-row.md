---
kind: added
title: The crew's seats are picked from a row — table, catalog or learn
pr: 1103
surface: [chat, engine]
invalidates:
  - "A seat on the computed rung read `crew frugal, computed`; it now reads `crew frugal, computed from the catalog`."
  - "The crew word was derived from the five live seats; under a pick of catalog or learn it is derived from the five stored rows, because the live seats are computed ids the preset tables do not hold."
  - "An unwritten seat read the preset's own table row on every surface; under a pick of catalog or learn the worker, careful work and mastermind seats are computed at the crew's preset instead, and a hand-typed model id still wins."
---

`models.crew.pick` is a new choice row beside the crew row: `table` (the default),
`catalog` and `learn`. The crew row keeps its meaning — how much to spend — and the pick
row says where the models for that budget come from when a tier row does not hold a model
id of its own.

- `config.CrewPickAt` and `config.SetCrewPick` are the row's reader and writer; a word
  the build does not know reads as the default and a writer refuses it, the way every
  choice row folds.
- The ladder applies the pick in one seam both ladders call — `config.pickedSeat`,
  beside `autoRow` in internal/config/auto.go. Under `catalog` or `learn` the worker,
  careful work and mastermind seats are computed at the crew's preset
  (`config.crewPresetUnder`), reflex and small work always read the table, a stored
  model id that is not the preset's own table value wins with source `crew`, and a flag
  or `CODEAF_MODEL`/`CODEAF_PLAN_MODEL` still outrank everything below them.
- `config.AutoPickWith(tier, family, preset, models, prior)` is the pick with the
  measured quality named: `catalog` passes a nil prior, `learn` passes the Model Pool's
  role-quality prior (`config.autoPrior`), and `config.AutoPick` keeps its behaviour —
  prior included — for the bare `auto` word a tier row may hold.
- `config.SeatLearned` is the new rung. `Seat.Rung()` says `crew balanced, computed from
  the catalog` for `SeatComputed` and `crew balanced, learned` for `SeatLearned`;
  `Seats.Report()`, `Seats.Line()` and the receipts follow. The headless doors read the
  same pick through `config.ResolveSeats`, so a run from the shell is seated exactly as
  the conversation is.
- internal/tui3: the row is on the Providers tab directly under the crew word
  (`picked from`, a cycle widget), `/crew` takes the three words beside the presets and
  refuses an unknown word by naming all six, and the crew word and status segment read
  `balanced · learn` / `crew balanced · learn` when the pick is off the table. The
  chooser names the pick on a reading line under the presets, and a crew applied under a
  pick confirms with the pick named beside the preset (`config.CrewSummaryPick`).
- internal/manual: the class-row section is now `Where the seats are picked from —
  table, catalog, learn`, with the bare `auto` word kept inside it as the per-seat
  alias, and the headless ladder page names the two rung words.
