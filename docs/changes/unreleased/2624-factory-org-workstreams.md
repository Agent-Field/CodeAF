---
kind: added
title: an organization's workstreams live on its server
pr: 2624
surface: [engine, factory]
invalidates:
  - "Was true: a workstream existed only in the desk's own SQLite on one laptop, and a factory server knew a workstream only as the free-text `group` of a go-ahead policy. Is true now: `codeaf factory serve` keeps an organization's workstreams (`/v1/workstreams*`): one owner (a person or a team), shares with several teams, a `rev` that a PATCH must send back in `If-Match` (412 when stale), archiving on DELETE (`?purge=1` is the operator's alone), owner and share changes, a preview of the linked workstreams `include_linked` would share, a one-shot import of a Personal workstream, and `workstream.changed` / `workstream.removed` on the event stream for the people who could see it."
  - "Was true: anyone who could change a group's go-ahead policy could approve one for any group. Is true now: when the group is an organization workstream id (`ws_…`), approving its go-ahead (PUT /v1/policy) needs write access to that workstream — its owner, its owning team or a team it is shared with — and the operator, who sees every workstream, is refused like anyone else who is not one of those."
---
