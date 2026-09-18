---
kind: fixed
title: the headless doors wait for the catalog rows a pick reads
pr: 1093
surface: [engine]
invalidates:
  - "`codeaf do`, `codeaf exec` and every door through `useAutoSeats` resolved their seats while the lazy catalog was still warming, so a pick taken off the table — or a tier row that says `auto` — read no rows and fell to the family's table row, reported as `crew balanced, table`, on a machine whose catalog was already cached beside the profile. They now wait for the warm within a three-second bound when the profile needs the rows, and fall exactly as before when the bound runs out — with the rung word saying which happened."
  - "internal/catalog's `Warmed(ctx)` is the bounded wait at the warm's door — true at once for an eagerly loaded catalog, false at the bound for one still fetching — and `config.AnyTierAutoAt` answers whether any tier row says `auto`, the second reason a door waits, beside `config.CrewPickAt`."
---

A pick off the table and a tier row that says `auto` are both computed from the
rows the process already holds (`config.AutoModels`), which the headless doors
set from a lazy catalog's non-blocking read. The chat surface never met the
defect because its picks happen after the warm has landed; a headless door's
picks happen a line after the read, so the warm was always still in flight and
the resolver saw no rows. The wait is bounded — three seconds, sized to the
disk read of a cached catalog — and the fetch is never waited on past it: a
cold cache on a slow network still resolves from the family's table row, and
the seat's receipt still names the rung that answered, so a run that fell says
it fell. A profile with neither a pick nor an auto row waits for nothing.
