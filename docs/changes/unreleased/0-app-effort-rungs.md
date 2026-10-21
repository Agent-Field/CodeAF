---
kind: fixed
title: Desktop effort uses the terminal conversation dial and all its rungs
surface: [engine, app, chat]
invalidates:
  - "agents.set_effort accepted only low, medium and high and set a per-model override. It now uses the terminal conversation dial for all rungs, including xhigh and max; auto clears that conversation choice, and hello advertises agents.set_effort.rungs."
  - "Agent effort was read from the current model override. It now reports the stored conversation choice and is omitted when that choice is unset; other scopes can still determine resolved effort."
---
