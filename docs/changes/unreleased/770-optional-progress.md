---
kind: changed
title: useful tool calls no longer wait for a mandatory progress note
pr: 770
surface: [chat, engine]
invalidates:
  - "The loop added narration demands after six and twelve quiet tool batches, then held calls and could stop the turn for omitting prose. Quiet useful calls now run normally; progress notes remain optional and genuine repetition guards still apply."
  - "The system prompt required a visible note before each tool batch and claimed reasoning between steps was lost. Provider continuation data is retained independently of narration; neither a plan nor a progress note is a prerequisite for tool execution."
  - "The process-rule registry existed to enforce its only rule, write-your-notes. That rule and its runtime registry/state are removed; historical notes endings and their stopped classification remain readable."
---

This is an isolated experiment. The real Submit regression crosses the former hold
boundary and executes all eighteen useful calls instead of stopping at twelve.
Optional visible notes and actual repeated-call/error/invalid-argument behavior
remain covered. No benchmark improvement is claimed, and the draft stays unmerged
until a matched run supports it.
