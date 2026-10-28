---
kind: fixed
title: Hold model question clocks in the app engine
pr: 0
surface: [engine]
invalidates:
  - "App Hold reported success on a model question while its clock kept running. It now uses the engine's existing HoldQuestion door and publishes the changed question under its existing ask id, so the chat and Inbox remove the deadline. Identical question replays remain deduplicated and task proposal Hold keeps its existing task action."
---
