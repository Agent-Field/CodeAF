---
kind: internal
title: Target the hold boundary test blocker at the pause save
pr: 0
surface: [factory]
invalidates: []
---

The output retention hook also runs during item reads. Arm the test blocker
after Hold's preliminary read so it waits on the pause save under the control
lock. Keep the boundary, cancellation, persistence and failed-save assertions.
