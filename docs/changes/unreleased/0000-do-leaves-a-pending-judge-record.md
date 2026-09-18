---
kind: added
title: a `codeaf do` errand leaves a pending landing for the Model Pool's judge
pr: 0000
surface: [engine]
invalidates:
  - "A headless `codeaf do` run was never scored by the Model Pool. The do door runs the resident's brain over a store journal and builds no `session.Agent`, so `Config.TaskLanded` — the seam a chat landing reaches the live judge on (`cmd/codeaf/poolrecord.go`) — never fired for it. The errand now appends one row to `<profile>/pool/pending.jsonl` at its tail, door `do`, for the restart-time sweep to judge later under that row's door. Nothing waits on a judge; the process exits at once."
  - "The pending row carries the run's own worker (`seats.Work.Model`, because the outcome's own seat is stamped after the errand returns), the root's deliverable, its artifact count, the tokens summed by `priceErrand`, and the unverified state a judge exists to resolve. Its id is minted from the run's start in nanoseconds so two runs never share one — the sweep's judged markers dedup on it."
  - "A pool whose mode forbids reading (`poolcfg.CanRead` false) writes nothing, and a run this process handed to a resident writes no row either: that work happens in another process, and a landing written here would name a model and a deliverable that are not this errand's."
---

The row is written by hand at the errand's tail, after `priceErrand`, because that
is the one place the run's own bill is already settled and there is no live
landing hook to carry it. The judge's own later spend stays in the usage ledger
under its own seat; it never enters this envelope, whose `Spend` is a read of
`SpendSinceSeq` and may not disagree with it.
