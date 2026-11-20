---
kind: fixed
title: A task's search of the whole disk is refused, and a search silent for 30 seconds is stopped
pr: 2860
surface: [chat, engine]
invalidates:
  - "A task could run `find /`, `rg x ~` or `grep -r x /` and it ran: a task read the whole machine, searches included, and a silent search held a hand-off worker for the full 600-second bash ceiling (qloop run32 FIRE t1, a canvas turn past 900 s). A task's search rooted at /, one level under it, the home or the folder holding it, with no depth bound of 3 or less, is now refused before it runs with \"… searches far beyond this task, so it was not run … This task works in <folder>\". Plain reads and searches of a named folder still run; conversations are unchanged."
  - "A task's foreground command had no cut except the 600-second ceiling. A task's search that prints nothing for 30 seconds is now stopped with its output and \"Search stopped: it printed nothing for 30 seconds …\"; printing resets the clock and non-search commands are not on it. A task's search is no longer handed to a job at the background-after clock."
  - "Hand-off workers built by the run engine (internal/run's bashworker.go into session.NewBeltWorker) do NOT carry the conversation's 30-second background-after clock; their BashBackgroundAfterSeconds is 0. That is still true after this change and is the reason a worker's quiet command waited 600 s."
---

The agent's shell inherits the engine's whole environment except TMUX and
provider keys (bare.StreamingEnv), so a harness that exports CARGO_TARGET_DIR
before launching the app hands it to every worker. That is left as it is: a
person's own build variables are what their builds rely on.
