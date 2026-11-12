---
kind: fixed
title: Service output pages keep every returned line contiguous
pr: 0
surface: [engine]
invalidates:
  - "Was true: a line that exceeded a service output page's byte cap could be skipped while later shorter lines were returned, leaving a gap behind the next-line cursor. Is true now: a page returns one contiguous prefix and resumes at its first unreturned line. The optional partial fact identifies a page cut by a cap; truncated continues to mean earlier output is no longer kept."
---
