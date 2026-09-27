---
kind: fixed
title: CLI help names task model choices and unattended chat budgets
pr: 1626
surface: [docs]
invalidates:
  - "The top-level help omitted do's worker limit and crew-selection flags. Its shared synopsis now lists --slots, --best, --cheap, --pin and --check-model."
  - "Chat budget flags were only discoverable in detailed help and the environment table. The summary now names --max-cost and --max-hours and their --yolo requirement."
---

The shared headless-work explanation also uses a complete sentence for the
plan-price refusal. This changes help text, not command behavior or engine
workspace policy. Part of #1558.
