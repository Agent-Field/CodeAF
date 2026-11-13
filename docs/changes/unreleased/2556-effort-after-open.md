---
kind: fixed
title: An effort set right after a conversation opens survives a restart
pr: 2556
surface: [engine]
invalidates:
  - "A conversation's effort, places and working copies were written to meta.json at once, while the stamp a person's message owed was written behind the path; if that stamp was still owed, it landed afterwards with the older values, so an effort set just after a conversation opened read back unset after a restart. The configuration stamp now waits for the owed message stamp before it writes, as the model stamp already did."
---
TestAppEffortRungsRoundTripRealEngine's unknown-word check now sets the rung it checks, so
the touched-packages classifier can retry a single rung on its own.
