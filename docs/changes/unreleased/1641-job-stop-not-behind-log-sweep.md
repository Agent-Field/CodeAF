---
kind: fixed
title: a stopped background job reads as ended at once, and its log-folder cleanup runs just after it
pr: 1641
surface: [chat, engine]
invalidates:
  - "Since the bounded job logs (#1603), every background job's ending swept its whole jobs folder before the job stopped reading as running, so a stop showed tens of milliseconds late under load. The sweep now runs in the background just after the ending, one pass per session at a time, and a job's status, its done signal and its completion note never wait for it."
  - "A failed job-log cleanup used to be named in that job's completion note. The completion note is written before the cleanup runs now, so the failure is named in the job's log footer (`jobs output`) instead."
  - "Publishing a job's ending before its sweep was tried and reverted (8b0edfd6d, 2e007e33b) because whoever saw the ending could remove the jobs folder while the sweep was still creating files in it. The post-ending cleanup now never creates anything in the folder, and closing the session or stopping its work waits for a cleanup already under way."
---

Retention still bounds each jobs folder (#1599): a new job's claim sweeps under the folder
lock before its log exists, the startup sweep still runs, and the background pass bounds the
folder again shortly after each ending. What moved is only who waits for it. A job still
running when its session closes and killed after the grace gets no background pass in that
session; the next job's claim or the next startup counts it.
