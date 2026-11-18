---
kind: fixed
title: An answer in words to an Ask is kept as the answer, never as the Ask's own line
pr: 2806
surface: [engine, factory, docs]
invalidates:
  - "A words-only answer to a workstream task's Ask was recorded in Decisions as \"<Ask title> <words>\". It is now \"Asked “<Ask title>”, answered: <words>\". An Ask titled with a problem no longer becomes a decision that the problem is settled, and the decision check no longer pauses work against the person's own answer."
  - "The chat manual did not mention Reply… on an agent's Ask. It now has its own section in workstream-asks-and-decisions.md."
---

Found by the workstreams v3 simulation. Afi asked "The release notes pasted were
placeholder text, not the notes" and Santosh replied with the notes. The decision
check then paused the finished launch post with Keep the decision / Go ahead anyway.
