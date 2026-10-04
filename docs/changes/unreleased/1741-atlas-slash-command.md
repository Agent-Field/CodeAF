---
kind: added
title: /atlas opens the architecture map inside the chat
pr: 1741
surface: [chat, docs]
invalidates:
  - "The architecture map of the two-computer work was reachable only as `codeaf atlas`, a separate command that takes the whole terminal. `/atlas` now opens the same map — the same internal/atlas model and data, not a copy — as a fullscreen sheet over the conversation you are in; esc or q closes it and returns to the chat intact, and a box you dragged keeps its position the next time you open it. Inside the chat, q closes the map instead of quitting: the standalone command still quits with q or ctrl+c."
  - "The chat's `/help` list named no way to reach the map. `/atlas` is a row on it now, beside `/pair` and `/devices`."
---

The sheet takes the frame whole and owns the keyboard and the mouse while it is
up, the way the wall and the rewind timeline do; the map's playing flows run on
the chat's own loop through one beat message (`atlas.Beat`) that the surface
re-arms. The standalone `codeaf atlas` command is unchanged and both draw from
the one model, so the two cannot drift apart.
