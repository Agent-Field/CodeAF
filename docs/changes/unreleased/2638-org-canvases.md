---
kind: added
title: an organization's canvases live on its server and two people draw on one canvas at once
pr: 2638
surface: [factory, engine, build]
invalidates:
  - "A canvas was only ever local (canvas.db on the laptop); a canvas made in an organization window became Personal. Now an organization keeps its canvases on its server (/v1/canvases*, engine factory.org_canvas*, hello word org_canvases), written through canvas-core's `canvas rpc`."
  - "canvas-core let every person write any frame. On the organization server two people never write one frame at once: a write into, or a hand on, a frame another person's hand is on answers 409 conflict with the frame, the holder and the ops not yet seen."
  - "The organization server ran codeaf alone. It now needs canvas-core's `canvas` program beside it (or $CODEAF_CANVAS_BIN, or on PATH) to keep canvases; without it the canvas routes are absent and org_canvases is still announced by the engine but the server answers not_found."
  - "A canvas frame's live document (canvas/<id>/frame/<fid>, org_docs) was always not_found. It now follows its canvas: found when the canvas is visible and the frame is on it, read with view, written with edit, and dropped when the frame or the canvas is deleted."
---

The canvas is not re-implemented in Go: the server execs `canvas rpc` (desk
repository crates/canvas, CONTRACT §10) for every read and write, decides who may
with the access core (view, comment, edit, full), and stamps every op as
person:<handle>. `factory.org_changed` now also relays `canvas.changed {id, seq, by}`
and `canvas.removed`.
