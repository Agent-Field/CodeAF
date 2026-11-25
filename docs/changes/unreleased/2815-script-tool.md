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
and every other tool answers its text. A script can call only `bash`, `read`,
`ls`, `grep`, `find`, `read_document`, `manual`, `recall`, `web_search`
and `web_fetch`; every other tool is refused inside one, so a script cannot
edit files, ask the person anything or start work. A script reached through a
saved program meets the same approval rules, and a script with no turn to check
its calls against does not run. A quick task's worker never carries `script`.
A script's memory is not limited. A script that ends without waiting for a
call says how many it left; those still running are cancelled. Inside a script,
`read` always returns the file, never the pointer to bytes the conversation
already holds, and no result carries the background-job footer, which is kept
for the model's next result. The app and the remote surface draw a script as
one call, as the chat does, and the chat draws a script's calls straight under
it even when the model ran other calls beside it. A failing check run inside a
script counts as a check the task ran, a script's count of calls includes only
calls that ran, and words in a script's source are not read as plan writes. A factory item's
log captions a script as one call, and a headless turn prints a script's calls
indented under it.

A script is one step: it counts once against a task's step limit and in the
step count the worker and the task's room show. The plan writes its `bash`
calls make are recorded as the script step's writes. A script that reads counts
as progress, so a task exploring through scripts is not stopped for making no
progress. In a conversation, a script's `bash` sees up to 1 MiB of output, as a
task's does, although the conversation's own `bash` keeps only the tail, and a
reopened conversation still draws a script's calls under it.

Measured with `bench/scriptbench` (Sonnet 5.5, three seeds): on questions
that need a `manual` lookup per page, the chat with the tool cost 4.5x and
8.7x less for the same answers. Where `bash` can do the work in one command,
and in `bench/oneroad`'s code changes, models did not use it and it changed
nothing. The benchmark cells also now start every harness from an empty
environment and record a cell stopped at an approval prompt as blocked.
