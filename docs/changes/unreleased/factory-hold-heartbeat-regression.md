---
kind: internal
title: Add a failing regression for a hold lost with its heartbeat reply
pr: 0
surface: [factory]
invalidates: []
---

The regression drops a heartbeat response after the factory drains the hold,
then checks that the running worker eventually reports a pause. It deliberately
remains failing to preserve the delivery defect for a separate fix.
