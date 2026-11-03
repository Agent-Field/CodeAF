---
kind: changed
title: codeaf asks before learning from chats and jobs
pr: 2266
surface: [engine, chat]
invalidates:
  - "Fresh homes automatically armed capture, while memory.enabled governed only noticing and defaulted on."
---

Memory is off by default. One live setting controls automatic learning from
chats, history and jobs, with its extra model-call cost explained in Settings.
Explicit saved on and off choices are kept. Terminal Build saves on. Saved
knowledge, kept lines and /remember remain available while off; reading saved
knowledge costs nothing. A call already in flight finishes and is billed.

Homes automatically armed by the old empty-history path, without a saved memory
choice, now read off. No saved knowledge is deleted. Older homes that used
terminal Build without a saved choice also read off, because those two cases
cannot be distinguished. Turn memory on in Settings or use Build to opt in.
