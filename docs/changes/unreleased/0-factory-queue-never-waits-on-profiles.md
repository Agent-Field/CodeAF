---
kind: fixed
title: A factory's queue answers at once on a busy repository
pr: 0
surface: [engine]
invalidates:
  - "Was true: every queue read asked GitHub for the public profile of each person it named that was not already kept, one request after another, and a profile GitHub refused was asked for again on every read. Is true now: the queue's `people` carries only profiles already read; the rest are read in the background, one at a time, and arrive on a later read. A person not read yet is in `people` with no name or photo, which draws as their handle. A refused profile is asked for again after 15 minutes (`ProfileRetry`), not on every read."
  - "Was true: the card about one person (`/v1/people/{handle}`) also waited on the profile of everyone named on the floor. Is true now: it waits only for the person it is about."
  - "Was true: the queue compared every job with every other to find overlapping changes. Is true now: only running jobs are compared, with each other; the `overlap` field says the same thing."
---

A factory watching a public repository with hundreds of open issues and pull
requests took ten seconds or more to answer its queue, every time, because it
read each author's GitHub profile before it answered. A queue of 800 jobs now
answers in about 110 ms on a laptop, and the wire shape is unchanged.
