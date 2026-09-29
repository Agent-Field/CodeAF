---
kind: fixed
title: Conversation names skip opening human shell commands
pr: 1667
surface: [chat, engine, docs]
invalidates:
  - "Conversation lists title-cased opening shell commands and collapsed their spaces. Shell-command previews now preserve casing, punctuation and spacing within the existing preview limit."
  - "The conversation namer treated the first human shell command as the opening question even after an ordinary message followed. It now skips user turns answered by a user_bash_ call and names the first ordinary exchange, including after reopening."
---
