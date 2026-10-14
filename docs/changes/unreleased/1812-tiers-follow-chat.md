---
kind: fixed
title: Unconfigured reflex and mastermind helpers follow the live chat model
pr: 1812
surface: [chat, engine, docs]
invalidates:
  - "An untouched reflex row chose Gemini while a cleared row followed the conversation. Both now follow the live chat model; a written model still wins."
  - "An unpinned mastermind used the standing crew router pick, and markreader and handoff withheld the chat fallback. They now use the live conversation model unless pinned."
  - "Low credits selected a separate free reflex model even beside a free chat. An unset reflex keeps its free fallback only beside a paid chat; a free chat and a cleared row follow the conversation."
---

Settings labels the blank planner row as following the conversation, and tier
seat sources report `follows conversation`. Worker and checker keep standing
crew picks; per-task crew routing and capability-qualified media are unchanged.
