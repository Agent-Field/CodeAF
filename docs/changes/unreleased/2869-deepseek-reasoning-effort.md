---
kind: fixed
title: DeepSeek's own API now receives the thinking level, and max reaches it
pr: 2869
surface: [chat, engine, docs]
invalidates:
  - "A thinking level on DeepSeek's own API (`codeaf connect deepseek`) was sent as OpenRouter's `reasoning` object, which that API ignores, so every level ran at DeepSeek's default and `max` was unreachable. It is now sent as DeepSeek's top-level `reasoning_effort`: low as `low`, medium and high as `high`, xhigh and max as `max`, and auto as nothing."
  - "Every connected service read the same reasoning shape. A service row now declares its reasoning dialect (`modelsource.Source.Reasoning`); only DeepSeek's declares `reasoning_effort`, and OpenRouter bodies, including `deepseek/...` models routed through it, are unchanged."
---

An off request on a model that cannot stop thinking sends `low` on that API,
the lowest word it reads; no thinking budget and no `reasoning` object go with it.
