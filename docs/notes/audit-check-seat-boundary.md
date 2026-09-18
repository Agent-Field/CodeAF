# What a task's audit step can actually execute, and where that boundary is set

A diagnosis, from the run records first and the code second. No fix, no design:
whether the auditor should ever run tests, and in what sandbox, is a separate call.

## 1. The boundary, from the code

The auditor is a fresh child agent built by `Agent.newAuditAgent`
(`internal/session/task_audit.go:3129`). What it gets:

- **Working copy.** Not the session's tree and not the node's worktree as the
  worker left it: `auditNode` builds a **clean restore of the selected work**
  (`auditGroundFor`, detached checkout of the branch with the landing's files
  laid over it, `internal/session/task_audit.go:1075`, `:2177`) and the
  auditor's `Workspace` is `ground.dir` (`internal/session/task_audit.go:3175`).
- **Tools.** The belt is replaced wholesale in `auditBelt`
  (`internal/session/task_audit.go:3262`): `read` (capped), `grep`, `find`,
  `ls`, and a bash wrapped by `verifyOnlyBash` (`:3336`) → `readingOnlyBash`
  (`:3379`) → `refuseOutsideDoor` (`:3394`). Shell composition is refused
  outright; one command remains and it must prefix-match the door.
- **The door.** What the bash may run is `auditDoorFor`
  (`internal/session/task_checks.go:314`): **the checks the work's own
  verification contract declared** (worker receipts grant no permission , 
  `task_audit.go:1120`) plus `auditReadCommands`
  (`task_checks.go:202`: `git diff`, `git log`, `git status`, `git show`,
  `pwd`, `wc`, `head`, `cat`). Nothing else, ever.
- **Sandbox/permissions.** No OS sandbox. `ApprovalPolicy` is allow-all,
  `AskConsent` false, `InTask` true (`task_audit.go:3196-3200`), the belt, not
  the approval gate, is the constraint. Results are size-capped
  (`boundedResult`, `:3291`); the whole check is bounded by the door's window
  (`auditDoor.window`, `task_checks.go:479`, 5 min with a runnable check, a
  reading-only deadline without).

**So: can the audit turn run `go test` today?** Only when the work's contract
**declared** that exact command as its verification check. It cannot run a
test the worker merely ran, cannot run any test on work that declared no
check, and cannot compose anything. That is decided in three places:
`auditDoorFor` (what counts as a check), `refuseOutsideDoor` (the gate), and
`auditBelt`/`verifyOnlyBash` (the only shell the auditor is handed).

## 2. The prompt

The system prompt is `auditPrompt` (`internal/session/task_audit.go:325`). The
operative lines:

> "You are READ-ONLY. You have read, grep, find and ls, and a bash that runs
> the repository's own verification and nothing else. … Run the verification
> yourself and read the diff. A claim you did not check is a claim you have
> not verified."

> "VERIFIED means you ran something and it passed. REFUTED means it did not
> pass, or there was nothing there to have passed, or you could not check.
> When in doubt, REFUTE."

And the per-audit orientation, `auditDoor.line()`
(`internal/session/task_checks.go:495`), which the question interpolates:

> "THIS WORK DECLARED NO REPEATABLE CHECK, so there is nothing here for you to
> re-run. Your bash will run only git diff, git log, git status, git show, pwd,
> wc, head, cat, none of which verifies anything. Do not go looking for a
> command to run … Read the files and the change, judge what you can see, and
> answer now. An answer from reading alone is a real answer; running out of
> time is not."

## 3. Two transcripts side by side

Both quotes are the check seat's final assistant message in its audit journal,
from the run records (161-row ledger of audit model and verdict). Both audited
the **same issue** (same starting commit, same acceptance), and, decisively , 
both received the identical door sentence above, because neither work declared
a repeatable check.

**The max crew's check seat**, verdict REFUTED (the run's node landed
failed, though the branch tip in fact passes the executable grade):

> "REFUTED, verification is incomplete: this audit environment cannot execute
> the required Go tests. Baseline diff preserves both tests' assertions and
> known-red.txt; `git diff --check` passes, and HEAD is clean. Inspection
> confirms synchronous greeting spend loading and general MaxSpread-based tail
> suppression, with six boundary cases. Targeted/full-suite passes, baseline
> reproduction, build/pr-ready completion, and remote-operation claims remain
> independently unverified."

**The high crew's check seat**, verdict VERIFIED, same acceptance, same
empty door (its journal even records the gate refusing a composed
`git status … && git log …` and naming the allowed list, the reading commands
only):

> "VERIFIED, read the full diff at HEAD (the issue's clean-dev tip): only
> comments changed in `lanewalk_test.go`, so `TestADecayedBeliefDrawsNoTail`'s
> assertions … are untouched, and `posterior.go` adds `MaxSpread` … while
> `lanes.go` … makes `laneTail`/`laneWhy`/`laneNote` silent for it …, the test
> passes for the stated reason, not a moved assertion."

## 4. The answer

The refusal is the auditor reading its **real situation** correctly, the
boundary is not wrong in the sense of a missing tool nobody told it about. Its
bash genuinely cannot run `go test` on that work: the door carries only the
reading commands, because the work declared no repeatable check, and worker
receipts (the worker did run the full suite) deliberately grant no permission
(`task_audit.go:1120`). Both crews' auditors were told this in identical
words. What differs is not capability but which sentence of the contract each
obeyed: the max crew's seat obeyed "VERIFIED means you ran something and it
passed … when in doubt, REFUTE" and refused; the high crew's seat obeyed "an
answer from reading alone is a real answer" and verified by reading. The
boundary is deliberate, but the prompt holds both instructions at once, and a
seat that cannot execute anything is pushed toward REFUTED by one line of the
same prompt that, three lines later, tells it reading alone is enough. The 8-of-20
refusal pattern is that tension resolving differently by model, on an
environment whose limits were correctly perceived.

## 5. One line

Does #1183's settle-turn ceiling interact with the audit turn? **No**, the
settle ceiling (`TaskNode.settleCeiling`, `internal/session/task_run.go:4684`)
bounds the conversation turn that a landing note wakes to read and settle the
node, while the audit is a child agent bounded by its own `auditPace` window
(`internal/session/task_audit.go:1123`, `:1270`); they are separate turns with
separate bounds.
