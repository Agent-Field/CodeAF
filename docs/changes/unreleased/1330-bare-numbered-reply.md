---
kind: fixed
title: Number-only assistant replies remain visible
pr: 1330
# Optional. Which part of the repository this touches, so a reader can skip it.
# One or more of: build, chat, docs, engine, remote, resident
surface: [chat]
# Optional, and the reason this file exists. Every statement that WAS true and is
# not any more, written whole: what it was, and what it is now. A model reading
# this has to be able to check its own memory against it, so "branch names" is
# useless and "the trunk was chat-v3-task and no longer exists; work goes to dev"
# is the whole point. Leave the list empty if nothing anybody believed changed.
invalidates: []
---

The Markdown renderer now keeps an empty ordered-list marker in the transcript,
so replies such as `32.` are not silently dropped.
