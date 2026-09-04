---
kind: added
title: Work handed over carries a few quoted lines of the conversation it came from
pr: 626
surface: [chat, engine, docs]
invalidates:
  - "A task was opened on its contract and the person's latest message alone; every door that admits work now also compiles a bounded, attributed selection of the surrounding conversation through one compiler, and no door composes its own."
  - "`propose_task`'s schema said the worker never sees this conversation and cannot see the calls that already ran; it now says the worker gets quoted lines of the conversation and sees which calls were made, never what they returned."
  - "The task and shaping prompts said nobody was watching and there was nobody to ask; a line can be sent into a running task and arrives in its next turn, and the prompts now say that, along with the fact that such a line does not replace the brief or the done-condition."
  - "Which user-role lines the person actually typed was not restored when a session was reopened, so work started after a restart silently carried none of the earlier conversation; it is now rebuilt from the journal's own note marks."
---

The new section is a SELECTION, not a record of constraints: at most eight lines
and six calls, each line bounded at about 600 bytes with the middle elided, one
overall budget measured against the bytes actually rendered, and at most three
generations of inheritance. A constraint older than that window, or buried in the
middle of a long message, is not in it — which is why each line names the session
journal it came from, and why the worker is told in the document that the
selection is partial and that the lines are what was *said* rather than what is
true. The brief and the done-condition are still the contract.

Nothing is summarised by a model, and nothing is promoted: a call whose outcome
this process did not record renders as "outcome unknown", and one with no result
at all says so rather than reading as a success.
