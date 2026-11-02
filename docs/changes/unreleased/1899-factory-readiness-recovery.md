---
kind: fixed
title: Factory final reviews recover automatically without requiring human reviews
pr: 1899
surface: [engine, chat, docs]
invalidates:
  - "Resume after a final-review failure used to turn it into a generic PR repair and could strand the same-head review cursor. It now retries the final review unless manual changes or explicit instructions require repair verification."
  - "A chained PASS/FAIL/SKIP output count could pause Auto when grep correctly found zero failures. Observed exits now govern repeated reporting of the same check; unrelated commands and targets remain guarded."
  - "Final-review report omissions and execution failures could be discovered only after the model stopped and immediately demand intervention. The reporting door returns omissions, execution recovery is bounded within the same conversation, and remaining shortfalls use the normal step rounds."
---

Ready for Review follows current-head green CI and a complete code/description/
feedback check; a reviewer's approving vote is a separate merge requirement.
Outstanding requested-change feedback is still read and assessed, but does not
prevent a final code review after it has been handled. The recorded publication
door revalidates the complete snapshot before marking ready. Merge policy,
required approvals, live budget enforcement and external-head protection remain
in force.

Verbose Go test output does not create a different check identity. Target,
subset, race flags, test-binary arguments, directory and execution context still
do. Genuine failures, skipped different checks, unknown receipts and compound
build failures cannot be cleared by a successful output filter.

Auto retries transient readiness-publication failures at most three times,
observing the live draft/head/material first each time. A successful but
unacknowledged transition is recognized without repeating the mutation; access
denials and changed snapshots stay distinct. Old in-flight polls cannot restore
a prior head's green CI or undo a newer ready transition on screen.
