---
kind: changed
title: the manual's wire lane prints what each pass spent, summed off the ledger as it asks
pr: 331
surface: [engine]
invalidates:
  - "the spend of a TestManualOnTheWire pass could not be recovered after the run (the ledger lives under the throwaway home the test deletes); it is now printed by the pass itself as THIS PASS SPENT, per set and in total."
---

A measurement without its price is a measurement whose next pass has to be argued for from memory. The three cued passes of #321 were reported as "low tens of cents, estimated" for exactly this reason. The lane already read the ledger after every question to pin the model; it now keeps the dollars it read.
