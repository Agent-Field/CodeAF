---
kind: added
title: The app can set up a local factory and explicitly enable its runner
pr: 0
surface: [engine]
invalidates: []
---

The typed factory app protocol can start or adopt this computer's factory,
connect GitHub, select repositories, read issues into Backlog, and clone or
skip checkouts. Setup starts no runner or job. A separate explicit runner
request uses the existing worker lifecycle and app credential custody.
Remote attachments survive local factory start and shutdown.
