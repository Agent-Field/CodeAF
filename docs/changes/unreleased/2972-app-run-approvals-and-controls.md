---
kind: fixed
title: Restore app run approvals and name missing screen controls
pr: 2972
surface: [app, engine]
invalidates:
  - Opening an app run approval used to lead to a conversation. The Inbox now opens the run and restores its pending approval after restart.
  - Check could pass a declared manual action or creatable record type with no control. Check now names the missing action or creation control for the Builder to repair.
---

An app run approval shows its steps, proposed effect and related records before
you approve or reject it. Check also catches create buttons that omit required
input, so the Builder can supply the fields on the screen.
