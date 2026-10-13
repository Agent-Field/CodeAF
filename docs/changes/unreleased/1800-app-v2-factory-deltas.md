---
kind: changed
title: Factory Contract 2 completes the app deltas and hand-off wire
pr: 1800
surface: [chat, engine, remote, docs]
invalidates:
  - "Loopback always granted factory access and operator authority. An app-hosted factory now requires its launch bearer token on every person route, including about, with served_by app and access token."
  - "Needs-you groups still used approval_start, spending_limit and out_of_attempts and omitted failures and PR attention. Contract 2 uses approve_start, budget and out_of_rounds and groups failed, changes_requested, conflict and runner_offline."
  - "POST /v1/jobs accepted only terminal-style words and autostart. It now persists hand-off source and originating agent refs, assignee and now/queued/backlog start choices, with validation before creation and the pipeline budget at start."
  - "Typed runner and Activity events existed but job state changes had only generic ticks. Durable job.state events now report creation and every projected state or reason change, including in-place PR review and conflict edits."
  - "Change assignee could assign an unassigned job and Assign to me could repeat on your own job. Reassignment now requires the current assignee, Assign to me requires unassigned work, and GitHub failure keeps the stored person."
---

Reuses jobs' server-side five-second approval hold/Undo, personal dismissal,
priority/set_by and Nudge, and people's persistent asleep runner state plus
runner/Activity producers. App factory mounting, hand-off cards and power
forwarding remain A14. Stored question kinds and numeric runner protocol ids
retain their bytes; the app consumes Contract 2 without its own mapping.
