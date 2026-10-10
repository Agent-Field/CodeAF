---
kind: fixed
title: Desktop place Home survives canonical tab-set loading
surface: [chat]
invalidates:
  - "Home was ensured only on arrival or a place rename; a later synced tab set could replace it. The workspace now ensures Home first after each tab-set change without resetting later focus."
---

Each place retains its saved strip through the workspace sync adapter. Now has
no place Home, switching preserves running work and drafts, and the Home
composer opens its chat after Home under the pinned ordering law.
