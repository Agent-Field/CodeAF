---
kind: fixed
title: A turn stopped at the second stage keeps its paid steps across a restart
pr: 2440
surface: [chat, engine]
invalidates:
  - "Was true: a turn let go of at the stop bound recorded its spend only on its abandoned line, which replay never bills, so a restart could save a lower total. Is true now: the turn's paid conversation steps are written as a usage seal without duration, and replay restores them."
---

The abandoned line stays evidence and is never billed; errands on the turn's clock
keep their own usage lines. A settle turn stopping at its bound and a final stage
report now write the same step seal, without duration. Transcripts written
before this change cannot recover an abandoned turn's missing steps.
