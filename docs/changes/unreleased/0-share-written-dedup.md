---
kind: fixed
title: Skip unchanged authored files when sharing deliverables
pr: 0
surface: [engine, chat]
invalidates:
  - "Sharing a file already recorded by write or edit created a second deliverable and emitted another update. Unchanged authored bytes now produce an already-recorded explanation without a new row, event or charge; other files in the call still share normally."
  - "A path alone does not prove a file is already recorded: sharing bytes changed outside write/edit records the new version. Content hashes decide, and the durable receipt preserves skipped files across retries with the same operation_id."
---
