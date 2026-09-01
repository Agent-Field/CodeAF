---
kind: fixed
title: spending is written down per call, so an interrupted turn's money is on every surface
pr: 290
surface: [engine, chat]
invalidates:
  - "The machine ledger `~/.aforge/v3/usage.jsonl` held one line per TURN carrying however many requests that turn took; it now holds one line per model CALL, written as that call's bill is decoded, and every new row says `calls: 1`. Rows written before this change still carry a whole turn on one row with a larger `calls` figure and add up the same."
  - "A turn that never sealed — interrupted, stopped, crashed, or still running while somebody looked — put nothing on the ledger at all, so the status line and the spend surfaces disagreed about the turn in front of you; every call is on the file the moment it is paid for now."
  - "`Agent.sealTurn` was \"the one place a turn's cost reaches the journal AND the machine ledger\"; it now writes the transcript alone, and `Agent.bank` is the one door money goes through into the meter and the ledger together."
  - "`session.TreeSpend{ConversationUSD, TasksUSD, TotalUSD()}` no longer exists; it is `session.Receipt{Direct, Children, Folded()}`, the one receipt object every spend surface reads."
  - "Settings→Spending's `this one` receipt reported what the conversation itself had spent, which was smaller than the money segment beside it whenever work was running; it is the same figure the status line draws now — the conversation and everything it started."
  - "The `/spend` place's own pointer line said `under a cent` for a sub-cent day where the Spending tab said `$0.0003`; the pointer line quotes that total the way every other surface does, and the sliver word stays on the page's own model and subject rows."
  - "The manual said the spend place opens with `/spend`; `/spend` is an alias of `/cost` and the place's doors are `alt+5`, `tab`, and typing the word at home."
---

Money was banked per call and journaled per turn. The meter moved as each provider answer
was decoded and the machine's ledger was written once, at the turn's seal — so any turn
that did not get there was money one source had and the other never heard about. One
measured chat showed four numbers for the same instant: $0.24 on the status line, $0.157
in the ledger, $0.16 on `today` and on `/spend`.

The durable row now goes out from the per-call site that already writes the call log, and
it goes out through the same function that moves the meter, so a caller cannot have one
without the other. A dropped or failed write — the writer still drops a row rather than
making a reply wait on a disk that has stopped answering — is counted and said on both
spend surfaces, because a figure that is short and does not say so reads as a cheaper day
than the one that happened.
