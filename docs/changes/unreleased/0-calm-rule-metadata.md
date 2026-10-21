---
kind: added
title: saved permission rules record origin and automatic usage with five-second Undo
pr: 0
surface: [engine, chat, docs]
invalidates:
  - "Always rules formerly stored only their permission text. App and terminal answers now save their conversation name, folder label and answer date in a rule-id sidecar, preserving the settings text format and accepting old files without metadata. Rule origin does not narrow its scope."
  - "Automatic rule allows formerly had no use history. They now count uses and last-use time, merge one-second batches across processes and flush at session settlement and close. rules.list includes readable text, origin, uses and last_used sorted most recently used first; unknown origins remain unknown."
  - "rules.remove formerly had no Undo. rules.restore takes the same id within five seconds, restores metadata and priority, and refuses expiry or replacement. Both refresh open gates without changing posture. Automatic receipt entries now retain structured rule details alongside their existing text and an origin/count explanation."
---

The sidecar is rule-metadata.json beside the profile settings. Older readers
still read the original tools.approval and tools.bashPatterns rows. A hard kill
can lose the pending one-second usage batch. Repository-scoped rules remain
undecided and are not introduced.
