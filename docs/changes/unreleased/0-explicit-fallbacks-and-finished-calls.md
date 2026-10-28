---
kind: fixed
title: Explicit fallback lists hold, and finished shell calls keep background processes
pr: 0
surface: [chat, engine]
invalidates:
  - "A cancellation watcher could kill background processes after their shell call had already finished. A completed call now takes precedence over a late cancellation."
  - "A fallback list containing only the failing model could trigger catalog guesses. Only a list with no named model now allows a catalog suggestion."
---

When a shell call's completion and cancellation are both ready, the cancellation
watcher checks completion before signalling the process group. Commands that are
still running retain their existing cancellation behavior.

A fallback list that names only the current model leaves no model to try after
it. Duplicates and surrounding blanks keep that choice; a list of only blanks
still allows a catalog suggestion.

Carries forward santoshkumarradha's completed-call cancellation and explicit-list
fixes and regressions from #1738. Pairing and moving chats already shipped
separately. The draft's broader task/checker pin and skill import policies remain
separate design decisions.
