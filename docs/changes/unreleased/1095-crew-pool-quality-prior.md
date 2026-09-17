---
kind: changed
title: The crew picker blends a measured pool rating into a seat's quality by its observation count
pr: 1095
surface: [engine]
invalidates:
  - "`crewpick.SeatQuality` and `crewpick.Front` read a seat's quality from the catalog's published indexes alone. They now have `SeatQualityWith` and `FrontWith`, which blend a `crewpick.Prior` — a pool's measured quality per seat, per canonical model id — into each seat by N/(N+PriorWeightAt): a rating the prior holds with a positive count moves the seat towards the rating's mean, and `Crew.Measured` says which seats a rating entered. `SeatQuality` and `Front` keep their signatures and read no prior."
  - "`config.AutoPick` resolved a seat from the catalog's published figures and nothing else. It now reads `config.AutoIndex`, a Model Pool index seam beside `config.AutoModels`, and blends the index's `role_quality` cells (a gaussian metric only, through `config.PoolQualityMetric`) into the pick through `crewpick.FrontWith`. A nil index leaves the answer exactly what it was."
---

The table of published indexes is a statement about models, and a pool that has
actually run a model on a seat holds something the table cannot: what it scored
there, over however many observations. The blend carries each by its evidence,
so a handful of pool ratings nudges a seat and a large pool of them decides it,
while a picker with no index in hand is unchanged.
