---
kind: fixed
title: a run ends when the request is satisfied, not when the plan runs out
pr: 464
surface: [engine, chat]
invalidates:
  - "The delivery gate's coverage finding used to fire on ACTIONS of the run. An acceptance point is now read as a behaviour of the finished work or an action of the run (`plan.Point.Kind`, `plan.PointBehaviour`/`plan.PointAction`, journaled as `store.AcceptancePoint.Kind`), and only behaviours are mapped to checks. An errand asking for one command to be run and one line reported states two actions; the gate asked which repository test exercises them, and none can."
  - "It also used to fire on a run that changed no code. `revision.codeChanged` now skips the mapping and the finding where a run left no patch and no record path inside the workspace, and the verdict says so — \"the work changed no code, so there is no check to ask for\". It answers yes wherever it cannot tell, so an unobserved run maps exactly as it did."
  - "An empty roster used to be treated as a measurement in every case. The comment in `settleAcceptance` calling the empty answer \"the true one\" is gone: `revision.rosterSpeaks` now separates a project with a runner and no tests — still a measurement, still a finding — from a suite that could not collect, was cut at its ceiling, or was never taken, which answers nothing. There the verdict carries `revision.CoverageUnread` and no round is bought."
  - "`revision.checkEvidence` read check identities from the project's roster and from `Evidence.Patch`, and the patch half has been DEAD ON EVERY BELT since `Outcome.Account` lost its only writer with `internal/exec/swe.go` in 05b99537 — so a run that wrote a 195-line test file reached the mapping call with a roster of nothing. `revision.declaredByTheRun` now reads the run's own new and changed check files off the tree (`verify.OwnChecks` + `verify.DeclaredChecks`). `Evidence.Patch`, `patchBlock`, `removedChecks` and `Compose`'s patch are still unreachable until the account plumbing is restored. Those two readings spell one check two ways — a declaration is bare, and the name a runner prints carries the node-id path, the class, the package and the subtest — so `verify.CheckIdentity` now says which check a name names and `verify.UniqueChecks` unions the readings by it, in `checkEvidence` and in `WeakenedChecks`; a roster still keeps the runner's own spelling, because `verify.SplitReplaced` reads the path out of it to tell a rewritten check from a deleted one."
  - "A failed delivery gate always bought a repair round. It now first asks one question with the verbatim request — `revision.RequestMet` — and a yes ends the job with no repair, no remainder and no reservation. It is asked at the two seams where a round would otherwise be bought (`cmd/aforge/chat.go requestSettled`, `revision.ExtendForGap`) and nowhere else, so its cost is bounded by the rounds it replaces. An unreadable answer buys the round it was going to buy."
  - "The request-met question is put ONLY of the judge's own prose. `revision.MeasuredFinding` shuts the door on a mechanical gap, a sourced finding, a behaviour nothing exercises, a check this work wrote that is red, a definition its callers no longer fit and a name the tree no longer binds — a model's reading may not overturn a measurement, which is the line `store.DeliveryGate.Overturned` already draws. `revision.RequestQuestionable` is the one predicate both doors read. And `Judgment.Request` is bound at `JudgeDeliverable`'s single exit, so `ExtendForGap`'s door is live for every caller rather than only for the chat surface."
  - "`settleUnmeasured` set `Unreadable` before anything had classified the request, so a read-only errand failed `DeliveryGate.Whole` because an unrelated whole-suite reading had been cut at its ceiling, and left as `partial`. `revision.unaskable` now takes it back off on the two exits where there was no coverage question to answer — no behaviour stated, no code changed. Where the request states behaviours over a change that WAS made, an unreadable suite still holds the delivery short."
  - "\"Done\" was when the plan ran out and nothing ever said the request had been met. `store.DeliveryGate.Receipt` is the first positive field on that row: `the request was met as stated` (`revision.RequestMetWords`) or `checked by tests, coverage not measured` (`revision.CheckedNotMeasured`), said under a ✓ in the headless stream. It is deliberately NOT read by `DeliveryGate.Whole` — a receipt says why a run stopped, never whether it landed — but the second receipt does clear `Unreadable`, so a person is never shown \"partial\" over green work and never a silent pass either."
---

Two measured runs and the same defect underneath both. A one-command errand — run one test
command, report the final line, change no files — was satisfied by its first leaf two
minutes in, and then spent more than 97% of its money and 82% of its wall on the delivery
gate's coverage finding, twice out of two on the shipped default: the finding asked which
repository test exercises "the command is run in this workspace", and bought the rounds
that went looking. And a correct fix to a real issue (Human-Agent-Society/reef#145, its own
fail-to-pass tests 6 of 6 green, nothing regressed) was refused three times on "6 behaviours
the request states have no check", ended at exit 2 and the word `partial`, and cost five
times what the same issue cost through the chat door — because the project's suite could not
collect, the run's own ten new tests never reached the check list, and six behaviours were
matched against an empty set.

The gate now asks the coverage question only where a check could exist, reads the work's own
checks off the tree so the mapping is not blind, and — at the one moment it would otherwise
buy a round — asks whether the person's request, exactly as they wrote it, is already
satisfied by what is in hand.
