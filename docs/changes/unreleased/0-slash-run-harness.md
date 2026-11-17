---
kind: added
title: Saved harnesses run from the app wire
pr: 0
surface: [engine, chat, remote]
invalidates:
  - "The app could list saved harnesses but could not explicitly run one. agents.run_harness now sends a named shape and request through the terminal picker's shared runner, with ordinary turn events and agents.send client_id correlation."
---

Unknown names refuse before turn events, busy conversations refuse, and engines
without the door say harnesses are unavailable here. Attachments remain staged
for ordinary messages.
