---
kind: added
title: Look closer on a factory job, and Read new issues as they arrive
pr: 2147
surface: [engine]
invalidates:
  - "A rule-started job launched on the arrival read alone. It is now looked at closer first while Read new issues as they arrive is on."
---

Look closer reads a job's issue with read-only repository tools under a
budget of 12 calls, one minute and $0.10, and shows files, a plan,
unknowns, tests and a cited rubric. Every shown path exists. Looks are
cached on the issue text and head, and limited to $10 a month and 20 a day
per repository. `read_on_arrival` (default on) turns off the arrival read
and the automatic look.
