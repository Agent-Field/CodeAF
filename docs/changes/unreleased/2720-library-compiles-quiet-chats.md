---
kind: changed
title: The Library compiles a chat once it has gone quiet, and charges the calls to it
pr: 2720
surface: [chat, app, engine]
invalidates:
  - "The Library's build loop compiled a growing chat about every two seconds while it was being written in (another pass 2s after any pass that found new lines), rerunning the vault-wide learning steps each time; a chat written within the last two minutes is now held and compiled in one pass after it goes quiet (internal/knowledge/build_idle.go, buildIdle)."
  - "A background Library compile carried no conversation, so its memory lines in the spending ledger belonged to no chat; a pass that compiles one conversation's sources now charges every call to it (knowledge.ChargedSession). A Build that compiles several at once still belongs to none."
---

Live lane E6b paid $0.654 of a $0.96 session — 68% — for 175 Library calls in
42 passes, still running minutes after the last request. Pass count was the
cost, not content. Pressing Build still compiles everything at once.
