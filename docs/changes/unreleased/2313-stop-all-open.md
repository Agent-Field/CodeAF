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

Stop-all restores a cold owner kept by a crew on the jobs side, under the same
5-second open bound and caller deadline as an open already under way. This
restores stopping jobs still running behind a light crew record. Non-kept
conversations and members the person closed or archived stay closed; held
owners and shutdown do not start another open. Lookup before admission does
not open or queue behind initialization.
The existing open is complete only after initialization and owner publication;
a timeout cannot use an owner published late. Services start while initialization
is blocked. A shared completion signal replaces the mutex waiter, so retries
against a stuck open join that signal without starting another opener or leaving
waiter goroutines behind. The 5-second open wait,
5-second jobs wait and 10-second method ceiling are unchanged.
