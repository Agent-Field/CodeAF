---
kind: fixed
title: a resumed conversation opens knowing what it has already spent
pr: 135
surface: [chat]
invalidates:
  - "A resumed conversation's status line started at $0.00 and stayed there until
    the person sent a turn or typed /cost. It now carries the conversation's real
    running total on its FIRST frame — the sum of its journal's usage lines,
    auxiliary calls included — and so do the phone status deck and the status
    sheet. Only the engine restored the total before; the surface never asked."
  - "The first turn after a resume drew the whole conversation's restored spend on
    its own receipt, because the receipt is a.cost minus what the turn opened at
    and a.cost opened at zero. A receipt is now the turn's own money on a resumed
    conversation as it always was on a fresh one."
  - "internal/tui3's timestamps fixture (openTurn) reported four cents before
    anybody had typed. An agent already holding money now means a RESUMED
    conversation, so a fixture that wants a turn to have a price gives it one with
    turnSpent while the turn runs."
---

The engine has restored a conversation's total from its journal since spend was
journaled; nothing on the surface asked for it until a frame of the paint clock
came round, and that clock only turns while something is animating. So the
reading is now taken where the context is already measured — on the first frame,
and again on every switch to another conversation, immediately after the meters
belonging to the one being left are zeroed. It goes through the existing
refreshUsage/take path, so there is no second total to drift, and a conversation
that spent nothing restores a zero Usage and draws nothing.

One figure is deliberately not restored: the "saved $x" half of the cache
segment. It is priced per turn at the moment the answering model's prices are
known, and a journal line records the model but not the prices in force — so it
stays empty rather than being invented.
