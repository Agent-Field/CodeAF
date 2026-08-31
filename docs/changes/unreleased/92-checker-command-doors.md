---
kind: fixed
title: a declared door is something that can actually run
pr: 92
surface: [tasks]
invalidates:
  - "The check's refusal may list bare backticked words like `origin` or `main` as runnable — no longer true. A declared span becomes a door only if its first word is a program the shell would find or it names a file in the tree the checker stands in."
---

A task's checker was told it could run `origin`, `main`, `Agent-Field/agentfield` and a
CodeQL rule id, because the acceptance backticked them and every one of those has the
shape of a command. It ran them, collected the shell's 126s and 127s, and wrote them into
a finding a person read as the state of the work. A dead entry does not cost a line of a
refusal; it costs the finding the whole check exists to produce.

The question a declared span now has to answer is asked of the machine rather than of a
list: is the first word a program the shell would find, or does the span name a file the
ground the checker was put in really holds. What the work RAN is not asked — a receipt is
the work having already run the thing.
