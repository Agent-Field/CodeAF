---
kind: fixed
title: Judge a job against the person's later requests
pr: 0
surface: [engine]
invalidates:
  - "Was true: the Brief read only the issue and plan as the request. Is true now: it also reads later answers and saved notes, and later requests take precedence."
---

The job Brief now reads what you asked at later gates and in saved notes.
Work that follows your later request is treated as asked, rather than a fault,
surprise or scope creep. A real miss against your request is still reported.
Adding a request makes an earlier Brief out of date.

The writer keeps the newest 12 distinct requests, up to 2 KiB per text and
16 KiB of request JSON. Requests survive the ordinary input cuts. Answers
retain their step, gate and time; notes have no recorded gate or time and
keep their saved order before dated answers.
