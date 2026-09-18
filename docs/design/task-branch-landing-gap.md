# Why a settled /task leaves its commits on task/<slug>, not the worktree branch

Diagnosis only — no code change is proposed for the engine, and none is needed for
the finding: the merge home works, targets exactly the branch the cell driver
reads, and lands there whenever the engine's gate lets it run. The gap is that the
driver's word "settled" is not the engine's word "verified".

## The one-paragraph answer

The engine cuts every node a worktree on a fresh `task/<slug>` branch off the
ground's HEAD (`internal/session/task_run.go:8024`, branch name at
`task_run.go:8039`). A node's edits stay uncommitted in that task worktree while
it runs (`internal/session/taskgit.go:19`); the first commit onto `task/<slug>`
is made at landing, by one of two roads. Only a **verified** audit verdict (or a
person/model accept of an unverified node) reaches `taskTree.comeHome`
(`task_run.go:8367`), which commits the work and merges the branch into the
ground (`mergeIntoGround`, `internal/session/groundcarry.go:75`) — and the ground
under the cell driver IS the driver's worktree, so the merge lands on the branch
the driver reads. Every run whose task rows are all `done` shows exactly that.
When the node instead ends **unverified or failed** — an audit that timed out, a
refuted verdict, a killed run — the gate stays shut and the settle-without-merge
road (`keepHome`, `internal/session/task_ledger.go:110` → `keptWork`,
`task_run.go:6119`) commits the work onto `task/<slug>` and deliberately does not
merge ("only work that was checked reaches the person's branch",
`task_run.go:5689-5692`). The cell driver (`~/src/doe/task-run.sh`) stops on its
own 180-second quiet rule and calls that "settled", then reads its worktree
branch — which the engine deliberately never moved — and reports zero commits
while the deliverable sits on `task/<slug>`.

## (a) Where the branch is created and where the commits are made

- **Ground resolution.** `prepareTaskTreeAt` (`internal/session/task_run.go:7936`)
  with no explicit `where` resolves the ground with `repositoryRoot`
  (`task_run.go:9058`): `git rev-parse --show-toplevel`. For the driver's linked
  worktree (`~/src/sd-<name>`) that answers the worktree itself, so the ground is
  the driver's worktree — confirmed by c215's node record (below), whose
  `ground` is `/home/santosh/src/sd-crewword`.
- **Branch creation.** `cutTaskWorktree` (`task_run.go:8024`) names the branch
  `task/<slugify(title)>-<shortID>()` at `task_run.go:8039`, then `carveGround`
  (`internal/session/groundladder.go:344`) reaches `cutWorktreeFrom`
  (`task_run.go:8067`), which records `home := currentBranch(root)` and
  `homeSha := branchCommit(root, home)` (`task_run.go:8069-8070` — the branch
  checked out in the driver's worktree and its commit at cut time) and runs
  `git worktree add -b <branch> <dir> <from>` at `task_run.go:8092`, cut from the
  ground's HEAD (or the ground ladder's sealed base). The worktree directory is
  the engine's own per-task tree under the run's profile:
  `<profile>/v3/projects/<project>/<session>/trees/<id>`.
- **Commits.** During the run the worker's edits sit uncommitted in that task
  worktree (`internal/session/taskgit.go:19`: committing is the landing road's
  job and nobody else's). The commit onto `task/<slug>` is made by
  `commitTaskWork` (`task_run.go:8784`) → `commitTaskWorkAs` (`task_run.go:8817`,
  staging via `stageTaskWork`, `task_run.go:8989`) at exactly two moments:
  `comeHome` (`task_run.go:8394`, the merge road) and `keptWork`
  (`task_run.go:6133`, the settle-without-merge road). Whichever runs, the commit
  lands on the task branch in the task worktree; the branch ref itself is shared
  with the ground repository from the moment the worktree was registered.

## (b) mergeTaskBranch: what it merges into, and when it runs

- **The merge itself.** `mergeTaskBranch(root, branch)`
  (`internal/session/groundcarry.go:352`) is one line:
  `git -C <root> merge --no-edit <branch>` under codeaf's commit identity
  (`internal/session/task_branch_protection.go:27-29`). `root` is `taskTree.root`
  — "the repository the branch merges back into" (`task_run.go:7819`) — and the
  merge moves whatever branch is checked out at `root` at landing time.
- **Callers.** Three, all inside the landing:
  `mergeIntoGround` (`groundcarry.go:75`, call at `:76`),
  `carryGroundWork` (`:157`), `carryUntrackedGround` (`:216`). The only entry
  into all three is `taskTree.comeHome` (`task_run.go:8367`), which calls
  `mergeIntoGround` at `task_run.go:8452` (after the
  `branchFastened` re-check at `:8453`).
- **When comeHome runs.** Only through `landHome`
  (`internal/session/task_ledger.go:71`), whose callers are the four landing
  roads: the ordinary finishing line `landFinished` (`task_ledger.go:143`, call
  at `:180`), the audit's **verified** verdict `landAudit`
  (`internal/session/task_audit.go:2936`, call at `:2976`), an **accept** of an
  unverified node `acceptTask` (`task_audit.go:2776`, call at `:2803`), and the
  merge-round resolve roads (`internal/session/task_merge_round.go:143, 628,
  666`). The gate is stated at `task_run.go:5689-5692`: *"Only a VERIFIED verdict
  reaches comeHome, so only verified work is ever merged onto the person's
  branch — and a refuted node keeps its branch exactly as a killed one does,
  because 'not proven' is not 'throw it away'."* The cell driver runs with
  `task.audit: on`, so the gate is closed.
- **What runs instead when the gate stays shut.** `keepHome`
  (`task_ledger.go:110`) → `keptWork` (`task_run.go:6119`): commit the ledger
  onto `task/<slug>` (`:6133`), replay the ground ladder's inheritance
  (`replayOwnWork`), `carryBranchHome` (`internal/session/groundladder.go:1109`
  — a no-op for a worktree ground, where the branch already lives in the shared
  repository; it fetches only a universe ground's branch, `:1133`), then
  `releaseKept` (`task_run.go:6195-6199`, `git worktree remove --force`, branch
  preserved). The node's `merge` mark becomes `"aborted"` (`abortedMerge`,
  `task_run.go:6095-6103`). `keepHome`'s callers are every non-merging ending:
  threshold stops (`task_run.go:5332, 5348`), errored/killed runs (`:5735, 5747,
  5797, 5862, 5908`), unread directions (`task_ledger.go:99`), family sketches
  (`internal/session/task_divide_sketch.go:403`). An audit that never answers
  does not even reach the landing: `landAudit` resettle(TaskUnverified) at
  `internal/session/task_audit.go:2965`; a refuted verdict resettle(TaskFailed)
  at `:2995`.
- **Does it run for a settled node under the driver?** Only if the node is
  verified or accepted — "settled" (the driver's quiet rule) is unrelated to the
  engine's landing. For a settled-unverified node the merge never runs.
- **Which root.** Under the driver the root IS the driver's worktree and the
  merge target IS the branch the driver reads: `home` was recorded at cut
  (`task_run.go:8069`) and the protection ladder only keeps the branch when the
  checkout is detached (`task_branch_protection.go:170-171`), has moved off
  `home` (`:172-173`), carries a protected name — `main`, `dev`, `staging`, the
  remote default, etc. (`:35-47`, `:174-175`) — or was moved by a person since
  the cut (`branchMovedByPerson`, `:191`). The driver's own guard already forces
  the worktree onto a private `task/<name>` branch (`task-run.sh` refuses
  `dev`/`main`/origin-known names), so none of the keep cases apply: **a verified
  verdict lands the commits on the branch the driver reads.** The run table
  proves it (below).

## (c) Evidence from run c215

Run c215 (`~/src/doe/peer/c215/`): started 01:25:20, ended 02:01:29, stop
`settled`, $0.95, one task node.

- **Driver side** (`~/src/doe/peer/c215/result.json`):
  `worktree=/home/santosh/src/sd-crewword`, `branch=task/c215-crew-word`,
  `commits=[]`, `dirty=[]`, and `task_branches` naming the deliverable branch:
  `task/the-task-door-must-honor-the-con-13c2f9 → ["280bc61f task: crew pin
  precedence"]`. (`task_branches` lists every `refs/heads/task/*` with commits
  since HEAD0 — eight entries here, and because a git worktree shares its ref
  store with every other worktree of the same repository, seven of them are
  branches older runs in other `sd-*` worktrees of this repo left behind,
  c201's and c210's among them — so a driver-side read must pick the branch off
  the node record, not "the newest task branch".)
- **Engine side** (`~/src/doe/peer/c215/profile/v3/projects/-home-santosh-src-sd-crewword/b0d9c75075b67c46/tasks.json`,
  node 1): `ground=/home/santosh/src/sd-crewword`, `groundMode=worktree`,
  `home=task/c215-crew-word`, `homeSha=57504187a6bd…` (the driver's HEAD0),
  `branch=task/the-task-door-must-honor-the-con-13c2f9`,
  `worktree=<profile>/…/trees/1` (released — the directory holds the files, no
  `.git`), `state=unverified`, `decider=model`, `merge=aborted`, `changed` = 9
  files. Outcome: *"the check ran out of time — one call ran 30s without
  answering and was abandoned · the window closed before a second"*. Audit
  journal: `…/tasks/20260918-015621.547203_1-audit-2a3d5a.jsonl`.
- **git proof** (run in `/home/santosh/src/sd-crewword`):
  `git merge-base --is-ancestor 57504187a 280bc61f` → true — the task branch was
  cut off the driver's base and the deliverable commit sits on top of it;
  `git branch --contains 280bc61f` → only
  `task/the-task-door-must-honor-the-con-13c2f9`. The deliverable commit is on
  no checked-out branch anywhere.
- **The last frame** (`~/src/doe/peer/c215/scrollback.txt`): the settle card
  reads "crew pin precedence — the check ran out of time · codeaf is deciding"
  (`decider=model`, set by `handToModelOnAuto`, `task_run.go:4519`, under
  `task.settle=auto`) while the worker was still mid-turn verifying in a temp
  worktree of the task branch. The driver's 180-second quiet rule ended the run
  under it. `handBackUnsettled` (`task_run.go:4551`) returns a model-held
  question to the person at the end of a turn — in an unattended run nobody is
  there to hold it, and here the session was killed before the model's turn
  ended either way.
- **After the run**: the driver branch received `44db1fd8` at 02:06:16,
  five minutes after the run ended — codeaf-authored, but a different patch from
  `280bc61f` (patch-ids differ; its tree carries the #1168–#1170 dev history).
  The run itself never moved the branch; the work reached it only by a separate
  later action, and the task branch's own commit remains unmerged today.
- **The pattern holds across the harness's runs**: of the 25 single-node runs
  c193–c221 whose task reached a verdict, every one whose task row is `done` has
  commits on the driver branch (c193, c194, c195, c196, c198, c201, c202, c204,
  c205, c206, c207, c208, c211, c212, c214, c216, c218, c220, c221 — 19 runs, none
  with an empty diff), and every one whose row is `failed`/`unverified` has `commits=[]` with the work on `task/<slug>` (c197, c203, c209, c210, c215,
  c219). The one exception is explained by the mechanism, not against it: c165's
  work reached the driver branch only because the model merged it there itself —
  the node record shows the engine's own landing refused (`merge=conflicted`,
  "conflicts with your branch … its work is on task/…-4b022f and was kept") and
  the worktree's reflog shows `merge task/codeaf-do-must-leave-a-pending-j-4b022f:
  Fast-forward` at 22:27:40, which the model's own accept reason cites
  ("Task branch merged into fix/judge-unverified-restart as a clean fast-forward
  … after the person's pick"). A hand merge is not the door landing.

## Corrected premise

The runs named in the brief did **not** have the checker's acceptance. In c185,
c189, c203 and c210 the node rows are `failed`; in c215 it is `unverified`
(the audit's check timed out — "the window closed before a second"). None of the
five reached a verified verdict, so none of them was ever offered to the merge
home. What settled was the run — the driver's quiet rule — not the work. Runs
where the checker *did* verify put their commits on the driver's branch, every
time.

## (d) The smallest change

**Rejected: door-side "merge home to the driver's checkout on settle".** The door
already does exactly that, into exactly the driver's checkout, whenever a verdict
verifies or an accept lands (`groundcarry.go:352` with `root` = the driver's
worktree). Extending it to unverified/failed settles would break the gate at
`task_run.go:5689` and publish unchecked work — the deliberate opposite of
`keptWork`'s law ("'not proven' is not 'throw it away', and it is not 'land it
either'"). Forcing one more model turn at run end to settle an unverified node
(`task.settle=auto` already hands it the decision) only races the driver's quiet
rule and cannot help a refuted node at all.

**Chosen: driver-side — read the deliverable off `task/<slug>`.** Roughly ten
lines in `task-run.sh`'s result-collection block, no engine change:

1. The driver already reads the engine's `tasks.jsonl` for the task rows. Carry
   the node's `branch` field into the `tasks[]` rows (the engine records it on
   every node — c215's node 1 carries
   `branch=task/the-task-door-must-honor-the-con-13c2f9`).
2. When `commits` is empty while that branch holds commits since `HEAD0`, bring
   the work into the worktree the harvest steps read — `git -C $W merge --ff-only
   task/<slug>` (`accept.sh` and friends operate on the worktree directory, so a
   deliverable that is only on the ref is invisible to them) — and record the
   branch as `delivered_branch` with its commit list; or, at minimum, fail loudly
   naming the branch instead of reporting an empty diff.

This covers both endings that leave work behind (unverified and failed), respects
the verified-only merge law, and costs the engine nothing: an unverified branch
kept as the durable recovery point is exactly what `keptWork` promises the person
it is.