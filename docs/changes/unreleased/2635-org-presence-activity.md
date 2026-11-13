---
kind: added
title: An organization says who is in an item and who changed it
pr: 2635
surface: [factory, app]
invalidates:
  - "Organization events (team.changed, workstream.changed, workstream.removed) carry no actor; the desk shows every author as you."
  - "Changes to teams, workstreams, go-ahead policy, rules, routes, spending and settings leave nothing in the factory's record."
  - "There is no presence on an organization server; the desk cannot know who else has a workstream open."
  - "WAVE2 E3a's page hand (PUT|DELETE /v1/workstreams/{id}/pages/{pid}/hand) is coming; it is not, live co-editing (E7) replaces it."
---

`POST /v1/presence {"item":{"kind","id"},"part"?,"editing"?,"left"?,"client"?}` is a heartbeat every 10 s with a 30 s TTL, answered with who is on that item now. Presence is kept in memory; only a change to who is here is published, as the event `presence {"item","here":[{handle,part,editing}]}`, and only to those who may view the item. An item the caller cannot view is 404. The engine relays it as `factory.org_presence` and serves the heartbeat as the method `factory.org_presence`; hello carries `org_presence`.

Every organization event, and `factory.org_changed`, now carries `by`, stamped by the server from the sign-in. Teams, workstreams, go-ahead policy, rules, routes, spending defaults and settings are written to the factory's record with the person who made the change; a workstream's changes are read by those who can see it only, at `GET /v1/workstreams/{id}/activity` (engine `factory.org_workstream_activity`), in `/v1/activity`, and on the live stream.
