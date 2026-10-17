---
kind: fixed
title: automatic task starts require a direct work order
pr: 1845
surface: [chat, engine]
invalidates:
  - >-
    Questions and product direction could be mistaken for permission to start a
    task. Both route judges now require a direct non-question work order, and a
    stopped task result cannot restart the same request.
---

Questions and suggestions stay in the conversation unless the person directly
asks codeaf to do work. A stopped task's result can be reported without starting
the same request again.
