---
kind: changed
title: The factory's issue read scores a complexity rubric and never guesses a price
pr: 2141
surface: [engine, chat]
invalidates:
  - "The arrival read asked a model for est_usd and an S, M or L size in hours of work. It now scores scope, clarity, coupling, proof and novelty 0 to 2 each; the sum's band is the size, and no model writes dollars."
  - "A job's triage carried est on the wire and the factory_add tool took estimate_usd. Both are gone; triage.rubric carries the factors, complexity and band."
  - "The cheap filter matched items whose model estimate was at most $2.50. It now matches items the read scored small."
---

The read sees only the item's text, so scope and coupling are marked low
confidence. The item's text is wrapped as data, never instructions.
