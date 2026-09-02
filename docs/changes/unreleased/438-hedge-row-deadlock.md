---
kind: fixed
title: concurrent hedge arms no longer freeze while recording their call rows
pr: 438
# Optional. Which part of the repository this touches, so a reader can skip it.
# One or more of: build, chat, docs, engine, remote, resident
surface: [engine]
# Optional, and the reason this file exists. Every statement that WAS true and is
# not any more, written whole: what it was, and what it is now. A model reading
# this has to be able to check its own memory against it, so "branch names" is
# useless and "the trunk was chat-v3-task and no longer exists; work goes to dev"
# is the whole point. Leave the list empty if nothing anybody believed changed.
invalidates:
  - "A hedge with two arms could freeze forever when both calls ended together. Concurrent arm rows now record without taking the race and watch locks in opposite orders."
---
