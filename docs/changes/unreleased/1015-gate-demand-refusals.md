---
kind: fixed
title: the OpenRouter-takeover gate counts a refusal only when the router's own pick was refused
pr: 1015
surface: [engine, docs]
invalidates:
  - "The gate counted every named refusal toward the takeover, including the refusals a takeover's own narrowed demands came back with. On a live chat (2026-09-12) that looped four times — demand, 404, re-arm — each with a full ~80k-token re-send, before the ladder's first rung went bare. Only a refusal of a bare (membership-unnarrowed) attempt counts now; a narrowed demand's refusal is evidence about our choice and never enters the gate (routefirst.go's Client.noteRouterRefusal)."
---

The manual page for lanes said every refused or unusable answer counts toward
aforge taking over from the router. It now says only refusals the router
itself earned count, and the takeover's own demands cannot keep it alive.
