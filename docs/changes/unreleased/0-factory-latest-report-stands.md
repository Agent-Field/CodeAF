---
kind: changed
title: A factory step's latest report stands, not its first
pr: 0
surface: [engine, chat, docs]
invalidates:
  - "Was true: a second `stage_result` in a round was refused with `this stage already reported, and its first report stands`, the tool said to call it ONCE and answered `reported · the stage ends when you stop`, and the round was cut two minutes after the first report even while the step was still working. Is true now: every call replaces the one before and answers `reported · the last report you send before you stop is the one the factory reads`; the description says `Call it again if your result changes; the last call before you stop is the one that counts.` The round ends when the step is idle, bounded only by the two-hour wall, and the brief closes with `End by calling stage_result with your final result.`"
  - "Was true: a step's round line in the Timeline was its report's first line. Is true now: it is the final report's, with `· updated 1 time` or `· updated N times` when the step reported more than once; the stored result carries `Reports`, the count."
---

A careful step reports `done: false` because a test fails, fixes the test,
and wants to say so. It was judged on its first word, and its later work was
cut off; it is now judged on its last.
