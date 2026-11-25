---
kind: added
title: Run app automations with saved progress and Inbox approvals
pr: 2946
surface: [app, engine]
invalidates: []
---

Start a run manually, on a schedule, or after a record change. Runs save their progress, timers, and retries across restarts. Outbound actions require Inbox approval.

Record triggers read the committed outbox and save their receipts with their runs. Declared record writes use flow permissions and can replace a person's value when record rules allow.
