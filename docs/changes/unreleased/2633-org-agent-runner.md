---
kind: added
title: an organization chooses where its workstreams' agents run, and one person drives each
pr: 2633
surface: [engine, factory]
invalidates:
  - "Was true: an organization workstream had no agent anywhere, and nothing said where one would run or on whose key. Is true now: the operator's `agent_runner` setting (GET for everyone, PATCH operator-only, in /v1/settings) says `driver` — the agent runs on the laptop of the person driving the workstream, with their key — and `agent_runners` lists the choices the server offers. Only `driver` is offered today; `server` is refused until the server runner exists."
  - "Was true: two laptops could each start an agent for the same organization workstream. Is true now: PUT /v1/workstreams/{id}/driver takes a 90-second driver hand that the holder renews; anyone else is refused 409 naming the holder (engine: conflict, detail.reason `driving`), and the workstream answers `driver` while it is held."
---
