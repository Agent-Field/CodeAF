---
kind: fixed
title: The desktop app carries on replies after an unexpected close and bounds automatic attempts
pr: 1879
surface: [engine, chat, remote]
invalidates:
  - "The desktop app only resumed replies recorded by a graceful quit, and a dead window could stay working forever. The engine now expires stale working claims and carries recent replies and their running tasks on after an unexpected close."
  - "Resumed after quit was only a live event. All quit and unexpected-close notices are now kept with the conversation and returned in place after reopening."
  - "Opening a session could restart its tasks before an automatic attempt was recorded, and a death before the reply began could lose the continuation. Admission and a pending reply now persist before open; pending retries use the same limits and notice. Person messages from another window reset the streak even while the desktop is absent."
  - "A receipt's position could drift into replacement work after rewind. New receipts anchor to the preceding entry and disappear when that entry is removed; older positional receipts remain readable."
---

Automatic continuation is limited to two consecutive attempts per conversation,
and stops for every affected conversation on the third affected start in ten
minutes. Replies left for over an hour are reported for the person to continue.
The state file records the attempts and handled claims before work starts.
