---
kind: changed
title: factory jobs use the v2 model and server-side approval Undo
pr: 1800
surface: [chat, engine, docs]
invalidates:
  - "The factory reported contract 1 and numeric job ids. Contract 2 reports human ids such as api#1498, keeps document_id for binary clients, and accepts either in job routes."
  - "A published PR immediately appeared Done and Drop was a sixth state. Jobs now have five states, keep open published PRs Running, and report merged, closed, dropped or answered in done_how."
  - "Approval invoked the runner immediately and dismissal was a state. Store-backed approvals now wait five seconds with server-side Undo and restart recovery; Dismiss is personal, clears on GitHub activity, and is searchable with is:dismissed."
  - "Priority and size came from the AI read and a job had no pipeline default budget. The v2 job fields use person-set priority or label defaults, size labels XS through XL, and the pipeline's default_budget with an assignee-only raise."
---

This lane lands on app/v2. It adds Run, Start now, Stop Undo, final Drop with
seven-day branch retention and Activity deletion, assignment, Unassign,
Nudge and closed-PR Run again. It derives Waiting for and changed-file overlap,
and retains the exact first-answer-wins collision copy. Existing runner ids,
old verbs and compatibility fields remain; new clients use Activity, approval,
rounds and runner. Model and HTTP transitions use deterministic clocks.
