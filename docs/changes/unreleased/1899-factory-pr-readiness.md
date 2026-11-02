---
kind: added
title: Factory drafts receive final review and recorded readiness approval
pr: 1899
surface: [engine, chat]
invalidates:
  - "Healthy factory drafts remained drafts indefinitely. Follow PR now reviews the exact PR and uses recorded Auto or Manual approval to mark it ready."
---

Factory drafts now receive a final read-only readiness review in Follow PR
before the existing recorded approval door marks them ready for review. Auto
handles this routine milestone; Manual asks with a specific reason. Required
check policy, actual PR description/diff, current head/base and feedback are
revalidated. Unread or changed facts block stale approval, uncertain mutations
are observed before retry, external ready transitions are recognized, and
monitoring continues. Readiness never grants merge authority.

Invalidates: a healthy factory draft previously remained a draft indefinitely,
and a separate generic GitHub connection could not supply a factory readiness
action. The existing factory identity now carries the recorded transition.
