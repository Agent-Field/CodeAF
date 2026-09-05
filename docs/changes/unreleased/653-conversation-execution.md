---
kind: changed
title: Conversation work keeps attributable direction and results while its execution model is measured
pr: 653
surface: [chat, engine, docs]
invalidates:
  - "A writing turn could delegate a wait for its own command to a task with no access to that command. The handoff call can now explicitly await live owned operations, with unread messages and changed requests invalidating that decision; the original completion notice still returns to its owner."
  - "A fork required every hand to claim writable paths, including readers. An explicit empty scope now creates a reading hand without edit or write tools; missing or null scope is still refused."
  - "Task coordination text could be confused with a person's revision. The conversation runtime now carries attributed directions and effective assignment revisions, while agent coordination cannot rewrite user authority."
  - "A compact task card was also the result available to the conversation. Full results now remain separately retrievable, with bounded excerpts for presentation."
  - "A running turn that said its request was finished could only stop its own handover if the second reader had drawn a done shape at the same moment, so an absent, failed or partless reading was spent as though it had said work remained. Only a reading that names independent parts still to do now refuses the claim; the accepted drop is charged once per request, agreement included, and a direction typed mid-turn is a new request with the claim unspent. A handover whose owning context is already cancelled admits nothing. A sketch naming parts that have already been done still moves the work."
  - "Correct artifacts in an initial benchmark were liable to be treated as success despite a timeout. Frozen conversation campaigns now retain deadline failures and unknown bills, and the current calibration does not establish Pareto performance."
---

This umbrella draft includes the previously local conversation-runtime wave and
the next execution plan. The plan distinguishes direct work, short forks and
durable tasks; its proposed consolidation is not yet implemented in full.
See docs/design/conversation-runtime/EXECUTION-PLAN.md and VALIDATION.md for
scope, evidence and limitations. Keep the PR draft while that work continues.
