---
kind: fixed
title: A computer left marked asleep no longer stops every Factory job
pr: 2195
surface: [engine, app]
invalidates:
  - "A saved asleep mark survived app restarts and only a single wake cleared it. An attach now clears it, and the running runner corrects a mark it was not told about within about five seconds."
  - "A queued job on a computer marked asleep read Starting now or told the person to change Jobs at once. Queued jobs whose computers are all asleep or offline now read Waiting for <computer>, and the manager says the same."
  - "/v1/runner/self gains asleep. Queued jobs with no pin can now present waiting_for_computer."
---
