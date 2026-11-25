---
kind: internal
title: The fixed prefix is back under its byte budget, by trimming two tool descriptions
pr: 2833
surface: [engine]
invalidates:
  - "TestTheFixedPrefixStaysUnderItsBudget was red on dev and every branch off it, at 52,510 bytes against a 52,497 cap, since the app/v3 landing (#2561). It is green at 52,442 with the cap and its waiver unchanged."
  - "The bash tool's description said \"Optionally provide a timeout in seconds.\" It no longer does; its timeout field describes itself, and the session's suffix states the 600-second cap."
---

Every byte of the fixed prefix rides every request of every turn, so the
`share_deliverable` and `bash` descriptions were shortened, without dropping
anything they promise, rather than raising the waiver.
