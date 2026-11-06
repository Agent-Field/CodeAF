---
kind: changed
title: An idle desktop app lets its saving helper exit instead of keeping it awake
pr: 2466
surface: [app, engine]
invalidates:
  - "The desktop engine sent its saving helpers a health request every 20 seconds for as long as it ran, so a quiet app kept the same helper alive. It now sends them only while a reply is running, a save is held or running, or a seal awaits its engine sync; an idle app's helper exits after about one minute and the next save starts it again."
---

A kept-alive `furrow serve` woke about 28 times a second on an otherwise idle
app. Starting it again for the next save takes about 4 ms on an Apple silicon
Mac, plus the first open of the store. Touches still never start or restart a
helper, and terminal and command-line helper lifetimes are unchanged.
