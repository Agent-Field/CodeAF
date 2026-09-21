---
kind: fixed
title: a time limit a person sets now ends a run, by the same road as the cost limit
pr: 1262
surface: [chat, engine]
invalidates:
  - "A session's elapsed-time limit (`--max-hours`) never reached a run that `/task` started. `chatBudget` turns the flag into `session.Budget.Wall`, the interactive check reads it before a new turn, and nothing handed it to the run engine: a run carried a dollar limit (the conversation's spend ceiling) and no time limit at all, so it ran on past the hours its person set."
  - "`RunSpec.Elapsed` now carries what is left of the session's wall when the run starts, measured on the same clock the interactive check uses, and `run.Limits.Elapsed` ends the run on it. The ending is the cost limit's: nothing new is launched, the outcome is `a limit you set stopped it`, and what finished lands the ordinary way. A session with no time limit puts none on its runs."
  - "THE ONE DIFFERENCE FROM THE COST LIMIT IS THE WORK IN FLIGHT. A cost limit lets it finish. A time limit ends it, and absorbs each ending the way the caller's wall does, so a cut task reads `incomplete` instead of `running` and what it spent is in the run's account."
  - "A run that ended on a limit with no result of its own now says the limit's sentence on its row. It said nothing there before."
---

`--max-cost` still does not reach a run: a run's dollar limit is the conversation's
spend ceiling. The manual says which limit is which.
