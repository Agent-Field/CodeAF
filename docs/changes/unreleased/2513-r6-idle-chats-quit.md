---
kind: fixed
title: An idle open chat costs nothing between heartbeats, and a slow quit names its conversation
pr: 2513
surface: [engine, chat]
invalidates:
  - "Every open session rewrote its presence.json every 7 seconds. A busy claim (working, waiting, work out) still does; an idle claim is written when it changes and otherwise refreshed once a minute, carrying leaseSeconds (180) that readers honour through SessionPresence.Window()."
  - "Readers judged every claim on a 15-second window. A claim with leaseSeconds is believed for that lease, clamped between 15 seconds and 10 minutes; a claim without one keeps the 15-second window. A build older than this one still uses 15 seconds and so drops idle sessions from this build between their refreshes."
  - "Every session read its doorstep (answers and requests) on each 5-second beat. A quiet session now reads it when the shared watch reports a write, and runs a safety pass once a minute; folders the watch cannot see are still knocked every beat."
  - "A job or step chat's budget watch woke every 100 ms and looked once a second while idle. It now sleeps 5 seconds between idle looks and switches to the 100 ms cadence the moment a turn, a command or a presence change starts work."
  - "Quitting the app printed only the total time conversations took. Each conversation slower than a second now gets its own line splitting the wait for it from its own close."
  - "An engine waiting for another to release the same home said nothing. The holder writes its pid into app.lock, and the waiting engine says once which pid it is waiting on."
---

WorkAF live run 6 idled at 7–9% of a core with 24 open job chats. Its quit took 18 seconds. Conversations were already let go in parallel; the new lines show which one is slow the next time it happens.
