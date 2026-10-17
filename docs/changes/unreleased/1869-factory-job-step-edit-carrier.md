---
pr: 1869
kind: added
title: App clients can read and edit a factory job's future steps through guarded methods
surface: [engine]
invalidates:
  - "Was true: the app protocol did not carry per-job step edits. Is true now: factory.job_step_edits_read and factory.job_step_edit carry the existing factory route, separate from repository pipeline editing."
  - "An edit requires the version the person read. Ownership, fixed and started-step restrictions remain the factory's rules. An uncertain result is read again before the person decides; no write is retried."
---

The optional `factory.job_step_edit` feature announces transport support. A
client also needs the job's edit control and a successful versioned read before
it offers an edit. Incomplete acknowledgements are uncertain; they never become
an empty successful edit. Native editor UI is separate.
