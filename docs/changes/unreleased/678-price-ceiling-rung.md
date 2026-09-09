---
kind: fixed
title: endpoint relaxation keeps the price ceiling until a second refusal
pr: 678
surface: [chat, engine, docs]
invalidates:
  - "The endpoint-refusal ladder removed `provider.max_price` together with membership filters on its first retry. It now widens endpoint membership under the same ceiling and drops the ceiling only when that wider request is also refused."
  - "A price recovery was narrated as `dropped the price ceiling and relaxed the endpoint filter`. Those changes now have separate attempt lines and separate names in the call trace."
---

A pin, ignore list, or parameter filter can empty an endpoint set without saying that a dearer endpoint is needed. The separate rung keeps availability recovery while requiring that extra evidence before aforge lifts its price guard.
