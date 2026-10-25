---
kind: fixed
title: Idle factory runners wait for changes without repeatedly decoding finished jobs
pr: 0
surface: [engine]
invalidates:
  - "An idle worker rechecked the full job store every 100 milliseconds. It now waits for the existing filesystem notifications or the pull deadline, and unchanged pulls reuse decoded items after checking file metadata."
---

The subscription starts before the first pull so an arriving job cannot slip
between the empty answer and the wait. Request authorization records the idle
heartbeat once; a notification does not rewrite that heartbeat and wake itself.
Lease reservation still rechecks the current item under its own lock.
Short saved runner timeouts shorten the pull deadline to half their duration.
Without filesystem notifications, the shared watch checks metadata every 500
milliseconds so enqueue pickup still takes less than a second. Repository
metadata changes invalidate cached alias resolution as well as job writes.
