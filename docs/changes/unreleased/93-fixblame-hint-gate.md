---
kind: fixed
title: a hint on a failed tool result is a command with evidence behind it, or it is silence
pr: 93
surface: [engine, docs]
invalidates:
  - "An error's hint could quote whatever ran next, including a regex fragment — a measured store offered `/\\/+$` as the cure for grep's own \"Path not found\". A suggestion must now be a command whose first word names a program this machine has."
  - "A pairing seen once was offered. It is not: an error and the command that followed it have to have been watched together twice, or the patch has to have been offered, taken, and seen to clear the error."
  - "`bash`, `grep` and `find` each kept an error→fix lane. Only `bash` does. A grep or find call's defining argument is the pattern it searched for, and a pattern is not a remedy."
---

The wording of the two lines is untouched; what changed is how often either is
said at all. The `worked` counter was already wired end to end — `worked: 0` on
a real store was the loop reporting honestly that nothing it offered was ever
taken, which is what an offer of a regex earns.
