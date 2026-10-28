---
kind: changed
title: Idle chats share one team watcher
pr: 0
surface: [engine]
---

Previously, every open conversation kept a one-second team timer, including
conversations in no team. Now the process shares one watcher and leaves
unaffiliated conversations alone until membership changes. Team directives,
replies, stops and wrap-up bounds keep their existing cadence and safety gates.
