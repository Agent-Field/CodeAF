---
kind: fixed
title: Stop all during a conversation's open stops the jobs its owner holds
pr: 2313
surface: [app]
invalidates:
  - >-
    Was true: processes.stop_all pressed while the owner was opening stopped no
    jobs and answered remaining_unknown. Is true now: it waits up to 5 seconds for
    that open, then stops the jobs. If the wait runs out, the receipt says
    remaining_unknown and services are still stopped.
---
