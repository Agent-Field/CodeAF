---
kind: fixed
title: a headless run's total includes the plan model's reading of the request
pr: 387
surface: [engine, resident]
invalidates:
  - "The total `aforge do` printed (`12s · 1 node · $0.0041`) and put in `spend` was short by one call: the plan model's acceptance pass, which read the request for the behaviours it states, ran on the plan client's snapshot — the router behind a wall, not behind the usage journal — and its usage was discarded. The row is billed to the spine now through the same seam every other planning pass uses, so the printed total equals the sum of the run's call-log end rows to the cent. The earlier reading that the plan model's calls were dropped wholesale was wrong: the delivery gate on the same model was always counted."
---
