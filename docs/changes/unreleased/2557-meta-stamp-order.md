---
kind: fixed
title: An effort, place or working copy recorded right after a message survives a restart
pr: 2557
surface: [engine]
invalidates:
  - "Since 2026-09-11 (#876, and in v0.8.2), a conversation's effort, places and working copies were written to meta.json at once, while the stamp a person's message owed was written behind the path; a stamp still owed then landed afterwards with the older values, so the next process read the effort as unset and could forget a working copy cut in that window. The configuration stamp now waits for the owed message stamp before it writes, as the model stamp already did."
---
The window is short. A person who set an effort, or whose work cut a copy, while the
write for their message was still owed would come back to the earlier state.
