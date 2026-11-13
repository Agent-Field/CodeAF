---
kind: changed
title: Organization workstreams are shared with people or teams as view, comment, edit or full, and can be private
pr: 2637
surface: [factory, app]
invalidates:
  - "PUT /v1/workstreams/{id}/shares takes only {teams, include_linked}, and every share can write and archive."
  - "Owner and shares are two calls; a share sheet cannot change both in one save."
  - "The server keeps a decision's by and at as the client sends them, and an import's created_by."
  - "The first person to approve a go-ahead on a team's workstream owns its go-aheads and locks out the rest of the team."
  - "Removing a person leaves their workstreams owned by someone who can no longer sign in."
  - "An import drops every link that names a Personal workstream."
---

`PUT /v1/workstreams/{id}/shares` (engine `factory.org_workstream_shares`) now takes `shares` — people by handle or teams by id, each `view`, `comment`, `edit` or `full` — together with an optional new `owner`, `private` and the `rev` the caller read, all in one save; a stale `rev` is refused 412. The older `{"teams":[...]}` still works and gives each team `edit`. A workstream row carries `shares`, `private` and `my_role`. `view` reads, `edit` changes words and approves go-aheads, and `full` (the owner's) archives and changes owner, shares or privacy; someone with `view` gets 403 on a change and on archive.

A private workstream (`private: true` on create or on the shares call; a person owner, no shares) is its owner's alone. The operator's list shows it as a name-only row (`my_role` `""`, no counts or shares), its content answers 404 to them, and its events never reach them. Sharing it with anyone makes it not private.

The server now signs every new or changed decision as the caller, now, and an import as the person importing. On a team's workstream any teammate with `edit` may change its go-aheads. When the operator removes a person, their workstreams go to `?heir=<handle>` or to the operator, private ones staying private, and they leave every share. An import's link to another Personal workstream the same person already moved in is kept by its organization id, and a later import is linked from earlier ones that named it. The engine says `org_roles` in hello.
