---
kind: fixed
title: a bare auto row names the pick word's rung, so a seat computed under learn says learned
pr: 1119
surface: [chat, engine]
invalidates:
  - "A tier row that says `auto` reported its rung as `computed from the catalog` whatever the crew pick word was, so a seat resolved under `pick=learn` claimed the catalog had answered it. It now names the word that ran: `learned` under `learn`, the catalog's rung under `catalog`. The rung names WHICH pick ran, not whether the pool's measurements moved the id — the rule a pick-computed seat already followed."
  - "The rung decision lived in two places, `autoRow` and `pickedModel`, and they disagreed. It now lives once, in `computedRung`, which both call, so the seam that exists to stop a seat meaning one thing in chat and another headless cannot drift again."
---
