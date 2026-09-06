---
kind: changed
title: Conversation work keeps attributable direction and results while its execution model is measured
pr: 653
surface: [chat, engine, docs]
invalidates:
  - The model-switch notification test now observes the synchronous receipt instead of competing with the live beat for its queue; the race reproduced on the unchanged baseline. Routing behavior is unchanged.
  - Switching directly between task views now releases the previous transcript subscription, as Escape already did; navigation does not cancel execution.
  - The narrow task shortcut now remains clickable for queued work and tasks needing attention even after the last worker stops; its phone summary prioritizes a waiting decision.
  - A conversation-and-tasks interaction prototype and product architecture are now available under docs/design/conversation-runtime; these are simulated design work, not changes to the live terminal or execution behavior.
  - Kept task branches now carry an explicit reminder to preserve the requested branch and review workflow; a completion notice is not a request to merge into main. This is model guidance, not a shell restriction.
  - 'Automatic routing carried acceptance prose but could not declare runnable checks. Its checks field now reaches whole-request tasks through the existing verification contract; changed requests and partial handoffs do not inherit those commands.'
  - 'A first-prompt wait could say it was trying another lane even when no rescue request started. It now says it is still waiting and reserves switching for an actual rescue.'
  - "Incomplete bash arguments could open a permission card even in an allow-by-default session. Invalid shell input now returns an actionable argument error to the model without execution or consent; corrected commands still pass through all existing approval rules."
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
  - "One editor served every reader of the chat box, so a sentence typed for the model was sent as steering to whichever task page was opened before enter. The composer is now owned per recipient — the conversation, each task page, each adaptive run — with its own text, caret, compact paste chips and attachment tray; opening a page, escaping, and going straight from one task to another carry nothing across, and a submit clears only the box that was sent."
  - "Only the conversation's sentence was durable, and it was read off whatever box was in front, so a line typed at a task could be written into the conversation's own crash file. Every recipient's composer is now written to a per-conversation JSON record beside the existing plain draft file, stamped with host, project and transcript, and restored to the same recipient of the same conversation; the plain file stays a compatible export and the fallback where there is no record. A line belonging to another conversation is neither shown nor deleted, and follows its own conversation into whatever window opens it next."
  - "Draft writes were unordered commands sharing one temporary path, so a save built before a submit could land after it and put the sent sentence back on disk. Saves now carry the revision the loop gave them and an overtaken save writes nothing. Failures are reported as `this draft could not be saved` instead of being swallowed, a record this build cannot read is moved aside rather than overwritten, and a reunion prunes the old record only after the new one has been written."
  - "A crash between the record and its plain export resurrected a sentence that had just been sent: the cleared record was deleted, or held no entry for the conversation's own box, and the restore fell through to the stale text file. An empty record is now a tombstone that says the box is empty, a record that speaks for this conversation is the whole answer with no fallback to the text file, and the stale export beside a record read by a reunion is cleared with it — in the same window, in a new window with a new draft filename, and with another task's line still unsent in the same record."
  - "A draft save was retired only when a newer one reached the disk, so a newer save that failed left an older queued one free to land and make spent words look current. A save now retires older ones the moment the loop builds it, and a failed write leaves the last record that worked in place."
  - "Enter sent a message whose compact paste tag had lost its document, delivering the tag as though it were the text. The conversation and a task page now both refuse with `a pasted block could not be restored · the words were not sent` and keep the whole line; missing attachments stay on the tray and are refused by the existing per-file validation at send."
  - "A conversation opened with no transcript path kept only its plain sentence, losing pasted documents and the tray on a crash. Its box is now durable under a window identity derived from the draft file; no task page's line is written under one, because a task id is only meaningful inside the graph that minted it."
  - "Restoring a draft quietly edited it: missing attachments were dropped from the tray and paste tags with no document were removed from the words. Both are now restored as they were and named — `a restored draft still names a file that is gone` and `a pasted block could not be restored · its tag is still in the draft` — and there is no size ceiling that silently keeps a large paste out of the record."
---

This umbrella draft includes the previously local conversation-runtime wave and
the next execution plan. The plan distinguishes direct work, short forks and
durable tasks; its proposed consolidation is not yet implemented in full.
See docs/design/conversation-runtime/EXECUTION-PLAN.md and VALIDATION.md for
scope, evidence and limitations. Keep the PR draft while that work continues.

The task page's footer now reports the task's state instead of the main
conversation's state and clock. Copy feedback retains priority; leaving the
task restores the main conversation's status.

Retained-branch notices for stopped, conflicted, moved and detached work now
offer inspection without directing a merge or checkout change. The requested
delivery workflow continues to govern the parent follow-up.

The reconnect fixture now allows the replacement window to arrive before the
server processes the old socket's EOF. It checks retained conversation identity
and eventual standing-subscription cleanup instead of assuming an arrival-time
attachment count of zero.
