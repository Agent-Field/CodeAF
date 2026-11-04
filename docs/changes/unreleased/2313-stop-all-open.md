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
    remaining_unknown and partial with zero jobs stopped and services are still
    stopped.
---

Stop-all never opens a cold kept crew owner or queues behind its initialization.
The existing open is complete only after initialization and owner publication;
a timeout cannot use an owner published late. Services start while initialization
is blocked. A shared completion signal replaces the mutex waiter, so retries
against a stuck open leave no waiter goroutines behind. The 5-second open wait,
5-second jobs wait and 10-second method ceiling are unchanged.
