---
kind: added
title: codeaf app drives tasks, quick tasks, processes, long replies and sleep
pr: 0
surface: [engine]
invalidates:
  - "A reply running long in an app conversation was moved to a task by the checkpoints on their own. In the app it now stays a reply: turn.long is said at 180 seconds and only agents.move_to_task moves it. The terminal is unchanged."
  - "A quick task could run any shell command. It now asks to become a full task before a shell call (tests included), media creation or its 31st file, and a finished quick task can be undone when none of its files changed since."
  - "App task ids were the conversation's own number. They are now <agent>:<n>, and process ids <agent>:p<n>, because two agents both have a task 1."
  - "tasks.list read only the project's task index and listed nothing for a terminal conversation the app was not holding. It now reads the conversation's own tasks.json, finished tasks included."
  - "models.list named each model's provider by its id's vendor prefix and could return only one service's models when the default catalog was still loading. Provider is now the service the picker groups the model under, with provider_name, and the default catalog is waited for."
---

New methods: `tasks.get`, `tasks.start` / `hold` / `decline` / `typing` /
`decide` / `note` / `stop`, `quick.undo`, `quick.promote`, `processes.list` /
`output` / `follow` / `unfollow` / `stop` / `rerun`, `agents.move_to_task`,
`power.sleep` / `power.wake`. Tasks carry `proposed`, `clock_until`,
`irreversible`, `your_call`, `result`, `files`, `spent_usd` and the live
`line`; listed entries carry `turn` and `client_id` for messages this launch
sent. Events: `quick.changed`, `process.changed`, `process.output`, `turn.long`.

The work log's detail: `turn.tool` and tool entries carry `kind`, `started_at`,
`ended_at`, `batch`, `exit_code`, `retried_of`, `files`, `connector`, `task`,
`ask`, `input`, `tail` and `lines`, and a call running past five seconds is
said again at most every half second with a fresh tail. `tools.output` reads a
call's whole output by line, after the turn too, and `tools.follow` /
`tools.unfollow` say a running call's new lines as `tool.output` events. The
engine sends a model's shell output as it arrives when the launch asks for it
(`liveToolOutput`), and tool begin and end events carry the batch, the account
and the exit code.
