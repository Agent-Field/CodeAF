---
kind: changed
title: Headless app plans propose reuse for you to confirm at Apply
pr: 2960
surface: [app, engine]
invalidates:
  - "Shared record proposals previously needed a separate answer during headless Build. A headless Builder now previews the proposal on synthetic records and waits for Apply before publishing."
---

A headless plan can say “Uses your existing Contacts from CRM.” The shared
records connect when you Apply. Desk presents its reuse choice during planning.
