---
kind: fixed
title: automatic task starts distinguish requests from questions
pr: 1845
surface: [chat, engine]
invalidates:
  - >-
    Questions and product direction could be mistaken for permission to start a
    task. Both route judges now distinguish clear polite requests from
    feasibility questions, ambiguous questions and future direction; a stopped
    task result cannot restart the same request.
---

Clear polite requests can start work even when phrased as questions. Questions
about feasibility, ambiguous questions and future direction stay in the
conversation. A stopped task's result can be reported without starting the same
request again.
