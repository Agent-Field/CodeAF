---
kind: changed
title: Crew wakes are kept until delivered, managers link crew work, and the Library batches a sitting
pr: 2794
surface: [chat, app, engine]
invalidates:
  - "A crew wake that could not be delivered (a manager moved to another window) was said once as `could not wake ◆` and dropped; it is now queued and retried (15s, doubling to 5m) until the conversation runs, and the desktop app reopens its own moved agents to take it (internal/session/team_rousequeue.go, cmd/codeaf/appteamresume.go)."
  - "`crews.link` was wire-only, so only the desk could link a crew's work; a manager in the app or an engine now has `team_link`, which writes crews.json under a cross-process lock and emits `crew.item`."
  - "A fenced run (a crew member, `--once`, exec) could not write the desktop app's canvas folder; it may now, when that folder already exists ($WORKAF_CANVAS_ROOT, else the platform default)."
  - "A run waiting in the workstream queue across a server restart lost D1's answered flag; task_runs now has an `answered` column and the restart sweep passes it on."
  - "The Library compiled each chat the moment it went quiet, one pass each; while any chat is still being written in, quiet chats now wait for it (at most buildBatchMax, 10 minutes) so a sitting is one pass. A scripted E6b replay: 12 calls before, 2 after, 0 in the idle hour after."
---

Follow-ups from the crews build (CREWS.md "What this build cannot do") and
E6b. A Personal workstream's decision-check Ask is yours; the card now names
the decision's maker, and the refusal tells the agent to name only who was
asked.
