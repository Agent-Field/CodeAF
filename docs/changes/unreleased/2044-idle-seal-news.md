---
kind: changed
title: Idle chats wait for seal news instead of polling every 100 ms
pr: 2044
surface: [chat, engine, remote]
invalidates:
  - "Every hosted chat asked its seal watch for news every 100 ms for as long as it lived. A healthy idle chat now runs no timer: the watch wakes the host when it queues a sentence, and the 100 ms look runs only while sealing is failing or a sentence is held for a running turn or a surface that has not attached."
---

What is said, and when a turn says it, is unchanged. A recovery that is found
only by asking is still looked for every 100 ms while sealing fails and a
surface watches, as before.
