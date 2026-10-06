---
kind: added
title: Ordinary local launches restore the last chat and place
pr: 1785
surface: [chat]
invalidates:
  - "Bare codeaf and codeaf chat chose this directory's recent chat and could greet with home regardless of the view left behind. They now restore the active chat and main place saved for the same profile and launch directory."
  - "Recent-completion concurrency tests recognized only the older Bubble Tea Batch closure name. They now recognize the pinned version's compactCmds helper and check the returned message type, so their scheduled readers are actually driven."
---

The bookmark is saved atomically on navigation and shutdown, for both the local
background engine and in-process chat. Explicit session, picker, headless and
remote launches keep their startup behavior. Missing conversations or workspaces
fall back to ordinary startup; setup and takeover screens retain precedence.
Temporary overlays, searches, scroll positions, task drill-ins and additional
tabs reset. The manual describes the restored view and its limits.
