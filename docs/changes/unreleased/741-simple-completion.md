---
kind: changed
title: ordinary completion follows the main model and actual execution results
pr: 741
surface: [engine, chat, docs]
invalidates:
  - "Ordinary answers could buy route classifiers and completion readers, and a missing tracked file change could reopen valid work. The main model now completes ordinary answers without these extra approval calls; actual unfinished task state and explicitly declared checks remain available."
  - "Fixed write counts, tool-round counts and wall shares could move a conversation into a new task and pre-submit a division sketch. Those automatic paths are removed; the model deliberately chooses the existing task, fork and division tools when available."
  - "Request keywords could refuse an explicit task proposal, and one prompt fragment still required launching before work. Tool availability and actual dependencies now govern proposals, and conditional prompts teach deliberate delegation only on belts that carry it."
  - "Completion could delete a session-created file outside the workspace as presumed scratch. Completion no longer guesses ownership from location or removes those files; explicit cleanup and episode recovery remain separate."
  - "Tool-call IDs were treated as unique across a transcript. Reused IDs now resolve within their assistant batch through model context, admission evidence, runtime outcomes, compaction and restored history."
  - "Task checks could receive a clipped or stale conclusion and assume changes were staged. They now receive the bounded current conclusion, actual directory being checked and rollback context; selected admission evidence can point to its existing full result file."
  - "After its last repetition warning the observer stopped reading results because the turn used to be forcibly ended. It now keeps observing progress while bounding repeated advice, so new work can restore an opportunity to explain a later error."
---

This includes the live context-fidelity repairs from #717 and supersedes its
completion-reader digest changes because that reader no longer exists. It also
supersedes the automatic handoff assumptions documented in #622, #635 and #664.

CRITICAL comments and behavioral regressions protect the context and completion
invariants. Provider generation defaults are unchanged. Real-task quality, cost
and time remain a benchmark question, not a conclusion of the unit tests.
