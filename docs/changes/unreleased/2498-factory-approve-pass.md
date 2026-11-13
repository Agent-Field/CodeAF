---
kind: fixed
title: Approve steps pass for real, say who may pass them, and show who did
pr: 2498
surface: [engine, app, chat]
invalidates:
  - "An approval Auto passed could leave its approve step waiting: a later report of the finished step before it moved the run's current step back, so the answer passed nothing, and the proof approval then refused with \"has an approve step nobody has passed yet\". A report of a finished step no longer moves the run off the approve step it waits at, and a yes always passes the waiting approve step."
  - "Auto passed every routine approve step. An approve step now says who may pass it with `pass:` — `person` (only a person; Auto leaves it in Needs you and the manager's Approve is refused), `manager` (a person or the manager), or `manager when <condition>` (the manager judges the condition against the work, else a person is asked with its reason). A step that says nothing reads as manager, so existing pipelines behave as before; a repository-fixed approve step is always person."
  - "A newly added approve step — from the pipeline editor, the library, Change with AI, a job edit or the manager — now says `pass: person` unless the edit says otherwise."
  - "A passed approve step kept no record of who passed it. Its phase now keeps Passed (by the manager or a person, the handle when known, and the manager's reason), the wire step carries `passed`, and the job log says \"approve passed by the manager\" or \"by a person\"."
  - "The log line after an approval said the manager may pass every approval; it now names the step's pass word (\"Only a person can pass this approval after plan; it waits for them.\")."
---
