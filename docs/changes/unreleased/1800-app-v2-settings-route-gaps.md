---
kind: added
title: Canonical app Settings routes, ChatGPT sign-in and snapshot facts
pr: 1800
surface: [engine, docs]
invalidates:
  - "Accounts, MCP and factory Settings exposed only the first resource lane's PATCH and DELETE adapters. Canonical board POSTs now share those mutations and publish typed receipts, account poll state and factory details in AppRoutes; MCP creation saves name and access atomically."
  - "App browser sign-in supported only OpenRouter and DELETE only canceled pending callbacks. ChatGPT now uses the existing PKCE flow, claims a refreshable bundle once and clears its memory credential on DELETE; OpenRouter DELETE also disconnects."
  - "Credential claims with Idempotency-Key replayed secret responses. All claim attempts after the first successful transfer now return conflict without retaining the response."
  - "Finished factory jobs had a fixed 24-hour Running window. App Settings now exposes factory.done_days and accepts a changes envelope; app-hosted queue and job projections apply the saved interval without deleting history."
  - "Agent snapshots left sleep and daily-cap facts to app defaults. Detail reads now serve asleep and day_cap_reached from successful power events and measured daily spending."
---

The app's opaque ChatGPT key is the full JSON token bundle, handed back through
keys.chatgpt on restart. Native codex model routing and refresh use that memory
source and cannot fall back to terminal token files. Existing flat Settings
patches remain accepted. The native factory retains operator checks, and
canonical account polls preserve the native store's one-time credential claim.
