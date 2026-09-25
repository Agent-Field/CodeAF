---
kind: fixed
title: a new task never picks up an earlier run, and every run ending is written on its record
pr: 1411
surface: [chat, engine]
invalidates:
  - "Only a person's stop wrote an ending on a run's own task. A run that reached its dollar or time limit, whose own worker failed, or whose `codeaf do --timeout` ran out stayed `running` in its store, and the next `/task` or `codeaf do` over the same store adopted it: the new request's words were dropped and the old brief ran again under the new number. Every one of those endings now writes the run's ending (the store's new `EndRoot`, which fails the run's task and cancels what was open), and a new request never adopts a store it did not start. A chat store still open (a closed conversation or a process that died) is set aside beside the new one with its open work ended as `interrupted`, readable with the earlier runs; an interrupted `codeaf do` keeps its own private folder. The run engine also refuses to run a root that already carries a different brief."
  - "Reaching the dollar limit on a worker's final receipt marked the limit and ended nothing, so the other workers in flight kept working and spending. Either road to the limit now ends every worker in flight."
  - "A `/task` started after the conversation's dollar limit was already spent might be taken to be refused before it starts, the way the next turn is. It is not: the run is handed the smallest figure above zero, because zero means no limit, and its first worker makes one paid call before the limit ends the run."
  - "A hand-off in the seconds while a run was landing joined it, and its work went into a store nothing would run again; its row settled failed with no report. It now waits for that run to be over and starts a run of its own."
  - "Closing the conversation cut the run and then landed its work and settled its row failed, while the row read back later said interrupted. Closing now lands nothing, settles nothing and writes nothing on the run's record."
  - "A note, pause, amendment or priority on a task of the run that finished last was accepted, because that run is still the live store until the next request. It now answers `that task's run has ended`."
  - "A part stopped with `x` from its page read `incomplete`, because the cancel carried no reason. It reads `stopped`, and so does what the stop took down with it."
  - "An interrupted row raised the `needs you` mark and offered `continue it` and `leave it`, and nothing could take either answer. It now raises no mark, asks nothing, draws the stuck mark on home rather than the asking one, and keeps its line: `nothing is driving it; everything it did is kept`."
  - "A hand-off that had joined a run came back after a restart saying its working copy was never written down. Only a run's own row says that now; a joined row shares its run's copy."
  - "A run whose own task already read done answered `done` even when a review the store would not seat had failed it. It answers incomplete."
  - "A worker was handed its own note back at its next step, and a worker woken on the same task was handed the task's older notes a second time. A note is handed to a task once, across every worker it has; the record keeps which notes the task has had."
---

The door that carries an interrupted run on (#1305) still has no caller, and it now
adopts a store only when that store's own run is the one it was asked to carry on.
