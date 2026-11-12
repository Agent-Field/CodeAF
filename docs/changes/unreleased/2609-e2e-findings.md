---
kind: fixed
title: AgentField pipelines hand off pull requests, report usage and keep queue state current
pr: 2609
surface: [factory, app, engine, chat, docs]
invalidates:
  - "Was true: a successful AgentField pipeline without a pull request could reach Done without a completion timestamp. Is true now: worker completion records the remote run's end time, or the factory's observation time, and preserves it after reload."
  - "Was true: a job filed by an AgentField trigger could reach a person gate without notifying connected queue views. Is true now: committed state, question and completion changes notify the app so Needs you refreshes immediately."
  - "Was true: watched chat JSON omitted tokens and spending. Is true now: chat --once --json shares exec accounting fields and reports this invocation after usage settles, including spending before a failure."
  - "Was true: the factory control socket had no isolated runtime-directory override. Is true now: CODEAF_FACTORY_RUNTIME_DIR selects its private runtime base, with an explained fallback for paths too long for portable Unix sockets; the unset default remains stable."
  - "Was true: a build-and-review pipeline rejected {pr_url} and required a separate job with a literal link. Is true now: downstream AgentField steps can explicitly bind the opened PR, with validation before dispatch when earlier work or its PR is missing."
  - "Was true: a later AgentField step that returned a pull request link said it opened that pull request, even when an earlier step supplied it. Is true now: that later step says finished, including after restart, and only the opening step says finished and opened."
---

AgentField pipelines that return an answer remain visible in completed job lists.

Watched JSON adds `tokens: {in, out}` and `spend_usd` without changing existing
answer or stop fields. Resumed calls exclude the conversation's earlier usage.

The factory library supports a build followed by review with `{pr_url}`.
The existing first-opened-PR rule remains; default inputs do not gain fields.
A later review step says `finished`; the opening step keeps
`finished and opened <pull request link>`.
Set `CODEAF_FACTORY_RUNTIME_DIR` for an isolated private control socket.

Runner socket tests use short, temporary runtime bases on Unix, including for
the default and long-path fallback, so macOS's long `TMPDIR` cannot redirect
tests into a live runner directory. Production socket paths are unchanged.
