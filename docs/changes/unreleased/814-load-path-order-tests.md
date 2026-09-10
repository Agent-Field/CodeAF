---
kind: fixed
title: a cancelled arm is waited off the temp home, and a recovered lane is in the order
pr: 814
surface: [engine]
invalidates:
  - "`TestACancelledRaceEndsEvenWhenAnArmCannotReport` could red at cleanup with `v3: directory not empty` (nightly 34033965253) and as a data race under `-race` in ~0.4s: cancel ends the hedge while the arm is still inside `Ledger.Note`, and letting that write finish during `TempDir` cleanup raced the directory away. The arm is let go and waited off the temporary home before RemoveAll."
  - "`internal/lane` scenario 3 (`TestS3ItComesBack`) required a recovered lane to be sampled in thirty requests. After three refreshes the chooser already had it in the order; whether a sampler picked it was luck, and nightly 33877387557 saw Baidu miss. It asserts the recovered lane is in the order, which is the penalty-box law."
---

Seven of the nine load-, path- and order-dependent reds named by #814 were
already fixed on `dev` (#417's TERM seam, #559's harness steering note, #631's
duty latch, #805's question-demo pin and retry room). These two were not.
The known-red ledger is unchanged.
