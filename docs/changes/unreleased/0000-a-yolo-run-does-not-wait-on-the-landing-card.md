---
kind: fixed
title: a landing card under --yolo takes its default instead of parking the run
pr: 0000
surface: [chat]
invalidates:
  - "Under `codeaf chat --yolo` a task that landed on the check road — the check ran out of time, nobody could check it — raised the landing card (`▸a accept · n not right · s tell it`) and parked on it for ever: a surface existed and nobody was at it, and the card never took a default. An unattended run now takes the check road's own default (accept) and settles, and the record says the dial answered rather than a person, so a restart sweep or `pool status` can tell it from a landing a check passed. A conflict, a shift or a ground that moved stays the person's and parks as it always did."
---
