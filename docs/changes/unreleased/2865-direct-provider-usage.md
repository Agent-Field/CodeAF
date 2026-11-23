---
kind: fixed
title: Direct OpenAI-compatible requests omit the router usage field
pr: 2865
surface: [chat, engine, docs]
invalidates:
  - "Connected OpenAI-compatible services received the router's top-level usage opt-in and strict gateways could reject the call. Direct requests now omit that field and opt in to streaming counts with stream_options.include_usage when supported."
---
