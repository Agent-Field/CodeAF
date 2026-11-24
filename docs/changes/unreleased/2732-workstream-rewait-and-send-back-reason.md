---
kind: changed
title: a done task waits again when what it rests on reopens, and Send back needs a reason
pr: 2732
surface: [engine, factory]
invalidates:
  - "Was true: reopening an organization workstream task (Send back, an untick) made only its unfinished dependants wait again; a done one stayed Done (factory.WaitAgain moved only a running task). Is true now: every task that waits on it waits again, a done one included: it goes back to Up next with `Waiting again: <title> was reopened`, and what waits on THAT one reopens in turn. A Factory task still keeps its job's state."
  - "Was true: a person could Send back a done task with no reason, and the doer's Inbox said only `Sent back by priya`. Is true now: the server refuses it with `say why it goes back`. The owner unticking their own person task is not a send back and needs no reason."
---
