---
kind: fixed
title: Repo notes and themes count as Factory AI on Spending
pr: 2281
surface: [engine]
invalidates:
  - "Was true: `factory_ai_usd` left out repo notes, repo themes, repo evals and a brief with no job, so they read as your chats."
---

Repo notes, repo themes, repo evals and a brief with no job now count in `factory_ai_usd`, not under your chats. Afi's small errands still count under your chats.
