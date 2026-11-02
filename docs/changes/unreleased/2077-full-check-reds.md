---
kind: fixed
title: a finished run's record is saved before it is announced, and dev's full check is green again
pr: 2077
surface: [engine, build]
invalidates:
  - >-
    dev's daily Full check was red from 2026-10-23 and the reds looked like
    flaky run-spend tests. They were a real ordering bug: a finished task run
    was announced before its row was saved, and closing the chat did not wait
    for it, so the save could land after close. The row is now saved first
    and close waits for a run that has already ended.
  - >-
    The promotion workflow tests demanded explicit dispatch only. The shipped
    contract is still the Friday 17:00 Toronto schedule, and the tests now say
    so.
---
