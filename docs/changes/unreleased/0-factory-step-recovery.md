---
kind: fixed
title: Interrupted factory steps have durable bounded recovery and a working manual retry
pr: 0
surface: [engine, chat]
invalidates:
  - "A lost runner stopped an Auto job indefinitely and an old opened-publication status hid step retry. Auto now persists a bounded recovery window, while finished publication no longer hides the failed step's recovery door."
  - "The app exposed Retry for a historical chat failure even when the backend retained no failed message. Agent retryable now states whether agents.retry can resend a retained message."
---

Automatic recovery admits only known transient step failures, up to three retries
inside two minutes, with 2/5/15-second backoff. Worker ownership, existing holds,
repository switches, mode and the shared job budget are checked under the desk/item
locks. Unknown publication and merge outcomes are never blindly replayed. Manual
step retry retains earlier results, PR identity, spend and increasing round history;
its expected revision is compared atomically. Service shutdown waits for in-flight
job timer callbacks. Failed-message retry capability stays distinct from step retry.
