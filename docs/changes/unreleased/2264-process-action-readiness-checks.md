---
kind: fixed
title: Process action checks respect a crew conversation still opening
pr: 2264
surface: [engine]
invalidates:
  - "Was assumed by the concurrent action check: every read of an idle crew member already has a live conversation. Is true: reads during opening can answer from saved history; the conversation announces readiness with agent.changed."
---
An ended process can temporarily have Run again unavailable while its crew
conversation opens. Once the conversation announces readiness, fresh action
reads use its live answer. The regression check now waits for that signal and
also checks that reads during opening return promptly. Product behavior is
unchanged.
