---
kind: fixed
title: codeaf accepts Stop after a running job moves on
pr: 2235
surface: [engine]
invalidates:
  - "Stop required the version the desk last read and could refuse while the job kept running. Stop now accepts an old version or no version while the job is active."
---

Stop ends an active job even when its page has fallen behind. Stop on the
current running step follows the same rule; it still refuses a step that
has ended or a job that is publishing. Ended jobs keep their existing
refusals, and permissions stay the same. Stopping revokes the worker's
lease, so later reports cannot start another step or restore publishing.
