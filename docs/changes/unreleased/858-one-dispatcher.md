---
kind: changed
title: One dispatcher, one plan — a failed call is bounded by one deadline
pr: 858
surface: [engine, chat]
invalidates:
  - "internal/provider/retry.go is gone. The file is internal/provider/dispatch.go and it is THE dispatcher: the one loop in this build that reaches the wire for a completion and the one place that decides what a failed call does next. Three go/ast laws in dispatch_law_test.go hold it there."
  - "The six budgets that loop owned are DELETED, not tuned: maxAttempts (3 faults), rateLimitAttempts (6 paced sends), patientAttempts (60), watchedPacingBudget (2m), patientPacingBudget (10m) and freeMoves (8), with outOfPatience and patienceOf. Nothing in internal/provider bounds a number of attempts any more. Code or notes that name any of them are describing a build that no longer exists."
  - "R4's recommendation in #853 — watchedPacingBudget 2m → 30s and patientPacingBudget 10m → 2m — is SUPERSEDED rather than applied. Pacing has no budget of its own at all now; a call held by 429s is bounded by the same deadline everything else is."
  - "How long a failed call may go on trying is lane.Role.GiveUp: lane.TurnGiveUp (90s) scaled by the role's own patience column, the same column lane.VisiblePatience is scaled by. Talk and an attached leaf 90s, an unattended leaf / memory / auxiliary 4m30, standing / judge / design 9m, a probe 45s, an unnamed role reads as unattended. It is one number and a person can be told it."
  - "control.Plan is no longer the hazard's alone. It carries Deadline, SpendUSD, Moves, Model, Role, Comeback and Shapes, and the dispatcher reads and writes the same object the hazard does. Plan.Spent is the one place a clock ends a call; Plan.Moves is a pointer because the arms of one race share it."
  - "control.Next is the one move generator: another machine, the same machine ONCE after the comeback it named itself and only when the set is one wide, a relaxed shape one rung at a time, none. A plan naming no machines is an OPEN set and not a set of one — the router picks and the deadline is the whole bound — so anything that read an empty serving set as 'nowhere to go' is wrong."
  - "A 4xx carrying error.metadata.provider_name no longer ends the call. It is one machine's answer about this request and the model's others have said nothing, so the dispatcher walks to another machine with that one excluded. The rotation used to happen only on the NEXT turn, through the ledger (refusal_test.go's three-attempt scenario). A refusal that named NOBODY is still our own bytes and still comes back whole, as does anything sendRepaired can fix; a second refusal from a machine this call already vetoed means the veto changed nothing, and that comes back too. 401, 402 and 403 are never walked."
  - "The ordinal a person reads is about MACHINES. `2 of 6` used to be the attempt ceiling, a statement about patience; it is now how many machines this request may go to, and where that is unknown it is how many this call has met. A denominator aforge cannot stand behind draws nothing."
  - "internal/session's turn loop no longer owns a budget that multiplies the transport's. It takes the same lane.Role.GiveUp deadline and, when that is spent, tells the boundary a spent ladder — so the one classifier still answers hop-or-end. taxonomy.TransportAttempts still bounds the count inside that deadline; folding the person's `response.attempts` setting into the deadline itself is named as owed in DESIGN.md §7."
  - "errandWalksOn in internal/session/auxiliary.go reads !evidence.OurBytes && !evidence.Overflow. It spelled `Status >= 400 && Status < 500 && Upstream == \"\"` by hand, which was a fourth rule about what a 4xx means. internal/taxonomy's seamsOwed is now EMPTY and must stay that way."
  - "docs/design/recovery/DESIGN.md §1 finding 1 is CORRECTED in place: the 869 `context deadline exceeded` rows at 60s and 90s are the CALLER's deadlines, not the stream wall and not internal/provider at all. §2 gains problem 13 — the hazard's only act was a purse-gated hedge, so a refused purse meant no act at all, 2,186 attempts in ten days, fixed by #853. §7 marks R0 #852, R1 #850, R2 #854, R4 #853 landed."
  - "The manual's lanes page no longer promises 'two minutes on a turn you are sitting in front of, ten inside a task' for an account-wide rate limit. It states the give-up table, says the status row counts machines rather than tries, and says a machine's own refusal is a move rather than the end of the turn."
---

Eleven controllers decided what happened to a failed call and each owned a budget
none of the others could see. Their product — three models times four attempts
times five arms times six paced sends times nine rungs, with four wall clocks
over the top — was nobody's number, and the call census of 2026-09-10 measured
what it produced: chains of sixteen and seventeen identical sends to one machine,
running eleven minutes, ending refused anyway.

There is one deadline now, it is the role's own patience, and what comes next
inside it is a pure function of what has already been tried.
