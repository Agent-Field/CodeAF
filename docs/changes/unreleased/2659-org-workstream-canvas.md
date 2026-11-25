---
pr: 2659
kind: fixed
title: a canvas made in an organization workstream belongs to that workstream
surface: [factory, engine]
invalidates:
  - "An organization's canvas stood on its own: owned by a person or team and shared one share at a time, so New canvas inside a team's workstream made the person's own canvas and the team never saw it (teams film D11). A canvas can now belong to an organization workstream (POST /v1/canvases {workstream}, engine factory.org_canvas_create workstream, hello word org_workstream_canvases), and is then reached exactly the way the workstream is — its owner, its shares and Private as they stand now — with the canvas's own shares only adding to that."
  - "Anyone who could make a canvas could make it anywhere. Putting a canvas in a workstream, by making it there or by PATCH /v1/canvases/{id} {workstream}, needs edit on the workstream: a viewer is refused 403, someone it is not shared with gets 404, an archived workstream answers 409."
---

Canvas rows now name their `workstream`, which is how a workstream's Canvases tab
lists its canvases. Taking a canvas out (`workstream: ""`, full on the canvas)
leaves it owned by the workstream's owner with only its own shares.
