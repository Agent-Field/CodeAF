---
kind: fixed
title: Factory publication removes local home paths and follows PR conversation reviews
pr: 1899
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

Redaction starts only where an absolute path starts (including after an option such
as `-I` or a `file://` scheme), so repository-relative paths through `users`, `home`
or `root` directories and API routes keep their public wording.

Paged CI reads reject lists whose totals change between pages, short incomplete
pages, read failures and lists beyond the bounded page limit rather than treating
partial successful rows as green CI. A repair for known CI failures or conflicts
preserves the last handled feedback key while feedback is temporarily unreadable.
The chat manual's thinking-chip heading now includes the question people ask so
factory additions do not make the existing keyboard guidance unreachable.
