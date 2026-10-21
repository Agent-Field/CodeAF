---
kind: fixed
title: Factory publication removes local home paths and follows PR conversation reviews
pr: 0
surface: [engine, chat]
invalidates:
  - "Command receipts were copied into public PR text with absolute local home paths. Public exports now use relative factory checkout paths or visible home placeholders; private receipts remain exact."
  - "Follow PR read only formal reviews and inline comments. Ordinary PR conversation comments and edits now reach the durable maintenance trigger; failed reads replace stale all-clear status."
  - "Stored automatic approval requests sounded live after completion, and fresh runs lacked explicit audit boundaries. New events use stable historical wording and record Start and Bring back."
---

The GitHub write boundary sanitizes cached as well as newly rendered bodies, titles
and comments. Known factory receipts and acknowledgement-only comments are excluded
from review triggers; substantive human and external bot comments remain untrusted
evidence. Existing head, ownership, attempt, budget, mode and publication guards
remain authoritative. Feedback pagination and size bounds fail explicitly.
