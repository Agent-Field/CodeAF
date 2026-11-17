---
kind: changed
title: An agent that can't do a task asks its owner, and a run carries every task it waits on
pr: 2753
surface: [factory, chat, docs]
invalidates:
  - "An agent that lacked an input or a capability (Afi on a server has no web) set its task Failed; now it raises an Ask, \"Can you give Afi what it needs?\", with Paste what it needs, Do it myself and Drop it, and none of the answers becomes a decision. Failed is kept for errors."
  - "A wait added to a task after it was made never reached its run's opening context; now every task it waits on is an input, brief or no."
  - "A task's packet said nothing when there were no decisions, and runs went to ws_decisions to check; now it says \"Decisions in force: none yet.\" and that everything it needs is already read."
---
