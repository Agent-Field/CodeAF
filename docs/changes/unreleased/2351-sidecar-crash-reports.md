---
kind: fixed
title: The desktop app's engine now prints why it crashed
pr: 2351
surface: [build]
invalidates:
  - "A panic or fatal runtime error in the desktop app's engine ended it with status 2 and no words, because sidecars were built with garble -tiny. They now build with -literals alone, the report prints, and make app-sidecar-reverse reads its names back with the build's own seed."
---
