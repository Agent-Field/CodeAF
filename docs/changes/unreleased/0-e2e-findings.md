---
kind: fixed
title: AgentField factory pipelines keep completion times and live queue updates
pr: 0
surface: [factory, app, engine, chat, docs]
invalidates:
  - "Was true: a successful AgentField pipeline without a pull request could reach Done without a completion timestamp. Is true now: worker completion records the remote run's end time, or the factory's observation time, and preserves it after reload."
  - "Was true: a job filed by an AgentField trigger could reach a person gate without notifying connected queue views. Is true now: committed state, question and completion changes notify the app so Needs you refreshes immediately."
  - "Was true: watched chat JSON omitted tokens and spending. Is true now: chat --once --json shares exec accounting fields and reports this invocation after usage settles, including spending before a failure."
  - "Was true: the factory control socket had no isolated runtime-directory override. Is true now: CODEAF_FACTORY_RUNTIME_DIR selects its private runtime base, with an explained fallback for paths too long for portable Unix sockets; the unset default remains stable."
---

AgentField pipelines that return an answer remain visible in completed job lists.

Watched JSON adds `tokens: {in, out}` and `spend_usd` without changing existing
answer or stop fields. Resumed calls exclude the conversation's earlier usage.
