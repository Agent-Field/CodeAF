---
kind: internal
title: The triage one-item-a-tick test counts reads on the worker goroutine and no longer races the clock
pr: 2205
# Optional. Which part of the repository this touches, so a reader can skip it.
# One or more of: app, build, chat, docs, engine, factory, remote, resident
surface: []
# Optional, and the reason this file exists. Every statement that WAS true and is
# not any more, written whole: what it was, and what it is now. A model reading
# this has to be able to check its own memory against it, so "branch names" is
# useless and "the trunk was chat-v3-task and no longer exists; work goes to dev"
# is the whole point. Leave the list empty if nothing anybody believed changed.
invalidates: []
---
