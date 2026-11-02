---
kind: added
title: Look closer on a factory job, Read on a click, and Read new issues as they arrive (off by default)
pr: 2147
surface: [engine]
invalidates:
  - "Every new issue was read by the factory's AI as it arrived. That read is now off by default (read_on_arrival); a person reads chosen issues with factory.read, or turns the setting on."
---

Look closer reads a job's issue with read-only repository tools under a
budget of 12 calls, one minute and $0.10, and shows files, a plan,
unknowns, tests and a cited rubric. Every shown path exists. Looks are
cached on the issue text and head, and limited to $10 a month and 20 a day
per repository. `read_on_arrival` (default off) turns on the arrival read
and a look before each automatic start; `factory.read` reads chosen issues
on a click. Starts are decided by size labels as before.
