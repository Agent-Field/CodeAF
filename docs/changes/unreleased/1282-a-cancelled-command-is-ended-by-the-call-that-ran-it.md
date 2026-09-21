---
kind: fixed
title: a cancelled command is ended by the call that ran it
pr: 1282
surface: [engine, chat]
invalidates:
  - "A command cancelled in the same instant it started could go on running after its call answered `Command aborted`. The call's wait returned on the cancellation and trusted a second goroutine to have ended the command already; when that one had not reached its wait yet it found two reasons to wake, was handed one at random, and on the other sent nothing. The command is started as the leader of a session of its own, so it lived on under no parent, in a folder that was then removed."
  - "It was seen on the real binary: a run whose dollar limit was reached by the very call that issued a command ended `incomplete`, and the command's shell was still running minutes later under no parent. A context already done when the call is made shows it every time, which is what the new test does, two hundred times. Shells that tests of a time limit have left looping on a shared box are the same shape but were NOT reproduced in a hundred quiet runs, so this change does not claim them."
  - "The part of the call that sees the cancellation now ends the command itself, unless the command was handed on as a background job or had already exited. A stop in the middle of a command was always ended and still is."
---

Nothing a person reads changed. The manual already says a stopped command is stopped;
this makes it true of a command that had only just begun.
