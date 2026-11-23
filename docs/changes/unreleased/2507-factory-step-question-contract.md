---
kind: fixed
title: A step's question arrives as kind step, is asked once, and an ended job offers no Give back
pr: 2507
surface: [engine, factory]
invalidates:
  - "A step's question with options went out on the wire as needs_you.question.kind \"options\" (\"yes_no\" for a yes/no pair, \"text\" with none), so a desk that draws a step's answer buttons for kind \"step\" drew none. Every question a step asks (QKind step) is now kind \"step\" with its options as its answers, which is what appwire's factory.job_answer already documented."
  - "An AgentField harness step's answered question was put to the job again by every relay reading until the step read the steer and sent ask_done, and again after a restart from the questions kept in agentfield.json; job 78 of live A+B run 2 sat in Needs you that way and missed its run's end. An answered question is now kept answered (by its id and the relay seq that asked it) and dropped from the kept questions, and a question the step takes down unanswered while it goes on comes off the job, which reads running again."
  - "A rejected job's state detail read \"Rejected · <words>.. Reopen…\" when the words ended in a full stop. The detail now ends the words' sentence once (., !, ? or … are kept, nothing added)."
  - "Close and Reject keep Item.Manual, and the wire's manual carried it, so a desk drawing its takeover bar from manual offered Give back on a rejected job. Manual still stays set on an ended job (it is what refuses old worker reports) and still round-trips; the new controls.give_back is true only for a taken-over job that has not ended and is not publishing, and is what the takeover bar should read."
---

Found by the second live A+B run (E1–E4). The desk side of E4 is to draw the
takeover bar from `controls.give_back` instead of `manual`.
