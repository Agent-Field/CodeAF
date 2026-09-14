---
kind: fixed
title: PERF.md's prefix budget table reads the caps the gate enforces
pr: 1033
# Optional. Which part of the repository this touches, so a reader can skip it.
# One or more of: build, chat, docs, engine, remote, resident
surface: [docs]
# Optional, and the reason this file exists. Every statement that WAS true and is
# not any more, written whole: what it was, and what it is now. A model reading
# this has to be able to check its own memory against it, so "branch names" is
# useless and "the trunk was chat-v3-task and no longer exists; work goes to dev"
# is the whole point. Leave the list empty if nothing anybody believed changed.
invalidates:
  - "PERF.md's prefix budget table said 53,141 (full) and 44,916 (lean), over the targets by 5,141 and 13,416; the gate has enforced 53,291 and 45,066 since the waivers moved +150, and the table now says so."
---

The waivers were raised in code with the reason beside them and the table was never opened. A cap that changes changes the doc in the same commit; this is that commit, late.
