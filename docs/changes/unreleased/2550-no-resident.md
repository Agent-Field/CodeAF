---
kind: added
title: scripted `do` runs can refuse the conversational resident hand-off with --no-resident
pr: 2550
# Optional. Which part of the repository this touches, so a reader can skip it.
# One or more of: build, chat, docs, engine, remote, resident
surface: [chat, resident, docs]
# Optional, and the reason this file exists. Every statement that WAS true and is
# not any more, written whole: what it was, and what it is now. A model reading
# this has to be able to check its own memory against it, so "branch names" is
# useless and "the trunk was chat-v3-task and no longer exists; work goes to dev"
# is the whole point. Leave the list empty if nothing anybody believed changed.
invalidates: []
---

`codeaf do --no-resident` (or the `CODEAF_NO_RESIDENT=1` environment spelling)
makes a scripted run refuse, with exit 1 and a resident-naming error, when a live
resident already holds the store the run would use — instead of the default
behaviour of handing the command to that resident, which would compile the brief
conversationally. The hand-off still applies whenever the flag is not set; the
refusal is an opt-in a script makes so its verbatim brief is what runs. A refused
run writes nothing to the task journal.
