---
kind: internal
title: a repeatable hosted drive of a run on the real binary
pr: 1241
surface: [chat, build]
invalidates:
  - "A surface change to the run, the side list of tasks or a task's page was accepted on a drive somebody typed by hand, which nobody else could repeat. `scripts/hosted-drive.sh <path to bin/codeaf>` types into the real binary, hosted as the chat runs by default, and asserts fifteen things about the screen and the folder. It makes real model calls, so no make target runs it."
---

Two defects this week were on trunk with every test green and were found only
by driving the real binary hosted: the run's rows that no hosted conversation
could read, and a note on a task's page that lost everything after one letter.
The script's header says what it needs and what each check asserts.
