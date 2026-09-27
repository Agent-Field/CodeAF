---
kind: fixed
title: a background job's log is a bounded spool that admits its truncation
pr: 1599
surface: [chat, engine]
invalidates:
  - "A background job's log was unbounded: everything the job ever wrote went to
    one <id>.log forever, so a watcher printing for a week filled the disk. The
    spool is now a window of two 4MB chunks (<id>.log and <id>.log.1); older
    output is discarded with a notice."
  - "The manual and the jobs tool promised the whole log on disk. What a job
    keeps is its most recent chunks, and when output has been discarded the
    footer and the completion note name the truncation instead of saying full
    log."
  - "A failed, short or unclosable job-log write was swallowed silently. It is
    still never fatal to the job, but the jobs footer now says the log stopped
    and why."
---

A job's retained output stays addressable by the read tool exactly as before;
the window is what changed. The bound is fixed bytes per job, never a
wall-clock or a rate.
