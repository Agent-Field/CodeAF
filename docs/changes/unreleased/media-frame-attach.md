---
kind: added
title: A captured video frame can be staged as an image attachment without a path grant
surface: [engine, app]
invalidates:
  - "A frame of a paused clip could only reach a message as a path the surface supplied. media.attach_frame now takes the PNG bytes the app captured plus the clip's agent, source, recorded version and moment, validates the PNG and the clip's live version, and keeps an owned immutable snapshot whose typed source names the clip and the moment. The engine never opens a caller path. Ordinary Send carries it as an image; abandoning an unsent frame removes only the snapshot, while accepted or uncertain delivery retains it."
---
