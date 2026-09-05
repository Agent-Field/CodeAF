---
kind: changed
title: Conversation work keeps attributable direction and results while its execution model is measured
pr: 653
surface: [chat, engine, docs]
invalidates:
  - "A worker's deadline was only noticed after a tool finished. The runner now watches the current allowance while commands, parked jobs and model requests are silent, retaining existing progress renewal and cancellation rules."
  - "Cross-compiling with GOOS and GOARCH also cross-compiled the manual generator, which could not execute on the build host. Generation now runs for the host while the final binary still targets the requested platform."
  - "A temporary task-transcript read failure could be shown as absent history, and a finished task's page would stop trying. The page now retains its last successful reading, shows the failed read and retries through the same reader used to open it."
  - "The default local engine window could refuse a task click because it had no remote hostname. Task conversations now open through the available engine capability in either location, and transcript refreshes preserve expanded entries and accepted corrections waiting to be journaled."
  - "Every specialist tool schema rode with each chat request. Media, settings and saved-procedure tools now load in one step through `load_capability`, within the same turn; core tools and task-worker tool sets remain direct. Original tool names, permissions and execution paths are retained. Reopening restores load calls still present in the saved transcript."
  - "A returning window could miss an unanswered question until the old connection detached. Questions are now offered per window, and the initial welcome and event replay deliver each held question once."
  - "A writing turn could delegate a wait for its own command to a task with no access to that command. The handoff call can now explicitly await live owned operations, with unread messages and changed requests invalidating that decision; the original completion notice still returns to its owner."
  - "A fork required every hand to claim writable paths, including readers. An explicit empty scope now creates a reading hand without edit or write tools; missing or null scope is still refused."
  - "Task coordination text could be confused with a person's revision. The conversation runtime now carries attributed directions and effective assignment revisions, while agent coordination cannot rewrite user authority."
  - "A compact task card was also the result available to the conversation. Full results now remain separately retrievable, with bounded excerpts for presentation."
  - "A running turn that said its request was finished could only stop its own handover if the second reader had drawn a done shape at the same moment, so an absent, failed or partless reading was spent as though it had said work remained. Only a reading that names independent parts still to do now refuses the claim; the accepted drop is charged once per request, agreement included, and a direction typed mid-turn is a new request with the claim unspent. A handover cancelled before or during its preparation admits nothing. A sketch naming parts that have already been done still moves the work."
  - "Correct artifacts in an initial benchmark were liable to be treated as success despite a timeout. Frozen conversation campaigns now retain deadline failures and unknown bills, and the current calibration does not establish Pareto performance."
---

This umbrella draft includes the previously local conversation-runtime wave and
the next execution plan. The plan distinguishes direct work, short forks and
durable tasks; its proposed consolidation is not yet implemented in full.
See docs/design/conversation-runtime/EXECUTION-PLAN.md and VALIDATION.md for
scope, evidence and limitations. Keep the PR draft while that work continues.

The reconnect fixture now allows the replacement window to arrive before the
server processes the old socket's EOF. It checks retained conversation identity
and eventual standing-subscription cleanup instead of assuming an arrival-time
attachment count of zero.
