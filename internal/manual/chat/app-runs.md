# App runs

## Start an app run and see its steps

A desktop app can start a run from a screen or command using its saved run definition. Its progress updates as each step runs. A run can create or update records the app has permission to write, ask the app agent for an answer, call an allowed connection, wait for a duration, or repeat steps up to a limit. Shell commands and repository workspaces are unavailable in app runs.

## What happens to scheduled runs when codeaf is closed?

Runs advance while codeaf is running. Completed steps, approval requests and wait deadlines are saved. When you reopen codeaf, a schedule with missed slots starts one run marked “caught up”, then returns to its regular schedule. A send interrupted before its result was saved is shown as incomplete so the same message cannot be sent twice.

## Start an app run when a record changes

Apps can start a run when a record is created, updated or deleted and its
trigger condition matches. codeaf reads committed record changes from its
saved queue. Each matching trigger creates one run for the change, including
when codeaf restarts before it finishes reading the queue.

## Approve, decline or raise the budget for an app run

The Inbox shows “Approval needed: send” before a run sends mail, posts a message or makes another outbound connection call. Approve to let that step proceed or decline to end the run. When a run reaches its spending limit, the Inbox shows “Per-run budget reached”. Raise the limit in dollars to continue or choose Stop.
