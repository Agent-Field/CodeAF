---
kind: added
title: Review and merge duplicate contacts and organizations
pr: 2959
surface: [app, engine]
invalidates:
  - Common type bindings alone shared record types. Owners can now find and review duplicate records, choose a surviving ID, and split a merge later.
---

Use `codeaf app match-candidates` to see open reviews. A merge keeps old
IDs and existing references working. Values you entered or cleared take
precedence over source contributions.
