---
kind: fixed
title: a fork's hands and an adaptive run's nodes are on the machine's day total once, not twice
pr: 180
surface: [engine, chat]
invalidates:
  - "The machine's day figure — the one the per-day wallet rail reads — counted a forked hand's spend TWICE, because the hand journaled its own calls into the ledger and the fold at the end of the hand wrote a second line for the same money. It is counted once; a day that included a fork used to read high, and the daily limit was reached before that much had been spent."
  - "An adaptive run's node had the identical defect through `orchestrateExec.spend` and is fixed in the same change. A task node never had it: `foldTaskUsage` has used the silent door all along."
  - "`Agent.addFoldedUsage` was the only fold door and it dropped the role. There are two: `addFoldedUsageAs` keeps the role on the JOURNAL line while still writing no ledger row, which is how a hand's fold keeps the word `hand` that says which of a turn's tokens the hands spent."
  - "A hand's ledger lines named no conversation at all — a hand keeps no journal, so they said `unfiled` and joined onto nothing. They carry `UsageLine.Root`, the conversation the fork was made in, so `/cost`'s tree total and the status line's `$` include a hand WHILE IT IS STILL OUT rather than when it comes home. A run's node carries it now too."
  - "A hand and a run's node fell back to the machine's own ledger when their conversation had been pointed at another file. `Config.usageLedger` travels to both, which is what a silent fold requires: money that used to land in the wrong file would otherwise land in no file."
  - "`SubjectSpend.Session` was documented as \"the conversation a task's work was journaled under\". It is the NODE's own journal id, exactly as `UsageLine.Session` is; the conversation a piece of work belongs to is `UsageLine.Root`."
---

Nothing was ever miscounted in a conversation's own books, and nothing changed
there: a fold is still what moves a hand's or a node's money onto the caller.
What was wrong was the second write. The ledger's first rule is that a line is
written where the call was made and that a fold is not a call, and two of the
three folds in the program went through the door that writes one anyway.
