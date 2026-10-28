---
pr: 2008
kind: fixed
title: Recovery and verification limits count attempts, not minutes
surface: [engine]
invalidates:
  - "Was true: an interrupted step's automatic retries stopped after 2 minutes even with attempts left, and a runner had 2 minutes to come back. Is true: retries stop only when RecoveryLimit attempts are used; a runner has RunnerReturnWindow (15 minutes) to take a queued retry."
  - "Was true: a repair whose completion report was refused was stopped 5 minutes later whatever it was doing. Is true: it is stopped after 3 reports with the same evidence or 6 refused reports in all; its stage's own time and money bounds still apply."
---

A job that lost its connection a few times over a long step, or whose repair
was making real progress between refused reports, was paused for a person
because a clock ran out. The limits are now counted in what was tried:
three retries of an interrupted step, three repeats of the same unresolved
evidence, six refused completion reports. The only clock left bounds how
long a queued retry waits for its runner to come back, now 15 minutes, so a
laptop closed briefly does not turn into "needs you".
