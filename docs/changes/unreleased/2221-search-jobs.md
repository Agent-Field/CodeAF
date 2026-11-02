---
kind: fixed
title: Find jobs across connected factories without opening their jobs lists first
pr: 2221
surface: [engine]
invalidates:
  - "Was true: search.all found conversation tasks but no factory jobs. Is true now: it also returns a separate job group from each connected factory's caller-visible queue."
---

Search finds jobs by title words, request text when the queue carries it,
repository, or number (`12`, `#12`, `repo#12`, `owner/repo#12`), ignoring case.
Title matches come first, with newest jobs first within each rank. One limit
covers jobs across all factories. Dismissed and dropped jobs are excluded;
other closed jobs appear only during the week they closed, like the jobs list.

Factories are read concurrently with one 1.5-second deadline. A slow or failing
factory contributes nothing and leaves the other search groups available.
Search never starts a stopped local factory. Hello announces `search.jobs`;
older clients can ignore the additional group, whose hits have no agent.
