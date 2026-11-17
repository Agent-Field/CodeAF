---
kind: changed
title: New job asks a member for a workstream they can edit; the refusal names what exists
pr: 2719
surface: [factory, engine, docs]
invalidates:
  - "A job on a repository the caller reads only through the organization, sent with no workstream, was refused with a sentence pointing to a workstream Jobs tab that no longer exists (film D22). It is now refused with \"Choose a workstream you can edit for this job\" when the caller can edit one, and with \"Ask someone to give you edit on a workstream, or ask the operator to make this job\" when they can edit none."
  - "GET /v1/queue did not say which repositories needed a workstream, so the desk's New job sheet could not ask for one until the server refused. Each repository the caller reads only through the organization now carries needs_workstream: true. The field is never set for the operator or on a solo factory."
  - "The chat manual said Add job on a workstream's Jobs tab offers the organization's repositories. Workstreams have no Jobs tab; Factory › New job's Workstream field does."
---

The desk side, the Workstream field in Factory › New job, is a separate
Agent-Field/codeaf-desk pull request into santosh/work.
