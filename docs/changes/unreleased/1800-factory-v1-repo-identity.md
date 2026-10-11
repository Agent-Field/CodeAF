---
kind: fixed
title: Factory repository identities keep owner/name through checkout aliases
pr: 1800
surface: [chat, remote, docs]
invalidates:
  - "Factory checkout keys became job and queue repository names, duplicating a watched owner/name and hiding jobs from viewers. Checkouts now resolve to GitHub owner/name from origin or the watched config, and stored short-key jobs migrate on load without changing their age."
---

Full checkout keys retain their owner. Ambiguous short names do not select
another owner's repository. Queue, job detail, ownership and viewer access use
the same resolved identity; Contract stays 1.
