---
kind: changed
title: the task engine has a ceiling on how many endings one function may have, and it only turns one way
pr: 284
surface: [engine]
invalidates:
  - "`runTaskChild` was one function of 506 lines holding 55 decisions, with two closures — a `checkpoint` and a `drain` — over fourteen shared locals. It is `childRun` and its methods in `internal/session/task_child_run.go`: `open`, `drain`, `step`, `count`, `trip`, `checkpoint`, `landIfStopped`, `drainLanding`, `foldParts`, `park`. The sharing is exactly what the closures had — a pointer receiver is what a closure over a local already was — so nothing about when a fact is written or read has moved, and nothing a person reads, no journal field and no ending changed."
  - "The landing finalisation — ground check, land, did-anything-land test, settle — was written THREE times: the ordinary finishing line, the audit-off road, and the threshold's ([Agent.landStopped]). It is `Agent.landFinished` once, in task_ledger.go beside the `landHome` and `keepHome` #237 started. That is the miss #255, #256 and #258 all were: a check added to one copy is a check the other copies do not get — and #277, landing days before this, had to make the same edit in all three at once. Its callers now pass the report as two halves — what leads and what stands under it — because that was the only thing that ever varied between the three; #277's `cameHome` reading and the mark it carries through to `landConflicted` are asked once, here."
  - "`Agent.workTaskNode` held 35 decisions over 400 lines. Its tree preparation is `openTaskWorld`, its handoff pre-flight is `briefMatchesItsWorld`, and its three settlements for a run that never reached the gate — a threshold, a cancel, an error — are `settleUnfinished`. It is 21 over 282, and the one-worker-or-two loop plus the gate's three verdicts are what is left."
  - "`Agent.divideOnce`'s refusal settlement — the record, the tiebreak refund, and the person's-own-job answer — was inline. It is `settleDivisionRefusal`, and it writes its decision onto the caller's record rather than journaling for itself, so the one-write law that function states in its own comment is still one write. divideOnce is 13 and off the ledger."
  - "There was no gate on function shape at all; CLAUDE.md's `no function over ~15 branches` was convention a reviewer had to remember. `internal/session/complexity_test.go` now counts gocyclo's own number — one, plus every `if`, `for`, `range`, named `case`, named comm clause, `&&` and `||`, with a `default` counting for nothing — over `go/ast` for every function in `task_*.go`, with no new dependency and `go test` as the gate. `complexityDebt` is the ledger of the eight still over fifteen, measured where the gate was fitted — which is why `taskNote` reads 19 rather than #260's 18: #277 gave it the arm for a landing that saved nothing before this test existed to have an opinion, and a ledger cannot refuse the debt it was built to hold and `whyTheDebtIsStillThere` is the sentence each row owes. A row may not go UP, a new function may not be written over the ceiling, a row that is paid off is deleted, and a row that FELL is told to write its new number down — a row left at the old figure is room the next change could spend with the gate green throughout."
  - "`tasknews_law_test.go`'s `taskNewsPairReaders` named `runTaskChild` as the reader that asks a parked parent's two questions one at a time. It names `count` — the no-progress switch, now `childRun.count` — with the same claim and the same argument. `effects_test.go`'s walk for the one place that reads the run of effects that changed nothing reads `task_child_run.go` rather than `task_run.go`, because that is where the leash is. Both are exception ledgers keyed by a name, and the thing they name moved."
---

Ten functions in the task engine were over fifteen decisions; eight are, and the
worst of them by a factor of two — `runTaskChild` at 56 — is gone. The four
defects filed off the family audit (#229/#237, #255, #256, #258) were all the
same miss rather than four different ones: a road with more endings than anybody
can hold in their head, so one ending forgets what the others do.

The ledger is the `SIZE-BUDGET` bargain applied to a shape instead of to bytes.
A binary that grows needs the budget raised in the same commit as the thing that
spent it, by somebody who can say what it bought; a function that grows past the
ceiling now needs the same, except that there is no road to raising it — the
ledger is closed and every row is a named debt with an issue's worth of work
behind it.
