---
kind: fixed
title: A stopped task's kept commit is signed by the worker that did the work, never the chat model
pr: 1768
surface: [engine]
invalidates:
  - "Was true: stopping a running /task signed its kept-branch commit with the conversation's model (e.g. `Assisted-by: CodeAF (deepseek-v4-flash-latest)`), while a task that finished and landed carried the worker's model or the bare `Assisted-by: CodeAF` line. Is true now: a stopped run's kept commit is signed the same way as a landed one — by the model that did the work, or by the conversation's model when no worker was named — and never by the chat model when a different worker ran the task."
---

A run stopped after about 20 seconds kept its branch with a commit that named
the chat model rather than the worker that had actually done the work. The stop
road now applies the same worker-model override the landing road already used,
so the `Assisted-by` line matches the model `codeaf logs` and the crew line
name. A run with no named worker keeps the conversation's model, as before.
