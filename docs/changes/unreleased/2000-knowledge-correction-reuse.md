---
kind: fixed
title: Reuse unchanged knowledge evidence groups after a correction
pr: 2000
surface: [chat, engine, docs]
invalidates:
  - "A correction could regroup unchanged evidence parts closed by the text-size limit; those completed parts now keep their membership."
---

Corrections refresh affected evidence groups while retaining other valid completed
groups. Source, claim, conflict, and paragraph-dependency validation still applies.
The final partial group remains available for new evidence. This reduces repeated
maintenance work without changing the knowledge model or removing review steps.
