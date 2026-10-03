---
kind: fixed
title: a pairing code that was released could be answered by a new pairing under the same short number
pr: 1739
surface: [remote, docs]
invalidates:
  - "The hosted relay drew a pairing number again the moment its mailbox was released, so a device still waiting on the old code could find a new, live mailbox under the same number and wait there until its own timeout. A released or expired number is now held back for ten minutes, and every mailbox carries a random generation that the device sends back, so a device from an earlier pairing is told the code is gone. Old builds send no generation and keep working. The self-hosted relay does not name generations yet."
---
