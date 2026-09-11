---
kind: fixed
title: A task's page says a call is still open, with that call's own clock and rate
pr: 951
surface: [chat, engine]
invalidates:
  - "A task's page drew nothing about a request that had gone out and not come back. A model call that is still streaming has written no entry yet, so the page's own rows could not know about it, and the header fell through to `still working` — one audited call ran twelve minutes with the header saying nothing but the task's age and a tool count. While a call is open the live segment is now THAT CALL's clock and its rate, and when the call comes back the segment reverts to the ladder it always had."
  - "`session.TaskBeatRow`, its `Working` method and `ReadTaskBeat` are exported (they were `taskBeatRow`, `working` and `readTaskBeat`). The pulse sidecar is the one source that knows a request is in flight, and a surface had no way to ask. `TaskRecord` carries `Beat` and `Agent.TaskBeat(id)` answers it, so the file's name crosses the wire as data and a page over a connection reads the pulse the same way a local one does."
  - "The path is CARRIED, never rebuilt from the node id. A reader that composed it would be a second spelling of where the pulse lives, and the two would drift the day the layout moved — which is the bargain the record's own field exists to end."
---

The pulse is read on the refresh tick the reading came from, never once at room
entry: a reading cached at the door would leave the header saying one thing for
the whole life of a call, which is the defect this closes wearing a different
hat.

Nothing new is spelled here. The rate is the same `PhaseNews.Rate` the status
row's right edge draws, through the same `tok/s` that line builds, and the clock
is `countUpWord`, the live spelling every moving figure on this surface already
uses. Two rows that disagreed about what *how fast* looks like would be worse
than one row saying nothing.
