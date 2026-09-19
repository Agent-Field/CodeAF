---
kind: fixed
title: a run's dollar limit now holds while a worker is still working
pr: 1268
surface: [chat, engine]
invalidates:
  - "A run's dollar limit was read only when a worker came home. A worker's spend reached the run's count in its report, so one long worker could pass the limit many times over before anything counted it, and the limit was a word about the run after it instead of a hold on this one."
  - "A worker now says what it has spent as each model call is paid for, and the run adds it while the worker is still working. When the sum reaches the limit the run launches nothing new, ends the work in flight, absorbs every ending, and answers `a limit you set stopped it`. That is the time limit's road, so the sentence in the entry for the time limit that said a cost limit lets work in flight finish is no longer true."
  - "A worker never waits on the run to report its spend. It writes a running figure and leaves a token the run may miss; the figure is the whole of what the worker has spent, so a missed token loses nothing, and a run that is draining is never held open by a worker that has something to say."
  - "What a run counts as spent never goes down. A worker whose task the store cancelled under it handed back no spend the run counted, so those dollars were missing from the run's figure and from the limit. Its words and steps are still dropped; its dollars are now counted, whatever way the task ended."
  - "The call that reaches the limit is already paid for, so a run can end a little over its figure. A run with no dollar limit counts as it always did, at each return."
---

`--max-cost` still does not reach a run: a run's dollar limit is the conversation's
spend ceiling.
