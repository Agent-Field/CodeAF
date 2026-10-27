---
kind: fixed
title: The desktop protocol document lists the current engine methods, events and commands
pr: 0
surface: [engine]
invalidates:
  - "The desktop protocol tests referenced a document missing from app/v3. The tracked document now lists every current wire method and event and the shared slash command forms and aliases."
---

The method and event index follows `codeaf app --schema`. The slash catalogue
records 54 commands, 93 forms and 31 aliases, including the commands added by
the dev merges. No engine method or terminal command changes in this repair.
