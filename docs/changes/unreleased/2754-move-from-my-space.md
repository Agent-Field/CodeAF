---
kind: changed
title: A move from My space into a workstream leaves one decision line, and its Undo takes it back
pr: 2754
surface: [factory, engine]
invalidates:
  - "A file moved from My space into a workstream left no record there, and its mover could take it back at any time. Now the workstream's Decisions say \"Priya moved “Launch plan” in from their space\" (decision kind move). Moving it back within the Undo window (30 s on the server, 10 s on the desk's note) withdraws that line. After that, only whoever looks after the workstream may move it out to a space."
---
