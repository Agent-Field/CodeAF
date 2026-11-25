---
kind: added
title: SENIOR_DEV_KEEP_SHELL_PROCESSES=1 leaves a senior-dev run's processes running when it ends
pr: 2950
surface: [engine]
invalidates: []
---
By default senior-dev stops every process its run started. With `SENIOR_DEV_KEEP_SHELL_PROCESSES=1`, background jobs and other descendants keep running after the run ends, so a service the request asked to be left up stays up.
