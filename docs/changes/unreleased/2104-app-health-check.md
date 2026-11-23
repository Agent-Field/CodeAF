---
kind: added
title: codeaf app answers health.check with facts about the engine
pr: 2104
surface: [engine]
invalidates:
  - "Was true: an app client had no single read of the engine's own condition. Is true now: `health.check` (hello says `health.check`) answers the version and build, when this engine started and its uptime in seconds, what its start carried on after the last quit (when, whether any reply, how many replies and tasks; absent when nothing), free and total bytes for the work folder's filesystem and for codeaf's home, and the local factory's status. It reads memory, the app state and one filesystem call per disk."
---
