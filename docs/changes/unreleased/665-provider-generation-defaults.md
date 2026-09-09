---
kind: changed
title: Model generation uses provider defaults until you choose an override
pr: 665
surface: [chat, engine, resident, docs]
invalidates:
  - "Aforge used to impose output ceilings on its own model calls, disable reasoning on several roles, and add high reasoning to shipped mastermind models. Default requests now leave generation behavior to the provider."
  - "The plain OpenAI SDK loop used to inject its own temperature and output defaults. It now clears those defaults before applying explicit caller options."
---

Default requests across chat, headless work, auxiliary roles, documents, saved
harnesses and resident work omit app-selected output, sampling and reasoning
controls. Explicit model levels, reasoning settings and per-call options still
travel, including zero temperature and output limits. Context reserves,
response-size bounds, run budgets and request deadlines are unchanged.
