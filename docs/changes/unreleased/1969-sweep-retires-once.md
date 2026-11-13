---
kind: fixed
title: The startup sweep retires a landed fork once, not on every launch
pr: 1969
surface: [chat, engine]
invalidates:
  - "The startup sweep asked furrow about every landed fork of every kept conversation on every launch and logged \"retired\" each time, because the checkpoint keeps naming them. It now remembers the forks it retired in forks-retired.json in the session folder and skips them."
---
A kept conversation's landed forks were looked up in furrow at every launch
(`furrow status`, then `furrow forks` per fork, in the person's project), and
the sweep log said the same forks were retired again. A fork is now retired
once, and later sweeps do not run furrow or git for it. A missing or unreadable
memory file only costs one more furrow question per fork.
