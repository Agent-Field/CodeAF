---
kind: added
pr: 2096
title: A job says what the factory's own two checks found
surface: [engine]
invalidates:
  - "A job's wire never said whether its start rules or its change check passed: `rule_hold` is cleared once the person answers, and a job the rules started carried nothing. A job now carries `rule_checks`: `start` is \"passed\" when the factory started it on its own and \"asked\" when its rules made it wait for a person; `publish` is the same for its change when its run landed, judged on the commit `publish_head`. A job a person started has no `start`, and a change made after the landing has no finding until the job lands again (`publish_head` is no longer `head_commit`)."
---

The app draws a passed check as a green ◆ and one that asked as amber, on the
job's Before it starts and Before publishing rows. Bring back clears both.
