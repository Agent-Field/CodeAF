---
kind: changed
title: Tests now pin the immediate chat start and a returning tab's kept text
pr: 2447
surface: [chat]
invalidates:
  - "The chat-start work in ~/src/chat-start-immediate looked unfinished; its behaviour had already landed on app/v3, and only its tests were missing, which this change adds."
  - "A first message from Home or a new tab once waited on an opening conversation page; it now lands in the ordinary chat at once, wearing opening chat until its engine is ready, and an e2e asserts it within 700ms with the engine paused."
---
