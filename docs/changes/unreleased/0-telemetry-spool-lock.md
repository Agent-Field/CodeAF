---
kind: fixed
title: Preserve usage events when processes share a telemetry spool
pr: 0
surface: [engine]
invalidates:
  - "The sending-file protocol alone did not protect an open append descriptor from another process renaming and removing its file; shared spool operations now use a directory file lock."
---

two processes could lose a usage event when one flushed while the other appended.

A directory file lock now keeps append, size checks, and flush ownership changes
together across processes. The lock is released before network delivery.
A synchronous append also reports an error if its spool path disappears.
