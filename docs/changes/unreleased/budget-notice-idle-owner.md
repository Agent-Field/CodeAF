---
kind: fixed
title: Idle crew owners keep the job budget stop notice
surface: [chat, app, engine]
invalidates:
  - "Was true: a job budget could stop an idle chat's crew without recording the reason in that chat. Is true now: the owning chat records the budget refusal once after its work finishes stopping."
---
The notice is kept whether the owning chat is idle or replying. A person's Stop
does not add a budget notice.
