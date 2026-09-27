---
kind: fixed
title: Home tasks and standing work retain their project, approval, and execution context
pr: 1632
surface: [chat, engine]
---

Home task drafts bind to the project currently displayed. Standing work respects
project scope, shows its allowance and activity, and hands off an approved
one-time run without accidentally creating a recurring schedule. Explicit branch
isolation preserves unfinished changes in the worker copy, while ordinary
one-time work continues in place. Standing timers have a single owner and
approval receipts reflect whether scheduling succeeded. Empty extracted briefs
are rejected before execution.
