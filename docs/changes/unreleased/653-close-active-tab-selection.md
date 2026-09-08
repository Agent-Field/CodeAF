---
kind: fixed
title: Select the last open chat when closing the current tab
pr: 653
surface: [chat]
invalidates:
  - "Closing the active tab always went Home on a shared engine connection, and could do so locally when another remembered tab had no live keeper entry. Closing now selects the most recently used remaining tab; Home is used only when none remain."
---

The existing conversation attach/open path owns the switch. A refused open
retains the outgoing tab and its draft. Local held agents continue running;
hosted connections retain their existing single-session swap semantics, and
no extra close is sent to the newly selected agent. Remote destinations do not
need to name a folder that exists on the UI machine.
