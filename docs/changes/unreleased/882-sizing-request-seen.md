---
kind: fixed
title: A task being sized shows the model thinking, the seconds and the tokens coming back
pr: 882
surface: [engine, chat, docs]
invalidates:
  - "While a task said `sizing the work`, the row under it named the model being asked and nothing moved: no thinking mark, no clock on the request, no token count. The rail's phase row now carries the request (`sizing the work · thinking 41s · ↓ 4,465 · deepinfra`) and the ladder's sentence stays on the row under it."
  - "A task being sized with nothing written on its page said `nothing on this page yet — it fills in as the task works` for the whole reading. The page now draws the request in that line's place: the thinking mark while the model thinks, which model of how many, how long it has been out, `↓` for what has come back, and the machine answering."
  - "An errand a node ran (the sizing reading) left no trace until its bill: the pulse file said `\"requests\": 0` and the node's journal held nothing. The pulse now counts it and the journal gets a `flight` line when it goes out and when it comes back, with first-token and total milliseconds, reasoning and answer counts, the machine, and how it ended. The `flight` line is evidence, never money; the `call` line is still the bill."
  - "A `TaskPhaseNotice` carried the phase, its rounds and one line of text. It also carries `Call`, the live request the phase is waiting on (nil the rest of the time), and a hosted window gets it over the task lane like every other phase field."
---

On 2026-09-11 a sizing reading thought for 219 seconds, and the screen said the model's
id and nothing else for all of it. The owner's rule is that if thinking is happening,
the person sees it, the way they already do for a worker's own requests. The request
now leaves the same trail a worker's does: the pulse, the journal, and the live row.
