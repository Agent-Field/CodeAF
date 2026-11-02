---
kind: added
title: A job's cost and time are measured from the factory's own finished jobs
pr: 2142
surface: [engine]
invalidates:
  - "A start rule compared a model's estimate with max_cost. It now compares what similar finished jobs cost (their 80th percentile); an unknown cost asks first only when the repository's file writes max_cost."
---

Fewer than five similar finished jobs gives no figure. Ranges are checked
against what later jobs spent and widen when fewer than 6 in 10 land inside.
