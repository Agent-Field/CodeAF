---
kind: fixed
pr: 2176
title: An errand's cost counts every model call it made, and a factory brief has room to finish
surface: [engine, app]
invalidates:
  - "ErrandAnswer.Usage carried only the final answer's usage. When the provider adapter asked again after a model's thinking ate the whole ceiling, the superseded call reached the ledger and the job meter but not the errand's usage, so a caller booking that usage (the factory brief writer) under-booked. It now sums every call the errand made."
  - "The factory brief writer's answer bound (briefMaxTokens) is 3600 tokens, up from 2000, set from ten live briefs on deepseek-v4.1-flash that wrote 1189 to 2860 tokens."
---
