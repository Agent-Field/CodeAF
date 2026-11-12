---
kind: fixed
title: AgentField factory pipelines keep completion times and live queue updates
pr: 0
surface: [factory, app, engine, chat, docs]
invalidates:
  - "Was true: a successful AgentField pipeline without a pull request could reach Done without a completion timestamp. Is true now: worker completion records the remote run's end time, or the factory's observation time, and preserves it after reload."
---

AgentField pipelines that return an answer remain visible in completed job lists.
