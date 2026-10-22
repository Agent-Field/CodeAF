---
kind: added
title: the pareto selector is wired — a fitted snapshot seats the unpinned seats, none routes as before
pr: 1892
surface: [chat]
invalidates:
  - "internal/selector's package doc said the engine is shadow-only, never applied to real routing — that is no longer true when a profile carries a fitted SharedSnapshot at models.crew.selector_snapshot; with no snapshot configured nothing changed, byte for byte."
---
---

The port of the pareto selection change-set sat in the tree as a shadow lane: the
engine was built by no production line, and the crew every task actually ran was
internal/crewroute's own pick. This wires it, at the one seam the trace found —
`config.RouteCrew`, the single door a task's crew comes through (the session's
`Config.RouteCrew`, `config.ResolveSeats` for every headless door, the redo paths all
build their decision there).

The shape: `config` loads the snapshot at `models.crew.selector_snapshot`, runs the
engine over the request's own candidate list (the pool the router already priced and
health-checked, with the avoided lineages left out), and hands the pick per seat to
`crewroute` as `Request.Chosen`. Inside `crewroute.Decide` the named seat is seated
through the same machinery a pin is ([pinned]) and then marked not pinned — so the
decision, its estimates, its ladder, its rescue and its log row are all still the live
engine's. A pin wins, a redo ignores the pick, a learned offset still moves the crew,
and a model nobody offers cannot be seated.

The safety property, which the tests lock: no snapshot configured — or one that stopped
loading, or a seat the engine cannot seat — leaves the crew exactly as `dev` routes it
today.
