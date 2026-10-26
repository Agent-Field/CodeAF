---
pr: 0
kind: fixed
title: An app engine finishes its wrap-up after its app has gone
surface: [engine]
invalidates:
  - "Was true: an engine started by an app ended with SIGPIPE if it wrote to stdout or stderr after the app had closed its pipes, cutting its leaving wrap-up short. Is true: those writes fail and the wrap-up finishes."
---

An app may quit without waiting for its engine, and an app that crashes
cannot wait. Either way the engine goes on stopping its processes and writing
its conversations down after the pipes to the app have closed. Go ends a
program that writes to a broken stdout or stderr unless SIGPIPE is ignored;
`codeaf app` now ignores it, so the bounded wrap-up always finishes.
