---
kind: added
title: Desktop conversations can offer contextual reply drafts
pr: 0
surface: [engine, chat]
invalidates:
  - "Was true: desktop message boxes only offered static instructions. Is true now: the optional agents.suggest_reply capability generates a short human-message draft from settled dialogue and current state, using a bounded, cached naming-role call metered to the existing conversation and job budget. Old or unavailable owners return an empty fallback."
  - "Was true: no engine operation generated an optional next-message draft without sending a turn. Is true now: held local and remote conversations can suggest one without tools, approval, sending or opening another conversation; budget exhaustion and session shutdown cancel generation."
---

The desktop accepts with Tab into an editable draft, never sending automatically.
Escape dismisses; typed text, active work, attachments and command/mention menus
take precedence. Stale replies are discarded. No new transcript item is added.
