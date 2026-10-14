---
kind: changed
title: Unconfigured reflex follows the live conversation model
pr: 1812
surface: [chat, engine, docs]
invalidates:
  - "An unwritten reflex row selected Gemini while only a cleared row followed chat. Both now follow the live conversation model; a written row or role pin still wins."
  - "Low credit selected a separate free reflex even beside a free chat. The unwritten reflex uses that fallback only beside a paid conversation; cleared rows still follow chat."
  - "App model reads did not expose resolved helper tiers and their sources. GET /app/v1/models now includes read-only helper_tiers, including the mastermind router pick."
---

The registered low-tier helpers (title, caption, router, consolidate, sentinel,
spellout, taskname, intake, jobname and guardian) keep small work's existing
V4.1 Flash default, free low-credit default and cleared-row conversation floor.
No other eligible fixed helper default was found. Mastermind roles retain their
independent crew picks; markReaderAbsent and the crew-only auxiliary floor are
unchanged. Worker/high seats and vision/media are unchanged.
