---
kind: fixed
title: task proposal countdown properly handles keypress and avoids starting declined task
pr: 1547
surface: [chat]
invalidates:
  - "During task proposal countdowns any keypress was swallowed and hitting enter immediately started a task that the user intended to decline. Enter and keypress handling now respects the decline status during countdown."
---
