---
kind: fixed
title: Desktop app helpers use each cell chat's own folder
pr: 0
surface: [engine, docs]
invalidates:
  - Desktop helpers treated a cell journal's .cell directory as its conversation folder; they now resolve the owning chat folder while preserving legacy and flat-file paths.
  - Earlier desktop builds saved cell chat receipts beside the journal; those receipts remain readable and merge by entry id into the chat folder on the next new receipt, while the old file stays intact.
---

Valid job logs in the chat's logs/jobs folder can be read again. Media scope,
deliverables, task working copies, presence and close recovery use the chat's
own roots. App receipts, archive marks, blank-chat cleanup and crew existence
checks use that same folder. Conversation identity and wire fields are unchanged.
Existing cell receipts stay in history after upgrading, including when receipts
already exist in both locations; writing again keeps one receipt per entry id.
Lazy recovery, task branch listings and confirmed branch lands still read task
and process records and task deletion marks from the cell's existing truth files.
