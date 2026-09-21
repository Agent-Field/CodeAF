---
kind: fixed
title: a run is handed what is left of every dollar limit
pr: 1281
surface: [engine, chat]
invalidates:
  - "A task run was handed the conversation's whole spend limit as its own dollar limit, however much the conversation had already spent, and `--max-cost` did not bound a run at all. A conversation that had spent four of its five dollars could start a run allowed five more."
  - "A run is now handed what is LEFT of the smaller dollar limit the person set: the conversation's spend limit (`/settings`, Spending, per conversation, off by default) and `--max-cost` when codeaf was started with one, less what the conversation has spent so far. Which limit is the smaller is decided in one place, the same one an adaptive run reads."
  - "A limit already spent hands the run the smallest positive figure, not zero, because zero means no limit to the run engine. Such a run ends on its first paid call with `a dollar limit you set stopped it`."
---

No new word on screen. The manual's two passages on a run's dollar limit are rewritten in
the same change, and the old sentence saying `--max-cost` does not bound a run is gone.
