---
kind: added
title: Run ended work again as a new job
pr: 2219
surface: [engine]
invalidates:
  - "Running ended work again could only restart the same job through Run again. Run again as a new job now makes a separate job from failed, stopped, closed or done work."
---

Run again as a new job keeps the original job unchanged and starts a new job
with the same request, steps, model choices, autonomy and budget limit. Details
are copied verbatim. Earlier spending, pull requests, failures, branches and
conversations stay with the original. Both records link the jobs and name the
person who acted. A source issue is a reference only, never the new job's issue
number. A refused start leaves no new job.
