---
kind: added
title: Recorded images can be staged as references without exposing path grants
surface: [engine, app]
invalidates:
  - "media.get could describe recorded images, but a caller could only stage a new attachment by path. media.attach now accepts an agent, opaque source ID and exact recorded version and creates a bounded immutable snapshot with source provenance. Ordinary Send carries it as an image; abandoning an unsent attachment removes only its owned snapshot, while accepted or uncertain delivery retains the file."
---
