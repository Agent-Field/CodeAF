---
kind: fixed
title: stopped factory write steps keep their earlier change on retry
pr: 2158
surface: [engine, factory]
invalidates:
  - "Was true: stopping Write before its first progress report could make a retry call its earlier committed change empty. Is true now: the job branch keeps its original comparison base, so restarting the stopped step counts that change."
---

Rerun, Try again after a failure, and restarting a stopped Write count work already
on the job's branch, including committed changes. They do not require a new edit in
each attempt. A truly empty branch still waits for your decision on the first
attempt and on a rerun. Retry checks now make the change their Write step promises.

If the original base cannot be saved, the round fails before editing so a later
retry cannot mistake its earlier work for an empty branch.
