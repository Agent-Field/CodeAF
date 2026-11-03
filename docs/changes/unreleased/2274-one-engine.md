---
kind: fixed
title: One desktop engine runs on a home at a time
pr: 2274
surface: [engine]
invalidates:
  - "A second desktop engine could start on a home while the first was still closing, and both could pay for the same Factory read."
---

`codeaf app` holds an OS lock on its home until it exits. A second engine waits
before saying hello and starts when the first ends. If the wait runs out it exits
with a clear log line and has started nothing. A crash releases the lock. Terminal
`codeaf`, headless runs and other homes don't take this lock.
