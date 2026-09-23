---
kind: fixed
title: a large paste no longer freezes the box for twenty seconds
pr: 1401
surface: [chat]
invalidates:
  - "Pasting a 4,000-line document into the box cost about twenty seconds of CPU before the box answered, because every word copied and lowercased the rest of the paste. The cost is now linear in the paste; the same paste takes about ten milliseconds."
---
