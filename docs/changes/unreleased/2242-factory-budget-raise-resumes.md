---
kind: fixed
title: Raising a job budget resumes its capped step
pr: 2242
surface: [engine]
invalidates:
  - "Changing Budget could save the higher limit while leaving the interrupted step waiting on the old spending question."
---

Raising Budget above recorded spend now resumes the stopped step in Auto and
Manual, on local and leased runners. The job keeps its conversation, round and
spend, and chat confirms the raised budget and continuation once. An increase
that still does not cover spend keeps the question with the current budget.
Decreasing the budget below spend is still refused. Continue remains available;
changing Budget does not approve publication or answer unrelated questions.
