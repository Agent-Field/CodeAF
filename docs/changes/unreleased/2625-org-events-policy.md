---
kind: added
title: Organization changes reach the desktop app; go-aheads are read only by those who see the workstream
pr: 2625
surface: [factory, app]
invalidates:
  - "The engine relays no event from a remote factory's stream; the desk must poll an organization to see a team or workstream change."
  - "GET /v1/policy?group= answers anyone in the organization, whatever the group names."
---

Once an organization connection has answered any organization method, the engine follows that server's event stream and sends the app `factory.org_changed` `{connection, kind, id}` for `team.changed`, `workstream.changed` and `workstream.removed`. An empty `id` means a gap the server could not replay, so the app reads both lists again. Following stops when the connection is detached, its sign-in changes or is refused, or the engine stops.

`GET /v1/policy?group=<organization workstream id>` now answers 404 to anyone who cannot see that workstream, the same answer its own route gives. Other groups are unchanged.
