---
kind: fixed
title: Factory mine filter uses the linked GitHub person
pr: 1800
surface: [chat, docs]
invalidates:
  - "The factory mine filter matched only items authored by santosh. It now matches the linked GitHub handle against the item's person or author, and matches nothing when that handle is unknown."
---

The person follows assignment, then taking, then starting. An unknown login
shows `mine needs your GitHub login` in the filter hint.
