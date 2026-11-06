---
kind: added
title: Tool step events carry the conversation's live spend so far
pr: 2413
surface: [app, docs]
invalidates:
  - A running turn said nothing about its conversation's spend until it ended; turn.tool now carries the live figure as spent_usd.
---

`spent_usd` on `turn.tool` is the same live meter `agents.budget.get` answers as
`engine.spent_usd`, so it can be ahead of `Agent.spent_usd`, which is saved at
turn end. Zero or unknown readings and history rows omit it.
