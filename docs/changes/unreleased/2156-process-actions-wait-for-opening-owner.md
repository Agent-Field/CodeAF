---
kind: fixed
title: Process actions wait for a crew owner that is still opening instead of saying it is not held
pr: 2156
surface: [app]
invalidates:
  - "Was true: a process action pressed while the light crew owner was still opening read the owner as absent and took the not-held branch. Is true now: the action waits for the open to finish and then uses that owner."
---
