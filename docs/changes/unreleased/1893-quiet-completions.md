---
kind: changed
title: task results stay on demand instead of filling the conversation
pr: 1893
surface: [chat, engine, docs]
invalidates:
  - "Task endings and crew receipts used to accumulate in chat and return on tab switches. Routine completions now remain in task results, while decisions and actionable stops stay visible."
  - "The sidebar used to list completed tasks and processes. It now shows active work and decisions; click Tasks for retained results or use /processes for process history and logs."
  - "Crew models, estimated and actual spend, and the stronger retry action used to live in a separate transcript receipt. They now live with the selected task result, including restored routed runs."
  - "Runtime completion notes used to replay as extra conversation rows. New receipts carry their presentation audience through compaction and reopen; exact standalone older receipts are projected quietly without rewriting their journal."
---
