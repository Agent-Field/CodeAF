---
kind: fixed
title: Factory Manual asks before every step
pr: 2200
surface: [engine, app, docs]
invalidates:
  - "Manual asked only at enabled approve steps and before publication or repair pushes. It now also asks before each step's first round, except where Start or an approve step already answered."
---

The ask uses the existing approval door and reads as `approve_plan` on the wire. Its
question names the step, a no sends back the finished step before it, and Auto passes a
standing ask through the same recorded approval. Jobs with no saved mode keep only their
own approve steps. A live run through the app's doors on one model covers Approve and
send-back.
