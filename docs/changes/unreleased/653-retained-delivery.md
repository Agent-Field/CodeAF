---
kind: fixed
title: Keep retained task work distinct from delivery to the requested workspace
pr: 653
surface: [chat]
invalidates:
  - "An unattended conversation could say what was asked is done when a finished task's branch was kept and the requested workspace was unchanged. Kept changed work is now an unmet delivery item unless the frozen original request explicitly accepts a branch or an actual report."
  - "A child merged into its parent could count as a landing for the root conversation. Only a task whose persisted parent names the current session supplies that session's landing witness."
---

The existing acceptance call records workspace, branch, or report delivery. Branch
and report exceptions require a verbatim quote from the original request; missing,
invalid and stale declarations cannot relax delivery. The destination freezes with
the acceptance and is journaled. Reopening a session does not turn an old receipt
into authority for a new ask. No extra inference call is introduced.

The task's merge policy is unchanged. Completion reads retained branch content
against the requested workspace so an explicit later integration can close the
gap. Shared-workspace work remains valid. A report exception requires an actual
produced answer, and a worker's completion claim cannot revise the destination.
