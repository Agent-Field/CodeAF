---
kind: fixed
title: The desktop app carries on replies after an unexpected close and bounds automatic attempts
pr: 1879
surface: [engine, chat, remote]
invalidates:
  - "The desktop app only resumed replies recorded by a graceful quit, and a dead window could stay working forever. The engine now expires stale working claims and carries recent replies and their running tasks on after an unexpected close."
  - "Resumed after quit was only a live event. All quit and unexpected-close notices are now kept with the conversation and returned in place after reopening."
---

Automatic continuation is limited to two consecutive attempts per conversation,
and stops for every affected conversation on the third affected start in ten
minutes. Replies left for over an hour are reported for the person to continue.
The state file records the attempts and handled claims before work starts.
