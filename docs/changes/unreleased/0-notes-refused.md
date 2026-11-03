---
kind: fixed
title: Execution notes preserve command refusals and interruptions
pr: 0
surface: [engine, factory, chat]
invalidates:
  - "Execution notes previously called approval and policy refusals completed. They now say refused and retain the real refusal reason."
  - "Partial PASS output could previously label an interrupted command passed. Cancellation and timeout receipts now keep their interrupted outcome."
---

Factory execution notes read the journal's producer refusal mark, with a
conservative fallback for older approval receipts. Refusals, cancellations and
timeouts remain unresolved for the completion guard. Later stage briefs retain
the reason instead of implying that the refused command ran. PR validation
continues to publish only passed command receipts.
