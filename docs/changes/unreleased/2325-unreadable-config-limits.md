---
kind: fixed
title: codeaf keeps unreadable profile approval limits in factory stages and task children
pr: 2325
surface: [engine, factory]
invalidates:
  - "A factory stage's allow posture bypassed an unreadable profile's saved approval rules. It now requires a person when those rules cannot be read."
  - "Task workers, repair rounds and auditors dropped their parent's profile read failure. They now retain that signal and refuse calls that need a person when nobody can answer."
---

A readable profile keeps its existing behavior, including a factory stage's own
allow posture. An unreadable profile cannot silently discard a saved denial:
the existing approval gate reports the failed file read and requires a person.
