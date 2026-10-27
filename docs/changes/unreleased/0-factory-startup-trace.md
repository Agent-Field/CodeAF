---
pr: 0
kind: added
title: The solo factory writes its startup timings to startup.log
surface: [engine]
invalidates:
  - "Was true: nothing recorded how long the solo factory took to start or to answer its first requests. Is true: each start appends its steps (discovery, lock, forgetting PR jobs, serve) and its first answer, first queue and first job, in milliseconds, to startup.log on the factory floor, keeping the last 400 lines."
---

A slow first read after a start can now be read back: startup.log on the
factory floor says how long each step of the solo factory's start took and how
long its first answers took, counted from when codeaf began. It holds step
names and times only.
