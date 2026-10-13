---
kind: added
title: App spending and memory HTTP resource handlers with execution-owner seams
pr: 1800
surface: [engine, docs]
invalidates:
  - "App spending and memory routes returned 501. They now read the native ledger and memory store, raise permanent day and agent caps, edit wording and enforce a five-second memory undo deadline across app launches."
  - "Rounding every model call to cents could lose sub-cent spending. App ledger totals now sum measured receipts before formatting money."
  - "A built HTTP resource must not imply every execution rule is wired. Daily-agent admission, temporary cap expiry, crew shelf ownership, native spend producers and model memory-add enforcement still require execution-owner integration outside this handler-only lane."
---

The resource interfaces accept owner-aware backends through Options.Context.
Unsupported native operations refuse explicitly; no crew, task or Inbox state
is edited by the spending/memory lane. This is partial A11b work, not completion
of the execution rules.
