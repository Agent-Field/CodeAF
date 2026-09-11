---
kind: changed
title: The token column shows what moves — ↑ is the request size, the moving side glows, tasks draw it
pr: 849
surface: [chat, engine, docs]
invalidates:
  - "↑ on the working block was the INPUT BILL summed over every request of the turn, so three steps on a 50k conversation read ~150k. It is now the size of the request in flight — the conversation's ContextTokens — re-read on the usage beat and on the frame after a tool ends. Spend is still the status line's and /cost's; nothing about billing moved."
  - "The column's comment said ↑ 'steps up as each tool result joins'. It did not: ContextTokens was only read at boot, compaction, settle, detach and rewind. It is now read off the loop three times a second while a turn runs, beside the usage ask."
  - "↓ was max(billed output, estimate of the whole turn's prose) and froze whenever the bill landed above the page's estimate. It is the billed output plus an estimate of what arrived since that bill, and it counts a call's streamed arguments (a `write` body) — prose alone froze ↓ while the model wrote hardest."
  - "A column figure above 1k read `2.5k` and moved once per hundred tokens. Under 10,000 it is spelled whole (`2,531`); tokenWord from 10,000 up."
  - "The column was drawn flat dim. The side that just moved now glows (reading ink, arrow one fade stop up) and decays over 1.3s; both rest dim while a tool runs. A jump in ↑ leaves a `+3.4k` receipt for 1.5s, shed first on a narrow row. No glow where palette.fade degrades (16-colour, NO_COLOR, linear)."
  - "A task room's ↑ was the sum of its steps' Input. It is one request: the worker's weight (new session.Agent.TaskContextTokens door) on this machine, the newest `call` line on a hosted node."
  - "A task room opened over the engine host (bare `aforge`, openFarRoom) drew no ↑ and a ↓ only once a message sealed — its column was fed by an event lane it does not have. It now reads the journal tail's per-request `call` lines through the same scan that rebuilds the transcript (session.Record.Requests)."
  - "A hosted task's price on the rail moved only when the node changed state, and its token count never showed — no notice carried one, and only the local pilot lane counted tokens. The node's notice is now re-published at each step end whose spend moved (childRun.tellSpend), on the existing task lane, and carries a live Tokens figure (TaskNotice.Tokens, TaskNode.burned). An unpriced model's row still moves only at state changes."
  - "The engine host stated facts on a tool end, one step before the batch's results joined the conversation. It restates them on the first word of the next request (weighsAgainLocked), so a hosted ↑ includes a big read when the request carrying it goes out."
---

The owner could not tell a working turn from a stalled one by the column, and
picked this treatment from an animated mockup. The column now answers the two
questions it exists for: how big is what is going up right now, and is anything
coming back — with the moving side lit so the difference between "a tool is
running" and "the stream died" is visible without reading a number.
