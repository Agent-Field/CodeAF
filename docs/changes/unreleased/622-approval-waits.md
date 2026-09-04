---
kind: fixed
title: an unanswered approval waits; silence is never a no
pr: 622
surface: [chat, engine, docs]
invalidates:
  - "An approval question that nobody answered in about ten seconds was recorded as denied, and the call — including a proposed task — was cancelled. Silence now pauses and keeps waiting; the card shows `waiting` or `paused`, never `denied · no answer`."
  - "The manual said the countdown's default was no: \"The countdown: silence denies\" and `/settings` called it \"seconds an approval question waits before it answers no\". Both now say it pauses and keeps waiting."
---

F41: a hidden ~10s timer answering no for the person.
