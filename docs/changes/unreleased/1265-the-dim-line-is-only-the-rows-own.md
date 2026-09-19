---
kind: fixed
title: the dim line under a step row is only drawn when it must be the row's own
pr: 1265
surface: [chat, engine]
invalidates:
  - "A step's row leaves out the change into the run's copy and any part addressed to the run's record, and under the row one dim line drew the head of what came back. That head is cut from the ONE observation the record holds for the whole line, so when a left-out part printed first, the dim line showed that part's words under a row that no longer showed the part."
  - "THE HEAD IS DRAWN UNDER A ROW ONLY WHEN EVERY PART LEFT OUT OF THE ROW CANNOT HAVE WRITTEN TO IT AND STOPS EVERYTHING AFTER IT IF IT FAILS. A left-out part addressed to the run's record withholds the dim line. A left-out change into the run's copy keeps it only when it is joined so that its failure ends the line. A row with nothing left out keeps it. The engine establishes the fact with the other display facts (`PlanStep.ObservationHeadWithheld`), it crosses the wire as a field, and the surface never reads the words that came back. The field is the negative on purpose: a step from an engine built before it draws its head as it did."
  - "Nothing can do better from the record as it is: the belt runs the whole line as one shell with its output interleaved, and no part's output has a boundary (diagnosis c340)."
---

How a command is cut for its row, the step numbers and the whole answer on disk are unchanged.
