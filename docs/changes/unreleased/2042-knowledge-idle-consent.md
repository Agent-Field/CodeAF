---
kind: changed
title: Idle chats no longer open the knowledge database every second
pr: 2042
surface: [chat, engine]
invalidates:
  - "Each live chat's knowledge-capture tick opened knowledge.sqlite twice a second to ask whether capture was on, whether it was on or off. An idle tick now reads one process-wide answer that is refreshed at most every five seconds and at once after this process's own opt-in."
---

A tick that finds new sources still reads consent from the database before it
admits them or moves past them, so what is captured is unchanged. Capture
enabled by another process is seen within five seconds; the turns written in
the meantime wait in the chat's journal and are captured then.
