---
kind: fixed
title: Default start rules start what they pick up, and a held start is decided again on new labels
pr: 0
surface: [engine, docs]
invalidates:
  - "Was true: with no `start.max_cost` the limit was $2 while a job's default budget is $5, and an issue with no size label was held, so every issue picked up under the defaults waited with \"Approval needed: start · no size label; job budget $5 is over $2\". Is true now: the default `start.max_cost` is the default job budget ($5), and an issue with no size label is M (the default `max_size`) unless the file sets `size.issue`. A file that sets `size.issue` still holds an unlabelled issue with \"no size label\", and a pull request is still measured, never sized by default."
  - "The start still compares the job's budget, the most it may spend, never an estimate: no read or guess can grant a start."
  - "Was true: once its start rules held an issue, nothing decided it again; adding a size label or a start label left it waiting on a person. Is true now: when a poll sees a held issue's labels change, it goes back to Backlog and the start rules decide it again on the same pass: it starts if they now allow it, or asks again with the new reason. A hold a person answered (declined or approved) and a job that ran are left as they are."
---
