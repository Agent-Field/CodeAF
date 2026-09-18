# A chat door pushed a task's commits to a shared branch under --yolo

Diagnosis only — no fix is in this tree. Every claim below carries the
file and line it was read from, on this branch as of 2026-09-18.

## Q1 — which road does the chat door's own bash `git push` take today?

The door's bash call passes through one chokepoint: `ep.preAction`
(`internal/session/loop.go:3246`), which walks the pre-action citizens in
registration order (`internal/session/hooks.go:405-425`). Two of them have a
say in a `git push`, and both let it through on the incident's shape.

**The consent gate — ran, and answered allow.** `approvalGate` is registered
first (`internal/session/hooks.go:200`; its `PreAction` at
`hooks.go:499-502` delegates to `Agent.approve`). `approve`
(`internal/session/consent.go:216`) asks `a.decide`
(`consent.go:191-201`), which asks `Policy.Check`
(`internal/approval/approval.go:202-247`). For a bash call with no readable
rule behind it the answer is `base` (`approval.go:232-246`): no `tools`
entry for bash — the built-in floor seeds only read/grep/find/ls/jobs/
remember/track/recall/manual/settings (`cmd/codeaf/chatv3.go:1775-1783`) —
so the decision falls to `Policy.Default`, and **`--yolo` is what sets that
default to allow**: `v3Policy` reads the mode row and then overwrites it —
`if yolo { mode = string(approval.ActionAllow) }`
(`cmd/codeaf/chatv3.go:1658-1666`, override at 1662-1664; the flag itself at
`cmd/codeaf/chatv3.go:79`). `git push` is not in the critical-command table
(`internal/approval/bash.go:245-271` — rm -rf, mkfs, dd, raw-disk
redirection, shutdown/reboot/halt/poweroff, the fork bomb) and bash is not
in the acts-in-the-person's-name table
(`internal/approval/approval.go:91-101` — gmail_send, calendar_create,
slack_send, `*_request`), so no floor lifts it back to a prompt
(`approval.go:255-261`). `Decision{allow, "default"}` short-circuits
`approve` before any card, memo or person:
`if !governed || decision.Action == approval.ActionAllow { return …, true }`
(`consent.go:227-228`). **No consent card is ever drawn for this call under
--yolo.**

**The git guard — ran, and passed it through unread.** `taskGitGuard` is
registered last (`internal/session/hooks.go:250`; `internal/session/taskgit.go:87-107`).
Its `PreAction` asks `whoseCopy` (`taskgit.go:117-128`): a task
(`Config.InTask`) and a Steward-headed session get the verb list; **a
Person-headed session returns unguarded** (`taskgit.go:127`, taken at
`taskgit.go:92-93`). The incident run kept a `Person`:
`newPrincipalFor` gives a Person to a task runner, an interactive
conversation, and an unattended run with no ceiling alike
(`internal/session/principal_wire.go:50-77` — InTask/Errand at 62-64,
Interactive at 68-70, no-ceiling at 72-74). `codeaf chat --yolo` with no
`--max-hours`/`--max-cost`
is exactly the last of those. So `refusedTaskGit` — the scanner that reads
every `git` in the line and refuses `push` at
`internal/session/taskgit.go:320` with the sentence the task actually saw
(`sessionGitVoice.push`, `taskgit.go:161`) — **never read the door's
command.**

**The resident's classifier — did not run at all.** `consequenceGated`
classifies `push`/`merge` as high-stakes at
`internal/head/head.go:1123` (the function from 1099). It belongs to the v1
resident's head, a different product in the same binary; `internal/session`
imports `internal/head` nowhere (no non-test import in the package), so
**the classifier never sees a chat door's tool call.** That is a finding,
not a fault line: nothing on the chat road classifies a bash command by
verb at all — the only matches are approval's pattern lists and the
critical table, neither of which contains `git push`.

**Roads that bypass all three — none found on the tool path.** The call the
model made goes through the seam like every other; what bypassed every
consent mechanism was not a hidden road but the two answers above.

## Q2 — does the answer differ for a person's session and for --yolo?

**Yes, by exactly one rung: the card.** Without `--yolo` the default is the
profile's `tools.approvalMode` row, falling back to `prompt`
(`internal/config/settings.go:1371` `DefaultToolApprovalMode = "prompt"`,
resolved through `ToolApprovalModeAt` at `settings.go:3591-3597` and
`cmd/codeaf/chatv3.go:1659-1661`). A prompt reaches `a.ask`
(`consent.go:287`) and the person is shown a consent card. With
`--yolo` the default is `allow` (`cmd/codeaf/chatv3.go:1662-1664`) and the
card never exists. Neither reading changes the git guard: a Person-headed
session is unguarded either way (`taskgit.go:124-127`), so the only thing
standing between a person's session and a shared-branch push is that one
card.

**The default --yolo answers for this class is `allow`, and it is written at
`cmd/codeaf/chatv3.go:1662-1664`.** There is no class for `git push` for the
flag to answer: the two floors that survive a blanket allow are the critical
command table (`internal/approval/bash.go:245-271`) and the acts-in-the-
person's-name table (`internal/approval/approval.go:91-101`), and `git push`
is on neither. `approval.AlwaysAsks` (`internal/approval/floor.go:27-46`)
reads those same two tables and answers false for `git push`, so even a
remembered "stop asking me about bash" memo would stand in for a person on
a prompt-mode card (`consent.go:247-253`) — the memo road did not run in the
incident (allow short-circuits before the memo is consulted) but it is the
same hole one keystroke wide.

**One posture IS covered, and it is not the one that ran.** A session whose
principal is a Steward — unattended WITH a ceiling
(`internal/session/principal_wire.go:72-84`; the Person return for the
no-ceiling case is 72-74 and the Steward is built at 75-84) — is refused `git push`
outright on the same seam, before the shell runs (`taskgit.go:124-126`,
refusal wording at `taskgit.go:161`; covered by
`TestASessionCarryingItsOwnWorkAnswersToTheSameGitList`,
`internal/session/taskgit_test.go:215-245`, which also pins the deliberate
opposite: an attended session and an unattended run with no ceiling are both
allowed their git at `taskgit_test.go:236-244`). The incident run named no
ceiling, so it sat on the Person side of that line.

## Q3 — what does the door do about the branch it is standing on?

**The landing merges into whatever branch the checkout was standing on when
the work was cut, and asks three questions about it — none of them
"did this run create it".** `comeHome` (`internal/session/task_run.go:8367`)
commits the node's work, carries the branch home, and merges with
`git merge --no-edit` under codeaf's own identity
(`internal/session/groundcarry.go:352-354`; `codeafGitIdentity` sets
`user.name=codeaf user.email=agentfield-bot@users.noreply.github.com`,
`internal/session/task_branch_protection.go:20-23` — the attribution the
worktree reflog carries). Before merging it consults
`keptLandingSentence` (`task_branch_protection.go:135-152`), which keeps
the branch unmerged on exactly four conditions: a detached HEAD, the
checkout having moved from `t.home` — the branch recorded at cut time
(`task_run.go:7827-7830`, set by `prepareTaskTree`, which deliberately cuts
"a fresh branch off the person's CURRENT HEAD — that is what makes the
node's work a merge later", `task_run.go:7901-7906`) — a **protected name**
(`protectedBranchNames`, `task_branch_protection.go:28-42`: main, master,
dev, develop, development, staging, stage, trunk, production, prod, release,
matched with `EqualFold` at `task_branch_protection.go:63-77`, plus each
remote's default branch), or the branch having moved by a person's hand
(`branchMovedByPerson`, `task_branch_protection.go:158-191`).

**`santos/dev` passes all four.** It is not one of the fixed names —
`EqualFold("santos/dev", "dev")` is false — and it is only protected if
`refs/remotes/origin/HEAD` points at a branch of that exact name
(`task_branch_protection.go:68-76`); since the incident's landing merged
rather than kept, that ref did not name it in that clone. A local branch
whose name merely CONTAINS a protected name gets no protection, and nothing
on the landing road or the bash road ever asks whether the branch the run
is standing on is a branch the run created. The merge in the reflog —
`merge task/internal-tui3-the-opening-hint-t-6cc05e: Fast-forward`,
authored as codeaf — is `mergeTaskBranch`'s own doing; the push that
followed it was the door's shell, and no mechanism anywhere in the tree
connects the two.

## What reading could NOT determine

- **Whether the gate's allow was the same in the second run.** Fact 6 says a
  second run of the same shape made the merge and did not push. The code
  road is identical for both — nothing in the tree makes a push automatic —
  so the difference was the model's choice, but the transcripts themselves
  were not available to this reading; that is an inference from facts 2-4
  and 6, not a traced record.
- **What `refs/remotes/origin/HEAD` pointed at in the incident clone.** The
  protected-name check consults it (`task_branch_protection.go:68-76`); the
  landing merged, so it did not name `santos/dev`, but whether it was
  absent or named another branch cannot be read from the records given.
- **Why the first `git push` was refused non-fast-forward.** The remote had
  moved or the local branch lacked something; git's own answer is in the
  fact list, the cause of the divergence is not, and nothing in the tree
  bears on it.
- **What the task model said to the door about the push.** The task's own
  text (fact 2) shows it reasoning about the door's git rights, but whether
  it ASKED the door to push, or merely reported the refusal and the door
  improvised, cannot be determined from this tree — the tool calls (fact 3)
  are the door's own words either way.

## The failing test

`internal/session/yolopush_test.go` — one test,
`TestAYoloRunDoesNotPushToABranchItDidNotCreate`, sending the three verbatim
bash lines of fact 3 down the real pre-action road (`agent.newEpisode()`,
then `ep.preAction`), behind the exact policy a `--yolo` launch builds
(`approval.Load` with `default: allow` and the built-in tool floor).

Command and output on this tree:

```
$ go test ./internal/session -run TestAYoloRunDoesNotPushToABranchItDidNotCreate
--- FAIL: TestAYoloRunDoesNotPushToABranchItDidNotCreate (0.00s)
    yolopush_test.go:97: "cd /home/santosh/src/hint-fix && git push origin santos/dev 2>&1 | tail -3" was ALLOWED for a --yolo run standing on a branch it did not create; the gate answered "default → allow" and no citizen refused it — this is the road the incident took
    yolopush_test.go:97: "cd /home/santosh/src/hint-fix && git rebase origin/santos/dev 2>&1 | tail -2 && git log --oneline -3 && git push origin santos/dev 2>&1 | tail -3" was ALLOWED for a --yolo run standing on a branch it did not create; the gate answered "default → allow" and no citizen refused it — this is the road the incident took
    yolopush_test.go:97: "cd /tmp/hint-fix-pr && git switch -c fix/opening-hint-test-owns-profile && git push -u origin fix/opening-hint-test-owns-profile" was ALLOWED for a --yolo run standing on a branch it did not create; the gate answered "default → allow" and no citizen refused it — this is the road the incident took
FAIL
FAIL	github.com/Agent-Field/codeaf/internal/session	0.111s
```

The test pins the **consent decision** — the decision that stood in front of
the push. What it could not cover, said in its own header comment too:

- a live remote: the push itself never runs in a test; the test reaches the
  decision that would have let it run, and no further;
- **the branch-ownership condition cannot be expressed at this layer.**
  Nothing in the consent road knows what a branch is — `internal/approval`
  matches command text against patterns, and there is no rule shape, table
  entry or classification anywhere on the chat road that can say "a branch
  this run did not create". The test therefore pins the whole command line
  and asserts a refusal of the act;
- the git guard's posture: `whoseCopy` leaving a Person unguarded is pinned
  as deliberate by `TestASessionCarryingItsOwnWorkAnswersToTheSameGitList`
  (`taskgit_test.go:236-244`); this test does not overturn that contract, so
  even if the gate were to refuse, the guard road for a Person-headed
  session remains open and uncovered by any test.

## The obvious fix, in one paragraph (not applied)

The narrowest fix that closes the measured road without touching the
Person-posture contract the C-series tests pin: give the git guard a third
register — or extend `whoseCopy` — so that a session standing on a branch it
did not create (`currentBranch(t.root) != t.home`, or any current branch when
the run created none) is refused `push` in the session register, the same way
a Steward already is. That leaves a person working in their own checkout
untouched (they cut their own branches) while stopping an unattended run
from pushing a shared name it merely found itself on. The alternative — a
bash pattern floor for `git push` in `criticalCommands` — is broader and
blunter: it would also refuse the interactive session's own push, which
`taskgit.go`'s header comment calls deliberately theirs.
