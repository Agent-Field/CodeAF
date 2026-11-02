---
kind: fixed
title: Opening a long chat no longer holds its whole journal in memory
pr: 2045
surface: [engine]
invalidates:
  - "Previously an opened chat kept the words of every journal line its replay read, including the copies a later compaction re-recorded; now it keeps only the messages it holds."
---

A resumed chat now lets go of every message line a later compaction replaced,
at the open and at each compaction, rewind or fold while it runs, as it
already did for the messages' presentation marks. A journal too large to keep
in the reading cache is read into a large amount of short-lived memory; once
such a reading is done, the engine returns that memory to the system in the
background. Opening a 1,371-entry chat on a Mac went from about 720 MB to
about 80 MB for the engine. Entries, their pages and times are unchanged.
