---
kind: added
title: the engine reads and changes an organization's teams and workstreams for the desk
pr: 2623
surface: [engine, factory, app]
invalidates:
  - "Was true: the engine had no way to reach an organization's teams or workstreams, and factory.me answered no teams. Is true now: factory.teams, factory.team_create/rename/delete/member_add/member_remove and factory.org_workstream(s)/_create/_update/_delete/_owner/_shares/_share_preview/_import forward to the attached server's /v1/teams and /v1/workstreams routes with the caller's session; factory.me answers teams; hello carries org_teams and org_workstreams."
  - "Was true: every factory.* method named its connection only as `factory`. Is true now: the organization methods take `connection` (and read `factory` when it is absent), and every factory read such as factory.me accepts either word."
---

The organization's server is the only copy: the engine keeps nothing. A 403
is `forbidden`, a 409 `conflict`, and a 412 (the workstream moved past the rev
sent as If-Match) is `conflict` with `detail.reason` `stale`.
