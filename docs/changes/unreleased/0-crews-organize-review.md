---
kind: fixed
title: Organize suggestions leave stored crews untouched and retain terminal behavior
pr: 0
surface: [engine, chat]
invalidates:
  - "Desktop model suggestions could open a normal conversation and repair teams.json before Apply. Naming now uses a caller with no journal or lifecycle watches, or joins the existing owning engine before checking its naming role."
  - "Shared Organize helpers applied desktop candidate and model rules to the terminal. The terminal again groups assigned conversations, suggests missing folder members and falls back to its conversation model; only desktop requests require an explicit naming role and unassigned candidates."
---

An initially unopened hosted conversation uses its owning engine's naming
configuration, including one-model mode. Desktop suggestions keep their existing
wire shape and read-only contract. Terminal folder and model merging retain the
behavior from before the desktop method was added.
