---
kind: fixed
title: Idle factory runners wait for changes without repeatedly decoding finished jobs
pr: 1931
surface: [engine]
invalidates:
  - "An idle worker rechecked the full job store every 100 milliseconds. It now waits for the existing filesystem notifications or the pull deadline, and unchanged pulls reuse decoded items after checking file metadata. A runner in the same process as the service does not re-read the job list to find that service, and one job-list read asks once whether this machine has a GitHub token. An ordinary request does not decode every job when nothing is due. While filesystem notifications are working, the fallback does not reread every job file on a quiet floor."
---

The subscription starts before the first pull so an arriving job cannot slip
between the empty answer and the wait. Request authorization records the idle
heartbeat once; a notification does not rewrite that heartbeat and wake itself.
Lease reservation still rechecks the current item under its own lock.
Short saved runner timeouts shorten the pull deadline to half their duration.
Without filesystem notifications, the shared watch checks metadata every 500
milliseconds so enqueue pickup still takes less than a second. While
notifications are working, that fallback checks the directory's own time
instead of rereading every job file. Repository
metadata changes invalidate cached alias resolution as well as job writes.
A runner does not re-read the job list to discover a service this process
already holds. One job-list read resolves the host GitHub token once.
An ordinary request does not decode every job when nothing is due. It looks
again when a job, a lease, or the repository list changes, or when a saved
deadline arrives. A runner heartbeat does not force that look.
A heartbeat is kept in memory. The runner file is rewritten at most every
15 seconds, so an idle presence check does not wake every waiting pull.
Listing the jobs reuses the last decode while the job files are unchanged.
A runner heartbeat does not count as a change to that list.
