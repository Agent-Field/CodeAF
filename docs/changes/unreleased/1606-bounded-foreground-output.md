---
kind: fixed
title: bound foreground bash spill files and preserve their initial output
pr: 1603
surface: [chat, engine]
invalidates:
  - "Only background job logs were bounded; foreground bash could still fill the temporary disk through unbounded pi-bash spill files."
  - "A spill beginning after the result limit silently lost its initial bytes while claiming Full output."
---

Fixes #1606. Foreground output now retains an 8 MiB initial snapshot under the
state home's logs/bash directory while the result continues to show the latest
in-memory tail. Incomplete snapshots and write/close errors are named honestly.
Promotion closes the foreground spill before forwarding output to the job log.

Completed snapshots have a 128 MiB, 64-file, seven-day retention policy. A
directory lock serializes publication and cleanup; independent file leases
protect active writers across processes. Unknown, linked and legacy files are
preserved. Recursive structured search excludes both new snapshots and old
pi-bash files in the current temporary directory. Explicit file reads remain
bounded and shell commands do not inherit these exclusions.
