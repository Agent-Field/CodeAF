---
kind: fixed
title: Desktop shell connects group suggestions and window-local undo
surface: [desktop, chat]
invalidates:
  - "The workspace rendered the older group offer. It now mounts the folder-based SuggestPill against the content card and hides it during overlays."
  - "The undo stack registered its own keys. Workspace now connects the shared undo-key hook to the guarded structural inverse stack, preserving editor undo."
  - "Legacy view offsets had no validated slot. Finite nonnegative offsets now validate and migrate into window-local persistence without entering shared workspace writes."
---

Existing closed timestamps, per-window focus, and the single app-level toast host
remain connected. The browser mock preserves workspace writers and refuses
unknown keys and unsupported methods like the canonical workspace route.
