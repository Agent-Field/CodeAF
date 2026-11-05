---
kind: fixed
title: Keep background chat-note faults from stopping the app
pr: 0
surface: [app]
invalidates: []
---

Background chat-note delivery and relay acknowledgement writes now use the
app's panic guard, so a fault in either goroutine is logged without stopping
the app.
