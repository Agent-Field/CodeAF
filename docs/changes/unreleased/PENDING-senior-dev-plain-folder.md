---
kind: fixed
title: senior-dev works in plain folders and resolves shell model names
pr: PENDING
surface: [chat]
invalidates:
  - "On dd0fcc654, senior-dev crashed before its first model call in a plain folder because the child was given a missing start-time ignore list. Shell and chat runs now receive a readable empty list there, work in place, and commit nothing."
  - "On dd0fcc654, a shell --high value had to include the openrouter service prefix or senior-dev could fail in models.dev. Bare OpenRouter ids, service-prefixed ids, and short crew model words now resolve before the child starts; an unserved model is refused with the /crew and codeaf connect doors."
---
