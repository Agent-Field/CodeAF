---
kind: fixed
title: A label added to an issue the factory already knows reaches its job
pr: 2530
surface: [engine]
invalidates:
  - "Was true: the GitHub issue list read each issue's id but never kept it, and the factory skips a read whose id differs from the job's, so once a job knew its id no later label, title or comment reached it, and a start label put on a known issue never started it. Is true now: listed issues carry their id, and a known job takes the forge's new labels, title and comments on the next poll."
---
