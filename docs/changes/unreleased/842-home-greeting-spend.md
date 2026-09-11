---
kind: fixed
title: A bare launch draws the spend panel and the head's money on its first frame instead of three seconds later
pr: 842
surface: [chat]
invalidates:
  - "The greeting (landHome) built home with its own short list of readings — the bands and the folders — so the first frame drew the spend panel as its placeholder line and the head with no money until the first home beat. It takes the same list the door takes now (furnishHome), and the first frame carries the spend, the machine's day and the repo readings."
  - "There were two homeView literals (raiseHome's own and newHomeView) and three lists of home readings (landHome, raiseHome, refreshHome). There is one constructor and one list; a reading added to furnishHome reaches every road."
  - "The delay was not a slow read or a missing cache: a fortnight's ledger is a 26 ms sequential read, the world walk 14 ms. Home still re-reads the ledger on its three-second beat, deliberately, and that is fine."
---

Measured frame by frame in tmux against a copy of a real home: first frame at t,
`today $13.44 of $500` at t+3.0s, on dev `ef36c3770`. The readings were never
slow; the greeting road simply did not ask for them.
