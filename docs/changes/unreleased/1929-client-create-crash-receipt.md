---
kind: fixed
title: Keep the created chat in an interrupted paired-client receipt
pr: 1929
surface: [engine, chat]
invalidates:
  - "A process death after agents.create returned could leave a started receipt with no chat id or chat line. The create handoff now reserves the chat identity and path before its folder exists, and records its line before the first turn starts."
---

The ordinary appwire create request supports an in-process before-start hook;
wire callers cannot supply executable hooks. Recovery never repeats creation.

Recovery repairs the chat line after an immediate post-mint process death. A
reservation whose folder was never created does not invent a chat.
