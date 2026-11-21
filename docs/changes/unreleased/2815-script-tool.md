---
kind: added
title: A script tool that combines tool calls in one JavaScript step, off by default
pr: 2815
surface: [engine, chat, docs]
invalidates:
  - "Was true: every tool call a bash-belt task made was a step of its own on its trajectory. Is true now: a script is one step, and the calls it made are recorded as calls on that step and drawn under it on the task page and in the live view."
  - "Was true: the plan store's live step named only a step number and a command. Is true now: it also names the tool the step is running, so a running script is drawn as a script."
---

Both switches are off by default. With `CODEAF_TASK_SCRIPT=on`, a bash-belt task
worker carries `script`, whether a plan run or the chat started the task; with
`CODEAF_CHAT_SCRIPT=on`, the conversation does. A script's JavaScript calls the
other tools through the same approval gate as a direct call, and only what it
prints or returns comes back. It is not Node.js: files and commands go through
`tools.bash`. Up to 4 calls run at once; a script stops after 200 calls, 10
minutes or 16 MiB of output, and a call refused for its own arguments does not
count. Inside a script, `bash` answers its output and exit code as an object
and every other tool answers its text. A script cannot ask the person anything
or start work: `script`, `propose_subharness`, `ask`, `request_secret`,
`propose_task`, `quick_task`, `divide_work`, `team_start`, `start_chat`,
`automation` and `factory_add` are refused inside one.

A script is one step: it counts once against a task's step limit and in the
step count the worker and the task's room show. The plan writes its `bash`
calls make are recorded as the script step's writes.

Measured with `bench/scriptbench` (Sonnet 5.5, three seeds): on questions
that need a `manual` lookup per page, the chat with the tool cost 4.5x and
8.7x less for the same answers. Where `bash` can do the work in one command,
and in `bench/oneroad`'s code changes, models did not use it and it changed
nothing. The benchmark cells also now start every harness from an empty
environment and record a cell stopped at an approval prompt as blocked.
