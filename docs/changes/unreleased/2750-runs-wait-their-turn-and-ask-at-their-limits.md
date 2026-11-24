---
kind: added
title: Afi's organization runs wait their turn, and ask the owner at a limit instead of failing
pr: 2750
surface: [factory, docs]
invalidates:
  - "Any number of Afi's tasks could run at once on an organization's server; now at most 3 per workstream and 10 per organization run, and the rest are accepted and read \"Queued · 2nd\", a free place going round the workstreams."
  - "A run had no spending, step or time limit and a model error failed it at once; now it pauses with a question to the owner at its spending cap ($0.50, or the workstream's Start without asking amount), the workstream's budget, 60 steps or 15 minutes, and a passing model error is tried once more 20 seconds later."
  - "A running task said only that it was running, and amber counted only Asks; now a running row's second line says what Afi is doing now (task.run.now, with its steps and spend), the Work header reads \"7 running · 1 failed\", and your Interrupted or Didn't report back task is amber, waiting on you."
---

Lane L6, slice 2. A task that starts by itself when its waits clear (#2736) waits its turn under the same limits. The limits are fixed for now; nothing in Settings changes them yet.
