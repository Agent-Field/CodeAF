---
kind: added
title: codeaf app steers, queues, docks questions and treats tasks as chats
pr: 0
surface: [engine]
invalidates:
  - "An app turn's answer entry carried the model's thinking, and thinking streamed as turn.delta. Thinking is now turn.thinking and turn.thinking_done only, never kept, and words before a tool call are an update entry."
  - "A stopped app turn ended with a receipt and a provider failure with an error entry. Both are now footer entries (footer_kind stopped_by_you, provider_failed)."
  - "agents.entries listed a compacted conversation's rewritten window twice. It now lists the earlier conversation, a summary mark, and the live transcript beneath the compaction's floor."
  - "An agent could start any number of background processes. The seventeenth is now refused with 16 processes are running for this agent."
---

New methods: `agents.steer`, `agents.queue`, `queue.edit` / `remove` / `move`,
`asks.hold`, `files.find`, `files.stat`, `attachments.add`, `tasks.pause` /
`resume` / `retry` / `brief` / `entries`; `asks.answer` takes `why`,
`batch_item`, `later` and `you_decide`; `agents.send` takes `attachments`;
`tasks.note` answers its entry. Events: `turn.thinking`,
`turn.thinking_done`, `turn.retrying`, `queue.changed`, `note.changed`. Entries:
`update`, `steer`, `note`, `summary`, `footer`. Asks carry source, reason,
evidence, choices with the suggestion, clock, batch, the tasks they hold up and
say_why. Tasks carry paused, waits_on, holding_up, model, steps, cost, clock,
started_by, instructions and their own tasks' counts. A tool row's kind is
`tool_kind`. Output longer than 512 KB keeps its last 512 KB.
