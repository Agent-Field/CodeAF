---
kind: fixed
title: Budget refusals name the limit that stopped the conversation
pr: 1671
surface: [chat, engine, docs]
invalidates:
  - "A conversation-limit refusal pointed at /budget, whose amount form changes only the daily allowance. It now points at /budget conversation, while daily refusals point at /budget day. Raising the daily limit does not release the separate conversation cap."
  - "The manual said a message refused at a spending limit stays in the message box. The box is cleared on enter, as it always was; the pages now say that `↑` brings the text back to send again."
---

The manual explains how to raise the running conversation's limit and retry the
refused message. A regression reproduces the separate daily and conversation
limits and verifies that the same live agent can continue after its cap changes.
The daily-budget tip now names its scope across all conversations, and a separate
conversation-budget tip teaches `/budget conversation`. Each tip retires only
when its own budget scope is used.
The manual also explains that daily and conversation limits both apply, with
examples of each blocking a new turn and how to recover when both are reached.
