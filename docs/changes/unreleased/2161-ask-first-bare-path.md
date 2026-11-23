---
kind: changed
title: A bare ask-first line that looks like a path is a protected path
pr: 2161
surface: [engine]
invalidates:
  - "Only touch, touches and path: lines under start.ask_first were protected paths; a bare line such as billing/ was a sentence a triage judged and no check read against a job's files. A bare line is now a path when it is one word or holds a slash or a star (billing/, *.lock, src/**), and it holds a job at its start, after its plan and before publishing exactly like touch <path>. Other lines stay sentences."
---

The rule is the one the desk shows for the same list, so a line it shows as
a protected path is one.
