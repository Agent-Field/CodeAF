---
kind: added
title: Afi's organization runs wait their turn, ask at a limit, and say what they are doing now
pr: 2750
surface: [factory, docs]
invalidates:
  - "Any number of Afi's tasks could run at once on an organization's server; now at most 3 per workstream and 10 per organization run, and the rest are accepted and read \"Queued · 2nd\", a free place going round the workstreams."
  - "A run had no spending, step or time limit and a model error failed it at once; now it pauses with a question to the owner at its spending cap ($0.50, or the workstream's Start without asking amount), the workstream's budget, 60 steps or 15 minutes, and a passing model error is tried once more 20 seconds later."
  - "A running task said only that it was running; now it carries what Afi is doing now, its steps and its spend, and GET /v1/workstreams/running (factory.org_workstreams_running) lists every agent's work across the workstreams a person is in."
---

Lane L6, slices 2 and 3. The limits are fixed for now; nothing in Settings changes them yet.
