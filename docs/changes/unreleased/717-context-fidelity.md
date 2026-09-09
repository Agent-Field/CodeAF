---
kind: fixed
title: tool rounds and worker handoffs retain their own evidence
pr: 717
surface: [engine, chat, docs]
invalidates:
  - "Tool-call IDs were treated as unique across a transcript. Reused IDs now resolve within their assistant batch, and runtime outcomes and consumed reads follow the actual call occurrence."
  - "Task checks could receive a clipped or stale conclusion and assume changes were staged. They now receive the existing bounded current conclusion, the actual directory being checked, and explicit rollback context."
  - "Admission evidence pointed only to a journal lookup by call ID. Selected evidence now includes an existing full-result file when available, while old records retain a journal fallback that requires matching arguments."
  - "A completed small write could reach the completion reader as only a path and byte count. Its whole input now accompanies its own result within the existing digest budget."
  - "Restored tool rows, change explanations, and rewind boundaries also joined repeated IDs globally. They now use the same batch-local result association as model context. Old internal continuation messages remain hidden after the prompt wording changes."
  - "A full-request handoff could omit checks already declared by its current steward. Empty declarations now inherit those existing typed checks only when the current request matches and no work remains with the steward."
---

This repairs context assembly without adding model calls or a new decision layer.
It does not infer executable checks from prose or change model generation defaults.
